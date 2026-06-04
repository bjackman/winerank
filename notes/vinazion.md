# Vinazion (vinazion.ch) scraping notes

Recon for a `vinazion/` scraper analogous to `realwines/`. Captured 2026-06-04.

## TL;DR
- Platform: **WooCommerce** (WordPress 6.9.4). Data source: the **WooCommerce
  Store API** — identical to realwines.
- Best target: `https://www.vinazion.ch/wp-json/wc/store/v1/products?per_page=100&page=N`.
  Pagination via `X-WP-Total` / `X-WP-TotalPages` response headers.
- **Total products: 305** (`X-WP-Total: 305`), 102 pages at `per_page=3` → ~4
  pages at `per_page=100`. Small, fast crawl.
- **This is the realwines case exactly — reuse `realwines/fetch.py` +
  `realwines/parse.py` almost verbatim**, just swap the `BASE_URL`.

## Platform / fingerprint
- Homepage body: `woocommerce`, `wp-content`, `wp-json` markers;
  `<meta name="generator" content="WordPress 6.9.4">`.
- Store API responds at `/wp-json/wc/store/v1/products` with HTTP 200 and the
  WooCommerce pagination headers (`X-WP-Total: 305`, `X-WP-TotalPages: 102` at
  per_page=3).

## URLs
- Catalogue: `/wp-json/wc/store/v1/products?per_page=100&page=N&orderby=id&order=asc`
  (same query shape realwines uses).
- Note `www.` host: `https://www.vinazion.ch/...`.

## Pagination & scale
- Same as realwines: probe page 1, read `X-WP-TotalPages` (with per_page=100),
  loop pages. ~305 products → ~4 pages at per_page=100.

## Extracting the data
- Each page is a JSON array of product objects (same Store API schema as
  realwines). `parse.py` from realwines already maps `name`, `prices.price`
  (integer minor units), `prices.currency_code`, and `attributes[]` (vintage).

## Product record schema
- WooCommerce Store API product: `name`, `prices.price` (**integer minor units**,
  e.g. cents), `prices.currency_code`, `attributes[]` (terms → vintage/region if
  the shop sets them as product attributes), `description` (HTML),
  `is_in_stock`, `permalink`, `images[]`.
- Vinazion sells multi-country wine (IT/ES/CH/FR/PT). Confirm whether vintage is
  modelled as a WooCommerce attribute or only in the title — realwines' parser
  reads a `vintage` attribute; if Vinazion doesn't set one, fall back to parsing
  the title.

## Suggested scraper shape
- Copy `realwines/` → `vinazion/`. Change `BASE_URL` to
  `https://www.vinazion.ch/wp-json/wc/store/v1/products` and `CACHE_DIR` to
  `vinazion/cache`. Verify the `parse.py` attribute names against a real product
  (vintage/region term naming may differ from realwines).

## Open questions / future work
- Does Vinazion expose vintage/region/grape as Store API `attributes`? If not,
  parse from `name`. Confirm against a couple of live records during build.
- 14 physical pickup locations mentioned on the site — irrelevant to inventory,
  ignore.
