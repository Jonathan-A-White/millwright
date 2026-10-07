"""The produced phonemes: a wav2vec2 CTC model that emits espeak-ng IPA, on CPU.

The model is facebook/wav2vec2-lv-60-espeak-cv-ft at a pinned revision, read
from the Hugging Face cache (~/.cache/huggingface, ~1.3 GB, put there by
install.sh). Greedy CTC decoding: the likeliest token per 20 ms frame, repeats
collapsed and blanks dropped; each token keeps the time of its frames.
"""

import io
import json
import wave

import numpy as np

MODEL = "facebook/wav2vec2-lv-60-espeak-cv-ft"
REVISION = "ae45363bf3413b374fecd9dc8bc1df0e24c3b7f4"
RATE = 16000
FILES = ["config.json", "preprocessor_config.json", "vocab.json", "pytorch_model.bin"]


def download():
    """Fetch the model into the cache; install.sh runs this once."""
    from huggingface_hub import snapshot_download

    return snapshot_download(MODEL, revision=REVISION, allow_patterns=FILES)


class Listener:
    def __init__(self, threads=4):
        import torch
        from huggingface_hub import hf_hub_download
        from transformers import Wav2Vec2FeatureExtractor, Wav2Vec2ForCTC

        torch.set_num_threads(threads)
        self._torch = torch
        self.features = Wav2Vec2FeatureExtractor.from_pretrained(MODEL, revision=REVISION)
        self.model = Wav2Vec2ForCTC.from_pretrained(MODEL, revision=REVISION, use_safetensors=False).eval()
        with open(hf_hub_download(MODEL, "vocab.json", revision=REVISION), encoding="utf-8") as f:
            vocab = json.load(f)
        self.tokens = {i: t for t, i in vocab.items()}
        self.skip = {i for t, i in vocab.items() if t.startswith("<") and t.endswith(">")}
        self.blank = self.model.config.pad_token_id
        self.frame = self.model.config.inputs_to_logits_ratio / RATE

    def tokens_heard(self, audio):
        """The tokens in the audio (float32, 16 kHz mono) as (token, start, end) seconds."""
        if len(audio) < RATE // 10:
            return []
        x = self.features(audio, sampling_rate=RATE, return_tensors="pt").input_values
        with self._torch.inference_mode():
            ids = self.model(x).logits[0].argmax(-1).tolist()
        out, prev = [], None
        for f, k in enumerate(ids):
            if k != self.blank and k not in self.skip:
                if k == prev:
                    out[-1][2] = (f + 1) * self.frame
                else:
                    out.append([self.tokens[k], f * self.frame, (f + 1) * self.frame])
            prev = k
        return [(t, round(s, 2), round(e, 2)) for t, s, e in out]


def silence(seconds):
    return np.zeros(int(seconds * RATE), dtype=np.float32)


def read_wav(data):
    """The samples of a PCM WAV file as float32 at 16 kHz mono, and its seconds."""
    try:
        with wave.open(io.BytesIO(data)) as w:
            rate, width, channels = w.getframerate(), w.getsampwidth(), w.getnchannels()
            raw = w.readframes(w.getnframes())
    except (wave.Error, EOFError) as err:
        raise ValueError("the audio is not a PCM WAV file: %s" % err) from None
    if width == 2:
        samples = np.frombuffer(raw[: len(raw) // 2 * 2], dtype="<i2").astype(np.float32) / 32768
    elif width == 1:
        samples = (np.frombuffer(raw, dtype=np.uint8).astype(np.float32) - 128) / 128
    elif width == 4:
        samples = np.frombuffer(raw[: len(raw) // 4 * 4], dtype="<i4").astype(np.float32) / 2147483648
    else:
        raise ValueError("the audio's samples are %d bytes wide: send 16-bit PCM" % width)
    if channels > 1:
        samples = samples[: len(samples) // channels * channels].reshape(-1, channels).mean(axis=1)
    seconds = len(samples) / rate if rate else 0.0
    if rate != RATE and len(samples):
        at = np.arange(int(len(samples) * RATE / rate)) * (rate / RATE)
        samples = np.interp(at, np.arange(len(samples)), samples).astype(np.float32)
    return samples, seconds
