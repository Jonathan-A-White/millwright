"""The scorer's HTTP face (docs/scorers.md): POST /score and GET /health.

One process, standard library only; the scorer behind it is anything with
score(target_text, lang, wav_bytes) -> ReadingResult dict and health() -> dict.
A ValueError from it is the request's fault (400); anything else is a 500.
"""

import base64
import binascii
import json
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MAX_BODY = 64 << 20


def make_server(host, port, scorer, log=None):
    """A server on host:port answering for scorer; port 0 picks a free one.
    A failure of the scorer (a 500) is also written to log, standard error by default."""
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
            target, lang, audio = req.get("target_text"), req.get("lang") or "en", req.get("audio_wav_base64")
            if not isinstance(target, str) or not target.strip():
                return self._text(400, "target_text is missing or empty")
            if not isinstance(lang, str):
                return self._text(400, "lang is not a string")
            if not isinstance(audio, str) or not audio:
                return self._text(400, "audio_wav_base64 is missing or empty")
            try:
                wav = base64.b64decode(audio, validate=True)
            except (binascii.Error, ValueError):
                return self._text(400, "audio_wav_base64 is not standard base64")
            try:
                result = scorer.score(target, lang, wav)
            except ValueError as err:
                return self._text(400, str(err))
            except Exception as err:  # noqa: BLE001 - every other failure is the scorer's, said as a 500
                return self._text(500, "the scorer failed: %s" % err)
            self._json(200, result)

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
