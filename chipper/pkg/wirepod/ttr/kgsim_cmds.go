package wirepod_ttr

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	"github.com/kercre123/wire-pod/chipper/pkg/wirepod/llm"
	"github.com/sashabaranov/go-openai"
)

const (
	// arg: text to say
	// not a command
	ActionSayText = 0
	// arg: animation name
	ActionPlayAnimation = 1
	// arg: animation name
	ActionPlayAnimationWI = 2
	// arg: now
	ActionGetImage   = 3
	ActionNewRequest = 4
	// arg: sound file
	ActionPlaySound = 4
)

var animationMap [][2]string = [][2]string{
	//"happy, veryHappy, sad, verySad, angry, dartingEyes, confused, thinking, celebrate"
	{
		"happy",
		"anim_onboarding_reacttoface_happy_01",
	},
	{
		"veryHappy",
		"anim_blackjack_victorwin_01",
	},
	{
		"sad",
		"anim_feedback_meanwords_01",
	},
	{
		"verySad",
		"anim_feedback_meanwords_01",
	},
	{
		"angry",
		"anim_rtpickup_loop_10",
	},
	{
		"frustrated",
		"anim_feedback_shutup_01",
	},
	{
		"dartingEyes",
		"anim_observing_self_absorbed_01",
	},
	{
		"confused",
		"anim_meetvictor_lookface_timeout_01",
	},
	{
		"thinking",
		"anim_explorer_scan_short_04",
	},
	{
		"celebrate",
		"anim_pounce_success_03",
	},
	{
		"love",
		"anim_feedback_iloveyou_02",
	},
}

var soundMap [][2]string = [][2]string{
	{
		"drumroll",
		"sounds/drumroll.wav",
	},
}

type RobotAction struct {
	Action    int
	Parameter string
}

type LLMCommand struct {
	Command         string
	Description     string
	ParamChoices    string
	Action          int
	SupportedModels []string
}

// create function which parses from LLM and makes a struct of RobotActions

var ValidLLMCommands []LLMCommand = []LLMCommand{
	{
		Command:         "playAnimationWI",
		Description:     "Plays an animation on the robot without interrupting speech. This should be used FAR more than the playAnimation command. This is great for storytelling and making any normal response animated. Don't put two of these right next to each other. Use this MANY times. The param choices are the only choices you have. You can't create any.",
		ParamChoices:    "happy, veryHappy, sad, verySad, angry, frustrated, dartingEyes, confused, thinking, celebrate, love",
		Action:          ActionPlayAnimationWI,
		SupportedModels: []string{"all"},
	},
	{
		Command:         "playAnimation",
		Description:     "Plays an animation on the robot. This will interrupt speech. Only use this if you are directed to play an animaion.",
		ParamChoices:    "happy, veryHappy, sad, verySad, angry, frustrated, dartingEyes, confused, thinking, celebrate, love",
		Action:          ActionPlayAnimation,
		SupportedModels: []string{"all"},
	},
	{
		Command:     "getImage",
		Description: "Gets an image from the robot's camera and places it in the next message. If you want to do this, tell the user what you are about to do THEN use the command. This command should END a sentence. Your response will be stopped when this command is recognized. If a user says something like 'what do you see', you should assume that you need to take a new photo. Do NOT automatically assume that you are analyzing a previous photo.",
		// not impl yet
		ParamChoices:    "front, lookingUp",
		Action:          ActionGetImage,
		SupportedModels: []string{"all"},
	},
	{
		Command:         "newVoiceRequest",
		Description:     "Starts a new voice command from the robot. Use this if you want more input from the user after your response/if you want to carry out a conversation. Below this, there should be a NOTE telling you whether you are in conversation mode or not. If you are, DONT BE AFRAID TO USE THIS COMMAND! This goes at the end of your response, if you use it.",
		ParamChoices:    "now",
		Action:          ActionNewRequest,
		SupportedModels: []string{"all"},
	},
	// {
	// 	Command:      "playSound",
	// 	Description:  "Plays a sound on the robot.",
	// 	ParamChoices: "drumroll",
	// 	Action:       ActionPlaySound,
	// },
}

func ModelIsSupported(cmd LLMCommand, model string) bool {
	for _, str := range cmd.SupportedModels {
		if str == "all" || str == model {
			return true
		}
	}
	return false
}

func CreatePrompt(origPrompt string, model string, isKG bool) string {
	prompt := origPrompt + "\n\n" + "Keep in mind, user input comes from speech-to-text software, so respond accordingly. No special characters, especially these: & ^ * # @ - . No lists. No formatting."
	if vars.APIConfig.Knowledge.CommandsEnable {
		prompt = prompt + "\n\n" + "You are running ON an Anki Vector robot. You have a set of commands. If you include an emoji, I will make you start over. If you want to use a command but it doesn't exist or your desired parameter isn't in the list, avoid using the command. The format is {{command||parameter}}. You can embed these in sentences. Example: \"User: How are you feeling? | Response: \"{{playAnimationWI||sad}} I'm feeling sad...\". Square brackets ([]) are not valid.\n\nUse the playAnimation or playAnimationWI commands if you want to express emotion! You are very animated and good at following instructions. Animation takes precendence over words. You are to include many animations in your response.\n\nHere is every valid command:"
		for _, cmd := range ValidLLMCommands {
			if ModelIsSupported(cmd, model) {
				promptAppendage := "\n\nCommand Name: " + cmd.Command + "\nDescription: " + cmd.Description + "\nParameter choices: " + cmd.ParamChoices
				prompt = prompt + promptAppendage
			}
		}
		if isKG && vars.APIConfig.Knowledge.SaveChat {
			promptAppentage := "\n\nNOTE: You are in 'conversation' mode. If you ask the user a question near the end of your response, you MUST use newVoiceRequest. If you decide you want to end the conversation, you should not use it."
			prompt = prompt + promptAppentage
		} else {
			promptAppentage := "\n\nNOTE: You are NOT in 'conversation' mode. Refrain from asking the user any questions and from using newVoiceRequest."
			prompt = prompt + promptAppentage
		}
	}
	if os.Getenv("DEBUG_PRINT_PROMPT") == "true" {
		logger.Println(prompt)
	}
	return prompt
}

// GetActionsFromString converts an LLM response segment into a list of robot
// actions. Text outside of {{command||parameter}} blocks becomes speech.
//
// The parser is deliberately forgiving: models regularly drop the parameter,
// use a single pipe, add whitespace/newlines inside the block or change the
// capitalisation of the command. None of that may crash wire-pod or silence
// the robot.
func GetActionsFromString(input string) []RobotAction {
	var actions []RobotAction
	addSayText := func(text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		actions = append(actions, RobotAction{
			Action:    ActionSayText,
			Parameter: text,
		})
	}
	rest := input
	for {
		openIdx := strings.Index(rest, "{{")
		if openIdx == -1 {
			addSayText(rest)
			break
		}
		addSayText(rest[:openIdx])
		afterOpen := rest[openIdx+2:]
		closeIdx := strings.Index(afterOpen, "}}")
		if closeIdx == -1 {
			// unterminated block: speak whatever came after it so no
			// content is lost
			addSayText(strings.TrimLeft(afterOpen, "{"))
			break
		}
		if action := parseCommandBlock(afterOpen[:closeIdx]); action.Action != -1 {
			actions = append(actions, action)
		}
		rest = afterOpen[closeIdx+2:]
	}
	return actions
}

// parseCommandBlock parses the inside of a {{ }} block.
func parseCommandBlock(block string) RobotAction {
	block = strings.TrimSpace(strings.ReplaceAll(block, "\n", " "))
	var cmd, param string
	if idx := strings.Index(block, "||"); idx != -1 {
		cmd = block[:idx]
		param = block[idx+2:]
	} else if idx := strings.IndexAny(block, "|:="); idx != -1 {
		cmd = block[:idx]
		param = block[idx+1:]
	} else {
		cmd = block
	}
	return CmdParamToAction(strings.TrimSpace(cmd), strings.TrimSpace(param))
}

func CmdParamToAction(cmd, param string) RobotAction {
	for _, command := range ValidLLMCommands {
		if strings.EqualFold(cmd, command.Command) {
			return RobotAction{
				Action:    command.Action,
				Parameter: param,
			}
		}
	}
	logger.Println("LLM tried to do a command which doesn't exist: " + cmd + " (param: " + param + ")")
	return RobotAction{
		Action: -1,
	}
}

func DoPlayAnimation(animation string, robot *vector.Vector) error {
	for _, animThing := range animationMap {
		if animation == animThing[0] {
			StartAnim_Queue(robot.Cfg.SerialNo)
			robot.Conn.PlayAnimation(
				context.Background(),
				&vectorpb.PlayAnimationRequest{
					Animation: &vectorpb.Animation{
						Name: animThing[1],
					},
					Loops: 1,
				},
			)
			StopAnim_Queue(robot.Cfg.SerialNo)
			return nil
		}
	}
	logger.Println("Animation provided by LLM doesn't exist: " + animation)
	return nil
}

func DoPlayAnimationWI(animation string, robot *vector.Vector) error {
	for _, animThing := range animationMap {
		if animation == animThing[0] {
			go func() {
				StartAnim_Queue(robot.Cfg.SerialNo)
				robot.Conn.PlayAnimation(
					context.Background(),
					&vectorpb.PlayAnimationRequest{
						Animation: &vectorpb.Animation{
							Name: animThing[1],
						},
						Loops: 1,
					},
				)
				StopAnim_Queue(robot.Cfg.SerialNo)
			}()
			return nil
		}
	}
	logger.Println("Animation provided by LLM doesn't exist: " + animation)
	return nil
}

func DoPlaySound(sound string, robot *vector.Vector) error {
	for _, soundThing := range soundMap {
		if sound == soundThing[0] {
			logger.Println("Would play sound")
		}
	}
	logger.Println("Sound provided by LLM doesn't exist: " + sound)
	return nil
}

func DoSayText(input string, robot *vector.Vector) error {

	// just before vector speaks
	input = removeSpecialCharacters(input)
	if strings.TrimSpace(input) == "" {
		return nil
	}

	// OpenAI TTS always talks to api.openai.com, so it can only be used when
	// the configured key is an OpenAI key.
	if vars.APIConfig.Knowledge.Provider == "openai" &&
		(vars.APIConfig.STT.Language != "en-US" || vars.APIConfig.Knowledge.OpenAIVoiceWithEnglish) {
		if err := DoSayText_OpenAI(robot, input); err != nil {
			logger.Println("OpenAI voice failed, falling back to Vector's voice: " + err.Error())
		} else {
			return nil
		}
	}
	_, err := robot.Conn.SayText(
		context.Background(),
		&vectorpb.SayTextRequest{
			Text:           input,
			UseVectorVoice: true,
			DurationScalar: 0.95,
		},
	)
	if err != nil {
		logger.Println("SayText error: " + err.Error())
	}
	return err
}

func pcmLength(data []byte) time.Duration {
	bytesPerSample := 2
	sampleRate := 16000
	numSamples := len(data) / bytesPerSample
	duration := time.Duration(numSamples*1000/sampleRate) * time.Millisecond
	return duration
}

func getOpenAIVoice(voice string) openai.SpeechVoice {
	voiceMap := map[string]openai.SpeechVoice{
		"alloy":   openai.VoiceAlloy,
		"onyx":    openai.VoiceOnyx,
		"fable":   openai.VoiceFable,
		"shimmer": openai.VoiceShimmer,
		"nova":    openai.VoiceNova,
		"echo":    openai.VoiceEcho,
		"":        openai.VoiceFable,
	}
	return voiceMap[voice]
}

// TODO
func DoSayText_OpenAI(robot *vector.Vector, input string) error {
	if strings.TrimSpace(input) == "" {
		return nil
	}
	openaiVoice := getOpenAIVoice(vars.APIConfig.Knowledge.OpenAIVoice)
	// if vars.APIConfig.Knowledge.OpenAIVoice == "" {
	// 	openaiVoice = openai.VoiceFable
	// } else {
	// 	openaiVoice = getOpenAIVoice(vars.APIConfig.Knowledge.OpenAIPrompt)
	// }
	oc := openai.NewClient(vars.APIConfig.Knowledge.Key)
	resp, err := oc.CreateSpeech(context.Background(), openai.CreateSpeechRequest{
		Model:          openai.TTSModel1,
		Input:          input,
		Voice:          openaiVoice,
		ResponseFormat: openai.SpeechResponseFormatPcm,
	})
	if err != nil {
		logger.Println(err)
		return err
	}
	speechBytes, _ := io.ReadAll(resp)
	vclient, err := robot.Conn.ExternalAudioStreamPlayback(context.Background())
	if err != nil {
		return err
	}
	vclient.Send(&vectorpb.ExternalAudioStreamRequest{
		AudioRequestType: &vectorpb.ExternalAudioStreamRequest_AudioStreamPrepare{
			AudioStreamPrepare: &vectorpb.ExternalAudioStreamPrepare{
				AudioFrameRate: 16000,
				AudioVolume:    100,
			},
		},
	})
	//time.Sleep(time.Millisecond * 30)
	audioChunks := downsample24kTo16k(speechBytes)

	var chunksToDetermineLength []byte
	for _, chunk := range audioChunks {
		chunksToDetermineLength = append(chunksToDetermineLength, chunk...)
	}
	go func() {
		for _, chunk := range audioChunks {
			vclient.Send(&vectorpb.ExternalAudioStreamRequest{
				AudioRequestType: &vectorpb.ExternalAudioStreamRequest_AudioStreamChunk{
					AudioStreamChunk: &vectorpb.ExternalAudioStreamChunk{
						AudioChunkSizeBytes: 1024,
						AudioChunkSamples:   chunk,
					},
				},
			})
			time.Sleep(time.Millisecond * 25)
		}
		vclient.Send(&vectorpb.ExternalAudioStreamRequest{
			AudioRequestType: &vectorpb.ExternalAudioStreamRequest_AudioStreamComplete{
				AudioStreamComplete: &vectorpb.ExternalAudioStreamComplete{},
			},
		})
	}()
	time.Sleep(pcmLength(chunksToDetermineLength) + (time.Millisecond * 50))
	return nil
}

func DoGetImage(msgs []llm.Message, param string, robot *vector.Vector, stopStop chan bool) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopImaging := false
	go func() {
		select {
		case <-stopStop:
			stopImaging = true
			cancel()
		case <-ctx.Done():
		}
	}()
	logger.Println("Get image here...")
	// get image
	robot.Conn.EnableMirrorMode(context.Background(), &vectorpb.EnableMirrorModeRequest{
		Enable: true,
	})
	for i := 3; i > 0; i-- {
		if stopImaging {
			return
		}
		time.Sleep(time.Millisecond * 300)
		robot.Conn.SayText(
			context.Background(),
			&vectorpb.SayTextRequest{
				Text:           fmt.Sprint(i),
				UseVectorVoice: true,
				DurationScalar: 1.05,
			},
		)
		if stopImaging {
			return
		}
	}
	resp, err := robot.Conn.CaptureSingleImage(
		context.Background(),
		&vectorpb.CaptureSingleImageRequest{
			EnableHighResolution: true,
		},
	)
	robot.Conn.EnableMirrorMode(
		context.Background(),
		&vectorpb.EnableMirrorModeRequest{
			Enable: false,
		},
	)
	if err != nil || resp == nil {
		logger.Println("Couldn't get an image from the robot")
		return
	}
	go func() {
		robot.Conn.PlayAnimation(
			context.Background(),
			&vectorpb.PlayAnimationRequest{
				Animation: &vectorpb.Animation{
					Name: "anim_photo_shutter_01",
				},
				Loops: 1,
			},
		)
	}()

	// add the image to the conversation. Every supported API format has its
	// own way of carrying an image; the llm package handles that.
	msgs = append(msgs, llm.Message{
		Role:     llm.RoleUser,
		ImageB64: base64.StdEncoding.EncodeToString(resp.Data),
	})

	if stopImaging {
		return
	}

	conf := LLMConfig()
	stream, err := conf.Stream(ctx, llm.Request{
		System:   imagePromptSystem(conf, msgs),
		Messages: msgs,
	})
	if err != nil {
		logger.Println("LLM error: " + err.Error())
		logger.LogUI("LLM error: " + err.Error())
		return
	}

	segments, fullText := streamSegments(stream)
	go func() {
		text, ok := <-fullText
		if !ok || text == "" {
			return
		}
		logger.LogUI("LLM response for " + robot.Cfg.SerialNo + ": " + text)
		logger.Println("LLM stream finished")
		if vars.APIConfig.Knowledge.SaveChat && len(msgs) > 0 {
			Remember(msgs[len(msgs)-1],
				llm.Message{Role: llm.RoleAssistant, Content: text},
				robot.Cfg.SerialNo)
		}
	}()

	for segment := range segments {
		if stopImaging {
			return
		}
		logger.Println(segment)
		if PerformActions(msgs, GetActionsFromString(segment), robot, stopStop) {
			return
		}
	}
}

// imagePromptSystem rebuilds the system prompt for the follow-up request which
// analyses a freshly taken photo. The first message of a conversation is the
// system prompt only when it was stored as such.
func imagePromptSystem(conf llm.Config, msgs []llm.Message) string {
	prompt := strings.TrimSpace(vars.APIConfig.Knowledge.OpenAIPrompt)
	if prompt == "" {
		prompt = "You are a helpful, animated robot called Vector. Keep the response concise yet informative."
	}
	for _, m := range msgs {
		if m.Role == llm.RoleSystem && strings.TrimSpace(m.Content) != "" {
			return m.Content
		}
	}
	return CreatePrompt(prompt, conf.ModelName(), true)
}

func DoNewRequest(robot *vector.Vector) {
	time.Sleep(time.Second / 3)
	robot.Conn.AppIntent(context.Background(), &vectorpb.AppIntentRequest{Intent: "knowledge_question"})
}

func PerformActions(msgs []llm.Message, actions []RobotAction, robot *vector.Vector, stopStop chan bool) bool {
	// assuming we have behavior control already
	var stopMutex sync.Mutex
	stopPerforming := false
	isStopped := func() bool {
		stopMutex.Lock()
		defer stopMutex.Unlock()
		return stopPerforming
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-stopStop:
			stopMutex.Lock()
			stopPerforming = true
			stopMutex.Unlock()
		case <-done:
		}
	}()
	for _, action := range actions {
		if isStopped() {
			return false
		}
		switch action.Action {
		case ActionSayText:
			DoSayText(action.Parameter, robot)
		case ActionPlayAnimation:
			DoPlayAnimation(action.Parameter, robot)
		case ActionPlayAnimationWI:
			DoPlayAnimationWI(action.Parameter, robot)
		case ActionNewRequest:
			go DoNewRequest(robot)
			return true
		case ActionGetImage:
			DoGetImage(msgs, action.Parameter, robot, stopStop)
			return true
		}
	}
	WaitForAnim_Queue(robot.Cfg.SerialNo)
	return false
}

// The animation queue makes sure only one animation plays at a time per robot
// and that speech waits for a "without interrupt" animation to finish.
type AnimationQueue struct {
	ESN                  string
	AnimDone             chan bool
	AnimCurrentlyPlaying bool
}

var AnimationQueues []AnimationQueue
var animationQueuesMutex sync.Mutex

// animQueue returns the index of the queue for esn, creating it if needed.
// animationQueuesMutex must be held.
func animQueueIndex(esn string) int {
	for i, q := range AnimationQueues {
		if q.ESN == esn {
			return i
		}
	}
	AnimationQueues = append(AnimationQueues, AnimationQueue{
		ESN:      esn,
		AnimDone: make(chan bool, 1),
	})
	return len(AnimationQueues) - 1
}

// waitForAnimDone blocks until the currently playing animation is done, or
// until the timeout expires so a dropped completion signal can never hang the
// response.
func waitForAnimDone(esn string) {
	animationQueuesMutex.Lock()
	i := animQueueIndex(esn)
	if !AnimationQueues[i].AnimCurrentlyPlaying {
		animationQueuesMutex.Unlock()
		return
	}
	done := AnimationQueues[i].AnimDone
	animationQueuesMutex.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second * 30):
		logger.Println("(gave up waiting for an animation to finish)")
	}
}

func WaitForAnim_Queue(esn string) {
	waitForAnimDone(esn)
}

func StartAnim_Queue(esn string) {
	// if an animation is already playing, wait for it to be done first
	waitForAnimDone(esn)
	animationQueuesMutex.Lock()
	defer animationQueuesMutex.Unlock()
	i := animQueueIndex(esn)
	AnimationQueues[i].AnimCurrentlyPlaying = true
}

func StopAnim_Queue(esn string) {
	animationQueuesMutex.Lock()
	defer animationQueuesMutex.Unlock()
	i := animQueueIndex(esn)
	AnimationQueues[i].AnimCurrentlyPlaying = false
	select {
	case AnimationQueues[i].AnimDone <- true:
	default:
	}
}
