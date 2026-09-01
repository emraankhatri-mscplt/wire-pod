package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// OpenAI Chat Completions format.
// https://apimodels.app/docs/llm?p=openai
//
// Used for every model which is not Claude or Gemini: gpt-*, o1/o3/o4, grok-*,
// MiniMax-*, together.ai models, ollama and any other OpenAI compatible host.

type openAIMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type openAIContentPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *openAIImageURL `json:"image_url,omitempty"`
}

type openAIImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

func (c Config) openAIBody(req Request, stream bool) ([]byte, error) {
	model := c.ModelName()
	body := map[string]any{
		"model":  model,
		"stream": stream,
	}

	msgs := make([]openAIMessage, 0, len(req.Messages)+1)
	if strings.TrimSpace(req.System) != "" {
		msgs = append(msgs, openAIMessage{Role: RoleSystem, Content: req.System})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, openAIMessage{Role: m.Role, Content: openAIContent(m)})
	}
	body["messages"] = msgs

	// Reasoning models reject max_tokens, temperature and top_p.
	if isReasoningModel(model) {
		body["max_completion_tokens"] = c.maxTokens()
	} else {
		body["max_tokens"] = c.maxTokens()
		if c.Temperature > 0 {
			body["temperature"] = c.Temperature
		}
		if c.TopP > 0 {
			body["top_p"] = c.TopP
		}
	}
	return json.Marshal(body)
}

// openAIContent returns a plain string for text-only messages (which every
// OpenAI compatible host accepts) and the multipart form when an image is
// attached.
func openAIContent(m Message) any {
	if m.ImageB64 == "" {
		return m.Content
	}
	parts := []openAIContentPart{}
	if strings.TrimSpace(m.Content) != "" {
		parts = append(parts, openAIContentPart{Type: "text", Text: m.Content})
	}
	parts = append(parts, openAIContentPart{
		Type: "image_url",
		ImageURL: &openAIImageURL{
			URL:    fmt.Sprintf("data:image/jpeg;base64,%s", m.ImageB64),
			Detail: "low",
		},
	})
	return parts
}

// openAIChunk covers both the streaming (delta) and non-streaming (message)
// response shapes, and tolerates content being either a string or an array of
// parts.
type openAIChunk struct {
	Choices []struct {
		Delta struct {
			Content          json.RawMessage `json:"content"`
			ReasoningContent string          `json:"reasoning_content"`
		} `json:"delta"`
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
		Text         string `json:"text"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func decodeOpenAIEvent(_ string, data []byte) (string, bool) {
	var chunk openAIChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return "", false
	}
	if chunk.Error != nil && chunk.Error.Message != "" {
		// The stream carries an error frame; end the response.
		return "", true
	}
	// Frames without choices (usage-only frames, prompt filter frames) carry
	// no text but must not end the stream.
	if len(chunk.Choices) == 0 {
		return "", false
	}
	ch := chunk.Choices[0]
	text := openAIText(ch.Delta.Content)
	if text == "" {
		text = openAIText(ch.Message.Content)
	}
	if text == "" {
		text = ch.Text
	}
	return text, false
}

// openAIText extracts text from a content field which may be null, a string,
// or an array of content parts.
func openAIText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return str
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var sb strings.Builder
		for _, p := range parts {
			if p.Type == "" || p.Type == "text" || p.Type == "output_text" {
				sb.WriteString(p.Text)
			}
		}
		return sb.String()
	}
	return ""
}

func parseOpenAIResponse(data []byte) (string, error) {
	var chunk openAIChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return "", fmt.Errorf("could not decode LLM response: %w", err)
	}
	if chunk.Error != nil && chunk.Error.Message != "" {
		return "", fmt.Errorf("llm error: %s", chunk.Error.Message)
	}
	if len(chunk.Choices) == 0 {
		return "", ErrNoResponse
	}
	ch := chunk.Choices[0]
	text := openAIText(ch.Message.Content)
	if text == "" {
		text = openAIText(ch.Delta.Content)
	}
	if text == "" {
		text = ch.Text
	}
	if strings.TrimSpace(text) == "" {
		return "", ErrNoResponse
	}
	return text, nil
}
