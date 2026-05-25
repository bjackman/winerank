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
llama-server --hf-repo bartowski/Qwen2.5-7B-Instruct-GGUF --hf-file Qwen2.5-7B-Instruct-Q4_K_M.gguf -ngl 99 -c 32768  [47] Um so yeah, maybe maybe ma --port 8080 -np 1
```

Now, the current design is to have one context segment the transcript into
individual reviews, and then a secondary context that extracts the scores. This
seems to be necessary coz for blind tastings he talks about the wine then quite
a long time passes before he reveals its name, the small open models aren't
capable of doing that in one step.

That seemed to work OK at least on one video, but then the second video I tried
was not a blind tasting. I decided to just try keeping the 2-stage structure and
adapt the segmentation prompts so that they would work on an open tasting too.
But, this completely trashed blind tasting segmentation performance.

To try this:

```sh
# Run segmentation step on a blind tasting video:
nix run .#extract-scores --  --video 25ld_xwvGDk --segment --review
# And on an open tasting:
nix run .#extract-scores --  --video 31axP6XC0MI --segment --review
```

So probably we need different approaches for different video formats, at least
for the model/quant above. Next things to try might be:

- See how a frontier model performs.
- Build an "eval" ??? (Is this fun?).
- Just keep going and trying to make this work.