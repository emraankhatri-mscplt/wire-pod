package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Anthropic Messages format.
// https://apimodels.app/docs/llm?p=claude
//
// Differences from the OpenAI format which matter here:
//   - the system prompt is a top level "system" field, not a message
//   - "max_tokens" is required
//   - messages must alternate user/assistant and start with user
//   - streaming uses named SSE events; text arrives in content_block_delta
//     frames of type "text_delta" (thinking models also emit
//     "thinking_delta" frames, which must not be spoken)

type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicContent struct {
	Type   string           `json:"type"`
	Text   string           `json:"text,omitempty"`
	Source *anthropicImgSrc `json:"source,omitempty"`
}

type anthropicImgSrc struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

func (c Config) anthropicBody(req Request, stream bool) ([]byte, error) {
	body := map[string]any{
		"model":      c.ModelName(),
		"max_tokens": c.maxTokens(),
		"stream":     stream,
	}
	if strings.TrimSpace(req.System) != "" {
		body["system"] = req.System
	}
	if c.Temperature > 0 {
		body["temperature"] = c.Temperature
	}
	if c.TopP > 0 {
		body["top_p"] = c.TopP
	}
	body["messages"] = anthropicMessages(req.Messages)
	return json.Marshal(body)
}

// anthropicMessages converts wire-pod's message list into Anthropic's shape,
// merging consecutive same-role messages and dropping a leading assistant
// message, both of which Anthropic rejects.
func anthropicMessages(in []Message) []anthropicMessage {
	var out []anthropicMessage
	for _, m := range in {
		role := m.Role
		if role != RoleAssistant {
			role = RoleUser
		}
		if len(out) == 0 && role == RoleAssistant {
			continue
		}
		var content []anthropicContent
		if strings.TrimSpace(m.Content) != "" {
			content = append(content, anthropicContent{Type: "text", Text: m.Content})
		}
		if m.ImageB64 != "" {
			content = append(content, anthropicContent{
				Type: "image",
				Source: &anthropicImgSrc{
					Type:      "base64",
					MediaType: "image/jpeg",
					Data:      m.ImageB64,
				},
			})
		}
		if len(content) == 0 {
			continue
		}
		if len(out) > 0 && out[len(out)-1].Role == role {
			out[len(out)-1].Content = append(out[len(out)-1].Content, content...)
			continue
		}
		out = append(out, anthropicMessage{Role: role, Content: content})
	}
	if len(out) == 0 {
		out = append(out, anthropicMessage{
			Role:    RoleUser,
			Content: []anthropicContent{{Type: "text", Text: "Hello"}},
		})
	}
	return out
}

type anthropicEvent struct {
	Type  string `json:"type"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
	ContentBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content_block"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func decodeAnthropicEvent(event string, data []byte) (string, bool) {
	var ev anthropicEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return "", false
	}
	typ := ev.Type
	if typ == "" {
		typ = event
	}
	switch typ {
	case "content_block_delta":
		// text_delta carries speech; thinking_delta / signature_delta do not
		if ev.Delta.Type == "text_delta" || ev.Delta.Type == "" {
			return ev.Delta.Text, false
		}
		return "", false
	case "content_block_start":
		if ev.ContentBlock.Type == "text" {
			return ev.ContentBlock.Text, false
		}
		return "", false
	case "message_stop":
		return "", true
	case "error":
		return "", true
	default:
		// message_start, ping, content_block_stop, message_delta...
		return "", false
	}
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func parseAnthropicResponse(data []byte) (string, error) {
	var resp anthropicResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		// some gateways answer Claude requests in the OpenAI shape
		if text, oerr := parseOpenAIResponse(data); oerr == nil {
			return text, nil
		}
		return "", fmt.Errorf("could not decode LLM response: %w", err)
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return "", fmt.Errorf("llm error: %s", resp.Error.Message)
	}
	var sb strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" || block.Type == "" {
			sb.WriteString(block.Text)
		}
	}
	if strings.TrimSpace(sb.String()) == "" {
		// fall back to the OpenAI shape before giving up
		if text, oerr := parseOpenAIResponse(data); oerr == nil {
			return text, nil
		}
		return "", ErrNoResponse
	}
	return sb.String(), nil
}
