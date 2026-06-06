# REB Wein (rebwein.ch) scraping notes

Recon captured live 2026-06-06. Scraper implemented at `scraping/rebwein/`.

## TL;DR
- Platform: **Laravel** (nginx; `XSRF-TOKEN` cookie, `csrf-token` meta).
  Server-rendered HTML — no JS needed, no JSON-LD.
- Whole catalogue is the `/weine` listing, paginated `?page=N`, ~18 product
  cards per page. The header shows `Anzahl Weine: 392` (→ ~22 pages). No
  sitemap or robots.txt (both 404).

## URLs
- Listing: `https://www.rebwein.ch/weine?page=N`
- Product: `https://www.rebwein.ch/wein/<slug>/<uuid>` (the trailing UUID is the
  product id; used as `sku`). Producers: `/winzer/<slug>/<uuid>`.
- Filtered listings via query params (`category`/`country`/`area`/`manufacturer`
  /`cultivation` as UUIDs) — not needed; the unfiltered `/weine` covers all.

## Extracting the data
- Each card is `<a href="…/wein/<slug>/<uuid>" class="wine-item js-sh">…</a>`:
  - `<h3 class="wine__title">` → name
  - `<div class="wine__description">` → producer (line 1) + vintage (line 2: a
    year or `NV`), split on `<br>`
  - `<div class="wine__price">CHF 25.00</div>` → price
  - presence of `wine__not-available` → out of stock
- Listing cards are self-sufficient, so the scraper never fetches PDPs.
- Coverage on a 3-page sample: 100% name/producer/price/stock/sku, vintage 83%
  (rest genuinely NV). ~392 wines → ~22 listing pages.
