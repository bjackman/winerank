# Merchant recon playbook

How to recon a wine merchant's website so we can build a `scraping/<merchant>/`
scraper that mirrors `scraping/realwines/` (cache-first `fetch.py` + `parse.py`).
Read this, then produce `notes/<merchant>.md` using the **template at the
bottom**. (All scraper code lives under `scraping/`; recon notes stay in
`notes/`.)

The goal of recon is **not** to build the scraper — it's to answer: *where does
the catalogue data live, how do we page through all of it, and what does one
product record look like?* Leave the code for the build stage.

## Fanning out (for a future session — no prior chat context needed)

Three merchants are already done (flaschenpost, vinazion, vergani, advanvinum —
see their `notes/*.md`). The remaining `todo` merchants in the table below can be
reconned in parallel, **one agent per merchant**. Suggested per-agent prompt:

> You are doing website recon for a wine merchant so we can later build a
> scraper. Read `notes/RECON.md` in full, then read one finished example that
> matches the merchant's platform (`notes/vergani.md` for Shopify,
> `notes/vinazion.md` for WooCommerce, `notes/flaschenpost.md` for a custom JS
> site). Recon **<MERCHANT> (<domain>)**, whose first-pass platform is
> **<PLATFORM>** (verify it). Probe with `curl` using the conventions in
> RECON.md. **Do not write any scraper code.** Produce `notes/<merchant>.md`
> using the template at the bottom of RECON.md, and update that merchant's line
> in `README.md`. Report the data source, pagination, total product count, and
> one product's field schema.

Batch order if doing a few at a time: the known-platform ones first (Bindella,
more-than-wine, Arvi, Smith & Smith, Mövenpick, Landolt), then the
reverse-engineering ones (Baur au Lac, Zweifel, Gerstl, REB, Passeur de Vin).
Agents don't depend on each other — pure fan-out.

## House conventions (apply to every scraper)

- **Pattern:** one directory per merchant under `scraping/` with `fetch.py`
  (cache-first; one raw file per page under `scraping/<merchant>/cache/`) and
  `parse.py` (cache → clean wine records as a JSON array). Copy
  `scraping/realwines/fetch.py` + `scraping/realwines/parse.py` as the skeleton.
  **Exception:** merchants on the *same* platform can share one parametrised
  scraper instead — see `scraping/shopify/` (`--shop`/`--name`), used for Vergani
  and AdvanVinum, with per-merchant cache under `scraping/shopify/cache/<name>/`.
- **Output record:** parsers emit a JSON array of records sharing a common core —
  `merchant, name, producer, vintage (int|null), currency, price (integer minor
  units / rappen), in_stock, sku, url` — plus any extras the source gives cheaply
  (e.g. Vinazion adds `country, region, grapes`). Convert decimal prices to minor
  units to stay consistent (Shopify `"12.00"` → `1200`).
- **Python:** there is no `python3` on PATH. Run everything as
  `nix develop -c python ...` (Python 3.13 from the devShell).
- **Politeness:** Firefox desktop User-Agent (string below), ~1.5 s delay
  **between live requests only** (cached pages = no delay). Never hammer.
- **Locale:** prefer Swiss German (`de-CH` / `de`) when a site is multilingual,
  to match realwines.
- **Money:** record the currency and watch units — some APIs give integer cents
  (e.g. Flaschenpost `3495` = CHF 34.95), some give decimal strings (Shopify
  `"34.95"`). Note which in the record.
- **Notes:** every recon writes `notes/<merchant>.md` in the template below.
  Check off / annotate the merchant's line in `README.md`.

```
User-Agent: Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0
```

## Recon procedure

1. **Confirm the platform.** It's pre-filled per merchant in the README/table
   below from a first-pass fingerprint — verify it, don't trust it blindly.
   Quick tells: response headers (`x-powered-by`, `server`, cookie names),
   `<meta name="generator">`, body markers (`/_next/`, `wp-json`, `cdn.shopify`,
   `Magento_`, `prestashop`, …). Note: `mage/` substring matches inside `image/`
   — false positive, ignore it.

2. **Use the known-platform shortcut if there is one** (table next section). A
   standard catalogue API beats HTML scraping every time — less brittle, has
   price/stock cleanly. If the shortcut works, recon is basically done: confirm
   pagination + total count + one product's schema and write it up.

3. **No known API? Find the data source.** In order of preference:
   - A JSON/REST/GraphQL endpoint the site itself calls (watch for `/api/...`,
     `*.json`, GraphQL). Check `robots.txt` + `sitemap.xml` for structure and
     scale.
   - Data embedded in the HTML: Next.js RSC stream
     (`self.__next_f.push([1,"..."])` — reconstruct by JSON-decoding +
     concatenating the chunks), `__NEXT_DATA__`, `window.__INITIAL_STATE__`,
     Nuxt `__NUXT__`, or JSON-LD (`application/ld+json`).
   - Last resort: parse the rendered product-card HTML directly.

4. **Figure out pagination + total count.** Page param? Cursor? Infinite scroll
   calling an API? How many products total (so we know the crawl size)? Are
   pages disjoint?

5. **Watch for bot-protection asymmetry.** Some sites serve listing/category
   pages fine but 404/403 product detail pages to non-browser clients (seen on
   Flaschenpost). Prefer whichever surface returns 200 to `curl` and already
   carries the data.

6. **Capture one full product record** and map the useful fields (name,
   producer, vintage, region/country, grape, bottle size, alcohol, price,
   stock/availability, url, image). Note which fields are missing and would need
   a PDP fetch or metafields.

7. **Note non-wine items.** Several shops also sell olive oil, gift sets,
   glassware, etc. Record how to filter to wine (category / product_type / tag).

## Known-platform shortcuts

| Platform | Catalogue endpoint | Pagination | Notes |
|---|---|---|---|
| **WooCommerce** | `/wp-json/wc/store/v1/products?per_page=100&page=N` | `X-WP-Total` / `X-WP-TotalPages` headers | Exactly the `realwines/` approach — reuse its code. |
| **Shopify** | `/products.json?limit=250&page=N` | page-based; stop when a page returns <250 | Price in `variants[i].price` (decimal string). Vintage/region usually only in `title`/`tags`/metafields — **metafields are NOT in products.json** (need PDP or `?fields`). Also `/collections/all/products.json`. |
| **BigCommerce** | Stencil `/api/storefront/...` or GraphQL Storefront API; also category pages | varies | Often needs a token for GraphQL; category page scrape is the fallback. |
| **PrestaShop** | Webservice API usually disabled; scrape category pages (predictable `?p=N` paging) | `?p=N` | Product data in HTML / JSON-LD. |
| **Magento** | `/rest/V1/products` often auth-gated; use category pages + layered nav | `?p=N` | Data in HTML; sometimes a GraphQL endpoint at `/graphql`. |
| **nopCommerce (.NET)** | no standard public API; scrape category pages | `?pagenumber=N` | Data in HTML. |
| **Next.js / custom headless** | no standard — reverse-engineer (see step 3) | varies | e.g. Flaschenpost: data in RSC stream, `?page=N`. |
| **Custom (Laravel/Java/Express)** | no standard — reverse-engineer | varies | Inspect XHR/JSON the page loads. |

## This project's merchants (first-pass platform fingerprint, 2026-06-04)

Verify before trusting. ✅ = recon written.

| Merchant | Platform | Status |
|---|---|---|
| flaschenpost.ch | Next.js/RSC (custom) | recon ✅ `notes/flaschenpost.md` |
| vinazion.ch | WooCommerce | **built** `scraping/vinazion/` |
| vergani.ch | Shopify | **built** `scraping/shopify/` |
| advanvinum-wein.ch | Shopify | **built** `scraping/shopify/` |
| bindella.ch | BigCommerce | todo |
| more-than-wine.com | PrestaShop | todo |
| arvi.ch | nopCommerce (.NET) | todo |
| smithandsmith.ch | ASP.NET/IIS (likely nopCommerce) | todo |
| moevenpick-wein.com | Magento | todo |
| landolt-weine.ch | Shopware likely (`session-` cookie + Vue) | todo |
| bauraulacvins.ch | Java app (JSESSIONID) — Intershop/custom | todo |
| zweifel1898.ch | Java app (JSESSIONID) | todo |
| gerstl.ch | Custom Node/Express headless | todo |
| rebwein.ch | Custom Laravel | todo |
| passeurdevin.ch | Unknown (no markers) | todo |

---

## Notes template — copy this into `notes/<merchant>.md`

```markdown
# <Merchant> (<domain>) scraping notes

Recon for a `<merchant>/` scraper analogous to `realwines/`. Captured <date>.

## TL;DR
- Platform: <...>. Data source: <endpoint / embedded JSON / HTML>.
- Best target: <url pattern>. Pagination: <how>. Total products: <N>.
- Gotchas: <bot protection, non-wine items, locale, price units, ...>.

## Platform / fingerprint
<headers, generator, body markers that confirm the platform>

## URLs
- robots.txt / sitemap highlights
- Catalogue endpoint or listing URL + how to page it

## Pagination & scale
- page param / cursor, page size, total count, disjoint?

## Extracting the data
- exact steps to get from a fetched page to product objects

## Product record schema
- the useful fields and where they live; price units/currency; missing fields
- how to filter wine vs non-wine

## Suggested scraper shape
- fetch.py / parse.py specifics for this merchant

## Open questions / future work
```
