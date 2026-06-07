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

### Running all scrapers

```sh
nix run .#scrape-all            # fetch + parse all 15 merchants → wines.json
nix run .#scrape-all -- --refresh  # ignore cache, re-download everything
```

This writes per-merchant files under `scraping/` and then merges them all into
`wines.json` at the repo root (unified schema, one record per wine, ~tens of
thousands of entries once full catalogues are fetched).

### Running a single merchant

Every merchant has its own `scrape-<name>` package. Arguments are forwarded to
the fetch step, so `--refresh`, `--max-pages`, `--max-products` etc. all work:

```sh
nix run .#scrape-arvi
nix run .#scrape-arvi -- --refresh
nix run .#scrape-flaschenpost -- --max-pages 5   # sample, don't pull 1700 pages
nix run .#scrape-bauraulac -- --max-products 20
```

Shopify merchants are exposed by merchant name, not platform:

```sh
nix run .#scrape-vergani
nix run .#scrape-advanvinum
```

### In the dev shell

After `nix develop` (append `.?shallow=1` if your checkout is a shallow clone —
see below), all `scrape-*` commands and `combine-wines` are on `$PATH`:

```sh
nix develop
scrape-arvi
scrape-all --refresh
combine-wines              # re-merge existing per-merchant files without re-fetching
```

### Fetch and parse separately

If you want to re-parse without re-fetching (e.g. after editing a parse script):

```sh
nix run .#fetch-arvi
nix run .#parse-arvi
```

### Output files

| Path | Contents |
|---|---|
| `scraping/<merchant>/wines.json` | per-merchant records (standard merchants) |
| `scraping/shopify/<merchant>.json` | per-merchant records (Shopify merchants) |
| `wines.json` | all merchants merged, unified schema |

`wines.json` is gitignored — regenerate it with `scrape-all` or `combine-wines`.

### Shallow-clone note

If `nix develop` (or `nix run`) fails with `Failed to retrieve the parent of Git
commit … object not found`, your checkout is a partial/blobless clone that Nix's
libgit2 can't walk. Either unshallow (`git fetch --unshallow`) or append
`".?shallow=1"` to every flake ref:

```sh
nix run ".?shallow=1"#scrape-all
nix develop ".?shallow=1"
```

`notes/RECON.md` is the recon playbook + per-platform shortcuts for adding the
rest; per-merchant recon lives in `notes/<merchant>.md`.

### Merchants in and around Zürich to scrape

Wine merchants with online catalogues, roughly in priority order. Each has its
own fetcher/parser unless noted. **Read `notes/RECON.md` first** — it has the
recon playbook, per-platform shortcuts, house conventions, and the notes
template. Per-merchant recon lands in `notes/<merchant>.md`.

All listed merchants are implemented and tested live except Bindella (blocked —
headless/JS-rendered storefront). Platforms are verified unless a line says
otherwise.

Legend: [x] tested live · ⛔ blocked (needs browser automation) · 🔧 code written, untested · 🔨 partial code (fetch only, no parse) · 📋 recon notes only · ☐ not started

- [x] [Vinazion](https://www.vinazion.ch/) — WooCommerce — `scraping/vinazion/` — ~305 wines
- [x] [Vergani](https://www.vergani.ch/) — Shopify — `scraping/shopify/` — ~740 wines / ~1000 variants
- [x] [AdvanVinum](https://advanvinum-wein.ch/) — Shopify — `scraping/shopify/` — ~215 wines / ~219 variants
- [x] [Flaschenpost](https://www.flaschenpost.ch/) — Next.js/RSC — `scraping/flaschenpost/` — recon ✅ `notes/flaschenpost.md` — ~20.6k wines / ~1718 pages — tested (fetch + parse verified live on a 3-page sample; records carry wine_type/alcohol/bottle_size/country)
- [x] [Bottleshop / more-than-wine](https://www.more-than-wine.com/) — PrestaShop — `scraping/more-than-wine/` — recon ✅ `notes/more-than-wine.md` — ~577 wines — tested (fetch + parse verified live on a 2-page sample; category is `/fr/3-vins`, JSON-LD path, vintage from URL slug). Note: JSON-LD `brand` is the shop, not the producer.
- [x] [Arvi](https://arvi.ch/en/) — nopCommerce (.NET) — `scraping/arvi/` — large fine-wine catalogue (pager reports ~2070 pages × 13) — tested (fetch + parse verified live on a 3-page sample; category `/en/Wines`, `.item-box` card parsing, producer/vintage/bottle from card + URL slug)
- [x] [Smith & Smith](https://www.smithandsmith.ch/de) — custom React/ASP.NET (IIS), *not* nopCommerce — `scraping/smith-and-smith/` — ~2984 products — tested (fetch + parse verified live on a 3-page sample; listing `/de/shop?page=N`, `card artikel-box` parsing gives producer/region/country/vintage/bottle/price)
- ⛔ [Bindella Weinshop](https://www.bindella.ch/weinshop/) — BigCommerce (verified) — recon ✅ `notes/bindella.md` — **blocked: headless/JS-rendered storefront** (no products/prices in static HTML, no JSON-LD, Stencil API 404s, no sitemap). Needs browser automation or XHR/API reverse-engineering — out of scope for the static-HTML scraper pattern.
- [x] [Landolt Weine](https://www.landolt-weine.ch/) — Shopware 6 (storefront on landolt-weine.nextag.ch) — `scraping/landolt/` — recon ✅ `notes/landolt.md` — ~693 products (sitemap-driven; microdata + properties table) — tested (fetch + parse verified live on a 25-product sample; vintage sparse on own-label range)
- [x] [Mövenpick Wein](https://www.moevenpick-wein.com/de/) — Magento — `scraping/moevenpick/` — recon ✅ `notes/moevenpick.md` — ~4300 wines (sitemap-driven; JSON-LD PDPs) — tested (fetch + parse verified live on a 20-product sample; producer not exposed in JSON-LD)
- [x] [Baur au Lac Vins](https://www.bauraulacvins.ch/) — Java app (JSESSIONID) — `scraping/bauraulac/` — recon ✅ `notes/bauraulac.md` — ~2342 wines (sitemap-driven; `data-shop-*` attrs + URL facets) — tested (fetch + parse verified live on a 20-product sample; producer not exposed)
- [x] [Zweifel 1898](https://www.zweifel1898.ch/) — Java app (JSESSIONID), same platform as Baur au Lac — `scraping/zweifel/` — recon ✅ `notes/zweifel.md` — ~781 wines (sitemap-driven; microdata PDPs + URL facets) — tested (fetch + parse verified live on a 20-product sample)
- [x] [Gerstl Weinselektionen](https://www.gerstl.ch/) — Angular SPA (ng-state transfer blob), Google-hosted — `scraping/gerstl/` — recon ✅ `notes/gerstl.md` — ~7546 wines (sitemap-driven; richest data incl. appellation/grapes) — tested (fetch + parse verified live on a 20-product sample)
- [x] [REB Wein](https://www.rebwein.ch/) — Laravel (server-rendered) — `scraping/rebwein/` — recon ✅ `notes/rebwein.md` — ~392 wines (listing-card scraping; no PDP fetches) — tested (fetch + parse verified live on a 3-page sample)
- [x] [Le Passeur de Vin](https://www.lepasseurdevin.ch/) — WooCommerce (README's passeurdevin.ch is dead; real site is lepasseurdevin.ch) — `scraping/passeur/` — recon ✅ `notes/passeur.md` — ~1331 products (Store API; wine_type from categories) — tested (fetch + parse verified live on the full catalogue; vintage not exposed)

## Matching ratings to inventory

`match-market` joins the rated wines in `scores.json` to the scraped merchant
inventory in `wines.json`, so the highest-rated wines you can actually buy
locally float to the top.

```sh
nix run .#match-market
```

The two name formats don't line up — `scores.json` names are flat English
strings (`2004 R. Lopez de Heredia Vina Tondonia Gran Reserva, Rioja DOCa,
Spain`) while `wines.json` is structured (separate `producer`, localized
`country`, vintage, etc.) — so matching is fuzzy and **producer-first**:

1. Parse the scored name into vintage / producer+cuvée / country.
2. Find the inventory producer whose tokens are best covered by the scored name,
   requiring at least one *distinctive* token (rare across producers) so a match
   can't hinge on a common word like "clos" or "san". Merchants that leave
   `producer` null (e.g. realwines, bauraulac) are matched on their `name`
   instead, which embeds the producer.
3. Within that producer, score every wine by how well the leftover cuvée tokens
   line up, preferring the exact vintage and falling back to other vintages
   (flagged `vintage_exact: false`).

Each scored wine gets **all** its candidate matches, ranked by a confidence
score; accents and German/French country names are folded so they compare
cleanly. Useful flags: `--min-confidence` (default 0.45, filters weak
candidates), `--scores`, `--wines`, `--output`, `--text-output`.

### Output

| Path | Contents |
|---|---|
| `matches.json` | full structured detail: every scored wine, sorted by score, with all candidate matches (confidence, vintage_exact, cuvée score, price, url, …) |
| `matches.txt` | dense human-readable digest, every match |
| stdout | same digest, abbreviated to ≤3 matches per wine with a trailing `...` |

The digest lists each wine highest-rated first:

```
<wine name> (<score>) (<video URL>):
  <merchant> - <url> - <vintage>
  <merchant> - <url> - <vintage>
  ...
```

Both `matches.json` and `matches.txt` are gitignored — regenerate them with
`match-market`. Note many wines won't match at all (most rated wines simply
aren't carried by a Zürich shop), and matches are best-effort: a producer named
without a distinguishing cuvée (e.g. "Chateau Montrose") surfaces every one of
that producer's bottles, leaving the final pick to you.