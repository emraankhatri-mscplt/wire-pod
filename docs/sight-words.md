# Sight words practice

Sight words practice turns Vector into a reading buddy: he shows a word on his
screen, says it out loud, waits a few seconds so the child can read it, then
moves on to the next word.

Everything runs on the wire-pod machine and the robot itself. No extra
services, accounts or API keys are needed, and no information about the child
is collected or stored.

## Setup

1. Make sure the robot is set up with wire-pod as usual and is paired for SDK
   access (this is what the web interface's "bot info" section shows). Sight
   words practice uses the same robot connection as the other wire-pod
   features which speak or draw on the screen.
2. Say `Hey Vector, practice sight words` once. If wire-pod has no word list
   yet, it creates `chipper/sightWords.json` with a short example list.
3. Edit the word list, either from the wire-pod web UI's "Sight Words" section
   or by hand-editing that file.

On packaged installs (Windows, macOS, Android) the file lives next to the
other wire-pod configuration files in the user configuration directory, in the
same folder as `apiConfig.json` and `customIntents.json`.

## Customizing the word list from the web UI

The wire-pod web interface (`index.html`) has a "Sight Words" section, next to
"Custom Intents", where you can:

- See the current active word list.
- Add, remove or edit words (one per line, or separated by commas).
- Set `seconds_per_word`.
- Turn organic/LLM conversation mode on or off (see below).

Clicking "Save sight words" writes the changes straight to `sightWords.json`
through the same validation described below (words with digits, punctuation
or more than one word per entry are dropped, and the list is capped at 100
words), so there is no need to hand-edit the file unless you prefer to.

## Customizing the word list by hand

`sightWords.json` looks like this:

```json
{
  "words": ["the", "and", "see", "look", "you", "me", "we", "go", "is", "like"],
  "seconds_per_word": 4,
  "organic_mode": false
}
```

- `words` is the list of words, practiced in the order they are written.
- `seconds_per_word` is how long each word stays on the screen after Vector
  says it in the rigid mode described under Usage below. It defaults to 4
  seconds and is limited to between 1 and 30 seconds.
- `organic_mode` turns on the LLM conversation mode described below. It
  defaults to `false`, which keeps the original rigid show/say/hold sequence.

If you only want to change the words, a plain list also works:

```json
["the", "and", "see"]
```

This bare list form has no way to express `organic_mode`, so it always keeps
rigid mode.

The file is read at the start of every session, so there is no need to restart
wire-pod after editing it.

Notes on the list:

- Words may contain letters, apostrophes and hyphens (`don't` and
  `well-known` are fine). Anything else, such as digits, punctuation or
  several words in one entry, is skipped and noted in the wire-pod log.
- Words longer than 16 characters are skipped, since they cannot be shown
  legibly on the screen.
- At most 100 words are used from one list, so a session always ends.
- If the list ends up empty, Vector says that he has no words yet instead of
  starting a session.

## Usage

Say one of these to start a session:

- `Hey Vector, practice sight words`
- `Hey Vector, start sight words`
- `Hey Vector, let's do sight words`

What happens next depends on `organic_mode`.

### Rigid mode (default)

Vector says "Let's practice sight words!" and, for each word, shows it on his
screen in large white letters and says it out loud. When the list is finished
he says "Great job! That's all the words." and goes back to his normal face.

While a session is running these follow-up commands are understood (each one
still needs the `Hey Vector` wake word, or a press of his back button):

- `next word` / `next one` - move on without waiting
- `repeat word` / `say it again` - show and say the same word again
- `stop sight words` / `stop practicing` / `I'm all done` - end the session

A session never depends on those commands: each word advances on its own after
`seconds_per_word`, so practice always finishes.

### Organic mode (LLM conversation)

When `organic_mode` is turned on, Vector does not show or say each word in
order. Instead, the active sight word list is given to the configured LLM
(the same one used for the knowledge graph/"Hey Vector" chat feature, set up
under Knowledge Graph in the web UI) as instructions to have a short, natural
back-and-forth conversation which gets the child to say or read each word out
loud, rather than reading them out as a rigid list. Vector's opening line
starts the conversation; each thing the child says afterwards (heard the same
way follow-up commands are, with the `Hey Vector` wake word) is sent to the
LLM along with the words still left to cover, and Vector speaks its reply.
Vector keeps track of which words have come up (said either by itself or by
the child) and finishes the session once every active word has been covered,
or if `stop sight words` is said, or after a few minutes without a reply.

Organic mode needs a working LLM connection, the same one used elsewhere in
wire-pod, and will use whatever provider/model/API key is configured for it.

## Notes and limitations

- Vector has to hear the wake word to accept a follow-up command, and while
  wire-pod is driving his screen he is less responsive to it than usual. If a
  follow-up command is missed, simply wait for the word to advance on its own,
  or say `Hey Vector, stop sight words`.
- Words are drawn with a small built-in bitmap font, so no font files or
  extra libraries are needed. The screen is 184x96 pixels, so short words fill
  the screen while longer words are drawn smaller. Drawing uses
  the robot's `DisplayFaceImageRGB` API, the same one wire-pod's Lua scripting
  uses for `showImage`, which is the supported way to put custom pixels on the
  face.
- Vector's speech is his normal text-to-speech voice, so the pronunciation of
  a word is whatever his voice does with it.
- Only one session runs on a robot at a time (rigid or organic). Starting
  practice again while a session is running is ignored until it ends.

## Where the code lives

- `chipper/pkg/wirepod/ttr/sightwords.go` - word list configuration, screen
  rendering, trigger phrases, the rigid practice sequence and the organic
  mode's word-coverage tracking and LLM prompt
- `chipper/pkg/wirepod/ttr/sightwords_font.go` - the bitmap font the words are
  drawn with
- `chipper/pkg/wirepod/ttr/sightwords_robot.go` - the robot side: behavior
  control, showing/saying words, running organic LLM conversations and
  per-robot session state
- `chipper/pkg/wirepod/ttr/sightwords_test.go` - tests for the configuration
  and the session logic
- `chipper/pkg/wirepod/config-ws/webserver.go` - the `get_sight_words` /
  `set_sight_words` web API handlers used by the web UI
- `chipper/webroot/index.html` / `chipper/webroot/js/main.js` - the "Sight
  Words" section of the web UI
