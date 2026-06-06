# Smith & Smith (smithandsmith.ch) scraping notes

Recon for a `smith-and-smith/` scraper analogous to `realwines/`. Captured 2026-06-06.

## TL;DR (verified live 2026-06-06 — supersedes the nopCommerce guess below)
- Platform: **custom React/ASP.NET on Microsoft-IIS — NOT nopCommerce.** The
  homepage carries `Server: Microsoft-IIS/10.0` + `X-Powered-By: ASP.NET` but
  none of the nopCommerce body markers (`item-box`, `?pagenumber=`, `/Themes/`).
- Target: the full catalogue listing at `https://www.smithandsmith.ch/de/shop`,
  paginated with **`?page=N`** (1-indexed), **27 product cards per page**.
  Out-of-range pages return 0 cards.
- Total products: **~2'984** (from `<div class="h4">2'984 Produkte</div>` on the
  listing) → ~111 pages.
- Product cards: `<div class="card artikel-box" data-artikelid="…">` with
  `artikel-box__herkunft` (producer + "Region, Country"), `artikel-box__name`,
  `artikel-box__eigenschaften` (vintage + bottle size), and
  `artikel-box__preisaktuell` (price, e.g. `CHF <!-- -->43.00`). No JSON-LD
  products (only an Organization block). See `scraping/smith-and-smith/parse.py`.
- The earlier recon below assumed nopCommerce because the egress proxy blocked
  every request at implementation time; it's kept for history but is wrong.

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
