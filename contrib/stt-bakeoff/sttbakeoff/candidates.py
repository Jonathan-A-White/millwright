"""The candidates, and the priming text each prompted one is given. Standard library only.

A candidate runs in its family's venv (install.sh makes them): 'whisper' (faster-whisper on CTranslate2),
'onnx' (onnx-asr on ONNX Runtime: Parakeet and Canary), 'qwen' (qwen-asr on PyTorch).

prime says how the passage's Greek words reach the model: None (not at all), 'prompt' (Whisper's
initial_prompt), 'hotwords' (faster-whisper's hotwords) or 'context' (Qwen3-ASR's context). words says how
they are written: 'respelt' (Lampas's respelling, as he sees it: TAF-ta) or 'greek' (ταῦτα). beam is
Whisper's beam size (default 5; 1 is greedy, about twice as fast).
"""

CANDIDATES = {
    'whisper-turbo': dict(family='whisper', model='large-v3-turbo'),
    'whisper-turbo+prompt-respelt': dict(family='whisper', model='large-v3-turbo', prime='prompt', words='respelt'),
    'whisper-turbo+prompt-greek': dict(family='whisper', model='large-v3-turbo', prime='prompt', words='greek'),
    'whisper-turbo+prompt-respelt-greedy': dict(family='whisper', model='large-v3-turbo', prime='prompt',
                                                words='respelt', beam=1),
    'whisper-turbo+hotwords-respelt': dict(family='whisper', model='large-v3-turbo', prime='hotwords', words='respelt'),
    'whisper-large-v3+prompt-respelt': dict(family='whisper', model='large-v3', prime='prompt', words='respelt'),
    'whisper-medium.en+prompt-respelt': dict(family='whisper', model='medium.en', prime='prompt', words='respelt'),
    'whisper-small.en+prompt-respelt': dict(family='whisper', model='small.en', prime='prompt', words='respelt'),
    'parakeet-v3': dict(family='onnx', model='nemo-parakeet-tdt-0.6b-v3'),
    'parakeet-v2': dict(family='onnx', model='nemo-parakeet-tdt-0.6b-v2'),  # English only: the phone's
    'canary-1b-v2': dict(family='onnx', model='nemo-canary-1b-v2', language='en'),
    'qwen3-asr-0.6b': dict(family='qwen', model='Qwen/Qwen3-ASR-0.6B'),
    'qwen3-asr-0.6b+context-respelt': dict(family='qwen', model='Qwen/Qwen3-ASR-0.6B', prime='context', words='respelt'),
    'qwen3-asr-1.7b+context-respelt': dict(family='qwen', model='Qwen/Qwen3-ASR-1.7B', prime='context', words='respelt'),
}


def verse_words(passage, verses):
    """The passage's words in the given verses, in the order the vocabulary holds them (first seen first)."""
    want = set(verses)
    return [w for w in passage.words if want & set(w.verses)]


def priming(candidate, passage, book, chapter, verses):
    """The text that primes candidate for a question on these verses, or '' when it is not primed.
    Whisper's prompt holds at most 224 tokens, so it is the verses on screen, not the whole chapter."""
    c = CANDIDATES[candidate]
    if not c.get('prime'):
        return ''
    words = verse_words(passage, verses)
    shown = [w.respelling if c['words'] == 'respelt' else w.greek for w in words]
    where = f"{book} {chapter}:{','.join(str(v) for v in verses)}"
    if c['prime'] == 'hotwords':
        return ' '.join(shown)
    return f"A question about {where}, with Greek words from it: {', '.join(shown)}."
