#!/usr/bin/env python3
"""
landolt/parse.py — parse cached Shopware PDPs into clean wine records.

Shopware 6 PDPs are server-rendered with schema.org microdata and a properties
table. NB: the `itemprop="name"` belongs to the *manufacturer* microdata, not
the product — the real product name is the `<h1>`. We read:

  - `<h1 class="product-detail-name">`  → name
  - `itemprop="price"` / `itemprop="priceCurrency"` → price / currency
  - the "Produzent" row of the properties table → producer (also Land/Region/
    Jahrgang/Rebsorten when present)
  - the `product-delivery-information` block → in_stock
  - the canonical URL → url; the trailing number → sku

Usage
-----
    nix develop -c python scraping/landolt/parse.py --from-cache
    nix develop -c python scraping/landolt/parse.py --from-cache --output scraping/landolt/wines.json
"""

import argparse
import html as html_module
import json
import re
import sys
from pathlib import Path

CACHE_DIR = Path.cwd() / "scraping" / "landolt" / "cache"
MERCHANT = "landolt"

_H1_RE = re.compile(r'<h1[^>]*class="[^"]*product-detail-name[^"]*"[^>]*>(.*?)</h1>', re.S)
_H1_FALLBACK_RE = re.compile(r"<h1[^>]*>(.*?)</h1>", re.S)
_PRICE_RE = re.compile(r'itemprop="price"[^>]*content="([^"]+)"')
_CURRENCY_RE = re.compile(r'itemprop="priceCurrency"[^>]*content="([^"]+)"')
_CANON_RE = re.compile(r'<link[^>]+rel="canonical"[^>]+href="([^"]+)"')
_DELIVERY_RE = re.compile(r"product-delivery-information(.*?)</div>", re.S)
# properties table: <th>Label</th> … <td>Value</td>  (Shopware product-detail-properties)
_ROW_RE = re.compile(r"<th[^>]*>(.*?)</th>\s*<td[^>]*>(.*?)</td>", re.S)


def load_files() -> list[Path]:
    pages = sorted(CACHE_DIR.glob("*.html"))
    if not pages:
        print("No cache files found. Run fetch.py first.", file=sys.stderr)
        sys.exit(1)
    return pages


def strip_tags(text: str) -> str:
    return html_module.unescape(re.sub(r"<[^>]+>", " ", text or "")).strip()


def to_rappen(price) -> int | None:
    try:
        return round(float(price) * 100)
    except (TypeError, ValueError):
        return None


def parse_vintage(*texts: str) -> int | None:
    for t in texts:
        m = re.search(r"\b(19|20)\d{2}\b", t or "")
        if m:
            return int(m.group(0))
    return None


def properties(html: str) -> dict:
    props = {}
    for label, value in _ROW_RE.findall(html):
        key = strip_tags(label).rstrip(":").lower()
        props[key] = re.sub(r"\s+", " ", strip_tags(value))
    return props


def page_to_record(html: str) -> dict | None:
    m_h1 = _H1_RE.search(html) or _H1_FALLBACK_RE.search(html)
    if not m_h1:
        return None
    name = re.sub(r"\s+", " ", strip_tags(m_h1.group(1)))
    if not name:
        return None

    props = properties(html)
    m_canon = _CANON_RE.search(html)
    url = html_module.unescape(m_canon.group(1)) if m_canon else None

    m_price = _PRICE_RE.search(html)
    m_cur = _CURRENCY_RE.search(html)
    m_delivery = _DELIVERY_RE.search(html)
    delivery_text = strip_tags(m_delivery.group(1)).lower() if m_delivery else ""
    in_stock = None
    if delivery_text:
        in_stock = not any(w in delivery_text for w in ("nicht verfügbar", "ausverkauft", "nicht lieferbar"))

    grapes_raw = props.get("traubensorten") or ""
    # Herkunft is "Country/Region" (e.g. "Schweiz/Kanton Zürich"); split it.
    herkunft = props.get("herkunft") or props.get("land") or ""
    country, _, region = herkunft.partition("/")
    return {
        "merchant": MERCHANT,
        "name": name,
        "producer": props.get("produzent") or props.get("hersteller") or None,
        # Landolt's (largely own-label) range has no Jahrgang property; fall back
        # to a year in the name, which is usually absent → vintage is often null.
        "vintage": parse_vintage(props.get("jahrgang", ""), name),
        "country": country.strip() or None,
        "region": region.strip() or None,
        "grapes": [g.strip() for g in re.split(r"[,/]", grapes_raw) if g.strip()],
        "alcohol": props.get("alkoholgehalt") or None,
        "currency": (m_cur.group(1) if m_cur else "CHF"),
        "price": to_rappen(m_price.group(1)) if m_price else None,
        "in_stock": in_stock,
        "sku": (url.rstrip("/").rsplit("/", 1)[-1] if url else None),
        "url": url,
    }


def parse_all(pages: list[Path]) -> list[dict]:
    records, skipped = [], 0
    for p in pages:
        rec = page_to_record(p.read_text(encoding="utf-8"))
        if rec is None:
            skipped += 1
            continue
        records.append(rec)
    print(f"Parsed {len(records)} wine records ({skipped} pages skipped)", file=sys.stderr)
    return records


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Parse landolt-weine.ch Shopware PDPs into wine records")
    src = parser.add_mutually_exclusive_group()
    src.add_argument("--from-cache", action="store_true", help="Load from ./landolt/cache/*.html")
    src.add_argument("--input", type=Path, metavar="FILE", help="Read one HTML file instead of the cache")
    parser.add_argument("--output", type=Path, metavar="FILE", help="Write output JSON here (default: stdout)")
    args = parser.parse_args()

    pages = [args.input] if args.input else load_files()
    wines = parse_all(pages)
    output_json = json.dumps(wines, ensure_ascii=False, indent=2)

    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(output_json, encoding="utf-8")
        print(f"Wrote {len(wines)} records → {args.output}", file=sys.stderr)
    else:
        print(output_json)
