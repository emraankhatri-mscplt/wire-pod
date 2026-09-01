// Package llm implements wire-pod's LLM interface layer.
//
// It speaks the three request/response formats documented by apimodels.app
// (https://apimodels.app/docs/llm), which are also the formats used by the
// upstream providers themselves:
//
//	openai    - OpenAI Chat Completions ("messages", choices/delta responses).
//	            Used for gpt-*, o1/o3/o4, grok-*, MiniMax-*, ollama, together...
//	anthropic - Anthropic Messages ("system" + "messages", content blocks,
//	            typed SSE events). Used for claude-*.
//	gemini    - Google Gemini ("contents"/"parts" + "generationConfig",
//	            candidates responses). Used for gemini-*.
//
// apimodels.app exposes all three on a single endpoint
// (POST https://apimodels.app/api/v1/messages) and routes on the model name
// prefix, so the format is auto-detected from the model name unless the user
// pins one in the config.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Roles used by all three formats (they are translated per format).
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Supported wire formats.
const (
	FormatAuto      = "auto"
	FormatOpenAI    = "openai"
	FormatAnthropic = "anthropic"
	FormatGemini    = "gemini"
)

// Message is a single conversation entry. Its JSON shape matches the OpenAI
// chat message shape so previously saved chats keep loading.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// ImageB64 optionally carries a base64 encoded JPEG which is sent
	// alongside Content as a vision input.
	ImageB64 string `json:"-"`
}

// Config describes how to reach the LLM. The caller fills it in from
// vars.APIConfig.Knowledge so this package stays independent of wire-pod's
// config handling.
type Config struct {
	// Provider is wire-pod's knowledge provider ("openai", "together",
	// "custom", ...). It only selects default endpoints and models.
	Provider string
	// Endpoint may be a base URL ("https://api.apimodels.app/v1",
	// "http://localhost:11434/v1") or a full path
	// ("https://apimodels.app/api/v1/messages").
	Endpoint string
	Key      string
	Model    string
	// Format pins the wire format. Empty or "auto" detects it from Model.
	Format string
	// Temperature and TopP are only sent when greater than zero.
	Temperature float32
	TopP        float32
	// MaxTokens is the output token cap. Zero means DefaultMaxTokens.
	MaxTokens int
}

// DefaultMaxTokens is used when the config does not specify a cap. Anthropic
// requires max_tokens, so a value is always sent for that format.
const DefaultMaxTokens = 2048

// Request is a format-independent completion request.
type Request struct {
	System   string
	Messages []Message
}

// Stream yields the assistant's visible text as it arrives. Recv returns
// io.EOF once the response is complete.
type Stream interface {
	Recv() (string, error)
	Close() error
}

var httpClient = &http.Client{
	Timeout: 10 * time.Minute,
}

// ErrNoResponse is returned when the endpoint answered successfully but the
// answer contained no text at all.
var ErrNoResponse = errors.New("llm returned no text")

// DetectFormat maps a model name onto a wire format using the same prefix
// routing apimodels.app performs server side.
func DetectFormat(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(m, "/"); i != -1 {
		m = m[i+1:]
	}
	switch {
	case strings.HasPrefix(m, "claude"), strings.HasPrefix(m, "anthropic"):
		return FormatAnthropic
	case strings.HasPrefix(m, "gemini"):
		return FormatGemini
	default:
		return FormatOpenAI
	}
}

// WireFormat returns the format which will be used for this config.
func (c Config) WireFormat() string {
	switch f := strings.ToLower(strings.TrimSpace(c.Format)); f {
	case FormatOpenAI, FormatAnthropic, FormatGemini:
		return f
	}
	return DetectFormat(c.ModelName())
}

// ModelName returns the model to request, applying provider defaults.
func (c Config) ModelName() string {
	if strings.TrimSpace(c.Model) != "" {
		return strings.TrimSpace(c.Model)
	}
	switch c.Provider {
	case "openai":
		return "gpt-4o-mini"
	case "together":
		return "meta-llama/Llama-3-70b-chat-hf"
	}
	return ""
}

func (c Config) maxTokens() int {
	if c.MaxTokens > 0 {
		return c.MaxTokens
	}
	return DefaultMaxTokens
}

// isReasoningModel reports whether an OpenAI-format model rejects
// "max_tokens" (and usually "temperature"/"top_p"), which is the case for the
// o-series and the GPT-5 family.
func isReasoningModel(model string) bool {
	m := strings.ToLower(model)
	if i := strings.LastIndex(m, "/"); i != -1 {
		m = m[i+1:]
	}
	return strings.HasPrefix(m, "o1") ||
		strings.HasPrefix(m, "o3") ||
		strings.HasPrefix(m, "o4") ||
		strings.HasPrefix(m, "gpt-5") ||
		strings.HasPrefix(m, "gpt5")
}

func (c Config) baseURL() string {
	ep := strings.TrimSpace(c.Endpoint)
	if ep == "" {
		switch c.Provider {
		case "together":
			ep = "https://api.together.xyz/v1"
		default:
			ep = "https://api.openai.com/v1"
		}
	}
	return strings.TrimRight(ep, "/")
}

// URL builds the full request URL for the given format.
//
// If the configured endpoint already points at a concrete path (as
// apimodels.app's unified "/api/v1/messages" endpoint does) it is used as is;
// otherwise the format's conventional path is appended to the base URL.
func (c Config) URL(format string) string {
	base := c.baseURL()
	lower := strings.ToLower(base)
	if strings.HasSuffix(lower, "/messages") ||
		strings.HasSuffix(lower, "/chat/completions") ||
		strings.HasSuffix(lower, "/completions") ||
		strings.Contains(lower, ":generatecontent") ||
		strings.Contains(lower, ":streamgeneratecontent") {
		return base
	}
	switch format {
	case FormatAnthropic:
		return base + "/messages"
	case FormatGemini:
		// Google's own API is per model; gateways which accept the Gemini
		// body on a fixed path use /messages.
		if strings.Contains(lower, "generativelanguage.googleapis.com") {
			return base + "/models/" + c.ModelName() + ":streamGenerateContent?alt=sse"
		}
		return base + "/messages"
	default:
		return base + "/chat/completions"
	}
}

// Stream starts a streaming completion. If the endpoint refuses to stream, the
// request is transparently retried without streaming and the whole answer is
// delivered as a single chunk.
func (c Config) Stream(ctx context.Context, req Request) (Stream, error) {
	format := c.WireFormat()
	body, err := c.buildBody(format, req, true)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, format, body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		apiErr := readAPIError(resp)
		if shouldRetryWithoutStream(apiErr) {
			text, nerr := c.Complete(ctx, req)
			if nerr != nil {
				return nil, apiErr
			}
			return newStaticStream(text), nil
		}
		return nil, apiErr
	}
	if !isEventStream(resp) {
		// The server answered a streaming request with a plain JSON body.
		data, rerr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if rerr != nil {
			return nil, rerr
		}
		text, perr := parseWholeResponse(format, data)
		if perr != nil {
			return nil, perr
		}
		return newStaticStream(text), nil
	}
	return newSSEStream(resp, format), nil
}

// Complete performs a non-streaming completion and returns the full text.
func (c Config) Complete(ctx context.Context, req Request) (string, error) {
	format := c.WireFormat()
	body, err := c.buildBody(format, req, false)
	if err != nil {
		return "", err
	}
	resp, err := c.do(ctx, format, body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", readAPIError(resp)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return parseWholeResponse(format, data)
}

func (c Config) buildBody(format string, req Request, stream bool) ([]byte, error) {
	switch format {
	case FormatAnthropic:
		return c.anthropicBody(req, stream)
	case FormatGemini:
		return c.geminiBody(req, stream)
	default:
		return c.openAIBody(req, stream)
	}
}

func (c Config) do(ctx context.Context, format string, body []byte) (*http.Response, error) {
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL(format), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "text/event-stream, application/json")
	if key := strings.TrimSpace(c.Key); key != "" {
		hreq.Header.Set("Authorization", "Bearer "+key)
		// Native Anthropic and Google endpoints use their own headers.
		// Gateways ignore the extra ones.
		if format == FormatAnthropic {
			hreq.Header.Set("x-api-key", key)
			hreq.Header.Set("anthropic-version", "2023-06-01")
		}
		if format == FormatGemini {
			hreq.Header.Set("x-goog-api-key", key)
		}
	}
	return httpClient.Do(hreq)
}

func isEventStream(resp *http.Response) bool {
	return strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream")
}

// APIError carries the upstream status and body so the real reason for a
// failure ends up in wire-pod's logs.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("llm request failed with status %d", e.StatusCode)
	}
	return fmt.Sprintf("llm request failed (%d): %s", e.StatusCode, e.Message)
}

func readAPIError(resp *http.Response) *APIError {
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
	msg := strings.TrimSpace(string(data))
	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
		Msg     string `json:"msg"`
	}
	if json.Unmarshal(data, &parsed) == nil {
		switch {
		case parsed.Error.Message != "":
			msg = parsed.Error.Message
		case parsed.Message != "":
			msg = parsed.Message
		case parsed.Msg != "":
			msg = parsed.Msg
		}
	}
	if len(msg) > 512 {
		msg = msg[:512]
	}
	return &APIError{StatusCode: resp.StatusCode, Message: msg}
}

func shouldRetryWithoutStream(err *APIError) bool {
	if err == nil {
		return false
	}
	m := strings.ToLower(err.Message)
	return strings.Contains(m, "stream") &&
		(strings.Contains(m, "not support") ||
			strings.Contains(m, "unsupported") ||
			strings.Contains(m, "must be verified") ||
			strings.Contains(m, "not allowed"))
}

func parseWholeResponse(format string, data []byte) (string, error) {
	switch format {
	case FormatAnthropic:
		return parseAnthropicResponse(data)
	case FormatGemini:
		return parseGeminiResponse(data)
	default:
		return parseOpenAIResponse(data)
	}
}

// staticStream delivers an already complete answer in one chunk.
type staticStream struct {
	text string
	done bool
}

func newStaticStream(text string) Stream {
	return &staticStream{text: text}
}

func (s *staticStream) Recv() (string, error) {
	if s.done || s.text == "" {
		return "", io.EOF
	}
	s.done = true
	return s.text, nil
}

func (s *staticStream) Close() error { return nil }
