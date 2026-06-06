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

All merchant scrapers live under `scraping/`, one directory per merchant (or per
platform), following the same cache-first fetch + parse pattern.

Each scraper is a `fetch.py` (cache-first; one raw page file under `cache/`) +
`parse.py` (cache → clean wine records), so every merchant runs the same way:

```sh
nix develop -c python scraping/<merchant>/fetch.py [opts]
nix develop -c python scraping/<merchant>/parse.py --from-cache --output scraping/<merchant>/wines.json
```

Notes on the invocation:

- **Use `python`, not `python3`** — there's no `python3` on PATH. The dev shell
  provides `python` (3.13) + `requests` transitively via the `get-transcripts`
  package inputs.
- `fetch.py` is cache-first: it reuses `scraping/<merchant>/cache/page_*.json`
  and only hits the network for missing pages. Pass `--refresh` to re-download.
- If `nix develop` fails to enter the shell with `Failed to retrieve the parent
  of Git commit … object not found`, your checkout is a partial/blobless clone
  that Nix's libgit2 can't walk. Either unshallow the clone
  (`git fetch --unshallow` / `git repack -d`) or append `".?shallow=1"` to the
  flake ref as a one-off (`nix develop ".?shallow=1" -c …`).

Tested and confirmed working:

```sh
# Vinazion (WooCommerce, ~305 wines)
nix develop -c python scraping/vinazion/fetch.py
nix develop -c python scraping/vinazion/parse.py --from-cache --output scraping/vinazion/wines.json

# Shopify shops (one parametrised scraper for all of them; --shop + --name)
nix develop -c python scraping/shopify/fetch.py --shop www.vergani.ch     --name vergani
nix develop -c python scraping/shopify/parse.py --name vergani --from-cache --output scraping/shopify/vergani.json
nix develop -c python scraping/shopify/fetch.py --shop advanvinum-wein.ch --name advanvinum
nix develop -c python scraping/shopify/parse.py --name advanvinum --from-cache --output scraping/shopify/advanvinum.json

# Flaschenpost (Next.js/RSC, ~20.6k wines / ~1718 pages — use --max-pages to sample)
nix develop -c python scraping/flaschenpost/fetch.py [--max-pages N]
nix develop -c python scraping/flaschenpost/parse.py --from-cache --output scraping/flaschenpost/wines.json

# more-than-wine (PrestaShop, ~577 wines — use --max-pages to sample)
nix develop -c python scraping/more-than-wine/fetch.py [--max-pages N]
nix develop -c python scraping/more-than-wine/parse.py --from-cache --output scraping/more-than-wine/wines.json

# Arvi (nopCommerce, large fine-wine catalogue — use --max-pages to sample)
nix develop -c python scraping/arvi/fetch.py [--max-pages N]
nix develop -c python scraping/arvi/parse.py --from-cache --output scraping/arvi/wines.json

# Smith & Smith (custom React/ASP.NET, ~2984 products — use --max-pages to sample)
nix develop -c python scraping/smith-and-smith/fetch.py [--max-pages N]
nix develop -c python scraping/smith-and-smith/parse.py --from-cache --output scraping/smith-and-smith/wines.json

# Mövenpick (Magento, sitemap-driven, ~4300 wines — use --max-products to sample)
nix develop -c python scraping/moevenpick/fetch.py [--max-products N]
nix develop -c python scraping/moevenpick/parse.py --from-cache --output scraping/moevenpick/wines.json

# Baur au Lac Vins (Java app, sitemap-driven, ~2342 wines — use --max-products to sample)
nix develop -c python scraping/bauraulac/fetch.py [--max-products N]
nix develop -c python scraping/bauraulac/parse.py --from-cache --output scraping/bauraulac/wines.json

# Zweifel 1898 (Java app, sitemap-driven, ~781 wines — use --max-products to sample)
nix develop -c python scraping/zweifel/fetch.py [--max-products N]
nix develop -c python scraping/zweifel/parse.py --from-cache --output scraping/zweifel/wines.json

# Gerstl (Angular ng-state, sitemap-driven, ~7546 wines — use --max-products to sample)
nix develop -c python scraping/gerstl/fetch.py [--max-products N]
nix develop -c python scraping/gerstl/parse.py --from-cache --output scraping/gerstl/wines.json
```

`notes/RECON.md` is the recon playbook + per-platform shortcuts for adding the
rest; per-merchant recon lives in `notes/<merchant>.md`.

### Merchants in and around Zürich to scrape

Wine merchants with online catalogues, roughly in priority order. Each needs its
own fetcher/parser. **Read `notes/RECON.md` first** — it has the recon playbook,
per-platform shortcuts, house conventions, and the notes template. Per-merchant
recon lands in `notes/<merchant>.md`. Platforms below are from a first-pass
fingerprint (verify before trusting).

Legend: 🔧 code written, untested · 🔨 partial code (fetch only, no parse) · 📋 recon notes only · ☐ not started

- [x] [Vinazion](https://www.vinazion.ch/) — WooCommerce — `scraping/vinazion/` — ~305 wines
- [x] [Vergani](https://www.vergani.ch/) — Shopify — `scraping/shopify/` — ~740 wines / ~1000 variants
- [x] [AdvanVinum](https://advanvinum-wein.ch/) — Shopify — `scraping/shopify/` — ~215 wines / ~219 variants
- [x] [Flaschenpost](https://www.flaschenpost.ch/) — Next.js/RSC — `scraping/flaschenpost/` — recon ✅ `notes/flaschenpost.md` — ~20.6k wines / ~1718 pages — tested (fetch + parse verified live on a 3-page sample; records carry wine_type/alcohol/bottle_size/country)
- [x] [Bottleshop / more-than-wine](https://www.more-than-wine.com/) — PrestaShop — `scraping/more-than-wine/` — recon ✅ `notes/more-than-wine.md` — ~577 wines — tested (fetch + parse verified live on a 2-page sample; category is `/fr/3-vins`, JSON-LD path, vintage from URL slug). Note: JSON-LD `brand` is the shop, not the producer.
- [x] [Arvi](https://arvi.ch/en/) — nopCommerce (.NET) — `scraping/arvi/` — large fine-wine catalogue (pager reports ~2070 pages × 13) — tested (fetch + parse verified live on a 3-page sample; category `/en/Wines`, `.item-box` card parsing, producer/vintage/bottle from card + URL slug)
- [x] [Smith & Smith](https://www.smithandsmith.ch/de) — custom React/ASP.NET (IIS), *not* nopCommerce — `scraping/smith-and-smith/` — ~2984 products — tested (fetch + parse verified live on a 3-page sample; listing `/de/shop?page=N`, `card artikel-box` parsing gives producer/region/country/vintage/bottle/price)
- 📋 [Bindella Weinshop](https://www.bindella.ch/weinshop/) — BigCommerce — recon ✅ `notes/bindella.md` — **no code** (egress proxy blocked; platform unconfirmed)
- 📋 [Landolt Weine](https://www.landolt-weine.ch/) — Shopware 6 — recon ✅ `notes/landolt.md` — **no code** (egress proxy blocked; platform unconfirmed)
- [x] [Mövenpick Wein](https://www.moevenpick-wein.com/de/) — Magento — `scraping/moevenpick/` — recon ✅ `notes/moevenpick.md` — ~4300 wines (sitemap-driven; JSON-LD PDPs) — tested (fetch + parse verified live on a 20-product sample; producer not exposed in JSON-LD)
- [x] [Baur au Lac Vins](https://www.bauraulacvins.ch/) — Java app (JSESSIONID) — `scraping/bauraulac/` — recon ✅ `notes/bauraulac.md` — ~2342 wines (sitemap-driven; `data-shop-*` attrs + URL facets) — tested (fetch + parse verified live on a 20-product sample; producer not exposed)
- [x] [Zweifel 1898](https://www.zweifel1898.ch/) — Java app (JSESSIONID), same platform as Baur au Lac — `scraping/zweifel/` — recon ✅ `notes/zweifel.md` — ~781 wines (sitemap-driven; microdata PDPs + URL facets) — tested (fetch + parse verified live on a 20-product sample)
- [x] [Gerstl Weinselektionen](https://www.gerstl.ch/) — Angular SPA (ng-state transfer blob), Google-hosted — `scraping/gerstl/` — recon ✅ `notes/gerstl.md` — ~7546 wines (sitemap-driven; richest data incl. appellation/grapes) — tested (fetch + parse verified live on a 20-product sample)
- ☐ [REB Wein](https://www.rebwein.ch/) — custom Laravel — central Zürich
- ☐ [Le Passeur de Vin](https://www.passeurdevin.ch/) — unknown platform — Pelikanstrasse, Zürich