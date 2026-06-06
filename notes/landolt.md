# Landolt Weine (landolt-weine.ch) scraping notes

Recon for a `landolt/` scraper analogous to `realwines/`. Captured 2026-06-06.

## TL;DR (verified live 2026-06-06 — supersedes the store-api guess below)
- Platform: **Shopware 6 confirmed** (`cms-element`, `product-box`). The
  storefront is served from **landolt-weine.nextag.ch** (www.landolt-weine.ch is
  a front). PDPs are server-rendered with schema.org microdata — no Store API
  key needed, so the scraper just parses HTML rather than calling `/store-api`.
- **Approach: sitemap-driven** (`scraping/landolt/`). `/sitemap.xml` → one
  gzipped sitemap (783 locs); **693 products** as `/<slug>/<number>`. Fetch each
  PDP, parse microdata + the properties table.
- Per-product data: `<h1>` = name (note: `itemprop="name"` is the *manufacturer*,
  not the product), `itemprop="price"`/`priceCurrency`, and a properties table
  with `Produzent`, `Herkunft` (= "Country/Region"), `Traubensorten` (grapes),
  `Alkoholgehalt`, `Gebinde` (bottles-per-case, not ml), `Ort` (winery village).
  Stock from the `product-delivery-information` block.
- **Gotchas:** no `Jahrgang` property (vintage is sparse — many are own-label
  "Sélection Landolt" wines); the first sitemap entries are gift/accessory items
  (Gutschein/Verpackung) that have no producer. ~693 products → `--max-products`
  for sampling.

## Platform / fingerprint
- First-pass fingerprint: Shopware 6 (from README table, 2026-06-04) — `session-` cookie
  prefix and Vue.js are characteristic Shopware 6 tells.
- Could not verify from headers/body — every curl attempt returned
  `HTTP/2 403 x-deny-reason: host_not_allowed` (Anthropic egress proxy blocks this host).
  IP: 5.148.188.138, TLS cert via Anthropic SDS gateway.
- Shopware 6 tells (to verify on first run):
  - Response cookies: `session-<hash>=...` (SW6 default session cookie name).
  - Response headers: `x-powered-by: shopware` or `sw-context-token` cookie.
  - Body markers: `data-shopware`, `shopware`, `sw-plugin`, `<meta name="shopware-access-key">`,
    Vue mount point `<div id="app">` with `data-access-key="SWSC..."`.
  - Store API: `POST /store-api/product` returns JSON `{"total":N,"elements":[...]}`.
  - Generator meta: `<meta name="generator" content="Shopware 6">` (common on default themes).
- Shopware 5 tells (if SW6 is wrong): `/api/articles?limit=5` returns articles JSON;
  `session-N` style cookie; Smarty templates rather than Vue.

## URLs
- Catalogue (Store API): `POST https://www.landolt-weine.ch/store-api/product`
- Category listing (HTML fallback): `https://www.landolt-weine.ch/weine` or
  `https://www.landolt-weine.ch/wein` (verify exact slug from homepage nav links).
- robots.txt: blocked during recon; typically Shopware disallows `/store-api/` —
  but the API endpoint is intended for storefront use (no auth beyond access key).
- sitemap.xml: usually at `/sitemap.xml` — check for total product count.

## Pagination & scale
- Shopware 6 Store API: `page` param (1-indexed), `limit` up to 100 per request.
  Response contains `{"total":N,"elements":[...]}` — read `total` from page-1 response
  to compute page count. Stop when `elements` array length < `limit`.
- Estimated scale: Landolt Weine is a small Zürich grower/merchant — likely 50–300 wine SKUs.
- Pages are disjoint (Store API result sets don't overlap by design).

## Extracting the data

### Step 1 — Find the access key
On first run, fetch the Landolt homepage and grep for the access key:
```
curl -sA "Mozilla/5.0..." https://www.landolt-weine.ch/ | grep -i 'access-key\|sw-access\|SWSC'
```
Look for patterns like:
- `<meta name="shopware-access-key" content="SWSC...">` (common SW6 theme)
- `data-access-key="SWSC..."` on `<div id="app">` or `<body>`
- `accessKey: 'SWSC...'` in inline JS

The access key is a public storefront key (not a secret) — safe to embed in scripts.

### Step 2 — POST to Store API
```
POST /store-api/product
Content-Type: application/json
sw-access-key: SWSC<value-from-page>

{
  "limit": 100,
  "page": 1,
  "filter": [{"type": "equals", "field": "active", "value": true}],
  "includes": {
    "product": ["id","productNumber","name","description","active",
                "price","stock","availableStock","available",
                "manufacturer","manufacturerNumber","categories","cover",
                "customFields","seoUrls"]
  }
}
```

Response shape (Shopware 6 standard):
```json
{
  "total": 150,
  "elements": [
    {
      "id": "abc123...",
      "productNumber": "LW-ROT-001",
      "name": "Langnaur 2021 — Roter Landolt",
      "price": [{"currencyId":"...","gross":28.0,"net":25.93,"linked":true}],
      "stock": 12,
      "availableStock": 10,
      "available": true,
      "manufacturer": {"name": "Landolt Weine"},
      "seoUrls": [{"seoPathInfo": "langnaur-2021-roter-landolt"}],
      "customFields": {"vintage": 2021, "producer": "..."}
    }
  ]
}
```

Price: `price[0]["gross"]` is a decimal float in CHF — multiply × 100 and round to int
(rappen). The `price` array may have multiple entries (one per currency); take the CHF one
(match `currencyId` or just take index 0 if only CHF is configured).

Stock: `available` (bool) → `in_stock`. `availableStock` gives the quantity.

### Step 3 — Filter wine vs non-wine
Landolt Weine is a focused grower/merchant — likely mostly wine. If accessories or
gift sets appear, filter by category name (keep categories matching `wein`, `rotwein`,
`weisswein`, `rosé`, `sekt`, `perlwein`; exclude `geschenk`, `accessoire`, `glas`).

## Product record schema
Expected fields from the Store API (Shopware 6 standard):
- `id` → internal UUID (use `productNumber` as sku if available)
- `productNumber` → sku
- `name` → parse vintage with regex `\b(19|20)\d{2}\b`; full name is the wine name
- `manufacturer.name` → producer (may equal "Landolt Weine" for all products if
  it's a grower — in that case producer = merchant)
- `seoUrls[0].seoPathInfo` → URL slug; prepend `https://www.landolt-weine.ch/`
- `price[0].gross` (decimal float CHF) → multiply × 100, round → price (minor units)
- `available` (bool) → in_stock
- `stock` or `availableStock` → quantity
- `categories[].name` → wine/non-wine filter

Missing fields (may need PDP fetch or `customFields` inspection):
- Vintage (usually only in product name — extract with regex)
- Region / appellation
- Grape variety
- Bottle size (may be in name or a property/option)
- Producer (if all products are own-label, producer = "Landolt Weine")

Custom fields vary by shop configuration — Landolt may store vintage, grape, region
in `customFields` (keys like `custom_wine_vintage`, `custom_producer`, etc.) or in
Shopware property groups (accessible via `properties` includes).

## Suggested scraper shape
- `fetch.py`:
  1. Fetch homepage, extract `sw-access-key` (grep for `SWSC` pattern).
  2. Cache access key to `cache/_meta.json`.
  3. POST to `/store-api/product?page=N` (with access key header), cache JSON
     responses as `cache/page_001.json`, `cache/page_002.json`, …
  4. Page 1 response gives `total` → compute total pages = ceil(total / limit).
  5. `--refresh` flag bypasses cache; `--max-pages N` safety cap; polite 1.5s delay.
- `parse.py`:
  - `--from-cache`: reads all `cache/page_*.json` files.
  - Maps Store API fields to core record: merchant, name, producer, vintage,
    currency ("CHF"), price (int rappen), in_stock, sku, url.
  - Extracts vintage from product name via regex `\b(19|20)\d{2}\b`.
  - Outputs JSON array.

## Open questions / future work
- **Network was blocked** — all platform assertions are unverified. First run with real
  internet should confirm: (a) is it Shopware 6 or 5? (b) does the Store API respond?
  (c) where is the access key in the page source? (d) how many products?
- If Shopware 5: the Store API doesn't exist. Fallback is `/api/articles?limit=100&start=N`
  (if not auth-gated) or HTML category page scraping.
- If the Store API requires a `sw-context-token` (session) in addition to the access key:
  bootstrap by hitting the homepage first to get the session cookie.
- Landolt may use property groups for wine attributes (region, grape, appellation) —
  add `"properties"` to the `includes.product` array to retrieve them.
- Bottle-size variants: Shopware products can have child variants; decide whether
  to emit one record per variant or per parent product.
- Custom fields: inspect a live product response to identify any `custom_*` keys
  containing structured wine metadata.
