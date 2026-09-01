package wirepod_ttr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	"github.com/kercre123/wire-pod/chipper/pkg/wirepod/llm"
)

func GetChat(esn string) vars.RememberedChat {
	for _, chat := range vars.RememberedChats {
		if chat.ESN == esn {
			return chat
		}
	}
	return vars.RememberedChat{
		ESN: esn,
	}
}

func PlaceChat(chat vars.RememberedChat) {
	for i, achat := range vars.RememberedChats {
		if achat.ESN == chat.ESN {
			vars.RememberedChats[i] = chat
			return
		}
	}
	vars.RememberedChats = append(vars.RememberedChats, chat)
}

// remember last 16 lines of chat
func Remember(user, ai llm.Message, esn string) {
	chatAppend := []llm.Message{
		user,
		ai,
	}
	currentChat := GetChat(esn)
	if len(currentChat.Chats) == 16 {
		var newChat vars.RememberedChat
		newChat.ESN = currentChat.ESN
		for i, chat := range currentChat.Chats {
			if i < 2 {
				continue
			}
			newChat.Chats = append(newChat.Chats, chat)
		}
		currentChat = newChat
	}
	currentChat.ESN = esn
	currentChat.Chats = append(currentChat.Chats, chatAppend...)
	PlaceChat(currentChat)
}

func isMn(r rune) bool {
	// Remove the characters that are not related to Vietnamese.
	// Retain the tonal marks and diacritics such as the circumflex, ơ, and ư in Vietnamese.
	keepMarks := []rune{
		'\u0300', // Dấu huyền
		'\u0301', // Dấu sắc
		'\u0303', // Dấu ngã
		'\u0309', // Dấu hỏi
		'\u0323', // Dấu nặng
		'\u0302', // Dấu mũ (â, ê, ô)
		'\u031B', // Dấu ơ và ư
		'\u0306', // Dấu trầm
	}
	if unicode.Is(unicode.Mn, r) {
		for _, mark := range keepMarks {
			if r == mark {
				return false
			}
		}
		return true
	}
	return false
}

func removeSpecialCharacters(str string) string {

	// these two lines create a transformation that decomposes characters, removes non-spacing marks (like diacritics), and then recomposes the characters, effectively removing special characters
	t := transform.Chain(norm.NFD, transform.RemoveFunc(isMn), norm.NFC)
	result, _, _ := transform.String(t, str)

	// Define the regular expression to match special characters
	re := regexp.MustCompile(`[&^*#@]`)

	// Replace special characters with an empty string
	result = removeEmojis(re.ReplaceAllString(result, ""))

	// Replace special characters with ASCII
	// * COPY/PASTE TO ADD MORE CHARACTERS:
	//   result = strings.ReplaceAll(result, "", "")
	result = strings.ReplaceAll(result, "‘", "'")
	result = strings.ReplaceAll(result, "’", "'")
	result = strings.ReplaceAll(result, "“", "\"")
	result = strings.ReplaceAll(result, "”", "\"")
	result = strings.ReplaceAll(result, "—", "-")
	result = strings.ReplaceAll(result, "–", "-")
	result = strings.ReplaceAll(result, "…", "...")
	result = strings.ReplaceAll(result, "\u00A0", " ")
	result = strings.ReplaceAll(result, "•", "*")
	result = strings.ReplaceAll(result, "¼", "1/4")
	result = strings.ReplaceAll(result, "½", "1/2")
	result = strings.ReplaceAll(result, "¾", "3/4")
	result = strings.ReplaceAll(result, "×", "x")
	result = strings.ReplaceAll(result, "÷", "/")
	result = strings.ReplaceAll(result, "ç", "c")
	result = strings.ReplaceAll(result, "©", "(c)")
	result = strings.ReplaceAll(result, "®", "(r)")
	result = strings.ReplaceAll(result, "™", "(tm)")
	result = strings.ReplaceAll(result, "@", "(a)")
	result = strings.ReplaceAll(result, " AI ", " A. I. ")
	return result
}

func removeEmojis(input string) string {
	// a mess, but it works!
	re := regexp.MustCompile(`[\x{1F600}-\x{1F64F}]|[\x{1F300}-\x{1F5FF}]|[\x{1F680}-\x{1F6FF}]|[\x{1F1E0}-\x{1F1FF}]|[\x{2600}-\x{26FF}]|[\x{2700}-\x{27BF}]|[\x{1F900}-\x{1F9FF}]|[\x{1F004}]|[\x{1F0CF}]|[\x{1F18E}]|[\x{1F191}-\x{1F251}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]|[\x{1F004}-\x{1F0CF}]|[\x{1F191}-\x{1F251}]|[\x{2B50}]`)
	result := re.ReplaceAllString(input, "")
	return result
}

// LLMConfig builds the LLM interface configuration from wire-pod's knowledge
// settings. The wire format (OpenAI / Anthropic / Gemini) is picked from the
// model name unless the user pinned one, which matches how apimodels.app
// routes requests on its unified endpoint.
func LLMConfig() llm.Config {
	k := vars.APIConfig.Knowledge
	return llm.Config{
		Provider:    k.Provider,
		Endpoint:    k.Endpoint,
		Key:         k.Key,
		Model:       k.Model,
		Format:      k.APIFormat,
		Temperature: k.Temperature,
		TopP:        k.TopP,
	}
}

// CreateAIReq builds the request which is sent to the LLM.
func CreateAIReq(transcribedText, esn string, isKG bool) (llm.Config, llm.Request) {
	defaultPrompt := "You are a helpful, animated robot called Vector. Keep the response concise yet informative."

	conf := LLMConfig()

	system := strings.TrimSpace(vars.APIConfig.Knowledge.OpenAIPrompt)
	if system == "" {
		system = defaultPrompt
	}
	system = CreatePrompt(system, conf.ModelName(), isKG)

	var messages []llm.Message
	if vars.APIConfig.Knowledge.SaveChat {
		rchat := GetChat(esn)
		logger.Println("Using remembered chats, length of " + fmt.Sprint(len(rchat.Chats)) + " messages")
		messages = append(messages, rchat.Chats...)
	}
	messages = append(messages, llm.Message{
		Role:    llm.RoleUser,
		Content: transcribedText,
	})

	logger.Println("Using " + conf.ModelName() + " (" + conf.WireFormat() + " API format) at " + conf.URL(conf.WireFormat()))

	return conf, llm.Request{
		System:   system,
		Messages: messages,
	}
}

// streamSegments consumes an LLM stream and pushes speakable segments onto the
// returned channel, which is closed once the response is complete. The full
// response text is sent on the second channel afterwards.
func streamSegments(stream llm.Stream) (<-chan string, <-chan string) {
	segments := make(chan string, 64)
	full := make(chan string, 1)
	go func() {
		defer close(full)
		defer close(segments)
		defer stream.Close()
		var seg segmenter
		var complete strings.Builder
		for {
			text, err := stream.Recv()
			if text != "" {
				text = removeSpecialCharacters(text)
				complete.WriteString(text)
				for _, s := range seg.Add(text) {
					segments <- s
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					logger.Println("LLM stream error: " + err.Error())
				}
				break
			}
		}
		for _, s := range seg.Flush() {
			segments <- s
		}
		full <- strings.TrimSpace(complete.String())
	}()
	return segments, full
}

func getRobot(esn string) (*vector.Vector, error) {
	for _, bot := range vars.BotInfo.Robots {
		if esn == bot.Esn {
			return vector.New(
				vector.WithSerialNo(esn),
				vector.WithToken(bot.GUID),
				vector.WithTarget(bot.IPAddress+":443"),
			)
		}
	}
	return nil, errors.New("robot " + esn + " is not authenticated with this wire-pod instance")
}

func StreamingKGSim(req interface{}, esn string, transcribedText string, isKG bool) (string, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	robot, err := getRobot(esn)
	if err != nil {
		return "", err
	}
	if _, err := robot.Conn.BatteryState(ctx, &vectorpb.BatteryStateRequest{}); err != nil {
		return "", err
	}

	// Buffered so that no signal can ever block (and therefore leak) a
	// goroutine, which is what used to happen when the LLM errored before
	// behavior control was granted.
	start := make(chan bool, 1)
	stop := make(chan bool, 1)
	stopStop := make(chan bool, 1)

	var searchingStopped sync.WaitGroup
	searchingCtx, stopSearching := context.WithCancel(ctx)
	defer stopSearching()

	if isKG {
		BControl(robot, ctx, start, stop)
		searchingStopped.Add(1)
		go func() {
			defer searchingStopped.Done()
			for {
				select {
				case <-searchingCtx.Done():
					return
				default:
				}
				robot.Conn.PlayAnimation(ctx, &vectorpb.PlayAnimationRequest{
					Animation: &vectorpb.Animation{
						Name: "anim_knowledgegraph_searching_01",
					},
					Loops: 1,
				})
				time.Sleep(time.Second / 3)
			}
		}()
	}

	// releaseControl stops the "thinking" animation and hands behavior
	// control back to the robot. Safe to call more than once.
	var releaseOnce sync.Once
	releaseControl := func() {
		releaseOnce.Do(func() {
			stopSearching()
			searchingStopped.Wait()
			select {
			case stop <- true:
			default:
			}
		})
	}

	conf, aireq := CreateAIReq(transcribedText, esn, isKG)
	stream, err := conf.Stream(ctx, aireq)
	if err != nil {
		log.Printf("Error creating chat completion stream: %v", err)
		logger.Println("LLM error: " + err.Error())
		logger.LogUI("LLM error: " + err.Error())
		if isKG {
			releaseControl()
			time.Sleep(time.Second / 3)
			KGSim(esn, "There was an error getting data from the L. L. M.")
		}
		return "", err
	}

	segments, fullText := streamSegments(stream)

	// Wait for the first speakable segment before taking over the robot.
	firstSegment, gotSegment := <-segments
	if !gotSegment {
		logger.Println("LLM returned no response")
		logger.LogUI("LLM returned no response for " + esn)
		if isKG {
			releaseControl()
			time.Sleep(time.Second / 3)
			KGSim(esn, "There was an error getting data from the L. L. M.")
		}
		return "", errors.New("llm returned no response")
	}

	// Log and remember the conversation once the whole response arrived.
	go func() {
		text, ok := <-fullText
		if !ok || text == "" {
			return
		}
		logger.LogUI("LLM response for " + esn + ": " + text)
		logger.Println("LLM stream finished")
		if vars.APIConfig.Knowledge.SaveChat {
			Remember(
				llm.Message{Role: llm.RoleUser, Content: transcribedText},
				llm.Message{Role: llm.RoleAssistant, Content: text},
				esn,
			)
		}
	}()

	if !isKG {
		IntentPass(req, "intent_greeting_hello", transcribedText, map[string]string{}, false)
		time.Sleep(time.Millisecond * 200)
		BControl(robot, ctx, start, stop)
	}

	var interrupted atomic.Bool
	go func() {
		interrupted.Store(InterruptKGSimWhenTouchedOrWaked(robot, stop, stopStop))
	}()

	var TTSLoopAnimation string
	var TTSGetinAnimation string
	if isKG {
		TTSLoopAnimation = "anim_knowledgegraph_answer_01"
		TTSGetinAnimation = "anim_knowledgegraph_searching_getout_01"
	} else {
		TTSLoopAnimation = "anim_tts_loop_02"
		TTSGetinAnimation = "anim_getin_tts_01"
	}

	// Wait for behavior control. If it is never granted, give up instead of
	// blocking forever.
	select {
	case <-start:
	case <-time.After(time.Second * 15):
		releaseControl()
		return "", errors.New("timed out waiting for behavior control")
	}

	if isKG {
		stopSearching()
		searchingStopped.Wait()
	} else {
		time.Sleep(time.Millisecond * 300)
	}

	robot.Conn.PlayAnimation(
		ctx,
		&vectorpb.PlayAnimationRequest{
			Animation: &vectorpb.Animation{
				Name: TTSGetinAnimation,
			},
			Loops: 1,
		},
	)

	ttsLoopCtx, stopTTSLoop := context.WithCancel(ctx)
	var ttsLoopStopped sync.WaitGroup
	if !vars.APIConfig.Knowledge.CommandsEnable {
		ttsLoopStopped.Add(1)
		go func() {
			defer ttsLoopStopped.Done()
			for {
				select {
				case <-ttsLoopCtx.Done():
					return
				default:
				}
				robot.Conn.PlayAnimation(
					ctx,
					&vectorpb.PlayAnimationRequest{
						Animation: &vectorpb.Animation{
							Name: TTSLoopAnimation,
						},
						Loops: 1,
					},
				)
			}
		}()
	}

	convo := append([]llm.Message{}, aireq.Messages...)
	convo = append(convo, llm.Message{Role: llm.RoleAssistant})

	// Make sure the stream reader can always finish, even if speaking is
	// interrupted, so its goroutine never leaks.
	defer func() {
		go func() {
			for range segments {
			}
		}()
	}()

	segment := firstSegment
	for {
		if interrupted.Load() {
			break
		}
		logger.Println(segment)
		convo[len(convo)-1].Content = strings.TrimSpace(convo[len(convo)-1].Content + " " + segment)
		if PerformActions(convo, GetActionsFromString(segment), robot, stopStop) {
			break
		}
		next, ok := <-segments
		if !ok {
			break
		}
		segment = next
	}

	stopTTSLoop()
	ttsLoopStopped.Wait()
	time.Sleep(time.Millisecond * 100)

	if !interrupted.Load() {
		select {
		case stopStop <- true:
		default:
		}
	}
	releaseControl()
	return "", nil
}

func KGSim(esn string, textToSay string) error {
	ctx := context.Background()
	robot, err := getRobot(esn)
	if err != nil {
		return err
	}
	controlRequest := &vectorpb.BehaviorControlRequest{
		RequestType: &vectorpb.BehaviorControlRequest_ControlRequest{
			ControlRequest: &vectorpb.ControlRequest{
				Priority: vectorpb.ControlRequest_OVERRIDE_BEHAVIORS,
			},
		},
	}
	go func() {
		start := make(chan bool, 1)
		stop := make(chan bool, 1)

		go func() {
			// * begin - modified from official vector-go-sdk
			r, err := robot.Conn.BehaviorControl(
				ctx,
			)
			if err != nil {
				log.Println(err)
				return
			}

			if err := r.Send(controlRequest); err != nil {
				log.Println(err)
				return
			}

			for {
				ctrlresp, err := r.Recv()
				if err != nil {
					log.Println(err)
					return
				}
				if ctrlresp.GetControlGrantedResponse() != nil {
					start <- true
					break
				}
			}

			<-stop
			logger.Println("KGSim: releasing behavior control")
			if err := r.Send(
				&vectorpb.BehaviorControlRequest{
					RequestType: &vectorpb.BehaviorControlRequest_ControlRelease{
						ControlRelease: &vectorpb.ControlRelease{},
					},
				},
			); err != nil {
				log.Println(err)
			}
			// * end - modified from official vector-go-sdk
		}()

		select {
		case <-start:
		case <-time.After(time.Second * 15):
			logger.Println("KGSim: timed out waiting for behavior control")
			return
		}
		time.Sleep(time.Millisecond * 300)
		robot.Conn.PlayAnimation(
			ctx,
			&vectorpb.PlayAnimationRequest{
				Animation: &vectorpb.Animation{
					Name: "anim_getin_tts_01",
				},
				Loops: 1,
			},
		)
		ttsLoopCtx, stopTTSLoop := context.WithCancel(ctx)
		var ttsLoopStopped sync.WaitGroup
		ttsLoopStopped.Add(1)
		go func() {
			defer ttsLoopStopped.Done()
			for {
				select {
				case <-ttsLoopCtx.Done():
					return
				default:
				}
				robot.Conn.PlayAnimation(
					ctx,
					&vectorpb.PlayAnimationRequest{
						Animation: &vectorpb.Animation{
							Name: "anim_tts_loop_02",
						},
						Loops: 1,
					},
				)
			}
		}()
		for _, str := range strings.Split(textToSay, ". ") {
			_, err := robot.Conn.SayText(
				ctx,
				&vectorpb.SayTextRequest{
					Text:           str + ".",
					UseVectorVoice: true,
					DurationScalar: 1.0,
				},
			)
			if err != nil {
				logger.Println("KG SayText error: " + err.Error())
				break
			}
		}
		stopTTSLoop()
		ttsLoopStopped.Wait()
		time.Sleep(time.Millisecond * 100)
		select {
		case stop <- true:
		default:
		}
	}()
	return nil
}
