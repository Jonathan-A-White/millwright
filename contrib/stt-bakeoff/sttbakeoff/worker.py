"""One candidate over the clips, in its own process and venv, so its peak memory is its own.

Reads one JSON job on standard input: {"family", "model", "options", "threads", "clips": [{"id", "path",
"prime"}]}. Prints one JSON line when the model is loaded ({"loaded": seconds}), then one a clip
({"id", "path", "text", "seconds", "cpu_seconds"}): cpu_seconds is CPU time over all threads, which a busy
host does not inflate as it does seconds. The first clip is heard once before timing begins (a server keeps the
model warm), and that warm-up is not counted.
"""

import json
import os
import subprocess
import sys
import time

import numpy as np

RATE = 16000


def audio(path):
    """The clip as 16 kHz mono float32, decoded by ffmpeg."""
    raw = subprocess.run(['ffmpeg', '-v', 'error', '-i', path, '-ac', '1', '-ar', str(RATE), '-f', 's16le', '-'],
                         check=True, capture_output=True).stdout
    return np.frombuffer(raw, dtype=np.int16).astype(np.float32) / 32768.0


def faster_whisper(model, options, threads):
    from faster_whisper import WhisperModel
    m = WhisperModel(model, device='cpu', compute_type='int8', cpu_threads=threads)

    def hear(wave, prime):
        kw = dict(language='en', beam_size=options.get('beam', 5), vad_filter=False, condition_on_previous_text=False)
        if prime and options.get('prime') == 'prompt':
            kw['initial_prompt'] = prime
        if prime and options.get('prime') == 'hotwords':
            kw['hotwords'] = prime
        segments, _ = m.transcribe(wave, **kw)
        return ' '.join(s.text.strip() for s in segments)
    return hear


def onnx(model, options, threads):
    import onnx_asr
    import onnxruntime as rt
    so = rt.SessionOptions()
    so.intra_op_num_threads = threads
    m = onnx_asr.load_model(model, quantization='int8', sess_options=so)
    kw = {'language': options['language']} if options.get('language') else {}

    def hear(wave, prime):
        return m.recognize(wave, sample_rate=RATE, **kw)
    return hear


def qwen(model, options, threads):
    import torch
    from qwen_asr import Qwen3ASRModel
    torch.set_num_threads(threads)
    m = Qwen3ASRModel.from_pretrained(model, dtype=torch.float32, device_map='cpu',
                                      max_inference_batch_size=1, max_new_tokens=256)

    def hear(wave, prime):
        kw = {'context': prime} if prime and options.get('prime') == 'context' else {}
        return m.transcribe(audio=(wave, RATE), language=None, **kw)[0].text
    return hear


FAMILIES = {'whisper': faster_whisper, 'onnx': onnx, 'qwen': qwen}


def main():
    job = json.load(sys.stdin)
    out = sys.stdout
    t = time.perf_counter()
    hear = FAMILIES[job['family']](job['model'], job.get('options', {}), job['threads'])
    print(json.dumps({'loaded': round(time.perf_counter() - t, 2)}), file=out, flush=True)
    clips = job['clips']
    if clips:
        hear(audio(clips[0]['path']), clips[0]['prime'])
    for c in clips:
        wave = audio(c['path'])
        t, cpu = time.perf_counter(), time.process_time()
        text = hear(wave, c['prime'])
        secs, cpu = time.perf_counter() - t, time.process_time() - cpu
        print(json.dumps({'id': c['id'], 'path': c['path'], 'text': text, 'seconds': round(secs, 3),
                          'cpu_seconds': round(cpu, 3),
                          'audio_seconds': round(len(wave) / RATE, 2)}, ensure_ascii=False), file=out, flush=True)


if __name__ == '__main__':
    os.environ.setdefault('TOKENIZERS_PARALLELISM', 'false')
    main()
