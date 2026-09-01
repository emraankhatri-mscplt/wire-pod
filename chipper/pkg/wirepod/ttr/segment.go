package wirepod_ttr

import (
	"strings"
	"unicode"
)

// maxSegmentLength is the point at which a response with no sentence
// punctuation is cut anyway, so the robot still speaks it.
const maxSegmentLength = 240

// segmenter splits a streamed LLM response into speakable segments.
//
// The old implementation only ever emitted text which contained '.', '?' or
// '!', so a perfectly valid answer such as
//
//	{{playAnimationWI||thinking}} One plus two is three {{playAnimationWI||happy}}
//
// was silently dropped and the robot said nothing. This implementation:
//   - never drops text: whatever is left over is returned by Flush
//   - never splits inside a {{command||parameter}} block
//   - only splits on punctuation which is actually followed by whitespace, so
//     numbers like 3.14 stay intact
//   - falls back to a length based cut for languages/answers without
//     terminal punctuation
type segmenter struct {
	buf string
}

// Add appends newly received text and returns every segment which is now
// complete.
func (s *segmenter) Add(text string) []string {
	if text == "" {
		return nil
	}
	s.buf += text
	var out []string
	for {
		idx := s.cutIndex()
		if idx <= 0 {
			break
		}
		seg := strings.TrimSpace(s.buf[:idx])
		s.buf = s.buf[idx:]
		if seg != "" {
			out = append(out, seg)
		}
	}
	return out
}

// Flush returns whatever is left in the buffer.
func (s *segmenter) Flush() []string {
	seg := strings.TrimSpace(s.buf)
	s.buf = ""
	if seg == "" {
		return nil
	}
	return []string{seg}
}

// cutIndex returns the byte index to cut the buffer at, or -1.
func (s *segmenter) cutIndex() int {
	runes := []rune(s.buf)
	inCommand := false
	lastSpace := -1
	byteIdx := 0
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		size := len(string(r))
		byteIdx += size

		if !inCommand && r == '{' && i+1 < len(runes) && runes[i+1] == '{' {
			inCommand = true
			continue
		}
		if inCommand {
			if r == '}' && i+1 < len(runes) && runes[i+1] == '}' {
				inCommand = false
				// skip the second brace
				byteIdx += len(string(runes[i+1]))
				i++
			}
			continue
		}

		if unicode.IsSpace(r) {
			lastSpace = byteIdx
		}

		if !isSentenceEnd(r) {
			continue
		}
		// consume repeated punctuation ("...", "?!") and a closing quote
		end := byteIdx
		j := i + 1
		for j < len(runes) && (isSentenceEnd(runes[j]) || runes[j] == '"' || runes[j] == '\'' || runes[j] == ')') {
			end += len(string(runes[j]))
			j++
		}
		if j >= len(runes) {
			// the sentence may continue in the next chunk
			return -1
		}
		if !unicode.IsSpace(runes[j]) {
			// e.g. "3.14" or "example.com"
			continue
		}
		return end
	}
	// no punctuation: cut on a word boundary if the buffer is getting long
	if !inCommand && len(s.buf) > maxSegmentLength && lastSpace > 0 {
		return lastSpace
	}
	return -1
}

func isSentenceEnd(r rune) bool {
	switch r {
	case '.', '!', '?', '\u2026', '\u3002', '\uFF01', '\uFF1F':
		return true
	}
	return false
}
