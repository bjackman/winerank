# Vergani (vergani.ch) scraping notes

Recon for a `vergani/` scraper analogous to `realwines/`. Captured 2026-06-04.

## TL;DR
- Platform: **Shopify**. Data source: the standard Shopify
  `/products.json` endpoint — clean public JSON, no auth.
- Best target: `https://www.vergani.ch/products.json?limit=250&page=N`.
  Page-based; stop when a page returns fewer than 250.
- **Total products: 756** across **4 pages** at limit=250. Fast crawl.
- Italian-wine specialist; catalogue also includes gift sets / wooden cases —
  filter by `product_type`.

## Platform / fingerprint
- Homepage body: `cdn.shopify`, `Shopify.shop` markers.
- `/products.json` returns HTTP 200 with `{"products":[...]}`.

## URLs
- Catalogue: `/products.json?limit=250&page=N` (`limit` max 250).
- Alternative: `/collections/all/products.json` (same shape, scoped to a
  collection). Per-product: `/products/<handle>.json`.

## Pagination & scale
- Page-based: `?page=1,2,3,...` with `limit=250`. No total header — stop when a
  page returns < 250 products. Measured: 756 products → pages 1–3 full, page 4
  partial (page 5 would be empty).

## Extracting the data
- Each page is `{"products":[ {product}, ... ]}`. JSON, no reconstruction needed.

## Product record schema
Top-level product keys: `id`, `title`, `handle`, `body_html`, `vendor`,
`product_type`, `tags[]`, `variants[]`, `images[]`, `options[]`,
`published_at`/`created_at`/`updated_at`.

- **Producer:** `vendor` (e.g. "Vergani" for own/gift items; real producer for
  wines).
- **Price:** `variants[i].price` — **decimal string** (e.g. `"12.00"`),
  currency not in this payload (shop is CHF). `compare_at_price` for was-price.
- **SKU / stock:** `variants[i].sku`, `variants[i].available` (bool).
- **Bottle size:** encoded in `variants[i].title` / `option1` (e.g.
  `"N/A / 75 cl"`). `grams` was 0 in sample (unreliable).
- **Vintage / region / grape:** NOT first-class fields. Live in `title`, `tags`,
  or Shopify **metafields** — and **metafields are not returned by
  products.json**. Parse from `title`/`tags`, or fetch the product page / use a
  metafield-aware endpoint if we need them reliably.
- **Wine filter:** `product_type` seen so far: `Weisswein`, `Roséwein`,
  `Schaumwein` (+ `Geschenkset`, `Holzkisten` = non-wine). Filter out gift sets /
  wooden cases; keep the *wein* types. (Full type list TBD across all 4 pages.)

## Suggested scraper shape
- `vergani/fetch.py`: page `/products.json?limit=250&page=N`, cache each page as
  `vergani/cache/page_NN.json`, stop on a short page. Firefox UA, 1.5 s delay.
- `vergani/parse.py`: flatten products → records; one record per variant (or per
  product, taking variant 0). Map vendor→producer, variant.price→price (parse to
  minor units or keep decimal — decide consistently with realwines),
  variant.sku, availability; regex vintage out of `title`.

## Open questions / future work
- Decide whether per-variant or per-product granularity (multi-size wines have
  multiple variants).
- If vintage/region/grape matter, evaluate scraping the PDP HTML or a
  metafield-exposing endpoint — products.json alone won't give them.
- Enumerate full `product_type` set to finalise the wine filter.
