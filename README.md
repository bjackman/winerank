# winerank

SLOP WARNING: Everything in this repo is AI generated in the laziest possible
way. To the extent that I have ever even looked at the code, it seems to be
fucking garbage. Don't train your AI on this and please don't judge my
engineering by it.

## Transcript fetcher

WARNING: Developing this got my residential IP banned from the YouTube API,
maybe permanently I don't know. Using YouTube via its own clients still works
fine. When I switched to my mobile network it worked for a while but then still
got banned again even with a rate limit applied.

```sh
nix run .#get-transcripts -- --num-vids 100
```

This will dump transcripts into `./transcripts/`.

## Score extraction

The `devShell` includes a llama-cpp build that works on my 3070Ti. Run a
`llama-server`, here's a command that I don't really understand but which Gemini
says is a reasonable model for this task with some tweak flags that are supposed
to keep things in VRAM:

```sh
llama-server --hf-repo bartowski/Qwen2.5-7B-Instruct-GGUF --hf-file Qwen2.5-7B-Instruct-Q4_K_M.gguf -ngl 99 -c 32768 --port 8080 -np 1
```

Opus 4.7's suggestion for a model to run on my Framework 13 (32GiB):

```sh
GGML_CUDA_ENABLE_UNIFIED_MEMORY=1 llama-server \
              -hf bartowski/Qwen_Qwen3-30B-A3B-Instruct-2507-GGUF:Q4_K_M \
              -ngl 99 -c 32768 -fa on \
              --cache-type-k q8_0 --cache-type-v q8_0 \
              --port 8080 -np 1
```

(Yes, the env var says CUDA but it still configures AMD too, I confirmed this
was required to get ROCM access to the full memory).

With ROCM this crashed due to a llama.cpp bug in the structured output grammar
logic. With Vulkan it crashed because it couldn't get access to the unified
memory.

The extractor talks to `http://localhost:8080` by default. Point it elsewhere
with `--server`, or set `LLAMA_SERVER_URL` (the flag wins if both are given).

### Eval

Grade the extractor against `scores_groundtruth.json`:

```sh
nix run .#eval
```

It runs `.#extract-scores` on the ground-truth videos, prints a metrics
summary, and writes `eval-report.json`. Override with `--extractor`,
`--groundtruth`, `--transcripts-dir`, `--report`. For faster iteration, build
the extractor once and point at the binary:

```sh
go build -C cmd/extract-scores -o extract-scores
nix run .#eval -- --extractor "$PWD/cmd/extract-scores/extract-scores"
```

Needs a running `llama-server` (see above).