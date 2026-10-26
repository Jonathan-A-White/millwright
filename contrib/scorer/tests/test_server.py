"""The HTTP face of the scorer, with a stand-in for the model: no torch needed.

These run with any python3 (test.sh --pure).
"""

import base64
import io
import json
import threading
import unittest
import wave
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
        self.took = 0
        self.clock = None

    def score(self, target_text, lang, wav, segments=None):
        self.asked.append((target_text, lang, wav) if segments is None else (target_text, lang, wav, segments))
        if self.clock:
            self.clock.now += self.took
        if self.raise_:
            raise self.raise_
        return ANSWER

    def health(self):
        return {"model": "stand-in"}


def silence_wav(seconds, rate=16000):
    """A mono 16-bit WAV of that many seconds of silence."""
    buf = io.BytesIO()
    with wave.open(buf, "wb") as w:
        w.setnchannels(1)
        w.setsampwidth(2)
        w.setframerate(rate)
        w.writeframes(b"\0\0" * int(seconds * rate))
    return buf.getvalue()


class Clock:
    """A clock the stand-in scorer moves: the server reads the wall time of a request from it."""

    def __init__(self):
        self.now = 1000.0

    def __call__(self):
        return self.now


class Parser(unittest.TestCase):
    AUDIO = base64.b64encode(b"RIFFwav").decode()

    def test_the_old_shape_is_still_accepted(self):
        got = server.parse_request({"target_text": "the cat", "lang": "el", "audio_wav_base64": self.AUDIO})
        self.assertEqual((got.target_text, got.lang, got.segments, got.wav), ("the cat", "el", None, b"RIFFwav"))

    def test_the_old_shape_defaults_to_english(self):
        got = server.parse_request({"target_text": "the cat", "audio_wav_base64": self.AUDIO})
        self.assertEqual((got.lang, got.segments), ("en", None))

    def test_segments_are_read_in_order_with_their_languages(self):
        got = server.parse_request({"segments": [{"text": "For God so loved", "lang": "en"},
                                                 {"text": "τὸν κόσμον", "lang": "el-GR"}],
                                    "audio_wav_base64": self.AUDIO})
        self.assertEqual(got.segments, [("For God so loved", "en"), ("τὸν κόσμον", "el")])
        self.assertEqual(got.target_text, "For God so loved τὸν κόσμον")

    def test_segments_win_over_a_target_text_beside_them(self):
        got = server.parse_request({"target_text": "ignored", "lang": "en", "audio_wav_base64": self.AUDIO,
                                    "segments": [{"text": "τὸν", "lang": "el"}]})
        self.assertEqual((got.target_text, got.segments), ("τὸν", [("τὸν", "el")]))

    def test_a_segment_must_be_en_el_or_he(self):
        with self.assertRaises(ValueError) as err:
            server.parse_request({"segments": [{"text": "hola", "lang": "es"}], "audio_wav_base64": self.AUDIO})
        self.assertIn("en, el or he", str(err.exception))
        got = server.parse_request({"segments": [{"text": "In", "lang": "en"}, {"text": "בְּרֵאשִׁית", "lang": "he-IL"}],
                                    "audio_wav_base64": self.AUDIO})
        self.assertEqual(got.segments, [("In", "en"), ("בְּרֵאשִׁית", "he")])

    def test_segments_with_no_words_are_refused(self):
        with self.assertRaises(ValueError) as err:
            server.parse_request({"segments": [{"text": " ", "lang": "en"}], "audio_wav_base64": self.AUDIO})
        self.assertIn("segments[0]", str(err.exception))


class Server(unittest.TestCase):
    def setUp(self):
        self.scorer = StandIn()
        self.log = io.StringIO()
        self.clock = Clock()
        self.httpd = server.make_server("127.0.0.1", 0, self.scorer, log=self.log, clock=self.clock)
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

    def test_segments_are_passed_in_order_with_the_audio(self):
        status, body = self.call("POST", "/score", {
            "segments": [{"text": "For God so loved", "lang": "en"}, {"text": "τὸν κόσμον", "lang": "el"}],
            "audio_wav_base64": base64.b64encode(b"RIFFwav").decode()})
        self.assertEqual(status, 200, body)
        self.assertEqual(self.scorer.asked, [("For God so loved τὸν κόσμον", "en", b"RIFFwav",
                                              [("For God so loved", "en"), ("τὸν κόσμον", "el")])])

    def test_a_bad_segment_is_refused_400_naming_it(self):
        audio = base64.b64encode(b"RIFF").decode()
        for segments, word in [
            ("For God", "segments"), ([], "segments"), (["For God"], "segments[0]"),
            ([{"text": "For", "lang": "en"}, {"text": "", "lang": "el"}], "segments[1]"),
            ([{"text": "bonjour", "lang": "fr"}], "fr"), ([{"text": "cat"}], "lang"),
            ([{"text": 7, "lang": "en"}], "text"),
        ]:
            status, body = self.call("POST", "/score", {"segments": segments, "audio_wav_base64": audio})
            self.assertEqual(status, 400, (segments, body))
            self.assertIn(word, body)
        self.assertEqual(self.scorer.asked, [])

    def score_taking(self, audio_seconds, wall_seconds):
        """Post a clip of audio_seconds to a scorer that takes wall_seconds; return the log lines."""
        self.scorer.took = wall_seconds
        self.scorer.clock = self.clock
        status, body = self.call("POST", "/score", {
            "target_text": "cat", "audio_wav_base64": base64.b64encode(silence_wav(audio_seconds)).decode()})
        self.assertEqual(status, 200, body)
        return self.log.getvalue().splitlines()

    def test_every_request_logs_one_line_with_the_audio_seconds_and_the_wall_seconds(self):
        lines = self.score_taking(5, 0.4)
        self.assertEqual(lines, ["mw-scorer: scored audio=5.0s wall=0.4s"])

    def test_a_request_over_60_seconds_also_logs_a_warning(self):
        lines = self.score_taking(600, 75.5)
        self.assertEqual(lines, [
            "mw-scorer: scored audio=600.0s wall=75.5s",
            "mw-scorer: WARNING: a request took 75.5s (over 60s) for 600.0s of audio: "
            "the scorer takes one request at a time and others waited behind it",
        ])

    def test_a_request_of_exactly_60_seconds_logs_no_warning(self):
        self.assertEqual(len(self.score_taking(10, 60)), 1)

    def test_audio_that_is_not_a_wav_logs_its_seconds_as_unknown(self):
        self.scorer.took = 1
        self.scorer.clock = self.clock
        self.call("POST", "/score", {"target_text": "cat", "audio_wav_base64": base64.b64encode(b"RIFFwav").decode()})
        self.assertEqual(self.log.getvalue().splitlines(), ["mw-scorer: scored audio=? wall=1.0s"])

    def test_a_refused_request_logs_no_scored_line(self):
        self.call("POST", "/score", {"target_text": "cat"})
        self.assertEqual(self.log.getvalue(), "")

    def test_an_unknown_path_is_404(self):
        status, _ = self.call("GET", "/nothing")
        self.assertEqual(status, 404)


if __name__ == "__main__":
    unittest.main()
