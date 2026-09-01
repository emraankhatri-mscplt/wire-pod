package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Google Gemini format.
// https://apimodels.app/docs/llm?p=gemini
//
// Differences from the OpenAI format which matter here:
//   - messages are "contents" with "parts", and the assistant role is "model"
//   - the system prompt is "systemInstruction"
//   - sampling settings live in "generationConfig"
//   - responses are candidates[].content.parts[].text; thinking models mark
//     their reasoning parts with "thought": true, which must not be spoken

type geminiPart struct {
	Text       string            `json:"text,omitempty"`
	InlineData *geminiInlineData `json:"inline_data,omitempty"`
}

type geminiInlineData struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

func (c Config) geminiBody(req Request, stream bool) ([]byte, error) {
	generationConfig := map[string]any{
		"maxOutputTokens": c.maxTokens(),
	}
	if c.Temperature > 0 {
		generationConfig["temperature"] = c.Temperature
	}
	if c.TopP > 0 {
		generationConfig["topP"] = c.TopP
	}
	body := map[string]any{
		// "model" and "stream" are ignored by Google's own per-model
		// endpoint but are required by gateways which route on them.
		"model":            c.ModelName(),
		"stream":           stream,
		"contents":         geminiContents(req.Messages),
		"generationConfig": generationConfig,
	}
	if strings.TrimSpace(req.System) != "" {
		body["systemInstruction"] = geminiContent{
			Parts: []geminiPart{{Text: req.System}},
		}
	}
	return json.Marshal(body)
}

func geminiContents(in []Message) []geminiContent {
	var out []geminiContent
	for _, m := range in {
		role := "user"
		if m.Role == RoleAssistant {
			role = "model"
		}
		var parts []geminiPart
		if strings.TrimSpace(m.Content) != "" {
			parts = append(parts, geminiPart{Text: m.Content})
		}
		if m.ImageB64 != "" {
			parts = append(parts, geminiPart{
				InlineData: &geminiInlineData{
					MimeType: "image/jpeg",
					Data:     m.ImageB64,
				},
			})
		}
		if len(parts) == 0 {
			continue
		}
		if len(out) > 0 && out[len(out)-1].Role == role {
			out[len(out)-1].Parts = append(out[len(out)-1].Parts, parts...)
			continue
		}
		out = append(out, geminiContent{Role: role, Parts: parts})
	}
	if len(out) == 0 {
		out = append(out, geminiContent{Role: "user", Parts: []geminiPart{{Text: "Hello"}}})
	}
	return out
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text    string `json:"text"`
				Thought bool   `json:"thought"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	PromptFeedback *struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (r geminiResponse) text() string {
	var sb strings.Builder
	for _, cand := range r.Candidates {
		for _, part := range cand.Content.Parts {
			if part.Thought {
				continue
			}
			sb.WriteString(part.Text)
		}
	}
	return sb.String()
}

func decodeGeminiEvent(_ string, data []byte) (string, bool) {
	var resp geminiResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		// Gemini gateways sometimes fall back to the OpenAI chunk shape.
		return decodeOpenAIEvent("", data)
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return "", true
	}
	text := resp.text()
	if text == "" && len(resp.Candidates) == 0 {
		// could still be an OpenAI shaped chunk
		if t, done := decodeOpenAIEvent("", data); t != "" {
			return t, done
		}
	}
	return text, false
}

func parseGeminiResponse(data []byte) (string, error) {
	trimmed := strings.TrimSpace(string(data))
	// Google returns a JSON array when streaming without alt=sse.
	if strings.HasPrefix(trimmed, "[") {
		var responses []geminiResponse
		if err := json.Unmarshal(data, &responses); err != nil {
			return "", fmt.Errorf("could not decode LLM response: %w", err)
		}
		var sb strings.Builder
		for _, resp := range responses {
			sb.WriteString(resp.text())
		}
		if strings.TrimSpace(sb.String()) == "" {
			return "", ErrNoResponse
		}
		return sb.String(), nil
	}

	var resp geminiResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		if text, oerr := parseOpenAIResponse(data); oerr == nil {
			return text, nil
		}
		return "", fmt.Errorf("could not decode LLM response: %w", err)
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return "", fmt.Errorf("llm error: %s", resp.Error.Message)
	}
	if resp.PromptFeedback != nil && resp.PromptFeedback.BlockReason != "" {
		return "", fmt.Errorf("llm blocked the prompt: %s", resp.PromptFeedback.BlockReason)
	}
	text := resp.text()
	if strings.TrimSpace(text) == "" {
		if t, oerr := parseOpenAIResponse(data); oerr == nil {
			return t, nil
		}
		return "", ErrNoResponse
	}
	return text, nil
}
