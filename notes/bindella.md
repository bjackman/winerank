# Bindella Weinshop (bindella.ch) scraping notes

Recon for a `bindella/` scraper analogous to `realwines/`. Captured 2026-06-06.

## TL;DR (verified live 2026-06-06 — supersedes the blind guess below)
- Platform: **BigCommerce confirmed** (`cdn11.bigcommerce.com` assets, Cornerstone)
  — BUT it's a **headless / JS-rendered storefront**, not a standard Stencil one.
  The catalogue is NOT scrapable from static HTML the way the other merchants are.
- What does NOT work (all checked live):
  - `/weinshop/` 301-redirects to `/weine`, which is a **marketing landing page** —
    zero product cards, zero prices (`CHF` count 0), no `?page=` pagination.
  - No JSON-LD anywhere (`application/ld+json` count 0 on every page tried).
  - Stencil API `/api/storefront/products` → **404**. No `/api/`, `graphql`,
    `storefront`, or Bearer-token markers in page source.
  - `/sitemap.xml`, `/sitemap_index.xml`, `/robots.txt` → **all 404**.
  - Catalogue is browsable only by `/wein/laender/<country>` and
    `/wein/produzenten/<slug>`; producer pages list wines but carry no prices and
    no buyable product-detail links in static HTML.
- Conclusion: products + prices are loaded client-side via XHR. A working scraper
  needs **either** browser automation (Playwright/headless Chromium) **or**
  reverse-engineering the storefront's XHR/GraphQL endpoint (capture via devtools
  Network tab — not discoverable from static HTML alone). This is out of scope for
  the regex-on-HTML pattern used by the other scrapers. **No code written yet.**

## Platform / fingerprint
- First-pass fingerprint: BigCommerce (from README table, 2026-06-04).
- Could not verify from headers/body — every curl attempt returned
  `HTTP/2 403 x-deny-reason: host_not_allowed` (Anthropic egress proxy blocks
  this host). IP: 20.93.188.253 (Azure), TLS cert via Anthropic SDS gateway.
- BigCommerce tells:
  - Response headers: `x-bc-store-version`, `x-bc-storefront-api-token`, or
    `set-cookie: SHOP_SESSION_TOKEN` cookies.
  - Body markers: `cdn11.bigcommerce.com`, `bigcommerce`, `Cornerstone` (theme).
  - Stencil API at `/api/storefront/products`.
  - GraphQL at `/graphql` (requires `Authorization: Bearer <token>` from the
    page source).

## URLs
- Catalogue: `https://www.bindella.ch/weinshop/?page=N` (HTML category pages).
- Stencil REST API: `https://www.bindella.ch/api/storefront/products?limit=50&page=N`
  (may work without auth; returns JSON array of product objects).
- robots.txt: blocked during recon; typically BigCommerce disallows `/api/`.
- sitemap.xml: usually at `/sitemap.xml` or `/sitemap_index.xml` — check for
  total product count.

## Pagination & scale
- BigCommerce category pages use `?page=N` (1-indexed). The page HTML typically
  includes a total-count element (e.g. `data-total-products` attribute or a
  `<span class="pagination-item-count">N items</span>`) and the default page
  size is configurable (often 20 or 24 per page).
- Stencil API: `?limit=50&page=N`; stop when the returned array has fewer than
  `limit` items.
- Estimated scale: Bindella has an Italian-wine focus and is a medium-sized
  Zürich merchant — estimate 200–600 wine SKUs.

## Extracting the data
Two complementary strategies (try Stencil API first):

### Option A — Stencil API (preferred)
`GET /api/storefront/products?limit=50&page=N`

Returns a JSON array of product objects. Key fields:
```json
{
  "id": 12345,
  "name": "Barolo DOCG 2018",
  "brand": { "name": "Gaja" },
  "sku": "GAJA-BAR-2018",
  "url": "/barolo-docg-2018/",
  "price": { "value": 89.00, "currencyCode": "CHF" },
  "availability": "available",
  "categories": [{ "name": "Rotwein" }],
  "description": "...",
  "images": [...]
}
```
Price is a decimal float — multiply × 100 and round to get minor units (rappen).

### Option B — Category page HTML (fallback)
GET `https://www.bindella.ch/weinshop/?page=N`

BigCommerce Stencil themes embed product data in:
1. **JSON-LD** (`<script type="application/ld+json">`) — one `Product` object per
   PDP, or a `ItemList` on category pages.
2. **Stencil context** — a `{{json product}}` pattern rendered as an inline
   `<script>window.BCData = {...}</script>` or similar.
3. **HTML product cards** — `<article class="card">` with `data-product-id`,
   `<span class="price">`, etc.

For category pages, prefer to grep for `application/ld+json` blocks containing
`"@type":"Product"` or the Stencil `BCData` context object.

## Product record schema
Expected fields from the Stencil API (BigCommerce standard):
- `id` → sku fallback
- `name` → parse vintage with regex `\b(19|20)\d{2}\b`
- `brand.name` → producer
- `sku` → sku
- `url` → prepend `https://www.bindella.ch` if relative
- `price.value` (decimal float) → multiply × 100, round → price (minor units)
- `price.currencyCode` → currency (expect "CHF")
- `availability` ("available" | "unavailable" | "preorder") → in_stock bool
- `categories[].name` → wine/non-wine filter

Wine filter: keep products whose category name matches wine keywords
(`wein`, `wine`, `rosé`, `weiss`, `rot`, `sekt`, `prosecco`, `champagne`,
`barolo`, `chianti`, etc.) or exclude known non-wine categories
(`geschenk`, `accessoire`, `glas`, `olivenöl`).

Missing fields (not in Stencil API without PDP fetch):
- Vintage (extract from name via regex)
- Region / appellation
- Grape variety
- Bottle size (sometimes in name or variant option)

## Suggested scraper shape
- `fetch.py`:
  - Tries `GET /api/storefront/products?limit=50&page=N` first (JSON, easy).
  - Falls back to `GET /weinshop/?page=N` HTML if the API returns 401/403.
  - Cache files: `bindella/cache/page_001.json` (API) or `page_001.html` (HTML).
  - `--refresh` flag to bypass cache; `--max-pages N` safety cap.
  - Progress to stderr; polite 1.5 s delay between live requests.
- `parse.py`:
  - `--from-cache`: reads all `cache/page_*.json` (or `.html`) files.
  - For JSON: parse Stencil API fields directly.
  - For HTML: use `html.parser` + regex to extract JSON-LD product blocks.
  - Outputs JSON array with core record fields.

## Open questions / future work
- **Network was blocked** — all platform assertions are unverified. First run
  with real internet should confirm: (a) is it BigCommerce? (b) does the Stencil
  API respond at `/api/storefront/products`? (c) how many products?
- If the Stencil API requires a `Storefront-Auth-Token` header (some BC stores
  gate it), fall back to HTML category scraping.
- BigCommerce sometimes uses a GraphQL Storefront API at `/graphql` which can
  be called with the per-session token injected in the page HTML — may be
  richer but more complex to bootstrap.
- Bindella may also sell olive oil, pasta, and other Italian deli items —
  confirm non-wine category names once real data is available.
- Bottle-size variants: BigCommerce products can have options (e.g. 75cl /
  150cl); decide whether to emit one record per variant or per product.
