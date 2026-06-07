#!/usr/bin/env python3
"""
combine.py — merge all per-merchant wines.json files into a single wines.json.

Reads every merchant output file, projects each record onto the unified schema
(filling missing fields with null), and writes one flat JSON array.

Unified schema
--------------
  merchant        str           always present
  name            str           always present
  producer        str | null
  vintage         int | null
  price           int | null    minor currency units (rappen / cents)
  currency        str
  in_stock        bool | null
  bottle_size_ml  int | null
  url             str | null
  sku             str | null
  wine_type       str | null
  country         str | null
  region          str | null
  appellation     str | null
  grapes          list | null
  alcohol         float | null
  categories      list | null

Merchant-specific fixes applied
--------------------------------
  realwines   — missing merchant/url/sku/etc. filled with null; merchant added
  moevenpick  — variants[] array dropped (top-level price/in_stock retained)
  vergani     — variant null-sentinel field dropped
  advanvinum  — variant null-sentinel field dropped

Usage
-----
    nix develop -c python scraping/combine.py
    nix develop -c python scraping/combine.py --output wines.json
"""

import argparse
import json
import sys
from pathlib import Path

ROOT = Path.cwd()

SCHEMA_FIELDS = [
    "merchant",
    "name",
    "producer",
    "vintage",
    "price",
    "currency",
    "in_stock",
    "bottle_size_ml",
    "url",
    "sku",
    "wine_type",
    "country",
    "region",
    "appellation",
    "grapes",
    "alcohol",
    "categories",
]

# (merchant_name, path_relative_to_ROOT)
SOURCES: list[tuple[str, Path]] = [
    ("arvi",           ROOT / "scraping/arvi/wines.json"),
    ("bauraulac",      ROOT / "scraping/bauraulac/wines.json"),
    ("flaschenpost",   ROOT / "scraping/flaschenpost/wines.json"),
    ("gerstl",         ROOT / "scraping/gerstl/wines.json"),
    ("landolt",        ROOT / "scraping/landolt/wines.json"),
    ("moevenpick",     ROOT / "scraping/moevenpick/wines.json"),
    ("more-than-wine", ROOT / "scraping/more-than-wine/wines.json"),
    ("passeur",        ROOT / "scraping/passeur/wines.json"),
    ("realwines",      ROOT / "scraping/realwines/wines.json"),
    ("rebwein",        ROOT / "scraping/rebwein/wines.json"),
    ("smith-and-smith",ROOT / "scraping/smith-and-smith/wines.json"),
    ("vinazion",       ROOT / "scraping/vinazion/wines.json"),
    ("zweifel",        ROOT / "scraping/zweifel/wines.json"),
    ("vergani",        ROOT / "scraping/shopify/vergani.json"),
    ("advanvinum",     ROOT / "scraping/shopify/advanvinum.json"),
]


def parse_vintage(v) -> int | None:
    if v is None:
        return None
    if isinstance(v, int):
        return v
    try:
        return int(str(v).strip())
    except ValueError:
        return None


def normalise(record: dict, merchant: str) -> dict:
    out: dict = {}
    for field in SCHEMA_FIELDS:
        out[field] = record.get(field)

    # Ensure merchant is always set.
    if not out["merchant"]:
        out["merchant"] = merchant

    # Coerce vintage to int.
    out["vintage"] = parse_vintage(out["vintage"])

    return out


def load_source(merchant: str, path: Path) -> list[dict]:
    if not path.exists():
        print(f"  [skip]  {path} not found", file=sys.stderr)
        return []
    records = json.loads(path.read_text(encoding="utf-8"))
    normalised = [normalise(r, merchant) for r in records]
    print(f"  [load]  {path.relative_to(ROOT)}  →  {len(normalised)} records", file=sys.stderr)
    return normalised


def combine() -> list[dict]:
    all_records: list[dict] = []
    for merchant, path in SOURCES:
        all_records.extend(load_source(merchant, path))
    print(f"Total: {len(all_records)} records from {len(SOURCES)} sources", file=sys.stderr)
    return all_records


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Combine all merchant wines.json files into one.")
    parser.add_argument("--output", type=Path, default=ROOT / "wines.json",
                        help="Output path (default: wines.json at repo root)")
    args = parser.parse_args()

    records = combine()
    out = json.dumps(records, ensure_ascii=False, indent=2)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(out, encoding="utf-8")
    print(f"Wrote {len(records)} records → {args.output}", file=sys.stderr)
