package wirepod_ttr

import "testing"

// A response with no sentence-ending punctuation must still be spoken.
func TestSegmenterNoPunctuation(t *testing.T) {
	var s segmenter
	var got []string
	got = append(got, s.Add("{{playAnimationWI||thinking}} One plus two is three {{playAnimationWI||happy}}")...)
	got = append(got, s.Flush()...)
	if len(got) == 0 {
		t.Fatal("segmenter produced no output")
	}
	var joined string
	for _, g := range got {
		joined += g
	}
	if joined != "{{playAnimationWI||thinking}} One plus two is three {{playAnimationWI||happy}}" {
		t.Fatalf("segmenter lost text: %q", joined)
	}
}

// Commands must never be split in half across two segments.
func TestSegmenterDoesNotSplitCommands(t *testing.T) {
	var s segmenter
	var got []string
	for _, chunk := range []string{"Hi there. {{playAnim", "ationWI||happy}} bye. "} {
		got = append(got, s.Add(chunk)...)
	}
	got = append(got, s.Flush()...)
	for _, g := range got {
		if c := countRunes(g, "{{"); c != countRunes(g, "}}") {
			t.Fatalf("segment has unbalanced command braces: %q", g)
		}
	}
}

func countRunes(s, sub string) int {
	count := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			count++
		}
	}
	return count
}

// Decimal points must not end a segment.
func TestSegmenterKeepsDecimals(t *testing.T) {
	var s segmenter
	out := s.Add("Pi is 3.14 and that is that. ")
	if len(out) != 1 || out[0] != "Pi is 3.14 and that is that." {
		t.Fatalf("unexpected segments: %#v", out)
	}
}

func TestGetActionsFromString(t *testing.T) {
	actions := GetActionsFromString("{{playAnimationWI||thinking}} One plus two is three {{playAnimationWI||happy}}")
	if len(actions) != 3 {
		t.Fatalf("expected 3 actions, got %#v", actions)
	}
	if actions[0].Action != ActionPlayAnimationWI || actions[0].Parameter != "thinking" {
		t.Fatalf("bad first action: %#v", actions[0])
	}
	if actions[1].Action != ActionSayText || actions[1].Parameter != "One plus two is three" {
		t.Fatalf("bad second action: %#v", actions[1])
	}
	if actions[2].Action != ActionPlayAnimationWI || actions[2].Parameter != "happy" {
		t.Fatalf("bad third action: %#v", actions[2])
	}
}

// LLMs often use a single pipe, a colon, or forget to close the block.
func TestGetActionsFromStringTolerance(t *testing.T) {
	for _, input := range []string{
		"{{playAnimationWI|happy}} hello",
		"{{playAnimationWI: happy}} hello",
		"{{PLAYANIMATIONWI||happy}} hello",
	} {
		actions := GetActionsFromString(input)
		if len(actions) != 2 || actions[0].Action != ActionPlayAnimationWI || actions[1].Parameter != "hello" {
			t.Fatalf("input %q gave %#v", input, actions)
		}
	}
	actions := GetActionsFromString("{{playAnimationWI||happy hello there")
	if len(actions) != 1 || actions[0].Action != ActionSayText {
		t.Fatalf("unterminated block dropped text: %#v", actions)
	}
}
