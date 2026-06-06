# winerank

SLOP WARNING: Everything in this repo is AI generated in the laziest possible
way. To the extent that I have ever even looked at the code, it seems to be
fucking garbage. Don't train your AI on this and please don't judge my
engineering by it.

## Transcript fetcher

WARNING: Developing this got my residential IP banned from the YouTube API,
so far I always get unbanned after a few days. I am exponentially bumping up the
gap between requests, presumably at some point this will reach a rate where it
no longer triggers the IP-ban.

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

`llama-server` supports a "router mode" where instead of specifying the model in
the invocation the client can request the model but since the most reasonable
setups depends completely on the server hardware, I think the right way to do
that is to set up a "menu" of supported models/inference configs (this is
supported by `llama-server`). For this project, no need to bother with that,
just hardcode the server args.

Note Qwen3 is quite old, superseded by both 3.5 and 3.6 by now. I tried
switching to `unsloth/Qwen3.6-35B-A3B-GGUF:Q4_K_M` but it was only able to get
accuracte scores in reasoning mode, which is really slow.

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

### Inference speed

I get like 10-14 t/s output token rate on my FW13 so it was crucial to avoid
outputting any unnecessary tokens. I get about 250 t/s for input tokens which
is dominated by actually passing in the transcript. The instruction prompt is
pretty small. So probably we can get some speedups by:

0. Not forcing the model to output quotes from its input. However, with
  `unsloth/Qwen3-30B-A3B-Instruct-2507-GGUF:Q4_K_M`, this was necessary to get
  full accuracy on the eval data.

1. Fiddling with the output schema (e.g. maybe we can avoid needing to output
   the whole wine name)

2. Compressing the transcript e.g. by stripping filler words.

But these won't be massive speedups.

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

- [x] [Flaschenpost](https://www.flaschenpost.ch/) — Next.js/RSC custom — 20k+ wines — recon ✅ `notes/flaschenpost.md` — built `scraping/flaschenpost/`
- [x] [Vinazion](https://www.vinazion.ch/) — **WooCommerce** — built (`scraping/vinazion/`, 305 wines)
- [x] [Vergani](https://www.vergani.ch/) — **Shopify** — built (`scraping/shopify/`, 740 wines / 1000 variants)
- [x] [AdvanVinum](https://advanvinum-wein.ch/) — **Shopify** — built (`scraping/shopify/`, 215 wines / 219 variants)
- [ ] [Bindella Weinshop](https://www.bindella.ch/weinshop/) — BigCommerce — Italian focus, Zürich
- [x] [Bottleshop / more-than-wine](https://www.more-than-wine.com/) — PrestaShop — natural wine — recon ✅ `notes/more-than-wine.md` — built `scraping/more-than-wine/`
- [ ] [Arvi](https://arvi.ch/en/) — nopCommerce (.NET) — fine & rare
- [ ] [Smith & Smith](https://www.smithandsmith.ch/de) — ASP.NET/IIS (likely nopCommerce) — Zürich + Bern
- [ ] [Mövenpick Wein](https://www.moevenpick-wein.com/de/) — Magento — Zürich-Enge + Wein-Bar, 3k+ wines
- [ ] [Landolt Weine](https://www.landolt-weine.ch/) — Shopware likely — Zürich grower/merchant
- [ ] [Baur au Lac Vins](https://www.bauraulacvins.ch/) — Java app (JSESSIONID) — 3k+ articles
- [ ] [Zweifel 1898](https://www.zweifel1898.ch/) — Java app (JSESSIONID) — Zürich winery (Höngg)
- [ ] [Gerstl Weinselektionen](https://www.gerstl.ch/) — custom Node/Express headless — since 1981
- [ ] [REB Wein](https://www.rebwein.ch/) — custom Laravel — central Zürich
- [ ] [Le Passeur de Vin](https://www.passeurdevin.ch/) — unknown platform — Pelikanstrasse, Zürich