package llm

import (
	"bufio"
	"io"
	"net/http"
	"strings"
)

// sseStream reads a text/event-stream response and converts every event into
// visible assistant text using the format specific decoder.
//
// It is deliberately tolerant: keep-alive comments, unknown event types,
// heartbeat frames and frames which carry no text (usage-only chunks, prompt
// filter results, reasoning/thinking deltas) are skipped instead of ending the
// stream, which is what several gateways emit.
type sseStream struct {
	resp    *http.Response
	reader  *bufio.Reader
	decoder eventDecoder
	closed  bool
}

// eventDecoder turns one SSE data payload into visible text. done reports that
// the provider signalled the end of the response.
type eventDecoder func(event string, data []byte) (text string, done bool)

func newSSEStream(resp *http.Response, format string) Stream {
	var dec eventDecoder
	switch format {
	case FormatAnthropic:
		dec = decodeAnthropicEvent
	case FormatGemini:
		dec = decodeGeminiEvent
	default:
		dec = decodeOpenAIEvent
	}
	return &sseStream{
		resp:    resp,
		reader:  bufio.NewReaderSize(resp.Body, 64*1024),
		decoder: dec,
	}
}

func (s *sseStream) Recv() (string, error) {
	if s.closed {
		return "", io.EOF
	}
	var eventName string
	var data strings.Builder
	for {
		line, err := s.readLine()
		if err != nil {
			s.Close()
			if err == io.EOF {
				// flush a final event which was not terminated by a
				// blank line
				if data.Len() > 0 && strings.TrimSpace(data.String()) != "[DONE]" {
					if text, _ := s.decoder(eventName, []byte(data.String())); text != "" {
						return text, nil
					}
				}
				return "", io.EOF
			}
			return "", err
		}
		trimmed := strings.TrimRight(line, "\r")
		switch {
		case trimmed == "":
			// end of one event
			if data.Len() == 0 {
				eventName = ""
				continue
			}
			payload := data.String()
			data.Reset()
			name := eventName
			eventName = ""
			if strings.TrimSpace(payload) == "[DONE]" {
				s.Close()
				return "", io.EOF
			}
			text, done := s.decoder(name, []byte(payload))
			if done {
				s.Close()
				if text != "" {
					return text, nil
				}
				return "", io.EOF
			}
			if text != "" {
				return text, nil
			}
		case strings.HasPrefix(trimmed, ":"):
			// comment / keep-alive
			continue
		case strings.HasPrefix(trimmed, "event:"):
			eventName = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
		case strings.HasPrefix(trimmed, "data:"):
			if data.Len() > 0 {
				data.WriteString("\n")
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(trimmed, "data:"), " "))
		default:
			// id:, retry: and anything else is not interesting
			continue
		}
	}
}

// readLine reads a single line, transparently handling lines longer than the
// buffer size.
func (s *sseStream) readLine() (string, error) {
	var sb strings.Builder
	for {
		chunk, isPrefix, err := s.reader.ReadLine()
		if err != nil {
			if sb.Len() > 0 && err == io.EOF {
				return sb.String(), nil
			}
			return "", err
		}
		sb.Write(chunk)
		if !isPrefix {
			return sb.String(), nil
		}
	}
}

func (s *sseStream) Close() error {
	s.closed = true
	if s.resp != nil && s.resp.Body != nil {
		err := s.resp.Body.Close()
		s.resp = nil
		return err
	}
	return nil
}
