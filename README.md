# winerank

SLOP WARNING: Everything in this repo is AI generated in the laziest possible
way. To the extent that I have ever even looked at the code, it seems to be
fucking garbage. Don't train your AI on this and please don't judge my
engineering by it.

## Transcript fetcher

WARNING: Developing this got my residential IP banned from the YouTube API,
maybe permanently I don't know. Using YouTube via its own clients still works
fine. When I switched to my mobile network it worked for a while but then still
got banned again even with a rate limit applied.

```sh
nix run .#get-transcripts -- --num-vids 100
```

This will dump transcripts into `./transcripts/`.

## Score extraction

The `devShell` includes a llama-cpp build that works on my 3070Ti. Run a
`llama-server`, here's a command that I don't really understand but which Gemini
says is a reasonable model for this task with some tweak flags that are supposed
to keep things in VRAM:

```sh
llama-server --hf-repo bartowski/Qwen2.5-7B-Instruct-GGUF --hf-file Qwen2.5-7B-Instruct-Q4_K_M.gguf -ngl 99 -c 32768 --host 0.0.0.0 --port 8080 -np 1
```

Opus 4.7's suggestion for a model to run on my Framework 13 (32GiB):

```sh
GGML_CUDA_ENABLE_UNIFIED_MEMORY=1 llama-server \
               -hf unsloth/Qwen3-30B-A3B-Instruct-2507-GGUF:Q4_K_M \
               -ngl 99 -c 16384 -fa on --host 0.0.0.0 --port 8080 -np 1
```

(Yes, the env var says CUDA but it still configures AMD too, I confirmed this
was required to get ROCM access to the full memory).

With Vulkan it crashed because it couldn't get access to the unified
memory. If you hit the grammar crash (`what():  Unexpected empty grammar stack
after accepting piece: ? (30)`), run the extractor with
`--structured-output=false` to drop the JSON schema `response_format` (it then
relies on the prompt + code-fence stripping to recover JSON).

The extractor talks to `http://localhost:8080` by default. Point it elsewhere
with `--server`, or set `LLAMA_SERVER_URL` (the flag wins if both are given).

### Eval

Grade the extractor against `scores_groundtruth.json`:

```sh
nix run .#eval
```

It runs `.#extract-scores` on the ground-truth videos, prints a metrics
summary, and writes a detailed report to `evals/<timestamp>.json`. Override the
path with `--report`, and the rest with `--extractor`, `--groundtruth`,
`--transcripts-dir`. For faster iteration, build the extractor once and point at
the binary:

```sh
go build -C cmd/extract-scores -o extract-scores
nix run .#eval -- --extractor "$PWD/cmd/extract-scores/extract-scores"
```

Each report embeds a `provenance` block so a run can be reproduced: the
winerank git commit (and dirty flag), the extractor command, and the live
server's `/props` and `/v1/models` (model path + revision, llama.cpp build,
context size, chat template, default sampling). The server it queries comes from
`--server` / `LLAMA_SERVER_URL` (same default as the extractor). These reports
are meant to be committed.

Needs a running `llama-server` (see above).

### Next steps

**Token efficiency — done (mostly).** The single-pass output schema was trimmed
to just `name` + `score` (the only fields the eval grades; producer/vintage/
region/notes_summary/matching_snippet are now `omitempty` and only the multi-pass
strategy fills them). The eval report now also carries per-video timing + token
counts (`stats`) and a top-level `performance` block. Measured on the 4-video GT
set: **10m04s → 3m11s (3.16x), completion tokens 5777 → 1130 (-80%)**, recall
unchanged. See `evals/2026-06-04T20:59:30Z.json` (before) vs `...T21:26:00Z.json`
(after).

The bottleneck has now **flipped from decode to prefill**: decode runs ~12 t/s
but prefill ~200-290 t/s, so once the output got small, the ~25k prompt tokens/run
dominate wall-clock. Next speed lever is **prompt size** (trim the transcript /
system prompt), but the ceiling is lower since prefill is ~20x faster per token.
Note `completion_tokens_per_sec` in the report is now misleading — it divides by
total (prefill-dominated) duration, so it no longer means "decode speed".

**Accuracy kinks (recall 26/27, score-match 25/26).** Investigated both:

- *Ruffino (recall miss)* — was a **GT bug**, now fixed. GT said `2021 Ruffino
  Riserva Ducale` but the video description (and the model) say `2019`; the bad
  `2021` had leaked from the *previous* wine's wine-searcher URL fragment. Same
  score (90), so it was getting double-penalized (unmatched GT + unjudged) — the
  observability note below. Fixing the label should give recall 27/27 next run.

- *Le Riche Richesse (score miss: model 91, GT 93)* — a real **model
  mis-attribution**, NOT number parsing. The ASR wrote "ninety-three" as
  "90 three", but `normalizeASRNumbers` (in `transcript.go`) already rewrites that
  to `93` before the model sees it — confirmed the model is handed a clean
  "...rate this 93 points". The problem is binding the score to the right wine:
  5 of the 7 wines in that blind tasting are rated 91, and the model grabbed a 91.
  Weak evidence the schema trim made this worse (full schema got it right 1/2
  runs, trimmed 0/2) — dropping `matching_snippet` removed the per-score
  "quote the supporting line" anchor. Confounded by the server running a **random
  seed** (`-1`), so it flickers run-to-run.

Concrete TODOs:
- Add a `--seed` flag to the extractor (server seed is currently random) so evals
  are reproducible — the score-match noise that made an earlier run look like
  26/26 was just seed luck on the borderline Le Riche wine.
- With a fixed seed, A/B the full vs trimmed schema on the Le Riche video to settle
  whether the trim has a real (small) attribution cost worth the 3x speedup.
- Eval observability: a vintage/name drift gets double-penalized (unmatched +
  unjudged) rather than shown as "matched wine, wrong vintage" — a
  precision / "extracted-but-unmatched" view would make this legible at a glance
  (and would have flagged the Ruffino GT bug immediately).
- Prompt-token reduction (see bottleneck note above) for further speedup.

## Merchant inventory scraping

We currently scrape [realwines.ch](https://realwines.ch) (a WooCommerce store —
see `scraping/realwines/`). All merchant scrapers live under `scraping/`, one
directory per merchant (or per platform), following the same cache-first fetch +
parse pattern.

Each scraper is a `fetch.py` (cache-first; one raw page file under `cache/`) +
`parse.py` (cache → clean wine records). Run them with `nix develop -c python`
(there's no `python3` on PATH). Built so far:

```sh
# Vinazion (WooCommerce, ~305 wines)
nix develop -c python scraping/vinazion/fetch.py
nix develop -c python scraping/vinazion/parse.py --from-cache --output scraping/vinazion/wines.json

# Shopify shops (one parametrised scraper for all of them)
nix develop -c python scraping/shopify/fetch.py --shop www.vergani.ch     --name vergani
nix develop -c python scraping/shopify/parse.py --name vergani --from-cache --output scraping/shopify/vergani.json
nix develop -c python scraping/shopify/fetch.py --shop advanvinum-wein.ch --name advanvinum
nix develop -c python scraping/shopify/parse.py --name advanvinum --from-cache --output scraping/shopify/advanvinum.json
```

`notes/RECON.md` is the recon playbook + per-platform shortcuts for adding the
rest; per-merchant recon lives in `notes/<merchant>.md`.

### TODO: merchants in and around Zürich to scrape

Wine merchants with online catalogues, roughly in priority order. Each needs its
own fetcher/parser. **Read `notes/RECON.md` first** — it has the recon playbook,
per-platform shortcuts, house conventions, and the notes template. Per-merchant
recon lands in `notes/<merchant>.md`. Platforms below are from a first-pass
fingerprint (verify before trusting). Checkbox = scraper built.

- [ ] [Flaschenpost](https://www.flaschenpost.ch/) — Next.js/RSC custom — 20k+ wines — recon ✅ `notes/flaschenpost.md`
- [x] [Vinazion](https://www.vinazion.ch/) — **WooCommerce** — built (`scraping/vinazion/`, 305 wines)
- [x] [Vergani](https://www.vergani.ch/) — **Shopify** — built (`scraping/shopify/`, 740 wines / 1000 variants)
- [x] [AdvanVinum](https://advanvinum-wein.ch/) — **Shopify** — built (`scraping/shopify/`, 215 wines / 219 variants)
- [ ] [Bindella Weinshop](https://www.bindella.ch/weinshop/) — BigCommerce — Italian focus, Zürich
- [ ] [Bottleshop / more-than-wine](https://www.more-than-wine.com/) — PrestaShop — natural wine
- [ ] [Arvi](https://arvi.ch/en/) — nopCommerce (.NET) — fine & rare
- [ ] [Smith & Smith](https://www.smithandsmith.ch/de) — ASP.NET/IIS (likely nopCommerce) — Zürich + Bern
- [ ] [Mövenpick Wein](https://www.moevenpick-wein.com/de/) — Magento — Zürich-Enge + Wein-Bar, 3k+ wines
- [ ] [Landolt Weine](https://www.landolt-weine.ch/) — Shopware likely — Zürich grower/merchant
- [ ] [Baur au Lac Vins](https://www.bauraulacvins.ch/) — Java app (JSESSIONID) — 3k+ articles
- [ ] [Zweifel 1898](https://www.zweifel1898.ch/) — Java app (JSESSIONID) — Zürich winery (Höngg)
- [ ] [Gerstl Weinselektionen](https://www.gerstl.ch/) — custom Node/Express headless — since 1981
- [ ] [REB Wein](https://www.rebwein.ch/) — custom Laravel — central Zürich
- [ ] [Le Passeur de Vin](https://www.passeurdevin.ch/) — unknown platform — Pelikanstrasse, Zürich