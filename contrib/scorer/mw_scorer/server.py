"""The scorer's HTTP face (docs/scorers.md): POST /score and GET /health.

One process, standard library only; the scorer behind it is anything with
score(target_text, lang, wav_bytes[, segments=...]) -> ReadingResult dict and health() -> dict.
A ValueError from it is the request's fault (400); anything else is a 500.
"""

import base64
import binascii
import io
import json
import sys
import time
import wave
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import NamedTuple

MAX_BODY = 64 << 20

# A request that takes longer than this many wall seconds is also logged as a warning:
# the scorer works one request at a time, so everything behind it waits.
SLOW_SECONDS = 60

# The languages a woven reading may have a segment in.
SEGMENT_LANGS = ("en", "el")


class Request(NamedTuple):
    """A /score request, read: the target's text, its language, the segments of a woven
    reading as [(text, lang)] in reading order (None for a one-language request) and the WAV."""
    target_text: str
    lang: str
    segments: list
    wav: bytes


def parse_request(req):
    """The Request in a decoded JSON object; a ValueError says what is wrong with it.

    Two shapes: {"target_text", "lang", "audio_wav_base64"} for a reading in one language
    (lang defaults to "en"), and {"segments": [{"text", "lang"}, ...], "audio_wav_base64"}
    for a woven one, the segments in reading order, each "en" or "el" (a region, "el-GR",
    counts as its language). A request with segments is scored by them; its target_text
    and lang are not used, and the Request's target_text is the segments' text joined."""
    segments = _segments(req["segments"]) if req.get("segments") is not None else None
    if segments is None:
        target, lang = req.get("target_text"), req.get("lang") or "en"
        if not isinstance(target, str) or not target.strip():
            raise ValueError("target_text is missing or empty")
        if not isinstance(lang, str):
            raise ValueError("lang is not a string")
    else:
        target, lang = " ".join(text for text, _ in segments), segments[0][1]
    audio = req.get("audio_wav_base64")
    if not isinstance(audio, str) or not audio:
        raise ValueError("audio_wav_base64 is missing or empty")
    try:
        wav = base64.b64decode(audio, validate=True)
    except (binascii.Error, ValueError):
        raise ValueError("audio_wav_base64 is not standard base64") from None
    return Request(target, lang, segments, wav)


def _segments(raw):
    if not isinstance(raw, list) or not raw:
        raise ValueError("segments is not a non-empty list of {text, lang}")
    out = []
    for k, seg in enumerate(raw):
        name = "segments[%d]" % k
        if not isinstance(seg, dict):
            raise ValueError("%s is not an object with text and lang" % name)
        text, lang = seg.get("text"), seg.get("lang")
        if not isinstance(text, str) or not text.strip():
            raise ValueError("%s text is missing or empty" % name)
        if not isinstance(lang, str) or not lang:
            raise ValueError("%s lang is missing: send en or el" % name)
        code = lang.split("-")[0].lower()
        if code not in SEGMENT_LANGS:
            raise ValueError("%s lang %r is not one a woven reading has: en or el" % (name, lang))
        out.append((text, code))
    return out


def audio_seconds(wav):
    """The length of a WAV file in seconds, or None when it is not one the standard library reads."""
    try:
        with wave.open(io.BytesIO(wav)) as w:
            return w.getnframes() / w.getframerate()
    except (wave.Error, EOFError, ZeroDivisionError):
        return None


def make_server(host, port, scorer, log=None, clock=time.monotonic):
    """A server on host:port answering for scorer; port 0 picks a free one.
    Written to log (standard error by default): a failure of the scorer (a 500), and for each
    request it scored one line with the audio's seconds and the wall seconds it took, and a
    warning line when that was over SLOW_SECONDS. clock is the wall clock, for a test to move."""
    log = log or sys.stderr

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            if self.path != "/health":
                return self._text(404, "no such path: GET /health or POST /score")
            self._json(200, {"status": "ok", **scorer.health()})

        def do_POST(self):
            if self.path != "/score":
                return self._text(404, "no such path: GET /health or POST /score")
            length = int(self.headers.get("Content-Length") or 0)
            if length > MAX_BODY:
                return self._text(413, "the request is larger than %d bytes" % MAX_BODY)
            try:
                req = json.loads(self.rfile.read(length) or b"null")
            except ValueError:
                return self._text(400, "the request is not JSON")
            if not isinstance(req, dict):
                return self._text(400, "the request is not a JSON object")
            try:
                parsed = parse_request(req)
            except ValueError as err:
                return self._text(400, str(err))
            started = clock()
            try:
                if parsed.segments is None:
                    result = scorer.score(parsed.target_text, parsed.lang, parsed.wav)
                else:
                    result = scorer.score(parsed.target_text, parsed.lang, parsed.wav, segments=parsed.segments)
            except ValueError as err:
                return self._text(400, str(err))
            except Exception as err:  # noqa: BLE001 - every other failure is the scorer's, said as a 500
                return self._text(500, "the scorer failed: %s" % err)
            self._note_scored(parsed.wav, clock() - started)
            self._json(200, result)

        def _note_scored(self, wav, wall):
            audio = audio_seconds(wav)
            heard = "?" if audio is None else "%.1fs" % audio
            print("mw-scorer: scored audio=%s wall=%.1fs" % (heard, wall), file=log, flush=True)
            if wall > SLOW_SECONDS:
                print("mw-scorer: WARNING: a request took %.1fs (over %ds) for %s of audio: the scorer takes "
                      "one request at a time and others waited behind it" % (wall, SLOW_SECONDS, heard),
                      file=log, flush=True)

        def _json(self, status, body):
            self._send(status, "application/json", json.dumps(body).encode())

        def _text(self, status, line):
            if status >= 500:
                print("mw-scorer: %s" % line, file=log, flush=True)
            self._send(status, "text/plain; charset=utf-8", (line + "\n").encode())

        def _send(self, status, kind, data):
            self.send_response(status)
            self.send_header("Content-Type", kind)
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def log_message(self, fmt, *args):
            pass

    return ThreadingHTTPServer((host, port), Handler)
