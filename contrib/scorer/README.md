# mw-scorer: the local phoneme scorer

The scorer `mw grist score --engine local` talks to. It listens to a clip of
someone reading a target text aloud and says, word by word, which phonemes it
expected, which it heard, and what went wrong. It runs on this host, on CPU,
with a free model: nothing leaves the host.

It is one Python process (standard library HTTP, torch on CPU) listening on
`127.0.0.1:8765` as the systemd user unit `mw-scorer.service`.

## Install

1. **Hand step (root):** `sudo apt install espeak-ng`. The scorer reads the
   target's expected phonemes from espeak-ng. `install.sh` never runs `sudo`; without espeak-ng it
   stops before changing anything, saying so.
2. `contrib/scorer/install.sh`. It makes, then waits for `/health`:
   - `~/.local/share/mw-scorer/venv`, a venv with every package pinned in
     `requirements.txt` (CPU-only torch; ~0.9 GB). It uses `uv` when it is on
     PATH, else `python3 -m venv` (which needs `python3-venv`).
   - `~/.local/share/mw-scorer/app`, a copy of `mw_scorer/`, so the unit never
     runs from a worktree.
   - the model `facebook/wav2vec2-lv-60-espeak-cv-ft` at a pinned revision in
     `~/.cache/huggingface` (**~1.3 GB**, downloaded once; the unit then runs
     with `HF_HUB_OFFLINE=1` and never reaches the network).
   - `~/.config/systemd/user/mw-scorer.service`, enabled and started.

It ends by printing `curl -s 127.0.0.1:8765/health`, which answers
`{"status": "ok", ...}`. Run it again to update the code or the packages.

Settings go in `~/.config/mw/scorer.env` (optional; `systemctl --user restart
mw-scorer` after a change):

```sh
MW_SCORER_HESITATION_SECONDS=0.8   # a gap longer than this before a word is a hesitation
MW_SCORER_THREADS=4                # CPU threads the model may use
```

## The way back

```sh
systemctl --user disable --now mw-scorer
rm ~/.config/systemd/user/mw-scorer.service ~/.config/mw/scorer.env
systemctl --user daemon-reload
rm -rf ~/.local/share/mw-scorer
rm -rf ~/.cache/huggingface/hub/models--facebook--wav2vec2-lv-60-espeak-cv-ft   # the model, 1.3 GB
sudo apt remove espeak-ng   # hand step, if nothing else uses it
```

## The contract

`docs/scorers.md` is the contract. `POST /score` takes
`{"target_text", "lang", "audio_wav_base64"}` (a WAV file; mw sends 16 kHz
mono, anything else PCM is converted) and answers `200` with a ReadingResult:
`engine` `local`, one entry per target word in reading order plus one per added
word, each with `expected_phonemes` and `produced_phonemes` in ARPAbet (IPA
for a language other than English, see Greek below),
`error` (`none`, `omission`, `insertion`, `mispronunciation`, `hesitation`),
`accuracy` 0 to 100 and `self_corrected`; then the reading's `accuracy` and
`seconds`. A bad request is a `400` with a plain line saying why; a failure of
the scorer is a `500`. `GET /health` answers `200` once the model is loaded.

## How it scores

1. **Expected.** Each word of the target, punctuation stripped, through
   espeak-ng (by `phonemizer`; `lang` `en` is read as `en-us`), IPA to ARPAbet.
2. **Produced.** The clip through the wav2vec2 CTC model, which emits espeak-ng
   IPA: the likeliest token per 20 ms frame, repeats collapsed, blanks dropped,
   each token keeping its time. IPA to ARPAbet, so both sides spell a phone the
   same way.
3. **Aligned.** The expected phones end to end against the produced ones, by a
   weighted Levenshtein distance: a far phone, an added or a left-out phone
   costs 1; a near phone (vowel for vowel, voicing pairs such as T/D, the
   nasals, th-fronting, r/w) 0.5; the cot-caught vowels AO/AA 0.25. Each word
   takes the produced phones aligned to it.
4. **Labelled**, one error per word:
   `omission` when nothing was heard for it; `mispronunciation` when its
   distance is 1 or more (one far phone, or two near ones); `hesitation` when
   the gap after the word before it is longer than 0.8 s; otherwise `none`.
   Two or more phones between words are an attempt at the next word when
   close to it (it is then `self_corrected` if the final reading is closer),
   otherwise an `insertion`. Word accuracy is 100 × (1 − distance ÷ expected
   phones); the reading's is the mean over the target's words.

## Greek

`lang` `el` is modern Greek, monotonic letters (a verse as a Greek reader says it
today; Erasmian is not scored). espeak-ng's own `el` gives the expected IPA of
each word; the model's tokens are already IPA. ARPAbet is English's alphabet,
so for any language but English the two sides stay IPA and are compared by
`mw_scorer/phones.py`'s `ipa_cost`: stress, length marks and tone are dropped,
variants of one Greek phone are folded together (`_IPA_FOLD`: the vowel
qualities to Greek's five, [ç] to /x/, [ɲ ŋ ɱ] to /n m/, the tap and trill, ...),
a voicing pair or a vowel a step away (e/i, o/u, a/e, a/o) costs 0.5, and the
few pairs the model blurs because it had no Greek in its training (the voiced
fricatives ð and ɣ, m/n, θ/s) cost 0.25. Everything else costs 1, and the
labels and accuracy work as for English. A language with no table is compared
as IPA with the generic rules. To add one, give it a row in `_IPA_FOLD`,
`_IPA_NEAR_VOWELS` and `g2p._LANGS`, a pair of fixture clips in
`fixtures/make.sh`, and a test class beside `GreekFixtures`.

The Greek fixtures are espeak-ng's robotic voice, which the model hears worse
than the English one: the clean clip scores 93 with `πάντα` called a
mispronunciation; a human reading is what it is for and has not been tried.

## What it is tuned for

An eight-year-old reading English aloud, for a tutor that cares about
decoding: was the word on the page the word read? So it is lenient with what speech,
accent or the model blur (a near phone costs accuracy but alone is not called a
mispronunciation) and strict with a different consonant or a phone added or
dropped, which is how a misread word sounds ("cap" for "cat"). It is the free
scorer beside Azure's: tuning it against Azure is a later epic's work.

## Known limits

- The model hears vowels loosely, so one vowel swap alone ("cut" for "cat") is
  not called a mispronunciation: the word's accuracy drops to 83 and
  `produced_phonemes` shows the vowel heard. Two near swaps, or one far
  phone, are.
- The expected pronunciation is espeak-ng's American English. Other accents
  are forgiven only as far as near phones and the AO/AA merger go.
- One error per word: a mispronounced word that also came after a long pause
  is a `mispronunciation`. The first word is never a `hesitation`.
- A single stray phone between words is ignored, so a one-phone added word
  ("a") or a short "um" is not reported. An `insertion`'s text is the IPA
  heard, not a spelling.
- A self-correction is found when the first try comes right before the
  final reading and has two or more phones.
- Numbers and abbreviations are scored as espeak-ng reads them ("12" as
  "twelve").
- Speed: about 0.3–0.5 s for a short clip on 4 threads, after the two
  warm-up passes it makes while starting; it holds ~1.8 GB of memory.
- The fixtures are espeak-ng's own `en-gb-x-rp` voice: the model hears it
  cleanly, where espeak-ng's `en-us` voice it mishears even read right.
  Synthetic speech is not a child's; real clips are the next test.

## Tests

```sh
contrib/scorer/test.sh          # everything, the three fixture clips included (needs the install)
contrib/scorer/test.sh --pure   # the alignment and the HTTP face only, with any python3
```

`fixtures/make.sh` remakes the three clips of "the cat sat on the mat" with
`espeak-ng -w`, offline: `clean.wav` (every word `none`), `cap.wav`
(`mispronunciation` on "cat") and `no-the.wav` (the first "the" an
`omission`). The mw gate never needs torch: `contrib/scorer_test.go` runs
`test.sh --pure`, and the whole of `test.sh` only where the venv is installed.
`MW_SCORER_URL=http://127.0.0.1:8765 go test ./infrastructure/scorer/` scores
`cap.wav` through mw's own client against the running unit.

To change a pin, edit `requirements.in` and remake `requirements.txt`:

```sh
uv pip compile --python-version 3.12 --index-url https://pypi.org/simple \
  --extra-index-url https://download.pytorch.org/whl/cpu --index-strategy unsafe-best-match \
  --emit-index-url --no-header --no-annotate requirements.in -o requirements.txt
```
