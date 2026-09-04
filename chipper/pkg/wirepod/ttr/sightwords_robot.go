package wirepod_ttr

// Robot-facing half of sight words practice: showing words on Vector's
// screen, saying them, and keeping track of which robots have a session
// running so follow-up commands can be routed to them.

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/scripting"
	"github.com/kercre123/wire-pod/chipper/pkg/wirepod/llm"
)

// how long to wait for behavior control before giving up on the session
const sightWordsControlTimeout = time.Second * 15

// screen brightness, as a percentage, used when converting a word to the
// format Vector's screen expects
const sightWordsOpacityPercent = 100

var (
	sightWordsMutex    sync.Mutex
	sightWordsSessions = map[string]chan sightWordsAction{}
)

// registerSightWordsSession claims the session slot for a robot. It returns
// false if that robot is already practicing.
func registerSightWordsSession(esn string) (chan sightWordsAction, bool) {
	sightWordsMutex.Lock()
	defer sightWordsMutex.Unlock()
	if _, exists := sightWordsSessions[esn]; exists {
		return nil, false
	}
	actions := make(chan sightWordsAction, 4)
	sightWordsSessions[esn] = actions
	return actions, true
}

func unregisterSightWordsSession(esn string) {
	sightWordsMutex.Lock()
	defer sightWordsMutex.Unlock()
	delete(sightWordsSessions, esn)
}

// SightWordsSessionActive reports whether the given robot is in the middle of
// a sight words practice session.
func SightWordsSessionActive(esn string) bool {
	sightWordsMutex.Lock()
	defer sightWordsMutex.Unlock()
	_, exists := sightWordsSessions[esn]
	return exists
}

// sendSightWordsAction hands a follow-up command to a running session. It
// never blocks, so a burst of commands can't wedge the voice pipeline.
func sendSightWordsAction(esn string, action sightWordsAction) bool {
	sightWordsMutex.Lock()
	actions, exists := sightWordsSessions[esn]
	sightWordsMutex.Unlock()
	if !exists {
		return false
	}
	select {
	case actions <- action:
		return true
	default:
		return false
	}
}

// robotSightWordsPresenter shows and says words on a real robot.
type robotSightWordsPresenter struct {
	robot *vector.Vector
	ctx   context.Context
}

func (p *robotSightWordsPresenter) Show(word string, holdMs uint32) error {
	faceData := sightWordFaceData(word)
	_, err := p.robot.Conn.DisplayFaceImageRGB(
		p.ctx,
		&vectorpb.DisplayFaceImageRGBRequest{
			FaceData:         faceData,
			DurationMs:       holdMs,
			InterruptRunning: true,
		},
	)
	return err
}

func (p *robotSightWordsPresenter) Say(text string) error {
	if text == "" {
		return nil
	}
	_, err := p.robot.Conn.SayText(
		p.ctx,
		&vectorpb.SayTextRequest{
			Text:           text,
			UseVectorVoice: true,
			DurationScalar: 1.0,
		},
	)
	return err
}

// Clear blanks the screen so the word doesn't linger once practice is over.
// Behavior control is released by StartSightWords, which brings back the face.
func (p *robotSightWordsPresenter) Clear() {
	_, err := p.robot.Conn.DisplayFaceImageRGB(
		p.ctx,
		&vectorpb.DisplayFaceImageRGBRequest{
			FaceData:         sightWordFaceData(""),
			DurationMs:       100,
			InterruptRunning: true,
		},
	)
	if err != nil {
		logger.Println("sight words: unable to clear the screen: " + err.Error())
	}
}

// sightWordFaceData renders a word into the 16 bit format Vector's screen
// expects. An empty word gives a blank screen.
func sightWordFaceData(word string) []byte {
	pixels := scripting.ConvertPixelsToRawBitmap(RenderSightWord(word), sightWordsOpacityPercent)
	buf := new(bytes.Buffer)
	for _, pixel := range pixels {
		binary.Write(buf, binary.LittleEndian, pixel)
	}
	return buf.Bytes()
}

// StartSightWords runs one sight words practice session on the given robot.
// It blocks until practice is finished, stopped or has errored, and always
// gives behavior control back.
func StartSightWords(esn string) error {
	config := LoadSightWords()
	robot, err := getRobot(esn)
	if err != nil {
		logger.Println("sight words: " + err.Error())
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := robot.Conn.BatteryState(ctx, &vectorpb.BatteryStateRequest{}); err != nil {
		logger.Println("sight words: unable to reach robot " + esn + ": " + err.Error())
		return err
	}

	actions, claimed := registerSightWordsSession(esn)
	if !claimed {
		logger.Println("sight words: " + esn + " is already practicing")
		return errors.New("sight words practice is already running")
	}
	defer unregisterSightWordsSession(esn)

	// buffered so signalling can never leak a goroutine
	start := make(chan bool, 1)
	stop := make(chan bool, 1)
	BControl(robot, ctx, start, stop)
	releaseControl := func() {
		select {
		case stop <- true:
		default:
		}
	}
	select {
	case <-start:
	case <-time.After(sightWordsControlTimeout):
		releaseControl()
		logger.Println("sight words: timed out waiting for behavior control")
		return errors.New("timed out waiting for behavior control")
	}
	defer releaseControl()

	logger.Println("sight words: starting practice on " + esn + " with " + strconv.Itoa(len(config.Words)) + " words")
	logger.LogUI("Sight words practice started on " + esn)

	presenter := &robotSightWordsPresenter{robot: robot, ctx: ctx}
	waitForAction := func(hold time.Duration) sightWordsAction {
		timer := time.NewTimer(hold)
		defer timer.Stop()
		select {
		case action := <-actions:
			return action
		case <-timer.C:
			return sightWordsTimeout
		case <-ctx.Done():
			return sightWordsStop
		}
	}
	err = runSightWordsSession(presenter, config.Words, config.HoldTime(), waitForAction)
	if err != nil {
		logger.Println("sight words: practice ended early: " + err.Error())
	} else {
		logger.Println("sight words: practice finished on " + esn)
	}
	return err
}

// sightWordsHandler matches the sight words trigger phrases and the follow-up
// commands. It is checked alongside plugins and custom intents in
// ProcessTextAll, and returns true when it took the utterance.
func sightWordsHandler(req interface{}, voiceText string, botSerial string) bool {
	if OrganicSightWordsSessionActive(botSerial) {
		if matchesAnyPhrase(strings.ToLower(strings.TrimSpace(voiceText)), sightWordsStopPhrases) {
			logger.Println("Bot " + botSerial + " stopped an organic sight words conversation")
			IntentPass(req, "intent_imperative_affirmative", voiceText, map[string]string{}, false)
			stopOrganicSightWords(botSerial)
			return true
		}
		logger.Println("Bot " + botSerial + " continuing an organic sight words conversation")
		IntentPass(req, "intent_greeting_hello", voiceText, map[string]string{}, false)
		go ContinueOrganicSightWords(botSerial, voiceText)
		return true
	}
	action, isStart, matched := sightWordsCommand(voiceText, SightWordsSessionActive(botSerial))
	if !matched {
		return false
	}
	if isStart {
		logger.Println("Bot " + botSerial + " matched sight words practice")
		IntentPass(req, "intent_greeting_hello", voiceText, map[string]string{}, false)
		organic := LoadSightWords().OrganicMode
		go func() {
			// the robot needs a moment to finish reacting to the intent
			time.Sleep(time.Millisecond * 200)
			if organic {
				StartOrganicSightWords(botSerial)
			} else {
				StartSightWords(botSerial)
			}
		}()
		return true
	}
	logger.Println("Bot " + botSerial + " matched a sight words session command")
	IntentPass(req, "intent_imperative_affirmative", voiceText, map[string]string{}, false)
	if !sendSightWordsAction(botSerial, action) {
		logger.Println("Bot " + botSerial + " sight words command could not be delivered, the session may have just ended")
	}
	return true
}

// organicSightWordsSession is the state for one robot's organic (LLM
// conversation) sight words practice session. Unlike a rigid session, which
// runs start to finish inside StartSightWords, an organic session is spread
// across several calls: StartOrganicSightWords begins it and each further
// utterance from the child is delivered through ContinueOrganicSightWords.
type organicSightWordsSession struct {
	mu             sync.Mutex
	presenter      *robotSightWordsPresenter
	state          *organicSightWordsState
	conf           llm.Config
	messages       []llm.Message
	ctx            context.Context
	cancel         context.CancelFunc
	releaseControl func()
	idleTimer      *time.Timer
}

var (
	organicSightWordsMutex    sync.Mutex
	organicSightWordsSessions = map[string]*organicSightWordsSession{}
)

// OrganicSightWordsSessionActive reports whether the given robot is in the
// middle of an organic sight words conversation.
func OrganicSightWordsSessionActive(esn string) bool {
	organicSightWordsMutex.Lock()
	defer organicSightWordsMutex.Unlock()
	_, exists := organicSightWordsSessions[esn]
	return exists
}

func getOrganicSightWordsSession(esn string) (*organicSightWordsSession, bool) {
	organicSightWordsMutex.Lock()
	defer organicSightWordsMutex.Unlock()
	session, exists := organicSightWordsSessions[esn]
	return session, exists
}

// StartOrganicSightWords begins one organic sight words conversation on the
// given robot: it grants Vector behavior control, asks the LLM for an opening
// line which weaves in the active sight words, and says it. Further turns of
// the conversation arrive via ContinueOrganicSightWords as the child replies.
func StartOrganicSightWords(esn string) {
	config := LoadSightWords()
	robot, err := getRobot(esn)
	if err != nil {
		logger.Println("sight words: " + err.Error())
		return
	}

	ctx, cancel := context.WithCancel(context.Background())

	if _, err := robot.Conn.BatteryState(ctx, &vectorpb.BatteryStateRequest{}); err != nil {
		logger.Println("sight words: unable to reach robot " + esn + ": " + err.Error())
		cancel()
		return
	}

	if len(config.Words) == 0 {
		presenter := &robotSightWordsPresenter{robot: robot, ctx: ctx}
		presenter.Say(sightWordsEmptyList)
		cancel()
		return
	}

	actions, claimed := registerSightWordsSession(esn)
	if !claimed {
		logger.Println("sight words: " + esn + " is already practicing")
		cancel()
		return
	}

	start := make(chan bool, 1)
	stop := make(chan bool, 1)
	BControl(robot, ctx, start, stop)
	select {
	case <-start:
	case <-time.After(sightWordsControlTimeout):
		select {
		case stop <- true:
		default:
		}
		unregisterSightWordsSession(esn)
		cancel()
		logger.Println("sight words: timed out waiting for behavior control")
		return
	}

	session := &organicSightWordsSession{
		presenter: &robotSightWordsPresenter{robot: robot, ctx: ctx},
		state:     newOrganicSightWordsState(config.Words),
		conf:      LLMConfig(),
		ctx:       ctx,
		cancel:    cancel,
		releaseControl: func() {
			select {
			case stop <- true:
			default:
			}
		},
	}

	logger.Println("sight words: starting organic practice on " + esn + " with " + strconv.Itoa(len(config.Words)) + " words")
	logger.LogUI("Organic sight words practice started on " + esn)

	// The opening reply is generated before the session is registered, so
	// that if the child speaks before it comes back, ContinueOrganicSightWords
	// (which requires a registered session) can't run ahead of it and get the
	// conversation history out of order.
	reply, err := session.conf.Complete(ctx, llm.Request{
		System: organicSightWordsSystemPrompt(session.state.remaining()),
	})
	if err != nil || strings.TrimSpace(reply) == "" {
		if err != nil {
			logger.Println("sight words: organic opening failed: " + err.Error())
		} else {
			logger.Println("sight words: organic opening returned no text")
		}
		session.presenter.Say(sightWordsOrganicLLMError)
		session.presenter.Clear()
		unregisterSightWordsSession(esn)
		cancel()
		session.releaseControl()
		return
	}

	session.mu.Lock()
	session.state.markSpoken(reply)
	session.messages = append(session.messages, llm.Message{Role: llm.RoleAssistant, Content: reply})
	done := session.state.allCovered()
	session.mu.Unlock()

	if !done {
		// Only now, with the opening reply already recorded, does the
		// session accept further turns: registering any earlier would let
		// ContinueOrganicSightWords race ahead of this reply and record the
		// child's turn before Vector's opening line.
		organicSightWordsMutex.Lock()
		organicSightWordsSessions[esn] = session
		organicSightWordsMutex.Unlock()

		session.idleTimer = time.AfterFunc(organicSightWordsIdleTimeout, func() {
			logger.Println("sight words: organic conversation with " + esn + " timed out")
			endOrganicSightWords(esn)
		})

		// the shared session registry is only used here to detect "stop
		// sight words" and to know a session is active; next/repeat don't
		// apply to an organic conversation and are ignored.
		go func() {
			for {
				select {
				case action, ok := <-actions:
					if !ok {
						return
					}
					if action == sightWordsStop {
						endOrganicSightWords(esn)
						return
					}
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	session.presenter.Say(reply)
	if done {
		session.presenter.Say(sightWordsOrganicDone)
		session.presenter.Clear()
		unregisterSightWordsSession(esn)
		cancel()
		session.releaseControl()
	}
}

// ContinueOrganicSightWords delivers one more thing the child said to a
// running organic session, gets the LLM's reply and has Vector say it,
// ending the session once every active word has come up.
func ContinueOrganicSightWords(esn string, voiceText string) {
	session, ok := getOrganicSightWordsSession(esn)
	if !ok {
		return
	}

	session.mu.Lock()
	select {
	case <-session.ctx.Done():
		session.mu.Unlock()
		return
	default:
	}
	if session.idleTimer != nil {
		session.idleTimer.Reset(organicSightWordsIdleTimeout)
	}
	session.state.markSpoken(voiceText)
	session.messages = append(session.messages, llm.Message{Role: llm.RoleUser, Content: voiceText})
	remaining := session.state.remaining()
	messages := append([]llm.Message{}, session.messages...)
	conf := session.conf
	ctx := session.ctx
	session.mu.Unlock()

	reply, err := conf.Complete(ctx, llm.Request{
		System:   organicSightWordsSystemPrompt(remaining),
		Messages: messages,
	})
	if err != nil || strings.TrimSpace(reply) == "" {
		if err != nil {
			logger.Println("sight words: organic reply failed: " + err.Error())
		} else {
			logger.Println("sight words: organic reply returned no text")
		}
		return
	}

	session.mu.Lock()
	session.state.markSpoken(reply)
	session.messages = append(session.messages, llm.Message{Role: llm.RoleAssistant, Content: reply})
	done := session.state.allCovered()
	session.mu.Unlock()

	session.presenter.Say(reply)
	if done {
		session.presenter.Say(sightWordsOrganicDone)
		endOrganicSightWords(esn)
	}
}

// stopOrganicSightWords ends a session in response to an explicit "stop
// sight words" from the child, saying goodbye first.
func stopOrganicSightWords(esn string) {
	session, ok := getOrganicSightWordsSession(esn)
	if !ok {
		return
	}
	session.presenter.Say(sightWordsStopped)
	endOrganicSightWords(esn)
}

// endOrganicSightWords tears down a session: it stops the idle timer,
// cancels the context, gives behavior control back and frees the slot in
// both the organic and shared session registries.
func endOrganicSightWords(esn string) {
	organicSightWordsMutex.Lock()
	session, exists := organicSightWordsSessions[esn]
	if exists {
		delete(organicSightWordsSessions, esn)
	}
	organicSightWordsMutex.Unlock()
	if !exists {
		return
	}
	if session.idleTimer != nil {
		session.idleTimer.Stop()
	}
	session.presenter.Clear()
	session.cancel()
	session.releaseControl()
	unregisterSightWordsSession(esn)
	logger.Println("sight words: organic practice ended on " + esn)
}
