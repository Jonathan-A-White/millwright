# stt-bakeoff: which speech model hears him best

Lampas's Hold to talk sends his question to the home host to be turned into text. He asks in English and says
Greek words from the passage the way Lampas respells them (TAF-ta for ταῦτα). This bake-off runs each candidate
model over one test set on this host's CPU and prints, per candidate:

- **Greek words**: how many Greek words it got right. A word counts when what it wrote, snapped to the
  passage's words by sound, names the right one (`sttbakeoff/score.py` says exactly how).
- **English WER**: word error rate on the English words only.
- **median s / p90 s**: seconds to transcribe one clip, after a warm-up, model already loaded.
- **peak MB**: the candidate's process's peak resident memory.

The results and the winner are in `docs/research/stt-greek.md`.

## The test set

`testset/questions.json` holds 26 questions, 13 on 1 John 2 and 13 on Romans 8, each with one to three Greek
words in braces as Lampas shows them. `testset/vocab.json` is every word of the two chapters with Lampas's
respelling, taken from Lampas's `public/data` and `respellModern` (src/speech/schemes/modern.ts).

`testset/synthetic/<voice>/<id>.ogg` are the questions said by two Piper English voices (en_US-ryan-high,
en_US-lessac-medium), each Greek word said from its respelling: 52 clips, 16 kHz mono Opus. Remake them with
`install.sh tts` then:

```sh
PYTHONPATH=contrib/stt-bakeoff ~/.local/share/stt-bakeoff/venv-tts/bin/python -m sttbakeoff.make_clips \
  --voices-dir ~/.local/share/stt-bakeoff/voices
```

**His own holds** are scored as a second table. Download one in Lampas (Developer mode, 'Download my
recording'), put it in a folder of your choosing as `<name>.webm` beside `<name>.json`:

```json
{"passage": "rom8", "verses": [15], "text": "What does {υἱοθεσίας} mean here?"}
```

`text` is what he said, the Greek words in braces as Lampas writes them; `passage` is `1jn2` or `rom8`. Then
add `--real <folder>` to `bakeoff run`. Keep his recordings out of the repository.

## Run it

```sh
contrib/stt-bakeoff/install.sh                 # the venvs (or name families: whisper onnx qwen tts)
contrib/stt-bakeoff/bakeoff list               # the candidates and which are ready
contrib/stt-bakeoff/bakeoff run --out /tmp/stt.json            # every candidate; progress on stderr
contrib/stt-bakeoff/bakeoff run --candidate parakeet-v3        # just one
contrib/stt-bakeoff/bakeoff table /tmp/stt.json --misses       # again, with every Greek word missed
contrib/stt-bakeoff/bakeoff table /tmp/stt.json --markdown     # for docs/research
```

`--threads N` (default 8) is what each candidate may use. Each model is downloaded to `~/.cache/huggingface`
the first time it runs (small.en 0.5 GB, medium.en 1.5 GB, turbo 1.6 GB, large-v3 2.9 GB, each Parakeet 0.6 GB,
Canary 1 GB, Qwen3-ASR 1.8 / 4.4 GB).
Timings are only as quiet as the host: note the load average beside a table. CPU s (CPU time a clip over
all threads) is the column a busy host does not inflate. A run saves transcripts and timings only;
`bakeoff table` scores them again, so a change to the scoring reaches old runs. `results/` holds the run
docs/research/stt-greek.md reports.

## The candidates

`bakeoff list`; `sttbakeoff/candidates.py` says each one's model and how it is primed. A primed candidate is
given the Greek words of the verse(s) the question is about, as Lampas would give it the verse on screen.

## Tests

`contrib/stt-bakeoff/test.sh` runs the scoring, table and test-set tests with any python3, no models;
`go test ./contrib/` runs it (contrib/sttbakeoff_test.go).

## The way back

The models first (about 15 GB; `hf` is in the whisper venv, and knows the cache's shared blob store),
then the venvs and the voices:

```sh
~/.local/share/stt-bakeoff/venv-whisper/bin/hf cache rm -y \
  model/mobiuslabsgmbh/faster-whisper-large-v3-turbo model/Systran/faster-whisper-large-v3 \
  model/Systran/faster-whisper-medium.en model/Systran/faster-whisper-small.en \
  model/istupakov/parakeet-tdt-0.6b-v3-onnx model/istupakov/parakeet-tdt-0.6b-v2-onnx \
  model/istupakov/canary-1b-v2-onnx model/Qwen/Qwen3-ASR-0.6B model/Qwen/Qwen3-ASR-1.7B
rm -rf ~/.local/share/stt-bakeoff     # the venvs (about 2 GB) and the Piper voices
uv cache prune                         # if uv made the venvs
```
