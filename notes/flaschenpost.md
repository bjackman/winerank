# Flaschenpost (flaschenpost.ch) scraping notes

Recon notes for building a `flaschenpost/` scraper analogous to `realwines/`.
Captured 2026-06-04.

## TL;DR

- **Not WooCommerce.** It's a **Next.js (app router, RSC) site behind Cloudflare**.
  No clean public JSON API like realwines' `/wp-json/wc/store/v1/products`.
- The real product data is **server-rendered into the page's RSC payload** (the
  `self.__next_f.push([1,"..."])` script chunks). Reconstruct that stream and the
  product objects — including price and stock — are right there as JSON.
- **Best scraping target: category listing pages**, e.g. `/wein` ("Alle Weine",
  the catch-all). Paginate with `?page=N`. ~12 products SSR'd per page.
- **Avoid product detail pages (PDPs):** the bare slug URLs from the sitemap
  return **404 to non-browser clients** (bot protection). The listing pages
  (`/wein`, `/rotwein`, …) return 200 fine and already carry the full per-product
  JSON, so we never need to hit a PDP.

## Site platform / fingerprint

- `server: cloudflare`, `cf-ray` present. Homepage returns 200 with
  `x-middleware-rewrite: /de` and sets cookies `fp-locale=de`, `FP-AID=<uuid>`.
- Next.js: `/_next/static/...` assets, `self.__next_f.push(...)` RSC stream,
  Turbopack chunks. 40 JS chunks on a listing page.
- Search/grid backend is **GraphQL** (`GraphQl`/`graphql` + `/api/v2` +
  `application_id` strings appear in the JS bundles), but it's called
  **server-side** from RSC — not exposed as a directly-callable client endpoint,
  and likely auth-gated. Algolia-style field names (`objectID`) appear in the
  data but no Algolia app id / search key is exposed in the HTML. Not worth
  chasing for v0; the SSR'd HTML already has everything.
- Internal image CDN: `https://d2uqzpf6d2kb73.cloudfront.net/<base64-blob>`
  (bucket `spine-product-images-prod`; the path is a base64-encoded JSON of
  `{key, bucket, edits:{resize,trim}}` — an imgproxy/thumbor-style transform).

## URLs

- Homepage: `https://www.flaschenpost.ch/` (locale-rewritten to `/de`).
- robots.txt: only disallows `/checkout`, `/cart$`, `/account`, `/404$`.
  Sitemap entry → `https://www.flaschenpost.ch/sitemaps/sitemap_index.xml`.
- Sitemaps:
  - `product_sitemap_{de,it,fr,en}.xml` → each splits into `-1`, `-2`, `-3`.
    German: **53,223 product URLs** (20000 + 20000 + 13223).
  - `page_sitemap_{de,...}.xml` → category / landing pages.
- Product (PDP) slug format: `<product-slug>_<producer-slug>`, e.g.
  `henriet-brut-grand-cru_henriet`. **These 404 for curl** even though they're in
  the sitemap. The `/de/<slug>` variant 307-redirects back to the bare slug
  (which 404s). The live PDP actually needs query params
  `?_size=<ml>&_vintage=<year>` (seen in the product `url` field), but it's still
  gated — don't rely on PDPs.
- Category pages (all return 200): `/wein` (all wines, **20,609** total),
  `/rotwein`, `/fondue`, etc. These are the scrape target.

## Pagination (the listing pages)

- `GET /wein?page=N`. **12 products per page**, SSR'd into the RSC payload.
- Pages are disjoint (verified page1 vs page2 SKUs: zero overlap).
- Page size is **fixed at 12** — `pageSize`, `size`, `_size`, `hitsPerPage`,
  `limit`, `perPage` query params were all ignored (still 12).
- Total count is embedded in the payload as `"total":20609` (for `/wein`).
  → page count = ceil(20609 / 12) ≈ **1718 pages** for the full catalogue.
- Implication: a full crawl of `/wein` is ~1718 polite requests. Cache
  aggressively (one HTML file per page, like realwines). RSC-only fetches (send
  `RSC: 1` header to get just the payload, not full HTML) could cut bytes later;
  not done yet.

## Extracting the data

In each listing page's HTML:

1. Collect the RSC chunks:
   `re.findall(r'self\.__next_f\.push\(\[1,(".*?")\]\)', html, re.S)`.
2. Reconstruct the stream: JSON-decode each captured quoted string and
   concatenate → one big string `payload`.
3. Product objects appear as `"product":{ ... }` (one per grid card). Find each
   `"product":{` and brace-match to the closing `}`, then `json.loads`. Keep
   objects that contain `objectID`/`sku`.
   - Count check: `payload.count('"isMasterVariant":true')` == products on page.

JSON-LD (`application/ld+json`) blocks are **empty** on these pages — don't rely
on them. Most `price`/`producer`/`vintage` string hits in the payload are i18n UI
labels; the real data is inside the product objects.

## Product object schema (the useful bits)

Top-level fields on each `product` object:

- `sku` (string, e.g. `"1150859"`), `productID` (uuid), `objectID`
  (`<uuid>.<variantIndex>`).
- `name` — `{en, de-CH, it-CH, fr-CH}` dict (also a flattened `name` string and
  `attributes.displayName`).
- `attributes` (nested), notable keys:
  - `producer: {key, label}` — label is the producer display name.
  - `vintage` / `year` (int; e.g. 2019).
  - `wineType: {key:"red"|..., label:{de-CH:"Rotwein",...}}`.
  - `mainGrape` (string), `grape` (**stringified JSON** array of `{id,name}`).
  - `alcohol` (float % vol), `bottleSize` (int ml, e.g. 750),
    `bottleSizeFormatted` ("75 cl"), `packagingUnit`.
  - booleans: `isBio`, `isBiodynamic`, `isVegan`, `isOwnWine`, `isNew`,
    `isDiscounted`, `isClearance`, `isBlend`, `isCuve`, `isAwarded`,
    `isTopSeller`, `isAlcoholFree`, `isPublished`.
  - `foodPairing`, `tastingNote`, `vinification`, `description`, `subtitle` —
    all `{en, de-CH, it-CH, fr-CH}` dicts.
  - `gtin` (often ""), `supplierName`, `supplierId`, `margin` (internal! e.g.
    61.8 — leaked business data), `criticScores`/`criticLevels`,
    `expertRatings`/`tags` (stringified JSON).
- `country`, `region`, `subregion` — each `{en, de-CH, it-CH, fr-CH}` dict.
- `price` (amounts are **integer cents** of CHF — divide by 100):
  ```json
  "price": {
    "initialPrice":  {"amount":3495,"currency":"CHF","validUntil":null,"isActive":true},
    "discountPrice": {"amount":2795,"currency":"CHF","validFrom":"...","validUntil":"...","isActive":true},
    "literPrice":    {"amount":372.67,"currency":"CHF","isActive":true}
  }
  ```
  (3495 ⇒ CHF 34.95. `discountPrice` only present/active when on sale.)
- `stock`: `{stockLevel:660, steps:{min,max,...}, packageSize, isRestockable, isAvailable}`.
- `images`: list of `{url, label}` with labels like `MAIN`, `BACKGROUND`
  (cloudfront transform URLs, see above).
- `url`: PDP path **with** required query params, e.g.
  `cair-..._dominio-de-cair?_size=750&_vintage=2019`.
- `totalActiveVariants`, `deliveryDate`, `priceCluster`, `tags`, `campaigns`.

Locale: pick `de-CH` from the multilingual dicts for a German catalogue (matches
realwines being CH/German).

## Suggested scraper shape (mirror realwines/)

- `flaschenpost/fetch.py` — cache-first. Probe `/wein?page=1`, read `"total"`,
  compute page count, fetch `/wein?page=N` for all N, cache raw HTML as
  `flaschenpost/cache/page_NNNN.html`. Polite delay (~1.5s) between live hits.
  Firefox UA. `--refresh` to bypass cache; add `--max-pages` for test runs.
- `flaschenpost/parse.py` — load cached HTML, reconstruct RSC payload, brace-match
  product objects, map to clean wine records (de-CH), emit JSON array. Convert
  price cents→CHF.

### Open questions / future work

- Confirm `/wein` (20,609) vs sitemap (53,223) gap — sitemap likely counts all
  vintages/sizes/variants and non-wine items (spirits, accessories); `/wein` is
  the wine category. Decide whether to also crawl other categories or just `/wein`.
- Try `RSC: 1` header fetches to download only the payload (smaller than full
  HTML) for the ~1718-page crawl.
- Investigate the server-side GraphQL `/api/v2` endpoint — if callable with a
  static `application_id`, it'd be far cheaper than 1718 HTML fetches.
