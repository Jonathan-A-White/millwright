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
| `words[].expected_phonemes` | the phonemes the target asks for, ARPAbet-style (IPA from `azure`), `[]` for an insertion |
| `words[].produced_phonemes` | the phonemes the engine heard, `[]` for an omission |
| `words[].error` | exactly one of `none`, `omission` (left out), `insertion` (added), `mispronunciation`, `hesitation`, `not_reached` (the reader stopped before it) |
| `words[].accuracy` | whole number 0 to 100 |
| `words[].self_corrected` | the reader got it wrong, then right |
| `accuracy` | the reading as a whole, whole number 0 to 100 |
| `seconds` | seconds of speech scored, 0 or more |

The JSON names are snake_case and the lists are never `null`.

**A reading that stops early.** `omission` is a word skipped inside the reading. A
reading that stops before the text does (the first words of a verse, then silence) scores
the words it reached as usual, and every word of the target after the last one reached is
`not_reached`: its `produced_phonemes` are `[]` and its `accuracy` 0. They are the tail of
`words`, in the target's order, and no phone heard is ever given to one. A tutor reads where
the reader stopped as the first `not_reached` word. The
reading's `accuracy` is the mean over the words reached, so a reading that stops early is
not marked down for the words it never came to. The `azure` engine does not report the kind:
Azure marks the same words as omissions.

## The engines

An engine is a name in config and a file in `infrastructure/scorer/`.

- **`local`** (`infrastructure/scorer/local.go`) is an HTTP client of the scorer
  in `contrib/scorer`, which runs on this host and listens with a local model.
  Nothing leaves the host. The scorer is a wav2vec2 phoneme model, espeak-ng
  and an alignment, on CPU, run as the systemd user unit `mw-scorer`:
  `contrib/scorer/install.sh` installs it (after the hand step
  `sudo apt install espeak-ng`), and `contrib/scorer/README.md` says how it
  scores, its limits and the way back.
- **`azure`** (`infrastructure/scorer/azure.go`) is Azure Speech's pronunciation
  assessment, the reference the local model is measured against. **The clip leaves
  this host**: it is sent to Microsoft, so add `azure` to `engines` only on a host
  where that is wanted. [The azure engine](#the-azure-engine) says what it needs and
  how it maps the answer.

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

### The azure engine

Config, in the `[scorers]` table: `azure_key_file` (a full path to a file holding the
Speech resource's key, mode 0600; a file others can read is refused, like
`grist_key_file`) and `azure_region` (the key's region, e.g. `westus2`). The key is read at
each score and never printed. With no key file, or no region, the engine refuses
naming the setting (`azure_key_file`, `azure_region`), and in a grist that is the
engine's `{"error": "..."}` while the other engines score as usual. `MW_AZURE_ENDPOINT`
replaces the address, for a test that stubs Azure; it is not a setting.

`mw` converts the recording with `ToWav16k`, then sends one request to the speech-to-text
REST API for short audio (checked against Microsoft Learn, 2026-10-07):

```
POST https://<azure_region>.stt.speech.microsoft.com/speech/recognition/conversation/cognitiveservices/v1?language=en-US&format=detailed
Ocp-Apim-Subscription-Key: <the key>
Content-Type: audio/wav; codecs=audio/pcm; samplerate=16000
Accept: application/json
Pronunciation-Assessment: <base64 of the JSON below>
```

```json
{"ReferenceText": "the cat sat", "GradingSystem": "HundredMark", "Granularity": "Phoneme",
 "Dimension": "Comprehensive", "EnableMiscue": true, "PhonemeAlphabet": "IPA", "NBestPhonemeCount": 5}
```

The body is the WAV file. `language` is `en-US` for `en` (`es` is `es-ES`; any other code
is sent as given). Azure takes at most 60 seconds of audio and says a pronunciation
assessment should be no more than 30, so `mw` refuses a longer clip with a plain line.
Microsoft's docs give the resource-name form
(`<resource>.cognitiveservices.azure.com/stt/speech/...`) as the current address; the
regional host above is the one `azure_region` names and is what `mw` calls.

**Free tier.** The F0 tier gives 5 audio hours of speech to text a month, shared with
custom speech. Microsoft's price page does not name a separate allowance for
pronunciation assessment, which is billed as an add-on beyond it: treat the 5 hours as
what is free and watch the Azure portal for the rest. A `429` means the rate limit or the
quota is used up, and is reported as that.

**What comes back.** `NBest[0].Words[]` is read as `Word` and
`PronunciationAssessment{AccuracyScore, ErrorType}` (the flat `AccuracyScore` and
`ErrorType` of the short-audio docs' older example work as well):

| Azure | `words[].error` |
| --- | --- |
| `None` | `none` |
| `Omission` | `omission` |
| `Insertion` | `insertion` |
| `Mispronunciation` (Azure's word accuracy below 60) | `mispronunciation` |
| `UnexpectedBreak` | `hesitation` |
| `MissingBreak`, `Monotone` | `none`: prosody notes, not misreadings; there is no field to carry the note |

Any other `ErrorType` is refused, naming it. Azure reports a break or a monotone only when
prosody assessment is asked for, which `mw` does not, so the last two rows are for answers
that carry them. `accuracy` is the word's `AccuracyScore` rounded to a whole number, and the
reading's is `NBest[0]`'s; `seconds` is the answer's `Duration` (100-nanosecond units); `self_corrected` is
always `false`, since Azure does not say.

**Phonemes are IPA, as Azure spells them** (`PhonemeAlphabet: IPA`), not the ARPAbet of the
local engine, so the two engines' phoneme lists are not comparable symbol by symbol yet.
In Azure's answer `Phonemes[].Phoneme` is the *expected* phoneme, and the likeliest phoneme
actually spoken is the best-scoring entry of its `NBestPhonemes` (asked for with
`NBestPhonemeCount`). So `expected_phonemes` is every `Phoneme` of the word, and
`produced_phonemes` is each phoneme's best `NBestPhonemes` entry; when an answer carries no
`NBestPhonemes`, it is the phonemes whose own `AccuracyScore` is 60 or more. An omission
produces `[]`, and for an insertion the `Phonemes` listed are what was said, so they are
`produced_phonemes` and `expected_phonemes` is `[]`. A `RecognitionStatus` other than
`Success` (`NoMatch`, `InitialSilenceTimeout`, ...) is an error naming it.

**Errors.** `401` and `403` say the key was refused and to check `azure_key_file` and that
`azure_region` is the key's region; `429` says the limit or quota is used up; anything else
shows the status and Azure's own words. A request that takes longer than two minutes is
timed out.

## In a grist

An app's grind file opts a recording in (`grinds/<kind>.json`, `application/grist.go`'s
`GrindFile`):

```json
{
  "attachments": {"min": 1, "max": 2, "mime": ["audio/webm"], "maxBytes": 4194304},
  "scoring": {"audio": true, "target_field": "target_text"},
  "maxTurns": 8
}
```

The factory carries `audio/webm`, `audio/ogg`, `audio/mp4`, `audio/mpeg` and
`audio/wav` as well as photos; the size caps (`[grist]` `max_attachment_bytes`) are
the same. A grind without `scoring.audio` refuses a recording, so a recording is only
ever taken to be scored. `maxTurns` is optional; nothing in the harness's command line
forbids subagents.

### The language

`scoring.langs` is optional: the language codes the grind scores in, the first the
default (`"langs": ["el", "en"]`). Absent, it is `["en"]`. The codes known are `en`
(American English), `el` (modern Greek, monotonic letters) and `es`; a grind file naming
another is refused as unreadable. The grist's request may carry `"lang"`, one of the
grind's (a region, `el-GR`, counts as its language); without it the first is used, and a
`lang` the grind does not list refuses the grist, saying which it does ("This grist asks
for the language fr; its grind scores in el and en."). The language chosen is what every
engine is given and what `scorers.json` records as `lang`.

An engine that cannot score the language is not run for the recording, and the pass
notes `<engine> skipped: no <lang>`; its entry is left out of `reading_result`. Azure's
pronunciation assessment has no Greek, so for `el` only the local engine scores. The
local scorer reads Greek through espeak-ng's `el` voice and compares in IPA, not ARPAbet
(`contrib/scorer/README.md`): a Greek word's `expected_phonemes` and `produced_phonemes`
are IPA symbols. Erasmian pronunciation is not scored by any engine.

Before the harness session, for each recording the mill runs **every** engine in
`[scorers]` `engines` that can score the language, at once, with the string in the
request's `target_field` as the target, and adds `reading_result` to the request the
session is given:

```json
{"mode": "reading", "target_text": "the cat sat",
 "reading_result": {"local": {"engine": "local", "words": [], "accuracy": 80, "seconds": 1.5},
                    "azure": {"error": "the azure key was refused"}}}
```

An engine that fails, or a request with no text in `target_field`, is that engine's
`{"error": "..."}`; the grind goes on. With more than one recording `reading_result` is
the first's and `reading_results` is the list of all, in order. The recording is held in
memory and kept in the run record; it is never written to the session's directory and
never in its prompt.

## The reply

The mill's answer to a grist whose grind scored a recording carries the same scores
beside `answer`, as the session was given them and never the audio: `reading_result`
(the first recording's, by engine, an engine that failed as `{"error": "..."}`) and,
with more than one recording, `reading_results` (all, in order). The app reads them to
show each engine's word table. A grind that scored nothing replies without either.

```json
{"re": "<txid>", "status": "answered", "answer": {"...": "..."},
 "reading_result": {"local": {"engine": "local", "words": [], "accuracy": 80, "seconds": 1.5},
                    "azure": {"error": "the azure key was refused"}},
 "grind": {"app": "cairn", "kind": "reading", "v": "1.1", "commit": "<sha>"}}
```

## The run record

Every grind that ran keeps its raw record under the state directory
(`grist_state_dir`, default `~/.local/state/mw/grist`), `runs/<txid>/`, 0700, files 0600,
and `mw` never deletes it:

| File | Holds |
| --- | --- |
| `input.json` | the app's request as the session was given it, with `reading_result` |
| `attachment-N.<ext>` | each attachment as it was opened, in order (photos too) |
| `scorers.json` | a list, one entry for each recording: `attachment`, `mime`, `target_text`, `lang`, `reading_result` by engine; `[]` when nothing was scored |
| `answer.json` | `status` (answered, refused or failed), `reason`, `answer`, `said`, `turns`, `total_cost_usd` |
| `timing.json` | `txid`, `app`, `kind`, `model`, `effort`; `sent` (the grist record's time; `received` minus it is the queue wait), `received`, `scored_at`, `harness_started`, `answered`; `scoring_seconds`, `scorers` (each engine's seconds), `harness_seconds`, `seconds` (received to answered); `sent` and `scorers` are left out when unknown |

`mw grist runs [--since 24h|2026-10-07|<RFC 3339>]` lists them, oldest first: txid,
kind, model, seconds and how it ended. `mw grist stats --kind <kind> [--last 20]` prints the
median and worst seconds of each phase (queue, scoring, each engine, harness, total) over the
last runs of that kind. See `features/grist_audio.feature`.

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

See `features/scorer.feature` and `features/grist_audio.feature`.
