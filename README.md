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

### 1. Creating a YouTube API Key (via gcloud CLI)

If you have `gcloud` installed and authenticated, you can easily create a YouTube Data API v3 key:

```bash
# 1. Set your active Google Cloud project
nix run nixpkgs#google-cloud-sdk -- gcloud config set project YOUR_PROJECT_ID

# 2. Enable the YouTube Data API v3 service
nix run nixpkgs#google-cloud-sdk -- gcloud services enable youtube.googleapis.com

# 3. Create the API key
nix run nixpkgs#google-cloud-sdk -- gcloud services api-keys create --display-name="winerank-api-key"
```

### 2. Running the script

Incremental fetching is enabled by default (it skips any video if its output JSON file already exists). To avoid hitting YouTube's rate limits, a default delay of **2.0 seconds** is enforced between video downloads, and the script automatically performs **exponential backoff retries** (up to 4 times) if a `429 Too Many Requests` error is encountered.

```bash
# Run for the default channel (@KonstantinBaumMasterOfWine) with default 2s delay:
nix run .#fetch-transcripts -- --api-key YOUR_KEY --output-dir ./transcripts

# Adjust the delay (e.g., 5 seconds) to be even safer against rate limits:
nix run .#fetch-transcripts -- --api-key YOUR_KEY --sleep 5.0 --output-dir ./transcripts

# Run with a plain-text file of video IDs (one per line) without an API key:
nix run .#fetch-transcripts -- --video-ids-file ids.txt --output-dir ./transcripts
```

#### Working around IP Blocks & Bot Checks

If YouTube blocks your requests (returning a `Could not retrieve a transcript...` or `IPBlocked` error), you can bypass this by passing browser cookies using `yt-dlp`'s built-in extraction:

- **Automatic Browser Extraction (Easiest)**: If you are logged into YouTube in your browser (e.g., Firefox or Chrome), the script can extract your cookies directly from your profile:
  ```bash
  nix run .#fetch-transcripts -- --api-key YOUR_KEY --cookies firefox --output-dir ./transcripts
  ```
  *(Note: You can specify other browsers like `chrome`, `safari`, `edge`, `brave`, etc. If you get a database lock error, close the browser before running).*

- **Manual Cookie File**: If you prefer, export your cookies in **Netscape** format using an extension, save it to a file (e.g., `cookies.txt`), and run:
  ```bash
  nix run .#fetch-transcripts -- --api-key YOUR_KEY --cookies ./cookies.txt --output-dir ./transcripts
  ```

#### Transcript language priority:
1. Manually created subtitles in the video's native language (as defined in YouTube metadata)
2. Auto-generated subtitles in the native language
3. First available track (any language) — logged as a warning

### Output JSON Format

Each video's JSON file is saved as `<video_id>.json` in the output directory:

```json
{
  "id": "dQw4w9WgXcQ",
  "title": "Video Title",
  "description": "Full video description text...",
  "channel": "@KonstantinBaumMasterOfWine",
  "native_language": "en",
  "transcript_language": "en",
  "transcript_kind": "manual",
  "transcript": [
    { "text": "Hello and welcome", "start": 0.0, "duration": 2.5 },
    ...
  ],
  "error": null
}
```

### Inside the dev shell

For interactive development and full list of command-line flags:

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
