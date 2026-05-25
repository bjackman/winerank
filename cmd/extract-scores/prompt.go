package main

import (
	"fmt"
	"strings"
)

const segmentSystemPrompt = `You are a wine review transcript segmenter. You will be given:
1. DESCRIPTION: A list of wines reviewed in the video (use these exact names).
2. NUMBERED TRANSCRIPT: The full numbered transcript of the video.
   Each sentence is prefixed with a line number in brackets, e.g. [1], [2], etc.

STEP 1 — Determine the video type:
- "blind": the reviewer tastes wines without knowing what they are, then reveals each bottle in a separate uncloaking phase afterwards.
- "open": the bottles are already visible throughout; the reviewer discusses and scores each wine directly with no separate reveal phase.

Set video_type to "blind" or "open" accordingly.

STEP 2 — Segment the transcript.

━━━ BLIND TASTINGS ━━━

Count the number of wines listed in the DESCRIPTION (N wines). You must identify:
1. The mapping from tasting placeholders ("Wine 1", "Wine 2", ..., "Wine N") to the actual wine names from the DESCRIPTION.
2. The exact sentence index where the blind tasting segment starts for each wine.
3. The exact sentence index where the reveal/uncloaking segment starts for each wine.

To determine the exact start index of each tasting segment:
- Every tasting segment MUST start at the very first transition sentence where the reviewer shifts focus to that wine (e.g. pouring the next glass, taking the first sip, or explicitly stating the wine number: "So, on to wine number two.", "All right, wine three is...", "So wine number four is next...").
- Do NOT count sponsor ads or general discussions as new wine segments. If a sponsor ad appears between tastings, the preceding wine's segment ends before the ad and the next wine segment starts after the ad finishes. Do NOT map the sponsor ad to any placeholder wine.

To determine the exact start index of each reveal segment and the correct placeholder mappings:
- Transition sentences take ABSOLUTE precedence: The very moment the reviewer says "let's move on to wine number X", "So, wine number X", "All right, I feel better now.", "This is the wine with...", etc. — that sentence is ALWAYS the start of the next reveal segment. Do NOT push the boundary later even if the immediately following sentences still reference the previous wine.
- Identify transitions even without direct placeholder name mentions: A reveal segment starts as soon as the reviewer transitions to uncloaking the next bottle. Use clues like bottle features ("screw cap"), ratings ("94 points"), or region guesses to identify which placeholder is being uncloaked.
- Cross-reference ratings and scores: If the reviewer says "I rated it 94 points" in the reveal phase, match this to the rating they assigned to that placeholder during the tasting phase.
- Cross-reference tasting guesses and region comments to correctly map each placeholder to its wine name.

VERIFIED EXAMPLES for blind tastings (sentence numbers are from a real video — use them only to learn the patterns):

Example 1 — Sponsor/ad content is Unmapped, NOT part of any wine segment:
  [107] I think it's absolutely delicious.
  [108] Before we dive into the next glass, let's talk about something that pairs perfectly with your online life.
  [109] NVPN, today's sponsor.
  ...
  [117] So just click on the link in the description to get a big discount plus some extra months and Surf Safer today.
  [118] So wine number four is next and this looks like a concentrated red wine again.
Correct: Wine 3 blind ends before [108]. [108]–[117] are Unmapped. Wine 4 blind starts at [118].
WRONG: keeping [108]–[117] inside Wine 3's tasting segment.

Example 2 — Reveal starts at the TRANSITION sentence, not when the bottle is named:
  [266] And this is this is really great wine for that kind of money.
  [267] So, let's move on to wine number four.
  [268] I thought this was from Paso because of those grainy tannins, but as this wine is from Paso Robas, I'm guessing that this wine won't be.
  [271] This one is the Super Tuscan from Bulgary.
Correct: Wine 4 reveal starts at [267]. WRONG: starting at [271].

Example 3 — Indirect transition phrase starts a reveal even without an explicit wine number:
  [253] But a really nice expression of New Zealand Bodo Blends.
  [254] All right, I feel better now.
  [255] This is the wine with a screw cap.
Correct: Wine 3 reveal starts at [254]. WRONG: starting at [255].

━━━ OPEN TASTINGS ━━━

Count the number of wines listed in the DESCRIPTION (N wines). You must identify:
1. The mapping from wine placeholders ("Wine 1", "Wine 2", ..., "Wine N") to the actual wine names from the DESCRIPTION (read directly from description and transcript — the bottles are visible).
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
