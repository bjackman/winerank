# Baur au Lac Vins (bauraulacvins.ch) scraping notes

Recon captured live 2026-06-06. Scraper implemented at `scraping/bauraulac/`.

## TL;DR
- Platform: **Java web app** (JSESSIONID cookie, `Path=/; Secure`). Not Magento
  (the "Mage" fingerprint is a false positive from `image-`/`mage-` substrings).
- The visible price is loaded by JS into a `.product-price` placeholder, BUT the
  PDP static HTML already carries the product data in `data-shop-product*`
  attributes — no need to render JS or hit an XHR endpoint.
- **Approach: sitemap-driven.** Enumerate products from the gzipped de sitemap,
  fetch each PDP, read the data attributes + URL facets.

## URLs
- Homepage `/` (200) sets `JSESSIONID`; `/de/` is a near-empty landing.
- robots.txt → `Sitemap: …/myinterfaces/cms/googlesitemap-overview.xml`.
- `/sitemap.xml` is a **sitemap index** → per-locale gzipped sitemaps:
  `…/standard/sitemap/www.bauraulacvins.ch-de-1.xml.gz` (+ `-fr-`, `-en-`).
  NB: the server may serve the `.gz` already decompressed — gunzip only when the
  bytes are gzip-framed (`\x1f\x8b`).
- de sitemap has 3241 locs: **2342 products under `/de/p/...`**, 402 under
  `/de/r/...` (region/producer pages), the rest CMS `…_content---…html` pages.
- Product URL shape:
  `/de/p/<type>/<country>/<region>/[<subregion>/]<name>-<vintage>-<number>.html`
  e.g. `/de/p/rotwein/frankreich/bordeaux/moulis/chateau-chasse-spleen-2022-22251722.html`

## Extracting the data
- Static-HTML `data-shop-*` attributes on the PDP:
  `data-shop-productname`, `data-shop-productnumber` (= sku/id, also
  `data-product-id`), `data-shop-productprice` (e.g. `198.0`),
  `data-shop-productcurrency` (`CHF`), `data-shop-pricetype` (`Endkunden`).
- Stock: presence of `productdetail__stock-icon--available` (vs `--…`).
- From the URL path: `wine_type` (rotwein/weisswein/spirituosen/…), `country`,
  `region`. `vintage` = year in the name/slug.
- **Gotchas:** no structured producer field (`producer` left null). A few PDPs
  (gift sets etc.) lack `data-shop-productname` and are skipped. ~2342 products
  → a full fetch is ~2300 requests; cache-first + `--max-products` for sampling.
