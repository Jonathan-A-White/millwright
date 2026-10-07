"""python -m mw_scorer [--host H] [--port P]: load the model, then listen.

python -m mw_scorer --download fetches the model into the cache and stops.
Settings from the environment (the unit's Environment= lines):
  MW_SCORER_HESITATION_SECONDS  the gap before a word that is a hesitation (0.8)
  MW_SCORER_THREADS             CPU threads the model may use (4)
"""

import argparse
import os
import sys

from . import align


def main():
    ap = argparse.ArgumentParser(prog="mw-scorer")
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--port", type=int, default=8765)
    ap.add_argument("--download", action="store_true", help="fetch the model into the cache and stop")
    args = ap.parse_args()

    if args.download:
        from . import listen

        print(listen.download())
        return 0

    from . import pipeline, server

    try:
        scorer = pipeline.Scorer(
            threads=int(os.environ.get("MW_SCORER_THREADS") or 4),
            hesitation_seconds=float(os.environ.get("MW_SCORER_HESITATION_SECONDS") or align.HESITATION_SECONDS),
        )
    except RuntimeError as err:
        print("mw-scorer: %s" % err, file=sys.stderr)
        return 1
    httpd = server.make_server(args.host, args.port, scorer)
    print("mw-scorer: listening on %s:%d" % (args.host, args.port), flush=True)
    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        pass
    return 0


if __name__ == "__main__":
    sys.exit(main())
