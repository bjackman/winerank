package main

import (
	"fmt"
	"strings"
)

const segmentSystemPrompt = `You are a wine review transcript segmenter. You will be given:
1. DESCRIPTION: A list of wines reviewed in the video (use these exact names).
2. NUMBERED TRANSCRIPT: The full numbered transcript of the video.
   Each sentence is prefixed with a line number in brackets, e.g. [1], [2], etc.

STEP 1 — Determine the video type.

Look at how each wine is introduced in the transcript:
- If the reviewer NAMES the wine before or at the moment of tasting it (e.g. "In 10th place we have the 2023 Château X..." or "Next up is the Barolo from producer Y..."), this is "open".
- If wines are tasted without being named and all bottles are uncloaked together in a later phase, this is "blind".

As a quick check: is there a block near the END of the video where multiple bottles are revealed in sequence? If yes → "blind". If no such reveal block exists → "open".

Set video_type to "blind" or "open" accordingly.

STEP 2 — Segment the transcript.

--- FOR BLIND TASTINGS ---

Count the number of wines listed in the DESCRIPTION (let's say there are N wines).
You must identify:
1. The mapping from tasting placeholders ("Wine 1", "Wine 2", ..., "Wine N") to the actual wine names from the DESCRIPTION.
2. The exact sentence index where the blind tasting segment starts for each wine.
3. The exact sentence index where the reveal/uncloaking segment starts for each wine.

To determine the exact start index of each tasting segment:
- Every tasting segment MUST start at the very first transition sentence where the reviewer shifts focus to that wine (e.g. pouring the next glass, taking the first sip, or explicitly stating the wine number: "So, on to wine number two.", "All right, wine three is...", "So wine number four is next...").
- Do NOT count sponsor ads or general discussions as new wine segments. If a sponsor ad appears between tastings, the preceding wine's segment continues through the sponsor ad, or the next wine segment starts after the sponsor ad finishes, at the sentence where the next wine is explicitly introduced (e.g. "So wine number four is next"). Do NOT map the sponsor ad to any placeholder wine.

To determine the exact start index of each reveal segment and the correct placeholder mappings:
- Transition sentences take ABSOLUTE precedence: The very moment the reviewer says "let's move on to wine number X", "So, wine number X", "All right, I feel better now.", "This is the wine with...", "All right, let's see what this is.", etc. — that sentence is ALWAYS the start of the next reveal segment. Do NOT push the boundary later even if the immediately following sentences still reference the previous wine's guess or tasting notes in a comparative way. The sentences after "let's move on to wine number 4" belong to wine 4's reveal segment, even if those sentences say things like "I thought this was from Paso because..." (which is the reviewer reflecting on their guess about wine 4 during wine 4's reveal).
- CRITICAL EXAMPLE: If you see:
    [X] "So, let's move on to wine number four."
    [X+1] "I thought this was from Paso because of those grainy tannins, but as this wine is from Paso Robas, I'm guessing that this wine won't be."
    [X+2] "But let's see."
    [X+3] "This one is the Super Tuscan from Bulgary."
  — then wine 4's reveal segment starts at [X], NOT at [X+3]. The sentences [X+1] and [X+2] are the reviewer contextualizing their wine 4 guess, still part of wine 4's reveal.
- Identify transitions even without direct placeholder name mentions: A reveal segment starts as soon as the reviewer transitions to uncloaking the next bottle, even if they do not explicitly say the placeholder number (e.g. "Wine 3" or "wine number three") right away. Use clues like bottle features ("screw cap"), ratings ("94 points"), or region guesses discussed at the start of the uncloaking to identify which placeholder is being uncloaked, and start the segment at the very first transition sentence (e.g., "All right, I feel better now.", "This is the wine with...").
- Cross-reference ratings and scores: If the reviewer says "I rated it 94 points" or "this is the highest rated wine with 95 points" in the reveal phase, match this to the rating they assigned to that placeholder during the tasting phase.
- Cross-reference tasting guesses and region comments: Match references in the reveal phase back to region guesses and tasting styles in the tasting phase (e.g. if they say "I thought this was from Paso... this is actually the Super Tuscan from Bulgery", and they guessed Wine 4 as Paso Robles during the tasting, then Wine 4 maps to Castello di Bolgheri Varvara Bolgheri).

VERIFIED EXAMPLES of correct boundary decisions (sentence numbers are from a real video, NOT the one you are segmenting — use them only to learn the patterns):

Example 1 — Sponsor/ad content is Unmapped, NOT part of any wine segment:
  [107] I think it's absolutely delicious.
  [108] Before we dive into the next glass, let's talk about something that pairs perfectly with your online life.
  [109] NVPN, today's sponsor.
  ...
  [117] So just click on the link in the description to get a big discount plus some extra months and Surf Safer today.
  [118] So wine number four is next and this looks like a concentrated red wine again.
Correct decision: Wine 3 blind tasting ends before [108]. Sentences [108]-[117] are Unmapped sponsor content — do NOT assign them to Wine 3 or Wine 4. Wine 4 blind starts at [118].
WRONG decision: keeping [108]-[117] inside Wine 3's tasting segment because the reviewer said "before we dive into the next glass."

Example 2 — Reveal starts at the TRANSITION sentence, not when the bottle is named:
  [266] And this is this is really great wine for that kind of money.
  [267] So, let's move on to wine number four.
  [268] I thought this was from Paso because of those grainy tannins, but as this wine is from Paso Robas, I'm guessing that this wine won't be.
  [269] But let's let's see.
  [270] Maybe we have two parcel wines in the mix.
  [271] This one is the Super Tuscan from Bulgary.
Correct decision: Wine 4 reveal starts at [267] ("So, let's move on to wine number four.").
WRONG decision: starting Wine 4 reveal at [271] ("This one is the Super Tuscan from Bulgary.") — by then you are already 4 sentences into Wine 4's reveal.

Example 3 — Indirect transition phrase starts a reveal even without an explicit wine number:
  [253] But a really nice expression of New Zealand Bodo Blends.
  [254] All right, I feel better now.
  [255] This is the wine with a screw cap.
  [256] So I put it into Australia.
Correct decision: Wine 3 reveal starts at [254] ("All right, I feel better now."). The phrase signals the reviewer is transitioning to the next uncloaking, even though no wine number is mentioned.
WRONG decision: starting Wine 3 reveal at [255] or [257] because those sentences are the first to mention the screw cap / tasting notes.

--- FOR OPEN TASTINGS ---

Count the number of wines listed in the DESCRIPTION (N wines). You must identify:
1. The mapping from wine placeholders ("Wine 1", "Wine 2", ..., "Wine N") to the actual wine names from the DESCRIPTION (read directly — the bottles are visible).
2. The exact sentence index where the discussion of each wine starts.

Leave reveal_segments empty ([]).

To determine each wine's start index:
- Find the sentence where the reviewer shifts focus to a new wine — pouring, first mention of the wine's name, or an explicit transition ("Next up is...", "Let's look at...", "Moving on to...").
- Do NOT include intro sections, sponsor ads, or outros as wine segments.

Return ONLY a JSON object with this schema:
{
  "video_type": "blind" or "open",
  "placeholder_mappings": [
    {
      "placeholder": "Wine 1",
      "wine_name": "Exact wine name from description"
    }
  ],
  "tasting_segments": [
    {
      "placeholder": "Wine 1",
      "start": 26
    }
  ],
  "reveal_segments": [
    {
      "placeholder": "Wine 1",
      "start": 244
    }
  ]
}

Rules:
- Locate the exact sentence index where the topic shifts.
- The "tasting_segments" and "reveal_segments" arrays must be in chronological order by "start" index.
- For blind tastings: exactly N tasting segments and N reveal segments. For open tastings: exactly N tasting segments and reveal_segments must be [].
- Do not skip any wine.
- Return ONLY the JSON object, no other text.`


const extractSystemPrompt = `You are a wine review score extractor. You will be given:
1. WINE NAME: The name of the wine being reviewed.
2. TRANSCRIPT SEGMENT: A focused segment of a video transcript discussing this wine.

Your task is to extract:
1. The producer or winery name.
2. The vintage year or "NV" for non-vintage.
3. The region and country (e.g. "Paso Robles, USA" or "Bordeaux, France").
4. The numerical score (on the 100-point scale) assigned to this wine by the reviewer, if any.
5. A brief 1-2 sentence summary of the tasting notes.
6. A verbatim matching snippet of 1-2 sentences from the provided transcript segment where the review or score is discussed.

Return ONLY a JSON object with this schema:
{
  "producer": "Producer/winery name",
  "vintage": "Vintage year or NV",
  "region": "Region, country",
  "score": 91,
  "notes_summary": "Brief 1-2 sentence summary of the tasting notes.",
  "matching_snippet": "Verbatim quote from the transcript segment."
}

Rules:
- Set "score" to null if no numerical score is explicitly assigned.
- The "matching_snippet" must be an exact verbatim substring from the transcript segment provided.
- Infer the producer, vintage, and region from the WINE NAME or TRANSCRIPT SEGMENT.
- Return ONLY the JSON object, no other text.`

func BuildSegmentUserMessage(description, numberedTranscript string) string {
	return fmt.Sprintf("DESCRIPTION:\n%s\n\nNUMBERED TRANSCRIPT:\n%s", cleanDescription(description), numberedTranscript)
}


func cleanDescription(desc string) string {
	startKeywords := []string{
		"tasted the following wines",
		"wines in this video",
		"wines in this Video",
		"tasted the following",
	}

	startIndex := -1
	for _, kw := range startKeywords {
		if idx := strings.Index(strings.ToLower(desc), kw); idx != -1 {
			startIndex = idx
			break
		}
	}

	if startIndex == -1 {
		return desc
	}

	cleaned := desc[startIndex:]

	endKeywords := []string{
		"100 point scoring system",
		"100 point scoring",
		"the 100 point",
		"robertparker.com",
	}

	endIndex := -1
	for _, kw := range endKeywords {
		if idx := strings.Index(strings.ToLower(cleaned), kw); idx != -1 {
			endIndex = idx
			break
		}
	}

	if endIndex != -1 {
		cleaned = cleaned[:endIndex]
	}

	return strings.TrimSpace(cleaned)
}

func BuildExtractUserMessage(wineName, focusedText string) string {
	return fmt.Sprintf("WINE NAME:\n%s\n\nTRANSCRIPT SEGMENT:\n%s", wineName, focusedText)
}
