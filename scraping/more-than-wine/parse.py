#!/usr/bin/env python3
"""
more-than-wine/parse.py — parse cached HTML pages into clean wine records.

Reads PrestaShop category-page HTML from the cache (or stdin) and writes a
clean JSON array to stdout (or --output file).

Data extraction strategy
------------------------
1. **JSON-LD first** — PrestaShop often embeds ``<script
   type="application/ld+json">`` blocks in category pages, each describing a
   product with ``@type: "Product"``.  Name, SKU, URL, price, currency and
   availability are all present without needing a PDP fetch.

2. **HTML card fallback** — If no JSON-LD ``Product`` blocks are found on a
   page, we parse the PrestaShop product miniature cards directly:
   ``<article class="product-miniature" data-id-product="N">`` with nested
   title link and ``.price`` span.

Post-processing (both paths):
- ``vintage``: regex for a four-digit year (1900–2099) in the product name.
- ``producer``: regex heuristic (first token(s) before the vintage / comma /
  dash).  PrestaShop products rarely expose producer as a structured field on
  listing pages; a PDP fetch would give cleaner data.
- ``price``: decimal string → integer rappen (× 100, rounded).

Usage
-----
    nix develop -c python scraping/more-than-wine/parse.py --from-cache
    nix develop -c python scraping/more-than-wine/parse.py --from-cache \\
        --output scraping/more-than-wine/wines.json

NOTE
----
The live site (www.more-than-wine.com) was unreachable during implementation.
HTML patterns below follow standard PrestaShop conventions but have NOT been
verified against live HTML.  If parsing yields 0 products after a real fetch,
inspect a cached page and adjust the regex patterns in this file.
"""

import argparse
import html as html_module
import json
import re
import sys
from pathlib import Path

CACHE_DIR = Path.cwd() / "scraping" / "more-than-wine" / "cache"

MERCHANT = "more-than-wine"


# ---------------------------------------------------------------------------
# Cache loading
# ---------------------------------------------------------------------------

def load_from_cache() -> list[str]:
    """Return a list of raw HTML strings from all cached page files."""
    pages = sorted(CACHE_DIR.glob("page_*.html"))
    if not pages:
        print(
            "No cache files found. Run fetch.py first.", file=sys.stderr
        )
        sys.exit(1)
    print(
        f"Loading {len(pages)} cached page(s) from {CACHE_DIR}",
        file=sys.stderr,
    )
    return [p.read_text(encoding="utf-8") for p in pages]


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def unescape(text: str) -> str:
    """Decode HTML entities and strip surrounding whitespace."""
    return html_module.unescape(text).strip()


def strip_tags(text: str) -> str:
    """Remove HTML tags and decode entities."""
    return unescape(re.sub(r"<[^>]+>", "", text or ""))


def parse_price(price_str: str | None) -> int | None:
    """
    Convert a decimal price string to integer minor units (rappen).

    Handles formats like "24.90", "24,90", "CHF 24.90", "24.9".
    Returns None if the string cannot be parsed.
    """
    if not price_str:
        return None
    # Strip currency symbols / codes and whitespace
    cleaned = re.sub(r"[^\d.,]", "", price_str.strip())
    # Normalise comma decimal separators
    cleaned = cleaned.replace(",", ".")
    # If there are multiple dots keep only the last (thousands separator case)
    parts = cleaned.split(".")
    if len(parts) > 2:
        cleaned = "".join(parts[:-1]) + "." + parts[-1]
    try:
        return round(float(cleaned) * 100)
    except ValueError:
        return None


def parse_vintage(name: str) -> int | None:
    """Extract a four-digit vintage year from a product name."""
    m = re.search(r"\b(19|20)\d{2}\b", name)
    return int(m.group(0)) if m else None


def parse_producer(name: str) -> str | None:
    """
    Heuristic: extract the producer name from a wine product title.

    Wine titles on PrestaShop stores typically follow:
      "Producer Name WineName Vintage" or "Producer, WineName Vintage"

    Strategy:
    - Remove the vintage year, then take everything before the first
      comma, dash, or long descriptor word as the producer.
    - Returns None if the heuristic yields a blank string.
    """
    # Remove year
    title = re.sub(r"\b(19|20)\d{2}\b", "", name).strip()
    # Take the first segment before a comma, " - ", or " – "
    m = re.split(r",\s*|\s+[-–]\s+", title, maxsplit=1)
    candidate = m[0].strip() if m else title.strip()
    return candidate if candidate else None


def availability_to_bool(avail: str | None) -> bool | None:
    """Convert schema.org availability URL to a boolean."""
    if not avail:
        return None
    return "InStock" in avail or "InStoreOnly" in avail


# ---------------------------------------------------------------------------
# JSON-LD extraction
# ---------------------------------------------------------------------------

# Matches both quoted and unquoted type values (application/ld+json)
_JSONLD_RE = re.compile(
    r'<script[^>]+type=["\']application/ld\+json["\'][^>]*>(.*?)</script>',
    re.S | re.I,
)


def extract_jsonld_products(page_html: str) -> list[dict]:
    """
    Extract all JSON-LD Product records from a single HTML page.

    Returns a list of raw dicts (one per product found in ld+json blocks).
    """
    products = []
    for raw_block in _JSONLD_RE.findall(page_html):
        try:
            obj = json.loads(raw_block)
        except json.JSONDecodeError:
            continue

        # Handle both a single object and an array at the top level
        if isinstance(obj, list):
            items = obj
        else:
            items = [obj]

        for item in items:
            if not isinstance(item, dict):
                continue
            # Accept @type: "Product" (plain or namespaced)
            dtype = item.get("@type", "")
            if isinstance(dtype, list):
                dtype = " ".join(dtype)
            if "Product" not in dtype:
                continue
            products.append(item)

    return products


def jsonld_to_record(item: dict) -> dict:
    """Map a JSON-LD Product dict to our standard wine record."""
    name = unescape(item.get("name", ""))
    sku = item.get("sku") or item.get("productID") or None

    offers = item.get("offers", {}) or {}
    # offers can be a list (AggregateOffer) or a single Offer dict
    if isinstance(offers, list):
        offers = offers[0] if offers else {}

    price_raw = offers.get("price") or offers.get("lowPrice")
    currency = (
        offers.get("priceCurrency")
        or item.get("priceCurrency")
        or "CHF"
    )
    avail = offers.get("availability")

    # URL: prefer item-level, then offers-level
    url = item.get("url") or item.get("@id") or offers.get("url") or None

    # Brand / producer (JSON-LD brand is optional on PrestaShop)
    brand = item.get("brand") or {}
    if isinstance(brand, dict):
        producer_raw = brand.get("name") or None
    else:
        producer_raw = str(brand) if brand else None

    vintage = parse_vintage(name)
    producer = producer_raw or parse_producer(name)

    return {
        "merchant": MERCHANT,
        "name": name,
        "producer": producer,
        "vintage": vintage,
        "currency": currency,
        "price": parse_price(str(price_raw)) if price_raw is not None else None,
        "in_stock": availability_to_bool(avail),
        "sku": str(sku) if sku is not None else None,
        "url": url,
    }


# ---------------------------------------------------------------------------
# HTML card fallback extraction
# ---------------------------------------------------------------------------

# PrestaShop product miniature article element
_MINIATURE_RE = re.compile(
    r'<article[^>]+class="[^"]*product-miniature[^"]*"[^>]*'
    r'data-id-product="(\d+)"[^>]*>(.*?)</article>',
    re.S | re.I,
)

# Product title link inside a miniature card
_TITLE_LINK_RE = re.compile(
    r'<(?:h[1-6]|span|div)[^>]+class="[^"]*product-title[^"]*"[^>]*>.*?'
    r'<a[^>]+href="([^"]+)"[^>]*>(.*?)</a>',
    re.S | re.I,
)

# Fallback: any <a> with a product URL pattern
_PRODUCT_LINK_RE = re.compile(
    r'<a[^>]+href="(https?://[^"]+)"[^>]*class="[^"]*product[^"]*"[^>]*>',
    re.S | re.I,
)

# Price span
_PRICE_RE = re.compile(
    r'<span[^>]+class="[^"]*(?:price|current-price)[^"]*"[^>]*>(.*?)</span>',
    re.S | re.I,
)

# In-stock / out-of-stock badge
_STOCK_RE = re.compile(
    r'class="[^"]*(?:product-unavailable|out-of-stock)[^"]*"',
    re.I,
)


def html_cards_to_records(page_html: str) -> list[dict]:
    """
    Parse PrestaShop product miniature cards from raw HTML.

    Used as a fallback when no JSON-LD Product blocks are found.
    """
    records = []

    for product_id, card_html in _MINIATURE_RE.findall(page_html):
        # Title + URL
        m_title = _TITLE_LINK_RE.search(card_html)
        if m_title:
            url = unescape(m_title.group(1))
            name = strip_tags(m_title.group(2))
        else:
            # Couldn't find title — skip this card
            continue

        # Price
        m_price = _PRICE_RE.search(card_html)
        price_str = strip_tags(m_price.group(1)) if m_price else None

        # Stock: absence of out-of-stock markers → assume in stock
        in_stock = not bool(_STOCK_RE.search(card_html))

        vintage = parse_vintage(name)
        producer = parse_producer(name)

        records.append({
            "merchant": MERCHANT,
            "name": name,
            "producer": producer,
            "vintage": vintage,
            "currency": "CHF",
            "price": parse_price(price_str),
            "in_stock": in_stock,
            "sku": product_id,  # data-id-product as a string proxy for SKU
            "url": url,
        })

    return records


# ---------------------------------------------------------------------------
# Main parse pipeline
# ---------------------------------------------------------------------------

def parse_page(page_html: str) -> list[dict]:
    """
    Parse one HTML page into wine records.

    Tries JSON-LD first; falls back to HTML card extraction.
    """
    # --- JSON-LD path ---
    jsonld_items = extract_jsonld_products(page_html)
    if jsonld_items:
        return [jsonld_to_record(item) for item in jsonld_items]

    # --- HTML fallback ---
    records = html_cards_to_records(page_html)
    if not records:
        print(
            "  [warn] no products found on this page "
            "(neither JSON-LD nor HTML cards matched)",
            file=sys.stderr,
        )
    return records


def parse_all(pages_html: list[str]) -> list[dict]:
    """Parse all HTML pages and deduplicate by URL."""
    seen_urls: set[str] = set()
    all_records: list[dict] = []

    for i, page_html in enumerate(pages_html, start=1):
        records = parse_page(page_html)
        new_records = []
        for r in records:
            url = r.get("url") or ""
            if url and url in seen_urls:
                continue
            if url:
                seen_urls.add(url)
            new_records.append(r)
        print(
            f"  [page {i:03d}] {len(new_records)} product(s) extracted",
            file=sys.stderr,
        )
        all_records.extend(new_records)

    print(f"Total: {len(all_records)} unique wine record(s)", file=sys.stderr)
    return all_records


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description=(
            "Parse more-than-wine.com cached HTML pages into clean wine records."
        )
    )
    src = parser.add_mutually_exclusive_group()
    src.add_argument(
        "--from-cache",
        action="store_true",
        help="Load from scraping/more-than-wine/cache/page_*.html (no stdin needed).",
    )
    src.add_argument(
        "--input",
        type=Path,
        metavar="FILE",
        help="Read a single HTML file instead of stdin.",
    )
    parser.add_argument(
        "--output",
        type=Path,
        metavar="FILE",
        help="Write output JSON to this file (default: stdout).",
    )
    args = parser.parse_args()

    if args.from_cache:
        pages = load_from_cache()
    elif args.input:
        pages = [args.input.read_text(encoding="utf-8")]
    else:
        print("Reading HTML from stdin…", file=sys.stderr)
        pages = [sys.stdin.read()]

    wines = parse_all(pages)
    output_json = json.dumps(wines, ensure_ascii=False, indent=2)

    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(output_json, encoding="utf-8")
        print(f"Wrote {len(wines)} records → {args.output}", file=sys.stderr)
    else:
        print(output_json)
