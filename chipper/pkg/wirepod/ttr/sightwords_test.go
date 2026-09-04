package wirepod_ttr

import (
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

// The full object form of the config must be read as-is.
func TestParseSightWordsConfigObject(t *testing.T) {
	config, err := ParseSightWordsConfig([]byte(`{"words": ["the", "and", "see"], "seconds_per_word": 6}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Join(config.Words, ",") != "the,and,see" {
		t.Fatalf("wrong words: %v", config.Words)
	}
	if config.HoldTime() != time.Second*6 {
		t.Fatalf("wrong hold time: %v", config.HoldTime())
	}
	if config.OrganicMode {
		t.Fatal("organic mode should default to false when not present in the config")
	}
}

// OrganicMode is a new field; existing configs without it must keep behaving
// exactly as before (rigid mode).
func TestParseSightWordsConfigOrganicModeDefaultsFalse(t *testing.T) {
	config, err := ParseSightWordsConfig([]byte(`{"words": ["the", "and"]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.OrganicMode {
		t.Fatal("organic mode should default to false for backward compatibility")
	}
}

// The object form must be able to turn organic mode on.
func TestParseSightWordsConfigOrganicModeEnabled(t *testing.T) {
	config, err := ParseSightWordsConfig([]byte(`{"words": ["the", "and"], "organic_mode": true}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !config.OrganicMode {
		t.Fatal("organic mode should be enabled")
	}
}

// The bare list form has no way to express organic mode, so it must default
// to false (rigid mode), same as an object form which omits the field.
func TestParseSightWordsConfigBareListOrganicModeDefault(t *testing.T) {
	config, err := ParseSightWordsConfig([]byte(`["the", "and"]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.OrganicMode {
		t.Fatal("bare list form should never enable organic mode")
	}
}

// A caregiver may just write a list of words.
func TestParseSightWordsConfigBareList(t *testing.T) {
	config, err := ParseSightWordsConfig([]byte("[\"look\", \"me\"]\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Join(config.Words, ",") != "look,me" {
		t.Fatalf("wrong words: %v", config.Words)
	}
	// no interval given, so the default applies
	if config.HoldTime() != time.Duration(defaultSightWordSeconds*float64(time.Second)) {
		t.Fatalf("wrong hold time: %v", config.HoldTime())
	}
}

// Words which can't be shown or said are dropped, and blank/broken configs
// must not blow up.
func TestParseSightWordsConfigSanitizes(t *testing.T) {
	config, err := ParseSightWordsConfig([]byte(`{"words": ["  go  ", "", "don't", "well-known", "1234", "two words", "aaaaaaaaaaaaaaaaaaaa"]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Join(config.Words, ",") != "go,don't,well-known" {
		t.Fatalf("wrong words: %v", config.Words)
	}

	if _, err := ParseSightWordsConfig([]byte("   ")); err == nil {
		t.Fatal("expected an error for an empty config")
	}
	if _, err := ParseSightWordsConfig([]byte("{not json")); err == nil {
		t.Fatal("expected an error for a broken config")
	}
}

// Very long lists are capped so a session can't run forever.
func TestParseSightWordsConfigCapsListLength(t *testing.T) {
	var words []string
	for i := 0; i < maxSightWords+25; i++ {
		words = append(words, fmt.Sprintf("\"word%s\"", strings.Repeat("a", i%10+1)))
	}
	config, err := ParseSightWordsConfig([]byte("[" + strings.Join(words, ",") + "]"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(config.Words) != maxSightWords {
		t.Fatalf("expected %d words, got %d", maxSightWords, len(config.Words))
	}
}

// The interval is clamped to something sensible for a child.
func TestSightWordsHoldTimeClamped(t *testing.T) {
	if got := (SightWordsConfig{SecondsPerWord: 0.1}).HoldTime(); got != time.Second*time.Duration(minSightWordSeconds) {
		t.Fatalf("hold time not clamped up: %v", got)
	}
	if got := (SightWordsConfig{SecondsPerWord: 500}).HoldTime(); got != time.Second*time.Duration(maxSightWordSeconds) {
		t.Fatalf("hold time not clamped down: %v", got)
	}
	if got := (SightWordsConfig{SecondsPerWord: -3}).HoldTime(); got != time.Duration(defaultSightWordSeconds*float64(time.Second)) {
		t.Fatalf("negative interval should use the default: %v", got)
	}
}

// A word must be drawn onto a screen sized image, and an empty word must give
// a blank screen rather than an error.
func TestRenderSightWord(t *testing.T) {
	img := RenderSightWord("the")
	bounds := img.Bounds()
	if bounds.Dx() != SightWordScreenWidth || bounds.Dy() != SightWordScreenHeight {
		t.Fatalf("wrong image size: %v", bounds)
	}
	lit := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if r, _, _, _ := img.At(x, y).RGBA(); r > 0 {
				lit++
			}
		}
	}
	if lit == 0 {
		t.Fatal("nothing was drawn on the screen")
	}
	// white letters on a black background, so most of the screen stays dark
	if lit > (SightWordScreenWidth*SightWordScreenHeight)/2 {
		t.Fatalf("too much of the screen is lit up: %d pixels", lit)
	}
	for _, corner := range [][2]int{{0, 0}, {SightWordScreenWidth - 1, SightWordScreenHeight - 1}} {
		if img.At(corner[0], corner[1]) != (color.RGBA{0, 0, 0, 255}) {
			t.Fatalf("background should be opaque black, corner %v is %v", corner, img.At(corner[0], corner[1]))
		}
	}

	blank := RenderSightWord("")
	for y := 0; y < SightWordScreenHeight; y++ {
		for x := 0; x < SightWordScreenWidth; x++ {
			if blank.At(x, y) != (color.RGBA{0, 0, 0, 255}) {
				t.Fatalf("empty word should give a blank screen, pixel %d,%d is %v", x, y, blank.At(x, y))
			}
		}
	}

	// a long word has to be scaled down to fit, but must still be drawn
	long := RenderSightWord("extraordinarily")
	if long.Bounds().Dx() != SightWordScreenWidth {
		t.Fatalf("wrong image size for a long word: %v", long.Bounds())
	}
}

var errNoScreen = errors.New("screen is not available")

// Every character a sight word may contain must have a well formed glyph, so
// no letter is ever silently missing from the screen.
func TestSightWordFontCoversAllowedCharacters(t *testing.T) {
	allowed := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ'-\u2019"
	for _, r := range allowed {
		glyph, ok := sightWordGlyph(r)
		if !ok {
			t.Fatalf("no glyph for %q", r)
		}
		rows := strings.Split(glyph, "/")
		if len(rows) != sightWordGlyphHeight {
			t.Fatalf("glyph for %q has %d rows, want %d", r, len(rows), sightWordGlyphHeight)
		}
		for _, row := range rows {
			if len(row) != sightWordGlyphWidth {
				t.Fatalf("glyph for %q has a row of width %d, want %d", r, len(row), sightWordGlyphWidth)
			}
			if strings.Trim(row, "#.") != "" {
				t.Fatalf("glyph for %q has an unexpected character in row %q", r, row)
			}
		}
	}
}

// fakeSightWordsPresenter records what a session asked the robot to do.
type fakeSightWordsPresenter struct {
	events    []string
	cleared   int
	showError error
}

func (f *fakeSightWordsPresenter) Show(word string, holdMs uint32) error {
	if f.showError != nil {
		return f.showError
	}
	f.events = append(f.events, "show:"+word)
	return nil
}

func (f *fakeSightWordsPresenter) Say(text string) error {
	f.events = append(f.events, "say:"+text)
	return nil
}

func (f *fakeSightWordsPresenter) Clear() {
	f.cleared++
}

// scriptedWait plays back a list of session commands, defaulting to "the word
// timed out" once the list runs out.
func scriptedWait(actions ...sightWordsAction) func(time.Duration) sightWordsAction {
	i := 0
	return func(time.Duration) sightWordsAction {
		if i < len(actions) {
			action := actions[i]
			i++
			return action
		}
		return sightWordsTimeout
	}
}

// Every word gets shown and said in order, and the session ends with a
// completion message and a cleaned up screen.
func TestSightWordsSessionShowsEveryWord(t *testing.T) {
	presenter := &fakeSightWordsPresenter{}
	if err := runSightWordsSession(presenter, []string{"the", "and"}, time.Millisecond, scriptedWait()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		"say:" + sightWordsIntro,
		"show:the", "say:the",
		"show:and", "say:and",
		"say:" + sightWordsDone,
	}
	if strings.Join(presenter.events, "|") != strings.Join(want, "|") {
		t.Fatalf("wrong session sequence:\n got %v\nwant %v", presenter.events, want)
	}
	if presenter.cleared != 1 {
		t.Fatalf("screen should be cleaned up exactly once, got %d", presenter.cleared)
	}
}

// "next word" moves on early and "repeat word" shows the same word again.
func TestSightWordsSessionNextAndRepeat(t *testing.T) {
	presenter := &fakeSightWordsPresenter{}
	wait := scriptedWait(sightWordsRepeat, sightWordsNext)
	if err := runSightWordsSession(presenter, []string{"see", "look"}, time.Minute, wait); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		"say:" + sightWordsIntro,
		"show:see", "say:see",
		"show:see", "say:see",
		"show:look", "say:look",
		"say:" + sightWordsDone,
	}
	if strings.Join(presenter.events, "|") != strings.Join(want, "|") {
		t.Fatalf("wrong session sequence:\n got %v\nwant %v", presenter.events, want)
	}
}

// "stop" ends the session right away, without going through the rest of the
// list, and still cleans up.
func TestSightWordsSessionStops(t *testing.T) {
	presenter := &fakeSightWordsPresenter{}
	wait := scriptedWait(sightWordsStop)
	if err := runSightWordsSession(presenter, []string{"we", "go", "is"}, time.Minute, wait); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		"say:" + sightWordsIntro,
		"show:we", "say:we",
		"say:" + sightWordsStopped,
	}
	if strings.Join(presenter.events, "|") != strings.Join(want, "|") {
		t.Fatalf("wrong session sequence:\n got %v\nwant %v", presenter.events, want)
	}
	if presenter.cleared != 1 {
		t.Fatalf("screen should be cleaned up exactly once, got %d", presenter.cleared)
	}
}

// An empty list must be explained to the caregiver instead of leaving Vector
// stuck with nothing to show.
func TestSightWordsSessionEmptyList(t *testing.T) {
	presenter := &fakeSightWordsPresenter{}
	err := runSightWordsSession(presenter, nil, time.Millisecond, scriptedWait())
	if err == nil {
		t.Fatal("expected an error for an empty word list")
	}
	if strings.Join(presenter.events, "|") != "say:"+sightWordsEmptyList {
		t.Fatalf("wrong session sequence: %v", presenter.events)
	}
	if presenter.cleared != 1 {
		t.Fatalf("screen should be cleaned up exactly once, got %d", presenter.cleared)
	}
}

// If the screen can't be drawn to, the session gives up rather than looping.
func TestSightWordsSessionShowError(t *testing.T) {
	presenter := &fakeSightWordsPresenter{showError: errNoScreen}
	if err := runSightWordsSession(presenter, []string{"the"}, time.Millisecond, scriptedWait()); err == nil {
		t.Fatal("expected the session to fail")
	}
	if presenter.cleared != 1 {
		t.Fatalf("screen should be cleaned up exactly once, got %d", presenter.cleared)
	}
}

// Trigger phrases start a session, and follow-up commands are only session
// commands while a session is running.
func TestSightWordsCommandMatching(t *testing.T) {
	for _, phrase := range []string{"practice sight words", "let's do site words", "start sight words"} {
		action, isStart, matched := sightWordsCommand(phrase, false)
		if !matched || !isStart || action != sightWordsTimeout {
			t.Fatalf("%q should start a session (matched=%v isStart=%v)", phrase, matched, isStart)
		}
	}

	if _, _, matched := sightWordsCommand("what's the weather", false); matched {
		t.Fatal("unrelated speech must not match")
	}

	if _, _, matched := sightWordsCommand("next word", false); matched {
		t.Fatal("session commands must not match when no session is running")
	}

	for phrase, want := range map[string]sightWordsAction{
		"next word":      sightWordsNext,
		"say it again":   sightWordsRepeat,
		"stop the words": sightWordsStop,
	} {
		action, isStart, matched := sightWordsCommand(phrase, true)
		if !matched || isStart || action != want {
			t.Fatalf("%q gave action %v (matched=%v isStart=%v)", phrase, action, matched, isStart)
		}
	}
}

// A robot can only practice one list at a time, and the slot is freed again
// afterwards so a second session can start.
func TestSightWordsSessionRegistry(t *testing.T) {
	const esn = "00e20100"
	if SightWordsSessionActive(esn) {
		t.Fatal("no session should be active yet")
	}
	actions, claimed := registerSightWordsSession(esn)
	if !claimed {
		t.Fatal("the first session should be able to start")
	}
	defer unregisterSightWordsSession(esn)
	if _, claimed := registerSightWordsSession(esn); claimed {
		t.Fatal("a second session must not start while one is running")
	}
	if !SightWordsSessionActive(esn) {
		t.Fatal("session should be active")
	}
	if !sendSightWordsAction(esn, sightWordsStop) {
		t.Fatal("commands should reach a running session")
	}
	if got := <-actions; got != sightWordsStop {
		t.Fatalf("wrong action delivered: %v", got)
	}
	unregisterSightWordsSession(esn)
	if SightWordsSessionActive(esn) {
		t.Fatal("session should be gone")
	}
	if sendSightWordsAction(esn, sightWordsStop) {
		t.Fatal("commands must not be accepted with no session running")
	}
}

// SaveSightWordsConfig must write a config which ParseSightWordsConfig reads
// back unchanged (aside from sanitization), including the organic mode flag,
// and it must sanitize the word list before writing.
func TestSaveSightWordsConfigRoundTrips(t *testing.T) {
	dir := t.TempDir()
	oldPath := vars.SightWordsPath
	vars.SightWordsPath = filepath.Join(dir, "sightWords.json")
	defer func() { vars.SightWordsPath = oldPath }()

	toSave := SightWordsConfig{
		Words:          []string{"the", "1234", "well-known"},
		SecondsPerWord: 7,
		OrganicMode:    true,
	}
	if err := SaveSightWordsConfig(toSave); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	written, err := os.ReadFile(vars.SightWordsPath)
	if err != nil {
		t.Fatalf("config file was not written: %v", err)
	}
	config, err := ParseSightWordsConfig(written)
	if err != nil {
		t.Fatalf("written config could not be parsed back: %v", err)
	}
	if strings.Join(config.Words, ",") != "the,well-known" {
		t.Fatalf("wrong words after save/load round trip: %v", config.Words)
	}
	if config.HoldTime() != time.Second*7 {
		t.Fatalf("wrong hold time after round trip: %v", config.HoldTime())
	}
	if !config.OrganicMode {
		t.Fatal("organic mode should have been preserved")
	}
}

// A fresh organic session must not consider any word covered.
func TestOrganicSightWordsStateStartsUncovered(t *testing.T) {
	state := newOrganicSightWordsState([]string{"the", "and", "see"})
	if state.allCovered() {
		t.Fatal("a fresh session should not be fully covered")
	}
	if strings.Join(state.remaining(), ",") != "the,and,see" {
		t.Fatalf("wrong remaining words: %v", state.remaining())
	}
}

// Words are marked covered whether they're said by Vector or the child, are
// matched case-insensitively, and only as whole words.
func TestOrganicSightWordsStateMarksSpokenWords(t *testing.T) {
	state := newOrganicSightWordsState([]string{"the", "see", "is"})
	state.markSpoken("The dog wants to see you!")
	if strings.Join(state.remaining(), ",") != "is" {
		t.Fatalf("wrong remaining words: %v", state.remaining())
	}
	// "is" appears inside "this", which must not count.
	state.markSpoken("this is fun")
	if !state.allCovered() {
		t.Fatalf("expected every word to be covered, remaining: %v", state.remaining())
	}
}

// The system prompt must mention every remaining word so the LLM knows what
// to practice.
func TestOrganicSightWordsSystemPromptListsRemainingWords(t *testing.T) {
	prompt := organicSightWordsSystemPrompt([]string{"the", "and"})
	if !strings.Contains(prompt, "the, and") {
		t.Fatalf("prompt should list the remaining words: %s", prompt)
	}
}
