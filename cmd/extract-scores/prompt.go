package main

import (
	"fmt"
	"strings"
)

const segmentSystemPrompt = `You are a wine review transcript segmenter. You will be given:
1. DESCRIPTION: A list of wines reviewed in the video (use these exact names).
2. NUMBERED TRANSCRIPT: The full numbered transcript of the blind tasting video.
   Each sentence is prefixed with a line number in brackets, e.g. [1], [2], etc.

Your task:
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

Return ONLY a JSON object with this schema:
{
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
- You must find and list exactly N tasting segments and N reveal segments. Do not skip any wine.
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
