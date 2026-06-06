# Smith & Smith (smithandsmith.ch) scraping notes

Recon for a `smith-and-smith/` scraper analogous to `realwines/`. Captured 2026-06-06.

## TL;DR
- Platform: **ASP.NET/IIS (likely nopCommerce)** — first-pass fingerprint from
  README; could not be confirmed — site returned `403 x-deny-reason:
  host_not_allowed` (Anthropic egress proxy) for every request. All conventions
  below follow the nopCommerce known-platform shortcut from RECON.md.
- Best target: German-locale category HTML at
  `https://www.smithandsmith.ch/de/wein?pagenumber=N` (most likely; also try
  `/de/weine`, `/de/wine`). Pagination: `?pagenumber=N` (nopCommerce standard).
- Total products: **unknown** — could not fetch. Smith & Smith is a Zürich + Bern
  boutique merchant; rough estimate 300–600 SKUs.
- Gotchas: egress proxy blocks all requests from this environment. Must run
  fetch.py from a residential or whitelisted IP. Category URL slug not verified —
  update `WINE_CATEGORY_URL` in fetch.py once live access is available.

## Platform / fingerprint

**Could not verify** — every HTTP request returned `HTTP/2 403
x-deny-reason: host_not_allowed` (Anthropic egress gateway). No response
headers, cookies, or page body were observable.

Standard nopCommerce fingerprints to look for when live access is available:
- Response header: `x-powered-by: ASP.NET` and `Server: Microsoft-IIS/*`
- Cookie: `Nop.customer` session cookie
- Body markers: `nopCommerce`, `/Themes/`, `nop-`, `sku="..."` in HTML
- Pagination: `?pagenumber=N` links in a `<div class="pager">` block
- URL structure: `/de/<category-slug>?pagenumber=N`

## URLs

- Homepage: `https://www.smithandsmith.ch/de`
- robots.txt: `https://www.smithandsmith.ch/robots.txt` — **not fetched** (403)
- Sitemap: typically `https://www.smithandsmith.ch/sitemap.xml` — not fetched
- Wine category candidates (in priority order):
  - `https://www.smithandsmith.ch/de/wein` (German standard)
  - `https://www.smithandsmith.ch/de/weine`
  - `https://www.smithandsmith.ch/de/wine`
  - `https://www.smithandsmith.ch/de/shop` (catch-all)
  - `https://www.smithandsmith.ch/de/catalog` (nopCommerce default)
- nopCommerce does not expose a standard public REST/JSON API; HTML scraping
  is the only viable approach.

## Pagination & scale

Standard nopCommerce listing pagination: `?pagenumber=N` (1-indexed). E.g.:
```
https://www.smithandsmith.ch/de/wein?pagenumber=1
https://www.smithandsmith.ch/de/wein?pagenumber=2
```
Page 1 can be fetched without the parameter (the site redirects or serves it
identically). Page size is configurable in nopCommerce back-office, typically
12–24 per page.

Total page count is found in page 1 HTML by:
1. Scanning the `<div class="pager">` block for the highest `?pagenumber=N`
   link value.
2. Or matching `Page \d+ of (\d+)` text.

Estimated total: unknown. Fetch page 1 live to discover.

## Extracting the data

nopCommerce listing pages embed product data in HTML product cards. Two
strategies, in order of preference:

**1. JSON-LD (preferred)** — nopCommerce themes often embed
`<script type="application/ld+json">` blocks:
```python
import re, json
blocks = re.findall(
    r'<script[^>]+type=["\']application/ld\+json["\'][^>]*>(.*?)</script>',
    html, re.S
)
for raw in blocks:
    obj = json.loads(raw)
    if obj.get("@type") == "Product":
        # name, sku, url, offers.price, offers.priceCurrency, offers.availability
```

**2. HTML card parsing (fallback)** — nopCommerce product listing cards:
```html
<div class="product-item">
  <div class="details">
    <h2 class="product-title">
      <a href="/de/wein/producer-name-2020">Producer Name 2020</a>
    </h2>
    <div class="prices">
      <span class="price actual-price">CHF 34.90</span>
    </div>
    <div class="add-info">
      <div class="sku">
        <span class="label">SKU:</span>
        <span class="value">ABC123</span>
      </div>
    </div>
  </div>
</div>
```

Fields:
- Product title `<a>` text → `name` (also parse vintage + producer from it)
- Product `href` → `url`
- `.actual-price` text → price (strip `CHF `, convert decimal × 100 → rappen)
- `.sku .value` text → `sku`
- Stock: nopCommerce adds `class="out-of-stock"` or hides add-to-cart for
  unavailable items; presence of `add-to-cart-button` → `in_stock = True`

## Product record schema

Expected minimal record:
```json
{
  "merchant": "smith-and-smith",
  "name": "Château X Bordeaux 2018",
  "producer": "Château X",
  "vintage": 2018,
  "currency": "CHF",
  "price": 3490,
  "in_stock": true,
  "sku": "CHX-BDX-2018",
  "url": "https://www.smithandsmith.ch/de/wein/chateau-x-bordeaux-2018"
}
```

Price is a **decimal CHF string** (e.g. `"34.90"`) in the HTML → multiply × 100
and round to integer rappen.

Producer and vintage are typically only in the product name; parse via regex:
- Vintage: `\b(19|20)\d{2}\b`
- Producer: words before the vintage year, or everything except the last N
  words (heuristic; confirm with live data).

Non-wine items: Smith & Smith may also sell spirits and accessories. Filter by
the wine category URL (the scraper only fetches that URL), or filter by name
if non-wine items appear in the wine category listing.

## Suggested scraper shape

- **fetch.py** (`scraping/smith-and-smith/fetch.py`):
  - `WINE_CATEGORY_URL = "https://www.smithandsmith.ch/de/wein"` — adjust if wrong.
  - Page 1 fetched/loaded first; `detect_total_pages(html)` finds max
    `?pagenumber=N` in the pager block, or falls back to 1.
  - Cache: `cache/page_{N:03d}.html`
  - Args: `--refresh`, `--max-pages N`
  - 1.5 s polite delay between live requests only

- **parse.py** (`scraping/smith-and-smith/parse.py`):
  - `--from-cache`: reads all `cache/page_*.html` files
  - Try JSON-LD `@type:Product` blocks first; fall back to HTML card regex
  - Map to core record: merchant, name, producer, vintage, currency, price,
    in_stock, sku, url
  - Args: `--from-cache`, `--output FILE`

## Open questions / future work

- **Connectivity**: Egress allowlist blocks all recon. Run fetch.py from a
  residential IP to confirm platform and category URL.
- **Category URL**: `/de/wein` is the best guess but unverified. Check homepage
  nav (`<a href="...">Wein</a>`) to find the correct slug.
- **Platform confirmation**: Verify nopCommerce via `x-powered-by: ASP.NET`
  header, `Nop.customer` cookie, and body markers once accessible.
- **Page size**: Determine products-per-page from page 1 HTML; adjust page-count
  detection accordingly.
- **Spirits / accessories filtering**: Smith & Smith likely sells spirits
  alongside wine; confirm whether the `/de/wein` category is wine-only or mixed.
- **Vintage/producer parsing**: Confirm naming convention from real product
  titles — Swiss merchants vary in how they order producer vs. wine name vs. year.
