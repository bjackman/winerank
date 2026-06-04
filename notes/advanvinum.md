# AdvanVinum (advanvinum-wein.ch) scraping notes

Recon for an `advanvinum/` scraper analogous to `realwines/`. Captured 2026-06-04.

## TL;DR
- Platform: **Shopify**. Data source: the standard Shopify `/products.json`
  endpoint — clean public JSON, no auth.
- Best target: `https://advanvinum-wein.ch/products.json?limit=250&page=N`.
  Page-based; stop when a page returns fewer than 250.
- **Total products: 270** across **2 pages** at limit=250. Tiny crawl.
- **Sells more than wine** (olive oil, food / `Lebensmittel`, etc.) — must filter
  to wine by `product_type` / `tags`.

## Platform / fingerprint
- Homepage body: `cdn.shopify`, `Shopify.shop` markers.
- `/products.json` returns HTTP 200 with `{"products":[...]}`.
- Note: **no `www.`** — host is `https://advanvinum-wein.ch`.

## URLs
- Catalogue: `/products.json?limit=250&page=N`.
- Alternatives: `/collections/all/products.json`, `/products/<handle>.json`.

## Pagination & scale
- Page-based `?page=N`, `limit=250`. 270 products → page 1 full (250), page 2
  partial (20), page 3 empty. No total header; stop on short page.

## Extracting the data
- `{"products":[ {product}, ... ]}` per page. Plain JSON.

## Product record schema
Same Shopify schema as Vergani (see `notes/vergani.md` for the field rundown):
`id, title, handle, body_html, vendor, product_type, tags[], variants[]
(price decimal string, sku, available, compare_at_price, option1=size),
images[]`.

- **Non-wine items present:** sample included "Olio extra vergine di oliva BIO"
  (vendor "Frantoio Priorelli", tags `Lebensmittel`/`olivenöl`/`öl`,
  `product_type` empty). **Filter to wine** — enumerate `product_type` and `tags`
  across both pages to define the keep-set (wine types vs food/oil/accessories).
- **Vintage / region / grape:** as with all Shopify shops, not first-class;
  parse from `title`/`tags` (metafields absent from products.json).

## Suggested scraper shape
- `advanvinum/fetch.py`: page `/products.json?limit=250&page=N` (host without
  `www.`), cache `advanvinum/cache/page_NN.json`, stop on short page.
- `advanvinum/parse.py`: same as Vergani's parser; add a wine filter
  (drop `Lebensmittel`/oil/accessory items).
- Could share a common `shopify_products` helper with `vergani/` since the
  fetch+parse logic is identical bar the host and wine filter.

## Open questions / future work
- Finalise the wine vs non-wine filter (product_type + tag allowlist).
- Same metafield limitation as Vergani for vintage/region/grape.
- Worth a shared Shopify scraper module (vergani + advanvinum, and any future
  Shopify merchants).
