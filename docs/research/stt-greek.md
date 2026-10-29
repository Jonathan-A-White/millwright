# Speech for Lampas's Hold to talk: which model hears his English with Koine Greek words in it

Research date: 2026-10-16. Story mw-5r3p30.245, from the Governor's card answer on mw-fnvjof.13
(2026-10-12): "just make sure you're using model you know that meets the needs of the speed and accuracy and
the multilingual aspect you know the Greek and the English". Decided there: Hold to talk records, the home
host turns it into text primed with the passage's Greek words, the phone's recogniser stays as the offline
fallback. This note picks the model. Every number below was measured on the Laptop with
`contrib/stt-bakeoff` (its README says how to run it again). No money was spent.

<!-- toc -->

- [Read this first](#read-this-first)
- [1. The table](#1-the-table)
- [2. The candidates, and where they came from](#2-the-candidates-and-where-they-came-from)
- [3. How it was scored](#3-how-it-was-scored)
- [4. The test set](#4-the-test-set)
- [5. The winner on the Laptop: setup and the way back](#5-the-winner-on-the-laptop-setup-and-the-way-back)
- [6. His real holds](#6-his-real-holds)
- [7. What the record-and-transcribe build needs](#7-what-the-record-and-transcribe-build-needs)
- [8. Paid services (described only; nothing was signed up for or spent)](#8-paid-services-described-only-nothing-was-signed-up-for-or-spent)
- [Sources](#sources)

<!-- /toc -->

## Read this first

**Winner: Whisper small.en (faster-whisper, int8, on the CPU), primed with the Greek words of the verse
on screen written as Lampas respells them.** On the 52 test clips it named 88% of the Greek words right
(67 of 76) at 1.4 s a clip (p90 1.6 s), 0.9 GB of memory and 1.1% English word error rate. The most
accurate candidate, Whisper large-v3 with the same prompt, named 92% (70 of 76), three words more, and
took 6.4 s and 3 GB. Turbo, which the story named, named 91% with greedy decoding at 4.7 s.

Three findings decide it:

1. **Priming is what matters, not a multilingual model.** He says the Greek the way the respelling
   reads (TAF-ta), so to any model the clip is English with strange words in it. Unprimed Whisper turbo
   named 64%; with the verse's words respelt in its prompt it named 89 to 91%. Whisper copies the
   respelling it was shown ('What does te-KNEE-a mean in verse 1?'), and the text snaps straight back to
   the Greek word.
2. **The multilingual models do not help.** Parakeet TDT 0.6B v3 lists Greek among its 25 languages,
   but it named 61%, below the English-only Parakeet v2 that the phone uses today (75%). Canary 1B v2
   named 62%. Neither takes a prompt. Qwen3-ASR takes context text and helped itself with it (62% to 66%
   at 0.6B, 76% at 1.7B), but it needs 6 to 13 GB and is slower than Whisper.
3. **Whisper's cost is its 30-second window.** Every clip, however short, goes through the encoder as 30
   seconds of audio. That is why small.en takes 1.4 s, turbo 5 s and large-v3 6 s, and why the CPU
   seconds column grows with the model rather than with the clip. Among the primed Whispers the accuracy
   difference is one to three words of 76, inside what one synthetic test set can tell apart. The speed
   difference is three to five times.

**Provisional.** The clips are synthetic: an English voice reading the respelling. His real holds are the
confirmation (section 6). If they show small.en missing clearly more than turbo, the upgrade is one
setting: turbo with greedy decoding, the same prompt, about 5 s.

## 1. The table

`contrib/stt-bakeoff/bakeoff table contrib/stt-bakeoff/results/2026-10-16-synthetic.json --markdown`:

| candidate | Greek words | English WER | median s | p90 s | CPU s | peak MB | clips |
|---|---|---|---|---|---|---|---|
| whisper-large-v3+prompt-respelt | 70/76 92% | 0.8% | 6.39 | 7.26 | 46.4 | 3063 | 52 |
| whisper-turbo+prompt-respelt-greedy | 69/76 91% | 2.0% | 4.74 | 6.71 | 33.4 | 1663 | 52 |
| whisper-medium.en+prompt-respelt | 68/76 89% | 0.6% | 3.45 | 3.87 | 25.7 | 2172 | 52 |
| whisper-turbo+prompt-respelt | 68/76 89% | 1.4% | 5.90 | 13.15 | 39.7 | 1664 | 52 |
| **whisper-small.en+prompt-respelt** | **67/76 88%** | **1.1%** | **1.39** | **1.56** | **10.7** | **902** | 52 |
| whisper-turbo+hotwords-respelt | 67/76 88% | 1.1% | 5.25 | 5.59 | 36.5 | 1664 | 52 |
| qwen3-asr-1.7b+context-respelt | 58/76 76% | 1.7% | 10.62 | 20.17 | 68.9 | 12719 | 52 |
| parakeet-v2 | 57/76 75% | 1.1% | 0.15 | 0.18 | 2.0 | 1226 | 52 |
| whisper-turbo+prompt-greek | 56/76 74% | 4.2% | 5.32 | 10.50 | 36.3 | 1663 | 52 |
| qwen3-asr-0.6b+context-respelt | 50/76 66% | 3.4% | 3.78 | 5.31 | 27.0 | 6012 | 52 |
| whisper-turbo | 49/76 64% | 2.2% | 5.71 | 7.65 | 38.5 | 1663 | 52 |
| qwen3-asr-0.6b | 47/76 62% | 1.7% | 2.83 | 4.04 | 20.0 | 6012 | 52 |
| canary-1b-v2 | 47/76 62% | 3.1% | 0.35 | 0.44 | 3.9 | 1553 | 52 |
| parakeet-v3 | 46/76 61% | 1.4% | 0.18 | 0.23 | 2.2 | 1206 | 52 |

What the columns mean:

- **Greek words**: a Greek word counts when what the model wrote, snapped by sound to the passage's
  words, names the right one (section 3).
- **English WER**: word error rate over the English words only.
- **median s and p90 s**: wall seconds a clip with the model loaded and warm, 8 threads.
- **CPU s**: the median CPU seconds a clip over all threads. A busy host does not inflate it the way it
  inflates wall seconds.
- **peak MB**: the peak resident memory of the candidate's process.

The conditions were noisy, and the order holds through it:

- The run was one invocation, all candidates one after another, from 2026-10-16 03:30Z to 04:30Z.
- It ran at nice 10 (a Builder session's) beside other sessions' test suites: load average median 23,
  between 14 and 88, on the 20 CPUs WSL2 sees (an AMD Ryzen AI 9 HX PRO 370).
- small.en's wall time (1.39 s) is its CPU seconds over 8 threads (1.34 s), so it got its threads even
  so. A quiet host at nice 0 would be no slower.

An earlier full run under the same kind of load (02:27Z to 03:26Z, without medium.en and small.en) gave
the same Greek-word counts and the same order for every candidate it had.

Not in the table:

- **whisper.cpp** (the build at ~/.local/bin/whisper-cli that postern-transcribe uses). Run by hand on
  one clip with turbo q8_0 and the respelt prompt, it took 21.8 s just to encode, and 8.4 s with
  `--audio-ctx 400` (a shorter window). faster-whisper is several times faster on this CPU, so
  whisper.cpp was not run further.

## 2. The candidates, and where they came from

- **Whisper large-v3-turbo**, faster-whisper 1.2.1 on CTranslate2 4.8.3, int8: plain; with the verse's
  words in its `initial_prompt`, respelt or in Greek letters; with them as `hotwords`; greedy (beam 1)
  as well as beam 5. The model is mobiuslabsgmbh/faster-whisper-large-v3-turbo.
- **Whisper large-v3, medium.en and small.en**: the same, with the respelt prompt (the Systran
  faster-whisper conversions). These were added to see where accuracy against speed falls.
- **Parakeet TDT 0.6B v3** (nvidia/parakeet-tdt-0.6b-v3, CC-BY-4.0), int8 ONNX via onnx-asr 0.12.
  **Its language list was checked: Greek (el) is one of its 25 European languages** (model card,
  huggingface.co/nvidia/parakeet-tdt-0.6b-v3). It detects the language itself and takes no prompt.
- **Parakeet TDT 0.6B v2**, English only: the phone's recogniser today (whisper-hid's README), as a
  baseline.
- **Canary 1B v2** (nvidia, 25 European languages including Greek), int8 ONNX via onnx-asr, language
  `en`. I found it because onnx-asr runs it.
- **Qwen3-ASR 0.6B and 1.7B** (Qwen, Apache-2.0, January 2026). Greek is one of its 30 languages, and its
  `transcribe` takes a `context` string, the nearest thing among open models to priming built for this.
  Sources: github.com/QwenLM/Qwen3-ASR and arXiv 2601.21337. Run on PyTorch 2.14 for CPU, float32.

A search for open models built for code-switching with context biasing found research (LLM-guided
hotword biasing for code-switched ASR, arXiv 2610.16579; OWSM biasing) but no released CPU model beyond
the ones above.

## 3. How it was scored

`contrib/stt-bakeoff/sttbakeoff/score.py`, tested in `contrib/stt-bakeoff/tests/test_score.py`:

1. **The reference is the question with each Greek word in braces**: 'What does {ταῦτα} mean in verse
   one?'.
2. **The transcript is aligned to it word by word.** A Greek word may take up to eight transcript words
   ('a po ka ra do kia'), and the alignment gives it the run that sounds most like it.
3. **The run is snapped to the passage's words by sound.** Greek letters are read as Modern Greek, a
   respelling or English spelling is folded (kh, ch and k alike; ee and i; and so on), and the words are
   compared by edit distance. The Greek word counts when the nearest passage word is the expected one
   and they are at least 0.7 alike. So 'tafta', 'TAF-ta', 'ταύτα' and 'pneuma' all count; another word
   of the passage, or an English word in its place ('technique' for τεκνία), does not.
4. **English WER counts substitutions, deletions and insertions against the English words only.**
   Digits and number words count the same.

## 4. The test set

- **26 questions** of the kind he asks the tutor, 13 on 1 John 2 and 13 on Romans 8, each with one to
  three Greek words from the verse (`contrib/stt-bakeoff/testset/questions.json`).
- **The passage words**: every word of the two chapters with Lampas's respelling, taken from Lampas's
  `public/data` and its `respellModern` (`testset/vocab.json`).
- **The clips**: each question said by two Piper voices, en_US-ryan-high and en_US-lessac-medium, with
  each Greek word read from its respelling in lower case (Piper reads capitals as letters). That makes
  52 clips of 16 kHz Opus.

Some misses are the voice's, not the model's:

- Piper reads 'a-va' (ἀββᾶ) so that every model hears 'Ivan' or 'Avon'.
- It reads 'too vee-oo' so that Whisper writes '2 VU'.
- It reads 'te-te-lee-o-te' as 'tee-tee-lee-o-tee'.

## 5. The winner on the Laptop: setup and the way back

For the record-and-transcribe build, the service needs one venv and one model:

```sh
uv venv -p 3.12 ~/.local/share/lampas-stt/venv
uv pip install -p ~/.local/share/lampas-stt/venv/bin/python faster-whisper==1.2.1   # ctranslate2 4.8.3; ~150 MB
~/.local/share/lampas-stt/venv/bin/python -c "from faster_whisper import WhisperModel; WhisperModel('small.en', device='cpu', compute_type='int8')"   # fetches 464 MB to ~/.cache/huggingface once
```

The call it makes, with the model loaded once and kept:

```python
model = WhisperModel('small.en', device='cpu', compute_type='int8', cpu_threads=8)
segments, _ = model.transcribe(audio, language='en', beam_size=5, condition_on_previous_text=False,
                               initial_prompt='A question about Romans 8:15, with Greek words from it: ou, ghar, ...')
```

The way back:

```sh
~/.local/share/lampas-stt/venv/bin/hf cache rm -y model/Systran/faster-whisper-small.en   # the model, 464 MB
rm -rf ~/.local/share/lampas-stt
```

The bake-off's own install (`contrib/stt-bakeoff/install.sh`, about 2 GB of venvs and 15 GB of models)
and its way back are in its README.

## 6. His real holds

**None have come yet.** `bakeoff run --real <folder>` scores them as a second table: each recording
beside a `.json` naming the passage, the verses and what he said. The README has the format. Today
Lampas's Developer mode 'Download my recording' sits under a reading check's result. That gives his
reading of a verse, which is not a question with Greek words in it, so a hold to the tutor cannot be
downloaded yet.

## 7. What the record-and-transcribe build needs

1. **A service on the home host that keeps small.en loaded.** It loads in a few seconds once downloaded,
   and the first call is slow. It needs 8 threads, about 1 GB, and one clip at a time, queued. It should run at nice 0 (a
   systemd user unit, like mw-scorer), so Builder sessions at nice 10 give way to it. Like mw-scorer, it
   needs nothing beyond the venv, and nothing leaves the host.
2. **The way in.** A question already reaches the home host as a grist through Postern with recordings
   attached (Lampas's `src/services/tutor.ts`, `src/services/reading.ts`). A hold can go the same way as
   its own kind of grist, carrying the recording (MediaRecorder's Opus is fine, as here), the book,
   chapter and verses on screen. The answer is the text for the composer (bsv-kit's
   `transcribeRecording`). The round trip through Postern adds its own time to the 1.4 s.
3. **The prompt is built from the verse on screen, or two verses at most.** Whisper keeps only the last
   223 tokens of a prompt. One verse's words respelt are 64 to 148 tokens, two are 163, and the whole of
   Romans 8 is 1601. Write them as Lampas shows them (respelt, not Greek letters: Greek letters scored
   74% against 89%, and in an exploratory run once set turbo writing garbage).
4. **Snapping.** The text comes back respelt ('What does te-KNEE-a mean'). To show or send ταῦτα,
   snap it to the passage's words as `score.py` does (`sounds`, `nearest`, 0.7). The tutor may cope
   with the respelling as it is; whether to replace it, or send both, is a product choice for the build.
5. **Fallback.** Use the phone's recogniser when the home host does not answer in time, as decided.
6. **'Download my recording' for a hold** in Developer mode, so his real holds can confirm the choice
   (section 6).
7. **The model name as a setting**, so turbo with greedy decoding (91%, about 5 s) is one change if
   his real holds need it.

## 8. Paid services (described only; nothing was signed up for or spent)

Hosted speech-to-text accepts vocabulary hints. Several kinds were looked at:

- OpenAI's transcription API (gpt-4o-transcribe, whisper-1) takes a text `prompt`, as faster-whisper's
  does here.
- Deepgram's Nova-3 takes key terms.
- AssemblyAI takes a key-terms prompt.
- Google Cloud Speech-to-Text takes phrase sets with a boost.
- Azure Speech takes a phrase list. millwright already has an Azure region setting for grist scoring.

All of them would send his voice off the home host. That is the opposite of the Governor's decision for
Postern's voice notes, which are never transcribed by a third party. They also charge per minute. A
local model already reaches 88 to 92% here at no cost, so none is recommended. Their prices were not
checked.

## Sources

- nvidia/parakeet-tdt-0.6b-v3 model card (language list, licence):
  https://huggingface.co/nvidia/parakeet-tdt-0.6b-v3
- Qwen3-ASR: https://github.com/QwenLM/Qwen3-ASR, https://huggingface.co/Qwen/Qwen3-ASR-1.7B, arXiv
  2601.21337
- LLM-HB, hotword biasing for code-switching ASR: https://arxiv.org/abs/2610.16579
- faster-whisper: https://github.com/SYSTRAN/faster-whisper; onnx-asr: https://github.com/istupakov/onnx-asr;
  Piper: https://github.com/OHF-Voice/piper1-gpl
- whisper-hid README (the phone's Parakeet v2), Lampas `src/speech/schemes/modern.ts` (the respelling)
