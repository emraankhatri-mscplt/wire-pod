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
3. Edit that file to hold the words the child is working on.

On packaged installs (Windows, macOS, Android) the file lives next to the
other wire-pod configuration files in the user configuration directory, in the
same folder as `apiConfig.json` and `customIntents.json`.

## Customizing the word list

`sightWords.json` looks like this:

```json
{
  "words": ["the", "and", "see", "look", "you", "me", "we", "go", "is", "like"],
  "seconds_per_word": 4
}
```

- `words` is the list of words, practiced in the order they are written.
- `seconds_per_word` is how long each word stays on the screen after Vector
  says it. It defaults to 4 seconds and is limited to between 1 and 30
  seconds.

If you only want to change the words, a plain list also works:

```json
["the", "and", "see"]
```

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

Vector then says "Let's practice sight words!" and, for each word, shows it on
his screen in large white letters and says it out loud. When the list is
finished he says "Great job! That's all the words." and goes back to his
normal face.

While a session is running these follow-up commands are understood (each one
still needs the `Hey Vector` wake word, or a press of his back button):

- `next word` / `next one` - move on without waiting
- `repeat word` / `say it again` - show and say the same word again
- `stop sight words` / `stop practicing` / `I'm all done` - end the session

A session never depends on those commands: each word advances on its own after
`seconds_per_word`, so practice always finishes.

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
- Only one session runs on a robot at a time. Starting practice again while a
  session is running is ignored until it ends.

## Where the code lives

- `chipper/pkg/wirepod/ttr/sightwords.go` - word list configuration, screen
  rendering, trigger phrases and the practice sequence
- `chipper/pkg/wirepod/ttr/sightwords_font.go` - the bitmap font the words are
  drawn with
- `chipper/pkg/wirepod/ttr/sightwords_robot.go` - the robot side: behavior
  control, showing/saying words and per-robot session state
- `chipper/pkg/wirepod/ttr/sightwords_test.go` - tests for the configuration
  and the session logic
