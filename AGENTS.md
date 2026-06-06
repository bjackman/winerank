# AGENTS.md

Working notes for agents. The `README.md` covers what the project *does* (transcript
fetching, score extraction, eval, scraping) — read it first. This file covers the
build/dev mechanics that aren't obvious from the code.

## Go and Python toolchains come from nix

There is **no `go` or `python` on `PATH`** by default. The default `nix develop` shell
provides Python + llama-cpp but **not** Go. Always invoke Python the same way as Go —
via the appropriate nix environment:

```sh
# one-off Python script
nix develop -c python scraping/vinazion/parse.py --from-cache

# inline Python (e.g. quick JSON inspection)
nix develop -c python -c "import json, sys; ..."
```

The Go toolchain lives in the `buildGoModule` build environments. `nix develop` (the default shell) does
**not** provide one — it only has Python + llama-cpp. The Go toolchain lives in the
`buildGoModule` build environments. To get a shell/command with `go`, enter the env for
the module you're touching:

```sh
# build/test the extractor
nix develop .#extract-scores -c sh -c 'cd cmd/extract-scores && go build ./... && go vet ./... && go test ./...'

# build/test the eval tool
nix develop .#eval -c sh -c 'cd cmd/eval && go build ./... && go vet ./... && go test ./...'
```

The `cd` is required: there is no top-level `go.mod`. The repo is **two independent Go
modules**, each with its own `go.mod` (go 1.22):

- `cmd/extract-scores` → `github.com/bjackman/winerank/cmd/extract-scores`
- `cmd/eval` → `github.com/bjackman/winerank/cmd/eval`

`go vet ./...` currently reports a few pre-existing "redundant newline" warnings in
`cmd/extract-scores/main.go` — those are not yours, ignore them.

To run the built tools, `nix run .#extract-scores -- ...` / `nix run .#eval -- ...`.
With a dirty working tree these build from your *tracked* working-tree files (nix prints
a "Git tree is dirty" warning), so uncommitted edits to tracked `.go` files are picked up;
untracked files are not.

## Running against a llama-server

The extractor needs a running `llama-server` (see README for the model command). It
defaults to `http://localhost:8080`. Point it elsewhere with `--server` **or** the
`LLAMA_SERVER_URL` env var.

**Gotcha — `eval --server` does NOT reach the extractor.** `eval` runs the extractor as a
subprocess (`runExtractor` in `cmd/eval/main.go`) and only appends `--transcripts-dir`,
`--output`, and the video IDs — it does **not** forward `--server`. The `eval --server`
flag is used *only* for provenance gathering (`/props`, `/v1/models`). So if you run the
eval against a remote server, set the env var, which the subprocess inherits:

```sh
LLAMA_SERVER_URL=http://HOST:8080 nix run .#eval
```

Just passing `eval --server http://HOST:8080` will make the extractor silently fall back
to `localhost:8080` and produce a `recall 0/27` report.

## Eval iteration loop

`eval` → runs extractor on the ground-truth videos → writes `evals/<timestamp>.json`
(metrics + `provenance` + per-video stats + a `performance` block). Reports are meant to
be committed as a record. For faster iteration, build the extractor binary once and point
eval at it instead of rebuilding via nix each run:

```sh
nix develop .#extract-scores -c sh -c 'cd cmd/extract-scores && go build -o /tmp/extract-scores .'
LLAMA_SERVER_URL=http://HOST:8080 nix run .#eval -- --extractor "/tmp/extract-scores --strategy=single-pass"
```

Expect this to be **slow**: inference runs ~10–15 tok/s on local hardware, roughly ~2 min
per video and ~8 min for the full 4-video ground-truth set. Don't re-run it casually.

## Architecture at a glance

- `cmd/extract-scores` — reads `transcripts/<id>.json`, calls the LLM, writes
  `scores.json` (`FinalOutput`). Two strategies: `single-pass` (default) and `multi-pass`
  (segment, then per-wine extract). `--review` is an interactive GT-curation mode.
- `cmd/eval` — grades extractor output against `scores_groundtruth.json` via bipartite
  name matching, emits the timestamped report. It re-runs the extractor itself.
- Each report's per-video `stats` + top-level `performance` block record wall-clock
  (`duration_ms`) separately from token counts, so throughput can be judged independently
  of server/hardware (llama-server, ROCm) variance.
