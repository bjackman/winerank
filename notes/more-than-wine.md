# Bottleshop / more-than-wine (more-than-wine.com) scraping notes

Recon for a `more-than-wine/` scraper analogous to `realwines/`. Captured 2026-06-06.

## TL;DR
- Platform: **PrestaShop** (first-pass fingerprint; could not be confirmed —
  see Connectivity below). Data source: **HTML category pages** with JSON-LD
  product schema embedded as `<script type="application/ld+json">`.
- Best target: `https://www.more-than-wine.com/<wine-category>?p=N` (standard
  PrestaShop `?p=N` pagination). Category slug unknown — likely `wein`, `vin`,
  `wine`, `vins`, or a German/French path; must be confirmed with live access.
- Pagination: `?p=N` (PrestaShop standard). Page size typically 24–48 per page.
  Total product count: **unknown** — site was unreachable during recon.
- Gotchas: CDN/WAF allowlist blocks all requests from the CI/server IP
  (`403 x-deny-reason: host_not_allowed`). Must run fetch.py from a residential
  or whitelisted IP. Likely non-wine items (accessories, gift sets) sold alongside
  wine — filter by category.

## Platform / fingerprint

**Could not verify** — all HTTP requests returned `403 x-deny-reason:
host_not_allowed` (CDN/WAF IP allowlist). No headers, body, or cookies were
observable. The first-pass fingerprint (PrestaShop) is from an earlier external
observation and is untested.

Standard PrestaShop fingerprints to look for when access is available:
- Response cookie: `PrestaShop-*` session cookie.
- Header: `x-powered-by: PrestaShop`.
- Body markers: `id_prestashop`, `prestashop`, `window.prestashop = `, or
  `<meta name="generator" content="PrestaShop">`.
- URLs: `/modules/`, `/themes/`, `?id_product=`, `?controller=category`.

## URLs

- Homepage: `https://www.more-than-wine.com/`
- robots.txt: `https://www.more-than-wine.com/robots.txt` — **not fetched** (403)
- Sitemap: typically `https://www.more-than-wine.com/sitemap.xml` — not fetched
- Wine category: unknown slug. Likely candidates:
  - `https://www.more-than-wine.com/wein`
  - `https://www.more-than-wine.com/vin`
  - `https://www.more-than-wine.com/wine`
  - `https://www.more-than-wine.com/vins`
  - Or a numeric category controller: `?controller=category&id_category=N`
- PrestaShop Webservice API (`/api/products`) is typically disabled on public
  stores; do not rely on it.

## Pagination & scale

Standard PrestaShop category pagination: `?p=N` (1-indexed). E.g.:
```
https://www.more-than-wine.com/wein?p=1
https://www.more-than-wine.com/wein?p=2
```

Page size (products per page) is configurable in PrestaShop back-office,
typically 12, 24, 36, or 48. Total product count and page count are usually
found in the HTML as:
- A `<span class="total-products">` or similar element.
- Pagination `<li>` elements listing page numbers (find the max).
- JSON-LD `offers.offerCount` if the category emits a `ProductCollection` schema.

Total products: **unknown** — fetch page 1 to discover.

## Extracting the data

Two strategies, in order of preference:

**1. JSON-LD (preferred)** — PrestaShop often embeds `<script
type="application/ld+json">` blocks in product cards or category pages. Extract
with:
```python
import re, json
blocks = re.findall(r'<script[^>]+type=["\']application/ld\+json["\'][^>]*>(.*?)</script>', html, re.S)
for block in blocks:
    obj = json.loads(block)
    # obj["@type"] == "Product" → individual product with name, sku, offers.price, url, etc.
```

**2. HTML card parsing (fallback)** — PrestaShop product listing cards typically
have:
```html
<article class="product-miniature" data-id-product="123">
  <a class="product-thumbnail" href="...">...</a>
  <h2 class="product-title"><a href="...">Product Name</a></h2>
  <span class="price">CHF 24.90</span>
</article>
```
Parse with regex on the `data-id-product`, `href`, `.product-title a`, and
`.price` elements.

## Product record schema

Expected fields from PrestaShop JSON-LD (`@type: "Product"`):
```json
{
  "@type": "Product",
  "name": "Domaine X Riesling 2021",
  "sku": "12345",
  "url": "https://www.more-than-wine.com/...",
  "image": "...",
  "description": "...",
  "offers": {
    "@type": "Offer",
    "price": "24.90",
    "priceCurrency": "CHF",
    "availability": "http://schema.org/InStock"
  }
}
```

Price is a **decimal string** → multiply × 100 and round to integer rappen.
Producer and vintage are typically only in the product name (parse via regex).
Region/grape/bottle-size available only on product detail pages (PDP), not on
category listing pages.

## Suggested scraper shape

- **fetch.py**: Fetch `<category_url>?p=N` in a loop. Discover page count from
  page 1 HTML (try the total-products count or max pagination link number).
  Cache each page as `cache/page_{N:03d}.html`. Args: `--refresh`, `--max-pages N`.

- **parse.py**: For each cached HTML page:
  1. Extract all JSON-LD `<script>` blocks with `re.findall`.
  2. `json.loads()` each block; keep `@type == "Product"` records.
  3. Map fields: `name`, `sku`, `url`, `offers.price` (→ int rappen),
     `offers.priceCurrency`, `offers.availability` (→ `in_stock` bool).
  4. Parse `producer` and `vintage` from `name` with regex.
  5. Fallback: if no JSON-LD products found, parse HTML card elements directly.

- Filter: Limit to the wine category URL (the scraper only fetches that
  category), which should exclude accessories. If the category still includes
  non-wine items, filter by `name` or add a `product_type` field when available.

## Open questions / future work

- **Connectivity**: The CDN allowlist must be resolved before fetch.py can run
  live. Residential IP or VPN required. Alternatively, contact the site operator
  for a scraping allowance.
- **Category slug**: Must be determined by browsing the live site. Check the
  homepage nav for a "Wein" / "Vin" / "Wine" link.
- **Platform verification**: Confirm PrestaShop via headers/cookies/body once
  accessible.
- **Page size**: Check `?n=` query param (PrestaShop products_per_page) — try
  `?n=100` or `?n=48` to reduce number of pages to fetch.
- **Non-wine filtering**: The site sells natural wine — check if accessories or
  non-wine items appear in the wine category and add a filter if needed.
- **Vintage parsing**: PrestaShop product names often include vintage year.
  Confirm format (e.g. "Name 2021" vs "2021 Name").
- **Producer field**: Usually only in the title or as a `brand` JSON-LD field.
  May require PDP fetch for clean producer data.
