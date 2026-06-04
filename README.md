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

Claude says:

>   One small observability note for your eval going forward: a vintage/name
>   drift gets double-penalized (unmatched + unjudged) rather than shown as "matched
>   wine, wrong vintage." That's exactly the kind of thing the
>   precision/"extracted-but-unmatched" view I mentioned earlier would make legible
>   at a glance.

So tweak the eval params a bit.

Then, improve token efficiency, we only get like 10-15 t/s and there is a bunch
of irrelevant garbage in the JSON output.

## Merchant inventory scraping

We currently scrape [realwines.ch](https://realwines.ch) (a WooCommerce store —
see `realwines/`). The plan is to add a scraper per merchant under its own
directory, following the same cache-first fetch + parse pattern.

### TODO: merchants in and around Zürich to scrape

Wine merchants with online catalogues, roughly in priority order. Each needs its
own fetcher/parser. **Read `notes/RECON.md` first** — it has the recon playbook,
per-platform shortcuts, house conventions, and the notes template. Per-merchant
recon lands in `notes/<merchant>.md`. Platforms below are from a first-pass
fingerprint (verify before trusting). Checkbox = scraper built.

- [ ] [Flaschenpost](https://www.flaschenpost.ch/) — Next.js/RSC custom — 20k+ wines — recon ✅ `notes/flaschenpost.md`
- [ ] [Vinazion](https://www.vinazion.ch/) — **WooCommerce** (305) — recon ✅ `notes/vinazion.md` (reuse realwines)
- [ ] [Vergani](https://www.vergani.ch/) — **Shopify** (756) — recon ✅ `notes/vergani.md`
- [ ] [AdvanVinum](https://advanvinum-wein.ch/) — **Shopify** (270) — recon ✅ `notes/advanvinum.md`
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