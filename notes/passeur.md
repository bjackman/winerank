# Le Passeur de Vin (lepasseurdevin.ch) scraping notes

Recon captured live 2026-06-06. Scraper implemented at `scraping/passeur/`.

## TL;DR
- **The README's URL `passeurdevin.ch` does not resolve.** The real site is
  **lepasseurdevin.ch** (note the "le").
- Platform: **WooCommerce** (WordPress; `wp-json`). The Store API is open, so
  this reuses the vinazion/realwines pattern exactly.
- ~1331 products / 14 pages (incl. some non-wine: spirits, saké, gift cards).

## URLs
- Store API: `https://www.lepasseurdevin.ch/wp-json/wc/store/v1/products`
  `?per_page=100&page=N&orderby=id&order=asc`. Pagination via `X-WP-Total` /
  `X-WP-TotalPages` response headers.
- Product pages: `…/produit/<slug>/`. Catalogue: `/categorie-produit/vins/`.
- robots.txt is nearly empty; `/sitemap.xml` is a Yoast index (post/page/
  winegrower sitemaps) — not needed since the Store API enumerates everything.

## Extracting the data
- Products carry **no attributes/brands** (unlike vinazion). So:
  - `price` = `prices.price` (integer rappen), `currency_code` = CHF
  - `in_stock` = `is_in_stock`, `sku`, `url` = `permalink`
  - `wine_type` from `categories` (Vin Rouge/Blanc/Rosé/Champagne/Vin
    Effervescent/Saké/Shochu); full category list kept under `categories`
  - `producer` ≈ the segment after the last comma in the name (e.g.
    "Garrut Vin de Table, Domaine Sicus" → "Domaine Sicus") — ~83% hit rate
- **Gotcha:** this is a natural-wine importer; product names/API rarely carry a
  vintage, so `vintage` is ~1%. `country`/`region`/`grapes` are not exposed.
