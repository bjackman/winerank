# Mövenpick Wein (moevenpick-wein.com) scraping notes

Recon captured live 2026-06-06. Scraper implemented at `scraping/moevenpick/`.

## TL;DR
- Platform: **Magento** (Cloudflare in front). de-CH locale under `/de/`.
- No clean server-rendered "all wines" listing: the SEO category pages
  (`/de/frankreich.html`, `/de/rotweine.html`, …) are **JS-rendered** (no product
  cards in static HTML). Only the raw `/de/catalog/category/view/id/<N>` URLs
  render product cards server-side, but categories overlap and ids must be
  discovered — not a clean enumerator.
- **Chosen approach: sitemap-driven.** The XML sitemap lists every product, and
  each product-detail page (PDP) embeds a rich JSON-LD `Product` block.
  fetch.py: sitemap → product URLs → cache each PDP. parse.py: JSON-LD `Product`.

## URLs
- Homepage: `https://www.moevenpick-wein.com/de/`
- robots.txt: `https://www.moevenpick-wein.com/robots.txt` (200) → points to
  `https://www.moevenpick-wein.com/media/sitemap/sitemap_ch_de.xml`
  (also `…_ch_fr.xml`, `…_ch_it.xml` for other locales).
- Sitemap: ~7879 `<loc>` entries; **6160 end in `.html`** (the rest are
  `/de/catalog/category/view/id/N` and CMS pages). Of the 6160, ~4300 are
  actual wine products (slug = `<vintage>-<name>-<region>-<producer>`); the
  first ~1855 are category/region landing pages that share the .html suffix.
- Product URL example:
  `…/de/2021-marques-de-murrieta-reserva-rioja-doca.html`

## Pagination & scale
- No pagination in the scraper — enumeration is the sitemap. ~4300 wines.
- A full fetch is ~6160 requests (incl. ~1855 non-product .html pages that the
  parser drops). Use `--max-products N` to sample. Cache-first, so resumable.

## Extracting the data
- Each PDP has 4–5 JSON-LD blocks; the `@type: "Product"` one carries
  `name`, `sku`, `description`, and `offers` — **one Offer per bottle format**,
  each with `sku`, `price`, `priceCurrency` (CHF), `availability` (In/OutOfStock).
- Parser emits one record per product: headline `price` = cheapest in-stock
  offer (else cheapest overall), `in_stock` = any offer in stock, per-format
  offers under `variants`. `vintage` from the leading year in the name.
- **Gotcha:** no structured producer/brand in the JSON-LD, so `producer` is
  null. Recovering it would need heuristics on the PDP body or the name.
- **Gotcha:** URL slug alone can't distinguish products from categories
  (`2020-angebot.html` is year-prefixed but a promo category). Filtering by the
  presence of a `Product` JSON-LD block is the only reliable test — the parser
  does this, so non-products are dropped automatically.
