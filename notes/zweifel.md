# Zweifel 1898 (zweifel1898.ch) scraping notes

Recon captured live 2026-06-06. Scraper implemented at `scraping/zweifel/`.

## TL;DR
- Platform: **same Java web-app as Baur au Lac Vins** (JSESSIONID, Apache,
  `/sitemap.xml` index → gzipped per-locale sitemap, products under `/de/p/`).
  Fetch strategy is identical (see `scraping/zweifel/fetch.py`); only the PDP
  parsing differs.
- ~781 products (de sitemap: 1377 locs, 781 under `/de/p/`).

## URLs
- robots.txt → `…/myinterfaces/cms/googlesitemap-overview.xml`; `/sitemap.xml`
  is an index → `…/standard/sitemap/www.zweifel1898.ch-de-1.xml.gz`
  (may be served already-decompressed; gunzip only when gzip-framed).
- Product URL shape (note the **producer** segment):
  `/de/p/<type>/<country>/<region>/<producer>/<name>-<number>.html`
  e.g. `/de/p/rotwein/italien/venetien/tinazzi/amarone-della-valpolicella-2004216.html`
  → type=rotwein, country=italien, region=venetien, producer=tinazzi.

## Extracting the data
- PDPs use **schema.org microdata** in static HTML (NOT the `data-shop-*` attrs
  that Baur au Lac uses):
  - `<meta itemprop="price" content="37.0">`, `itemprop="currency" content="CHF">`
  - `<meta itemprop="availability" content=" in_stock ">` → in_stock
  - name in `<h1>`, bottle size in `<span class="product-size">75cl</span>`
  - sku from `data-shop-productkeys="<number>_0_0"` (or the URL trailing number)
- **vintage** is in its own `<p class="product-subtitle">2021</p>` element (a
  sibling of the producer subtitle); the name/slug usually omit the year. Parser
  reads the subtitle first, falling back to a year in the name/URL.
- producer/country/region/type come from the URL path segments.
- ~781 products → full fetch is ~780 requests; cache-first + `--max-products`.
