package main

import (
	"fmt"
	"strings"
)

const segmentSystemPrompt = `You are a wine review transcript segmenter. You will be given:
1. DESCRIPTION: A list of wines reviewed in the video (use these exact names).
2. NUMBERED TRANSCRIPT: The transcript of the video, split into sentences, with each sentence prefixed by a line number in brackets like [1], [2], etc.

Your task:
Identify all sentences in the transcript that relate to each wine. Note that this is often a blind tasting:
- In the first half, the reviewer tastes and rates the wines, calling them "Wine 1", "Wine 2", etc.
- In the second half, the reviewer uncloaks/reveals the bottles (e.g. "Wine 1 was the Daou Vineyards Cabernet...").
You must match each wine name from the description to BOTH its blind tasting sentences (where it is evaluated under a placeholder) and its uncloaking/reveal sentences.

Return ONLY a JSON object with this schema:
{
  "mappings": [
    {
      "wine_name": "Exact wine name from description",
      "sentence_indices": "10-13, 85-87"
    }
  ]
}

Rules:
- Include all sentences where the wine or its placeholder (like "wine number one" or "wine 1") is discussed.
- Ensure the wine names match the description exactly.
- Represent the sentence indices as a comma-separated list of ranges or individual numbers, e.g. "10-13, 85-87, 90". Do not output a list of numbers, use a single string.
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
