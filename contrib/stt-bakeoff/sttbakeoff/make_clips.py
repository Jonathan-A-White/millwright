"""Make the synthetic clips: each question said by each Piper voice, the Greek in Lampas's respelling.

Runs in the tts venv (install.sh tts). Writes testset/synthetic/<voice>/<id>.ogg: 16 kHz mono Opus,
what a phone's recorder sends too. noise_scale and noise_w_scale are 0 so a voice says a text the same way
every run.
"""

import argparse
import os
import subprocess
import tempfile
import wave

from piper import PiperVoice, SynthesisConfig

from . import testset

VOICES = ('en_US-ryan-high', 'en_US-lessac-medium')


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--voices-dir', required=True)
    ap.add_argument('--out', default=os.path.join(testset.TESTSET, 'synthetic'))
    ap.add_argument('--voice', action='append', help='a Piper voice name (default: %s)' % ', '.join(VOICES))
    args = ap.parse_args()
    vocab = testset.load_vocab()
    config = SynthesisConfig(noise_scale=0.0, noise_w_scale=0.0)
    for name in args.voice or VOICES:
        voice = PiperVoice.load(os.path.join(args.voices_dir, name + '.onnx'))
        out = os.path.join(args.out, name)
        os.makedirs(out, exist_ok=True)
        for q in testset.questions():
            said = testset.spoken(q['text'], vocab[q['passage']])
            with tempfile.NamedTemporaryFile(suffix='.wav') as tmp:
                with wave.open(tmp.name, 'wb') as w:
                    voice.synthesize_wav(said, w, syn_config=config)
                subprocess.run(['ffmpeg', '-v', 'error', '-y', '-i', tmp.name, '-ac', '1', '-ar', '16000',
                                '-c:a', 'libopus', '-b:a', '32k', os.path.join(out, q['id'] + '.ogg')], check=True)
            print(name, q['id'], said)


if __name__ == '__main__':
    main()
