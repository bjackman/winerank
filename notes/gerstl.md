# Gerstl Weinselektionen (gerstl.ch) scraping notes

Recon captured live 2026-06-06. Scraper implemented at `scraping/gerstl/`.

## TL;DR
- Platform: **Angular SPA** (Google Frontend hosting). Pages are JS-rendered
  (`_ngcontent-*`, `window.__jsaction…`), no JSON-LD — BUT each PDP embeds an
  Angular **TransferState** blob, `<script id="ng-state" type="application/json">`,
  in the static HTML with fully structured product data. No browser needed.
- ~7546 products.

## URLs
- robots.txt → `https://www.gerstl.ch/sitemap.xml` (a sitemap index) →
  `https://cdn.gerstl.ch/seo/sitemap-{categories,products,pages}.xml`.
- `sitemap-products.xml`: ~7546 product URLs, each ending `/p`
  (e.g. `https://www.gerstl.ch/2024-chateau-haut-bailly-…-fra-264841-2024-f6/p`).

## Extracting the data
- Parse the `ng-state` JSON. It's keyed by numeric Angular TransferState cache
  ids; product objects live at `…/<cacheId>/data[<n>]`. A PDP's blob may also
  carry related products, so pick the product whose `slug` matches the page.
- A product object is richly structured (more than any other merchant):
  - `title1` (estate), `title3` (cuvée) → name; `vendor.{key,name}` → producer
  - `year.{key,name}` → vintage; `country`/`region`/`appellation`/`wineType`
    (and `color`) are all `{key,name}` dicts
  - `price` (number, CHF; also `prices`/`pricesV2` for ws/hr/ek tiers)
  - `grapes` (list of strings), `size.{value,name}` (value in cl),
    `sku`, `stock.stock` (int → in_stock), `alcohol`, `classification`
- Coverage on a 20-sample: 100% name/wineType/price/stock/sku/size, 95%
  producer/vintage/country/region, 70% appellation, 45% grapes (not all wines
  list grapes). ~7546 products → full fetch is ~7500 requests; `--max-products`
  for sampling, cache-first so resumable.
