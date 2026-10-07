"""The HTTP face of the scorer, with a stand-in for the model: no torch needed.

These run with any python3 (test.sh --pure).
"""

import base64
import io
import json
import threading
import unittest
import urllib.error
import urllib.request

from mw_scorer import server

ANSWER = {
    "engine": "local",
    "words": [{"text": "cat", "expected_phonemes": ["K", "AE", "T"], "produced_phonemes": ["K", "AE", "T"],
               "error": "none", "accuracy": 100, "self_corrected": False}],
    "accuracy": 100,
    "seconds": 0.5,
}


class StandIn:
    """Records what it was asked and answers ANSWER, or raises what it is given."""

    def __init__(self):
        self.asked = []
        self.raise_ = None

    def score(self, target_text, lang, wav):
        self.asked.append((target_text, lang, wav))
        if self.raise_:
            raise self.raise_
        return ANSWER

    def health(self):
        return {"model": "stand-in"}


class Server(unittest.TestCase):
    def setUp(self):
        self.scorer = StandIn()
        self.httpd = server.make_server("127.0.0.1", 0, self.scorer, log=io.StringIO())
        self.url = "http://127.0.0.1:%d" % self.httpd.server_address[1]
        self.thread = threading.Thread(target=self.httpd.serve_forever, kwargs={"poll_interval": 0.05}, daemon=True)
        self.thread.start()

    def tearDown(self):
        self.httpd.shutdown()
        self.httpd.server_close()

    def call(self, method, path, body=None):
        data = None if body is None else (body if isinstance(body, bytes) else json.dumps(body).encode())
        req = urllib.request.Request(self.url + path, data=data, method=method,
                                     headers={"Content-Type": "application/json"})
        try:
            with urllib.request.urlopen(req, timeout=10) as resp:
                return resp.status, resp.read().decode()
        except urllib.error.HTTPError as err:
            return err.code, err.read().decode()

    def test_health_answers_ok(self):
        status, body = self.call("GET", "/health")
        self.assertEqual(status, 200)
        self.assertEqual(json.loads(body)["status"], "ok")

    def test_score_passes_the_request_to_the_scorer_and_answers_its_result(self):
        status, body = self.call("POST", "/score", {
            "target_text": "the cat", "lang": "en", "audio_wav_base64": base64.b64encode(b"RIFFwav").decode()})
        self.assertEqual(status, 200, body)
        self.assertEqual(json.loads(body), ANSWER)
        self.assertEqual(self.scorer.asked, [("the cat", "en", b"RIFFwav")])

    def test_lang_defaults_to_en(self):
        self.call("POST", "/score", {"target_text": "cat", "audio_wav_base64": base64.b64encode(b"RIFF").decode()})
        self.assertEqual(self.scorer.asked[0][1], "en")

    def test_a_request_that_is_not_json_is_refused_400(self):
        status, body = self.call("POST", "/score", b"not json")
        self.assertEqual(status, 400)
        self.assertIn("JSON", body)

    def test_a_request_with_no_target_or_no_audio_is_refused_naming_the_field(self):
        status, body = self.call("POST", "/score", {"audio_wav_base64": "UklGRg=="})
        self.assertEqual(status, 400)
        self.assertIn("target_text", body)
        status, body = self.call("POST", "/score", {"target_text": "cat"})
        self.assertEqual(status, 400)
        self.assertIn("audio_wav_base64", body)
        status, body = self.call("POST", "/score", {"target_text": "cat", "audio_wav_base64": "@@not base64@@"})
        self.assertEqual(status, 400)
        self.assertIn("audio_wav_base64", body)
        self.assertEqual(self.scorer.asked, [])

    def test_a_refusal_from_the_scorer_is_a_400_with_its_reason(self):
        self.scorer.raise_ = ValueError("the audio is not a WAV file")
        status, body = self.call("POST", "/score", {"target_text": "cat", "audio_wav_base64": "UklGRg=="})
        self.assertEqual(status, 400)
        self.assertIn("not a WAV file", body)

    def test_a_failure_in_the_scorer_is_a_500(self):
        self.scorer.raise_ = RuntimeError("out of memory")
        status, body = self.call("POST", "/score", {"target_text": "cat", "audio_wav_base64": "UklGRg=="})
        self.assertEqual(status, 500)
        self.assertIn("out of memory", body)

    def test_an_unknown_path_is_404(self):
        status, _ = self.call("GET", "/nothing")
        self.assertEqual(status, 404)


if __name__ == "__main__":
    unittest.main()
