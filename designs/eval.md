# Automated eval for score extraction

WARNING: Written by Opus 4.6, reviewed by slightly drunk human with minimal
focus.

## Goal

Grade the score extractor against the checked-in ground truth without coupling
the eval to any particular extractor architecture. The current pipeline is
2-stage (segment then extract); a future rewrite might be 1-stage, 3-stage, or a
direct frontier-model call. The eval should not care.

## The contract

Treat the extractor as a black box with this stable I/O:

- **Input:** a transcript file in the existing `transcripts/<videoID>.json`
  shape.
- **Output:** the existing `scores.json` shape — a list of `{video_id, wines:
  [{name, score, ...}]}` records. Other fields (producer, vintage, region,
  notes) are bonus and not graded.

`scores_groundtruth.json` already matches this contract: it's a flat list of
`{video_id, wine_name, score, transcript_snippet}`. The snippet field is for
human auditing of the GT itself, not for grading the extractor.

The `segment_groundtruth/` files are **not** part of this eval. They describe an
intermediate artifact of today's 2-stage pipeline and would not exist for a
1-stage extractor. Keep them as a separate dev-only diagnostic that runs only
when an extractor opts into emitting segment output.

## Ground truth is not exhaustive

GT covers some wines from some videos. The eval must not assume:

- that every wine in a labeled video has a GT entry, or
- that every video has any GT entries.

Concretely: an extracted wine with no matching GT record is **unjudged**, not a
false positive. We can't compute classical precision because we don't know the
denominator. We report what we can measure (recall and score accuracy on the
matched subset) and surface unjudged extractions as a count for visibility,
never as a penalty.

This also means new wines can be added to GT incrementally without invalidating
older runs.

## Flow

The eval runs the extractor live and grades its output:

1. Read `scores_groundtruth.json` and collect the set of video IDs that have at
   least one GT entry.
2. Shell out to the extractor once, passing it the GT video IDs and a temp
   output path. The extractor's existing CLI already accepts a list of video
   IDs as positional args:

   ```
   extractor --output <tmp> <videoID1> <videoID2> ...
   ```

3. Parse the resulting `scores.json`, match extracted wines to GT wines, and
   compute metrics.

A future architecture rewrite — Go, Python, a frontier API wrapper — just needs
to honor the same CLI surface. The eval does not import any extractor types.

## Matching wines to GT

Wine names from an LLM won't be byte-equal to GT. Use a deterministic fuzzy
key:

1. Normalize: lowercase, strip punctuation, collapse whitespace, drop common
   filler words (`the`, `vineyards`, `estate`, `chateau`, etc. — small fixed
   list).
2. Extract a token set and the vintage (4-digit year if present).
3. For each GT wine, search extracted wines for a match. A pair matches if:
   - vintages agree (or one is missing), AND
   - Jaccard similarity over normalized token sets ≥ a threshold (start at
     0.5).
4. Solve as bipartite matching (greedy by similarity is fine to start; Hungarian
   if it ever matters) so each GT wine pairs with at most one extracted wine.

No LLM judge. It reintroduces the coupling and noise we're trying to eliminate,
and bipartite matching on normalized names is good enough for the volumes
involved.

## Metrics

Reported aggregated across all videos with any GT:

- **GT recall** — fraction of GT wines that got matched to some extracted wine.
- **Score exact-match rate** — among matched wines, fraction where extracted
  score equals GT score (treating `null == null` as a match).

Also surfaced for visibility (not graded):

- **Unjudged extractions** — count of extracted wines that did not match any GT
  entry.

Output format:

- One-line summary to stdout, suitable for CI logs.
- Detailed JSON report to a file (e.g. `eval-report.json`) with the matched
  pairs, the unmatched GT entries, and the unjudged extractions.

## Where it lives

A new `cmd/eval/` Go binary (matches the rest of the repo). It depends only
on:

- `scores_groundtruth.json` (read).
- The path to the extractor binary (configurable; default to `nix run
  .#extract-scores`).
- The `transcripts/` directory (passed through to the extractor).

It must not import anything from `cmd/extract-scores/`.

CLI sketch:

```
eval [--extractor "nix run .#extract-scores --"]
     [--groundtruth scores_groundtruth.json]
     [--transcripts-dir ./transcripts]
     [--report eval-report.json]
```

Add a `nix run .#eval` wrapper.

## Cleanup in the existing extractor

The auto-approve / mismatch / segment-accuracy printing currently in
`cmd/extract-scores/main.go` conflates three jobs: extraction, eval, and GT
curation. With the new eval in place:

- Keep `--review` as the GT-curation tool (it edits `scores_groundtruth.json`).
- Remove the inline "auto-approve from GT" and `[MISMATCH]` printing from the
  non-review path. The extractor should be silent about GT.
- The `--segment` diagnostic stays as-is; it's a dev tool for the current
  architecture and doesn't claim to be the eval.

This severs the last coupling between the extractor's internals and the
grading.

## Out of scope

- Caching extractor outputs across eval runs. The extractor is non-deterministic
  and cheap enough relative to a human's attention span; just re-run.
- Grading producer / region / vintage / notes fields. The score is the headline
  number; bonus fields can be added later if they start mattering.
- Segmentation accuracy. Stays as a separate dev tool tied to the current
  2-stage architecture.

## TODO

Implementation broken into small, well-scoped commits:

- [x] Scaffold `cmd/eval/` with its own Go module, CLI flag parsing, and a
      `nix run .#eval` entry in `flake.nix`. No logic yet; just prints the
      parsed flags.
- [x] Load and parse `scores_groundtruth.json` and the extractor's
      `scores.json` shape (copy the minimal struct definitions; do not import
      from `cmd/extract-scores`).
- [x] Implement wine-name normalization and the fuzzy matching function
      (vintage + Jaccard over normalized token sets). Add unit tests with
      hand-picked examples covering exact match, vintage mismatch, and partial
      token overlap.
- [x] Implement bipartite matching (greedy by similarity) between a GT list and
      an extracted list for one video.
- [ ] Implement the metric computation (GT recall, score exact-match rate,
      unjudged-extraction count) over a parsed `scores.json` + GT pair, and
      print the one-line summary.
- [ ] Wire up the extractor subprocess: collect GT video IDs, invoke the
      extractor with them as positional args and a temp output path, parse the
      result, run the metrics, print the summary.
- [ ] Write the detailed `eval-report.json` with matched pairs, unmatched GT,
      and unjudged extractions.
- [ ] Remove the inline GT auto-approve and `[MISMATCH]` printing from
      `cmd/extract-scores/main.go` so the extractor is silent about GT in
      non-review runs.
