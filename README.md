# winerank

SLOP WARNING: Everything in this repo is AI generated in the laziest possible
way. To the extent that I have ever even looked at the code, it seems to be
fucking garbage. Don't train your AI on this and please don't judge my
engineering by it.

Two tools in one repo:

1. **`fetch-transcripts`** — download YouTube transcripts and descriptions for every video on a channel.
2. **`fetch-realwines` / `parse-realwines`** — scrape the full [realwines.ch](https://realwines.ch) wine catalogue to JSON via the public WooCommerce Store API.

All tools are packaged as Nix flake apps and work inside a `nix develop` shell.

---

## Prerequisites

- [Nix](https://nixos.org) with flakes enabled
- Run all commands from the repo root

---

## realwines.ch scraper

Fetches the full realwines.ch inventory (462 wines) via the shop's public WooCommerce Store API (`/wp-json/wc/store/v1/products`). No authentication required. The raw API pages are cached locally so the network is only hit once — useful on slow connections and for iterating on the parser.

### Quick start

```bash
# 1. Download all pages from the live API → realwines/cache/page_NNN.json
nix run .#fetch-realwines

# 2. Parse the cache into a clean JSON file → realwines/wines.json
nix run .#parse-realwines -- --from-cache --output realwines/wines.json
```

After step 1, the cache is permanent. Re-running step 2 is instant and makes no network requests.

### How caching works

`fetch-realwines` saves each API page as `realwines/cache/page_NNN.json` and a metadata file as `realwines/cache/_meta.json`. On subsequent runs it skips any page file that already exists.

```
realwines/
├── cache/
│   ├── _meta.json        # total product count, total pages
│   ├── page_001.json     # raw API response, page 1
│   ├── page_002.json
│   └── ...
└── wines.json            # final parsed output (gitignored)
```

To force a full re-download (e.g. to pick up new stock):

```bash
nix run .#fetch-realwines -- --refresh
```

To re-download a single page, just delete it:

```bash
rm realwines/cache/page_003.json
nix run .#fetch-realwines
```

### Iterating on the parser

The parser reads from the local cache and writes to stdout or a file — no network involved:

```bash
# Write to file
nix run .#parse-realwines -- --from-cache --output realwines/wines.json

# Pipe to jq for quick exploration
nix run .#parse-realwines -- --from-cache | jq '[.[] | select(.colour == "Red")]'

# Inside nix develop (faster — no derivation rebuild)
nix develop
python3 realwines/parse.py --from-cache --output realwines/wines.json
```

### Output format

Each record in `wines.json`:

```json
{"name": "Lafite Rothschild Pauillac – 1953", "currency": "CHF", "price": 227000, "vintage": "1953"}
```

| Field | Type | Notes |
|---|---|---|
| `name` | string | HTML entities decoded (e.g. `–` not `&#8211;`) |
| `currency` | string | Always `"CHF"` for this shop |
| `price` | integer | Current price in minor units (Rappen). Divide by 100 for CHF. e.g. `227000` = CHF 2270.00 |
| `vintage` | string\|null | Four-digit year, or `null` if not set |

---

## YouTube transcript fetcher

Downloads transcripts and descriptions for every video on a YouTube channel, saving one JSON file per video.

### Quick start

```bash
# With YouTube Data API v3 key (recommended):
nix run .#fetch-transcripts -- --api-key YOUR_KEY --channel-id UC... --output-dir transcripts/

# From a plain-text file of video IDs (one per line):
nix run .#fetch-transcripts -- --video-ids-file ids.txt --output-dir transcripts/

# Both: use file for IDs, API key to enrich with titles/descriptions:
nix run .#fetch-transcripts -- --video-ids-file ids.txt --api-key YOUR_KEY --output-dir transcripts/
```

Transcript language priority:
1. Manually created subtitles in the video's native language
2. Auto-generated subtitles in the native language
3. First available track (any language) — logged as a warning

### Inside the dev shell

```bash
nix develop
python3 scripts/fetch_transcripts.py --help
```

---

## Nix apps reference

| Command | Description |
|---|---|
| `nix run .#fetch-realwines` | Fetch realwines.ch API pages to local cache |
| `nix run .#parse-realwines` | Parse cache → clean JSON |
| `nix run .#fetch-transcripts` | Fetch YouTube transcripts |
| `nix develop` | Drop into a shell with all dependencies available |
