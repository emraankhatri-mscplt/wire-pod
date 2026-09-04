package wirepod_ttr

// Sight words practice.
//
// A caregiver puts a list of words in sightWords.json (vars.SightWordsPath).
// When the child says "hey vector, practice sight words", Vector shows each
// word on his screen and says it out loud, holding each word for a
// configurable amount of time before moving on.
//
// This file holds the parts which don't need a robot: reading/validating the
// word list, drawing a word onto a screen-sized image, matching the trigger
// phrases and running the practice sequence. The robot-facing side lives in
// sightwords_robot.go.

import (
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

// Vector's face screen, in pixels.
const (
	SightWordScreenWidth  = 184
	SightWordScreenHeight = 96
)

const (
	// how long a word stays up if the caregiver didn't choose a value
	defaultSightWordSeconds = 4.0
	minSightWordSeconds     = 1.0
	maxSightWordSeconds     = 30.0
	// a practice session shouldn't run forever
	maxSightWords = 100
	// longest word which can still be drawn legibly
	maxSightWordLength = 16
	// extra time the word stays up while it is being spoken
	sightWordSpeechPaddingMs = 3000
)

// DefaultSightWords is the example list which is written to disk the first
// time sight words practice is used. It is meant to be replaced.
var DefaultSightWords = []string{
	"the", "and", "see", "look", "you", "me", "we", "go", "is", "like",
}

// spoken phrases
const (
	sightWordsIntro     = "Let's practice sight words!"
	sightWordsDone      = "Great job! That's all the words."
	sightWordsStopped   = "Okay, we can practice later. Great job!"
	sightWordsEmptyList = "I don't have any sight words yet. Please add some words to my sight words list."
)

// SightWordsConfig is the on-disk sight words configuration.
type SightWordsConfig struct {
	Words          []string `json:"words"`
	SecondsPerWord float64  `json:"seconds_per_word"`
}

// HoldTime is how long each word should stay on the screen.
func (c SightWordsConfig) HoldTime() time.Duration {
	seconds := c.SecondsPerWord
	if seconds <= 0 {
		seconds = defaultSightWordSeconds
	}
	if seconds < minSightWordSeconds {
		seconds = minSightWordSeconds
	}
	if seconds > maxSightWordSeconds {
		seconds = maxSightWordSeconds
	}
	return time.Duration(seconds * float64(time.Second))
}

// ParseSightWordsConfig reads a sight words configuration. Both the full
// object form ({"words": ["the"], "seconds_per_word": 4}) and a bare list of
// words (["the", "and"]) are accepted, since a caregiver may well type the
// simpler one. Words which cannot be shown or spoken safely are dropped.
func ParseSightWordsConfig(configBytes []byte) (SightWordsConfig, error) {
	var config SightWordsConfig
	trimmed := strings.TrimSpace(string(configBytes))
	if trimmed == "" {
		return config, errors.New("sight words config is empty")
	}
	if strings.HasPrefix(trimmed, "[") {
		var words []string
		if err := json.Unmarshal([]byte(trimmed), &words); err != nil {
			return config, err
		}
		config.Words = words
	} else {
		if err := json.Unmarshal([]byte(trimmed), &config); err != nil {
			return config, err
		}
	}
	config.Words = sanitizeSightWords(config.Words)
	return config, nil
}

// sanitizeSightWords trims the caregiver's list down to words which Vector can
// actually show and say: single words made of letters (apostrophes and hyphens
// are kept, "don't" and "well-known" are sight words too).
func sanitizeSightWords(words []string) []string {
	var cleaned []string
	for _, word := range words {
		word = strings.TrimSpace(word)
		if word == "" {
			continue
		}
		if len([]rune(word)) > maxSightWordLength {
			logger.Println("sight words: skipping '" + word + "', it is too long to show on the screen")
			continue
		}
		if !isSightWord(word) {
			logger.Println("sight words: skipping '" + word + "', only letters, apostrophes and hyphens are supported")
			continue
		}
		cleaned = append(cleaned, word)
		if len(cleaned) == maxSightWords {
			logger.Println("sight words: only the first " + strconv.Itoa(maxSightWords) + " words will be used")
			break
		}
	}
	return cleaned
}

func isSightWord(word string) bool {
	hasLetter := false
	for _, r := range word {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case r == '\'' || r == '-' || r == '’':
		default:
			return false
		}
	}
	return hasLetter
}

// LoadSightWords reads the sight words config from disk. If there is no config
// yet, the example list is written out so the caregiver has something to edit.
// It is read on every session so edits take effect without a restart.
func LoadSightWords() SightWordsConfig {
	configBytes, err := os.ReadFile(vars.SightWordsPath)
	if err != nil {
		logger.Println("sight words: no list found, creating " + vars.SightWordsPath)
		config := SightWordsConfig{
			Words:          DefaultSightWords,
			SecondsPerWord: defaultSightWordSeconds,
		}
		writeBytes, err := json.MarshalIndent(config, "", "  ")
		if err == nil {
			if err := os.WriteFile(vars.SightWordsPath, writeBytes, 0644); err != nil {
				logger.Println("sight words: unable to write example list: " + err.Error())
			}
		}
		return config
	}
	config, err := ParseSightWordsConfig(configBytes)
	if err != nil {
		logger.Println("sight words: unable to read " + vars.SightWordsPath + ": " + err.Error())
		logger.LogUI("Sight words list could not be read: " + err.Error())
		return SightWordsConfig{}
	}
	return config
}

// RenderSightWord draws one word, white on black, as large as will fit on
// Vector's screen. The font is the bundled bitmap font scaled up with nearest
// neighbour sampling, which keeps the letters blocky and readable on the small
// display and avoids shipping font files.
func RenderSightWord(word string) image.Image {
	screen := image.NewRGBA(image.Rect(0, 0, SightWordScreenWidth, SightWordScreenHeight))
	draw.Draw(screen, screen.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
	word = strings.TrimSpace(word)
	if word == "" {
		return screen
	}

	face := basicfont.Face7x13
	metrics := face.Metrics()
	textWidth := font.MeasureString(face, word).Ceil()
	ascent := metrics.Ascent.Ceil()
	textHeight := ascent + metrics.Descent.Ceil()
	if textWidth < 1 || textHeight < 1 {
		return screen
	}

	text := image.NewRGBA(image.Rect(0, 0, textWidth, textHeight))
	draw.Draw(text, text.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
	drawer := &font.Drawer{
		Dst:  text,
		Src:  image.NewUniform(color.White),
		Face: face,
		Dot:  fixed.P(0, ascent),
	}
	drawer.DrawString(word)

	scale := sightWordScale(textWidth, textHeight)
	scaledWidth := textWidth * scale
	scaledHeight := textHeight * scale
	offsetX := (SightWordScreenWidth - scaledWidth) / 2
	offsetY := (SightWordScreenHeight - scaledHeight) / 2
	for y := 0; y < scaledHeight; y++ {
		for x := 0; x < scaledWidth; x++ {
			screenX := offsetX + x
			screenY := offsetY + y
			if screenX < 0 || screenX >= SightWordScreenWidth || screenY < 0 || screenY >= SightWordScreenHeight {
				continue
			}
			screen.Set(screenX, screenY, text.At(x/scale, y/scale))
		}
	}
	return screen
}

// sightWordScale is the largest whole-pixel scale which keeps the word inside
// the screen, with a small margin so nothing touches the edges.
func sightWordScale(textWidth, textHeight int) int {
	scale := (SightWordScreenWidth - 8) / textWidth
	if heightScale := (SightWordScreenHeight - 8) / textHeight; heightScale < scale {
		scale = heightScale
	}
	if scale < 1 {
		scale = 1
	}
	return scale
}

// sight words session controls
type sightWordsAction int

const (
	// the word's time on the screen ran out
	sightWordsTimeout sightWordsAction = iota
	sightWordsNext
	sightWordsRepeat
	sightWordsStop
)

// sightWordsPresenter is what a session needs from a robot. The real
// implementation is in sightwords_robot.go; tests use a fake.
type sightWordsPresenter interface {
	// Show puts a word on the screen for up to holdMs milliseconds.
	Show(word string, holdMs uint32) error
	// Say speaks a word or sentence, returning once it has been said.
	Say(text string) error
	// Clear takes the word off the screen and gives the face back.
	Clear()
}

// runSightWordsSession shows and says each word in order. waitForAction is
// given the amount of time the word should stay up and reports how the wait
// ended, which is how "next word", "repeat word" and "stop" are handled.
func runSightWordsSession(presenter sightWordsPresenter, words []string, hold time.Duration, waitForAction func(time.Duration) sightWordsAction) error {
	defer presenter.Clear()
	if len(words) == 0 {
		presenter.Say(sightWordsEmptyList)
		return errors.New("sight words list is empty")
	}
	presenter.Say(sightWordsIntro)
	// the word stays up while it is being said as well as during the wait
	holdMs := uint32(hold/time.Millisecond) + sightWordSpeechPaddingMs
	for i := 0; i < len(words); {
		word := words[i]
		if err := presenter.Show(word, holdMs); err != nil {
			logger.Println("sight words: unable to show '" + word + "': " + err.Error())
			return err
		}
		if err := presenter.Say(word); err != nil {
			logger.Println("sight words: unable to say '" + word + "': " + err.Error())
			return err
		}
		switch waitForAction(hold) {
		case sightWordsStop:
			presenter.Say(sightWordsStopped)
			return nil
		case sightWordsRepeat:
			// same word again
		default:
			i++
		}
	}
	presenter.Say(sightWordsDone)
	return nil
}

// trigger phrases. these are matched against the transcribed text before the
// normal intent matching, in the same way plugins and custom intents are.
var sightWordsStartPhrases = []string{
	"practice sight words",
	"practice site words",
	"practise sight words",
	"practise site words",
	"start sight words",
	"start site words",
	"sight words practice",
	"site words practice",
	"sight word practice",
	"site word practice",
	"let's do sight words",
	"let's do site words",
}

var sightWordsNextPhrases = []string{
	"next word",
	"next one",
}

var sightWordsRepeatPhrases = []string{
	"repeat word",
	"repeat the word",
	"repeat that",
	"say it again",
	"say that again",
	"what was that word",
}

var sightWordsStopPhrases = []string{
	"stop sight words",
	"stop site words",
	"stop practicing",
	"stop practising",
	"i'm all done",
	"we're all done",
	"stop the words",
}

func matchesAnyPhrase(voiceText string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.Contains(voiceText, phrase) {
			return true
		}
	}
	return false
}

// sightWordsCommand works out what the child asked for. sessionActive tells it
// whether a practice session is currently running on that robot, since "next
// word" only means something during a session. action is only meaningful when
// matched is true and isStart is false.
func sightWordsCommand(voiceText string, sessionActive bool) (action sightWordsAction, isStart bool, matched bool) {
	voiceText = strings.ToLower(strings.TrimSpace(voiceText))
	if sessionActive {
		if matchesAnyPhrase(voiceText, sightWordsStopPhrases) {
			return sightWordsStop, false, true
		}
		if matchesAnyPhrase(voiceText, sightWordsRepeatPhrases) {
			return sightWordsRepeat, false, true
		}
		if matchesAnyPhrase(voiceText, sightWordsNextPhrases) {
			return sightWordsNext, false, true
		}
	}
	if matchesAnyPhrase(voiceText, sightWordsStartPhrases) {
		return sightWordsTimeout, true, true
	}
	return sightWordsTimeout, false, false
}
