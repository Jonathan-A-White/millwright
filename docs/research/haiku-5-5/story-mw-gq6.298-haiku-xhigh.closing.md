Closing comment, mw-gq6.298 (REPLAY in a scratch worktree; no bead touched, no bd run, nothing pushed)

What was done
- Root .gitignore: added `__pycache__/` and `*.pyc` under the existing `bin/` line. The rig already keeps its ignore rules in the root file; contrib/scorer has no .gitignore of its own. Commit 78d3b34.
- Untracked the six compiled files under contrib/scorer/mw_scorer/__pycache__/ (__init__, align, g2p, listen, phones, pipeline; all cpython-312) with `git rm --cached`. The files stay on disk and are now ignored. Commit e2c5aba.
- No other change.

What was verified and how
- Before any edit, the acceptance checks failed for the right reason: `git ls-files | grep -E "__pycache__|\.pyc$"` printed the six files, and `git check-ignore contrib/scorer/mw_scorer/__pycache__/x.pyc` exited 1.
- After the two commits: the ls-files grep printed nothing (grep exit 1); check-ignore exited 0 and reported `.gitignore:2:__pycache__/`. align.py and __init__.py are not ignored.
- `make build`: exit 0.
- `make test`: exit 0. 38 package lines reported ok and the log has no FAIL or panic (/tmp/haiku-trial/story-mw-gq6.298-haiku-xhigh.meta/make-test.log).
- `make lint`: exit 0 (log: /tmp/haiku-trial/story-mw-gq6.298-haiku-xhigh.meta/make-lint.log). Two lines in it are properties of this box, not of this change: check-install-units printed "shellcheck is not installed here, so the scripts were NOT linted", and mw-heavy ran its stand-in path with no memory cap because systemd-run is not installed.
- `git status` is clean after the commits. Neither commit message has a Co-Authored-By, Generated or other AI attribution line.

Left undone or not run
- contrib/scorer/test.sh (the Python tests) was not run: it needs the scorer venv, espeak-ng and the model cache. No Python source changed.
- The six .pyc files were not deleted from disk in this worktree, because the story said --cached. They are ignored there.
- Process note: while checking the pyc history with `git log --all`, the output listed a later commit (ffbf76e) whose subject carries this story's id. I did not open its diff; this change was written from the tree and the story text alone.

Internal: nothing for the Governor to look at

What's new: `git ls-files` lists no compiled Python under contrib/scorer, and the root .gitignore ignores `__pycache__/` and `*.pyc` in every directory.

For the rig memory:
- make lint exits 0 on a box without shellcheck; check-install-units then prints "shellcheck is not installed here, so the scripts were NOT linted". Read that line before calling lint clean.
- A .gitignore rule does not untrack a file already in the index; `git rm --cached` is the step that does.
