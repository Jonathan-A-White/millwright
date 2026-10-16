"""The test set: the questions, what a clip says for each, and which clips there are. Standard library only."""

import json
import os
import re

from . import score

HERE = os.path.dirname(os.path.abspath(__file__))
TESTSET = os.path.join(os.path.dirname(HERE), 'testset')
AUDIO = ('.ogg', '.opus', '.webm', '.wav', '.m4a', '.mp3', '.flac')


def questions(path=None):
    with open(path or os.path.join(TESTSET, 'questions.json'), encoding='utf-8') as f:
        return json.load(f)['questions']


def speakable(respelling):
    """Lampas's respelling as an English voice reads it best: lower case (capitals are read as letters),
    kh, dh and gh as an English reader says them ('khree-STO' is 'kree-sto')."""
    s = respelling.lower()
    return s.replace('kh', 'k').replace('dh', 'th').replace('gh', 'g')


def spoken(text, passage):
    """The question as the clip says it: each braced Greek word in its speakable respelling."""
    return re.sub(r'\{([^}]*)\}', lambda m: speakable(passage.word(m.group(1)).respelling), text)


def greek_words(text):
    return re.findall(r'\{([^}]*)\}', text)


def clips(directory, qs):
    """Every audio clip under directory whose name (without extension) is a question's id,
    as [(path, question)], in the questions' order then by path."""
    by_id = {q['id']: q for q in qs}
    found = []
    for root, _, files in os.walk(directory):
        for f in files:
            stem, ext = os.path.splitext(f)
            if ext.lower() in AUDIO and stem in by_id:
                found.append((os.path.join(root, f), by_id[stem]))
    order = {q['id']: i for i, q in enumerate(qs)}
    found.sort(key=lambda pq: (order[pq[1]['id']], pq[0]))
    return found


def real_clips(directory):
    """His own holds: <name>.<audio> beside <name>.json holding {"passage", "verses", "text"} with braced
    Greek words, as [(path, question)]."""
    found = []
    for f in sorted(os.listdir(directory)):
        stem, ext = os.path.splitext(f)
        meta = os.path.join(directory, stem + '.json')
        if ext.lower() in AUDIO and os.path.exists(meta):
            with open(meta, encoding='utf-8') as m:
                q = json.load(m)
            q.setdefault('id', stem)
            found.append((os.path.join(directory, f), q))
    return found


def load_vocab():
    return score.load_vocab(os.path.join(TESTSET, 'vocab.json'))
