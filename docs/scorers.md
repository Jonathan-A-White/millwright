# Scorers

A scorer listens to someone reading a text aloud and says, word by word, how
it went. `mw grist score` hands a recording to one **engine** and prints what
the engine answers. This page is the contract every engine meets; the engines
plug in behind one port, `Scorer` in `application/scorer.go`, and the command
never changes when one is added.

```sh
mw grist score --engine local --target "the cat sat" --audio clip.webm [--lang en]
```

`--engine`, `--target` and `--audio` are required; `--lang` is a language code
and defaults to `en`. The result is printed as JSON on standard output. An
engine that is not configured is refused before anything is read or sent, and
the refusal names the engines that are.

## The result

Every engine returns one `ReadingResult` (`application/scorer.go`), and an
engine that returns anything else fails where it is read, not where it is used.

```json
{
  "engine": "local",
  "words": [
    {"text": "the", "expected_phonemes": ["DH", "AH"], "produced_phonemes": ["DH", "AH"],
     "error": "none", "accuracy": 96, "self_corrected": false},
    {"text": "cat", "expected_phonemes": ["K", "AE", "T"], "produced_phonemes": ["K", "AH", "T"],
     "error": "mispronunciation", "accuracy": 61, "self_corrected": false},
    {"text": "sat", "expected_phonemes": ["S", "AE", "T"], "produced_phonemes": [],
     "error": "omission", "accuracy": 0, "self_corrected": false}
  ],
  "accuracy": 52,
  "seconds": 1.5
}
```

| Field | Meaning |
| --- | --- |
| `engine` | the engine's name; `mw` fills it with `local` when the local scorer leaves it out |
| `words` | one entry for each word of the target, in reading order, and one for each word the reader added; at least one |
| `words[].text` | the word, as the target spells it (the word as heard, for an insertion); never empty |
| `words[].expected_phonemes` | the phonemes the target asks for, ARPAbet-style, `[]` for an insertion |
| `words[].produced_phonemes` | the phonemes the engine heard, `[]` for an omission |
| `words[].error` | exactly one of `none`, `omission` (left out), `insertion` (added), `mispronunciation`, `hesitation` |
| `words[].accuracy` | whole number 0 to 100 |
| `words[].self_corrected` | the reader got it wrong, then right |
| `accuracy` | the reading as a whole, whole number 0 to 100 |
| `seconds` | seconds of speech scored, 0 or more |

The JSON names are snake_case and the lists are never `null`.

## The engines

An engine is a name in config and a file in `infrastructure/scorer/`.

- **`local`** (`infrastructure/scorer/local.go`) is an HTTP client of the scorer
  in `contrib/scorer`, which runs on this host and listens with a local model.
  Nothing leaves the host. The scorer is a wav2vec2 phoneme model, espeak-ng
  and an alignment, on CPU, run as the systemd user unit `mw-scorer`:
  `contrib/scorer/install.sh` installs it (after the hand step
  `sudo apt install espeak-ng`), and `contrib/scorer/README.md` says how it
  scores, its limits and the way back.
- **`azure`** is declared in config and used by a later story.

### Audio

Recordings come in whatever the phone's browser makes (`.webm`, `.m4a`, `.ogg`).
The `local` engine runs them through
`ffmpeg -i pipe:0 -ac 1 -ar 16000 -f wav pipe:1` (`ToWav16k` in
`infrastructure/scorer/audio.go`), so every engine that needs it gets 16 kHz,
mono WAV. `ffmpeg` must be installed (`apt install ffmpeg`); when it is not, the
command stops with a line saying so.

### The local scorer's request

`POST <local_url>/score`, `Content-Type: application/json`:

```json
{
  "target_text": "the cat sat",
  "lang": "en",
  "audio_wav_base64": "UklGRiQAAABXQVZFZm10IBAAAAEAAQAwHQAAYDsAAAIAEAA..."
}
```

`audio_wav_base64` is the standard base64 of the 16 kHz mono WAV file. The
scorer answers `200` with a `ReadingResult` as above. Any other status is an
error, and its body is shown. A connection refused names `local_url` and this
page; a request that takes longer than two minutes is timed out.

## Config

The `[scorers]` table of `~/.config/mw/config.toml`; a host with no table runs
`local` at its default address.

```toml
[scorers]
engines        = ["local"]               # the engines this host runs; default ["local"]
local_url      = "http://127.0.0.1:8765" # where contrib/scorer listens; default as shown
azure_key_file = "/home/jwhite/.config/mw/azure.key" # a full path; used by the azure engine
azure_region   = "westus2"               # used by the azure engine
```

A name in `engines` that this `mw` has no engine for stops `mw grist score`
with a line saying so, rather than running without it.

See `features/scorer.feature`.
