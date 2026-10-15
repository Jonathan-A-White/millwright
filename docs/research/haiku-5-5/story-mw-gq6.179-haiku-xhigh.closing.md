mw-gq6.179 closing comment (REPLAY, haiku, xhigh, scratch worktree; no bead touched, nothing pushed)

WHAT WAS DONE
- The story's stated cause is not what the code does. contrib/mail-notify's $count is the number of NEW ids (`count=$(echo "$new" | wc -l ...)`, ~line 327), not the inbox total. On the normal path (announced file present) the line already said the new count. I reproduced the story's exact case (233 listed, 231 already announced, 2 new): the line said 2 before any change.
- The one path where the whole inbox is typed as new is the first run, or after the `announced` file is deleted: `if [ ! -f "$STATE/announced" ]; then new=$current`. A probe (temporary test, deleted) confirmed it types "New mail for mayor: 233 message(s)". This is documented in README ("deleting that file makes it announce whatever is unread again") and is intentional. I did not change it.
- Commit 1bb31c6: adds TestTheTypedCountIsTheNewMessagesNotTheWholeInbox (contrib/mailnotify_test.go): 233 listed, 231 announced, 2 new, expects the line to say 2. It passes on HEAD: the count was already right.
- Commit 8b7d828: the wording the story's DO step asks for, "New mail for mayor: <n> new message(s). Run bd mail inbox." Changed in contrib/mail-notify (typed line and header comment), the announcement constant in contrib/mailnotify_test.go, the fallback expectation in contrib/mailwait_test.go, and README.md's description of the line.

WHAT WAS VERIFIED, AND HOW
- Red before the wording change: with the expected string changed, twelve mail-notify tests failed in a filtered run. I read three of them (the new test, TestTheLimitDefaultsToTheCoreCount, and TestTheMayorsWindowIsFoundFromWhateverTheActingFileSays): each failed only on the wording, e.g. typed "New mail for mayor: 2 message(s)..." want "...2 new message(s)...". I did not read the other nine.
- Green after: `make build` exit 0; `make test` exit 0 (1m22s, every package ok, including contrib and features); `make lint` exit 0; `gofmt -l` on the changed Go files prints nothing; `sh -n contrib/mail-notify` clean.
- Commit 1bb31c6 checked alone in an exported copy (`git archive`, no .git): its new test passes. That export also failed TestCodemapAttributeIsUnion, because `git check-attr` needs a repository. That is the export, not the commit; the real worktree run passed it.
- Tests use stand-ins for bd, mw and tmux; nothing was run on a live host, and the Mayor's window was not typed into.

LEFT UNDONE, FOR THE MAYOR TO FILE
- The reset path still types the whole unread inbox as "new" (`233 new message(s)` now). The wording change does not fix that. Whether the first run should announce silently, or say "N unread", is a decision for the Governor. It may be what he saw.
- The Millhand's line (application/millhandtick.go, MillhandNudgeFormat, "New mail for millhand: %d message(s)") counts all unread mail in the Millhand's boxes, so it has the same whole-box shape. Not changed: it is another notifier and outside this story. Its comment (millhandtick.go:537-540) says it uses "the same words the Mayor's own notifier types", and features/millhand_tick.feature:21-22 says the same. Both are now stale for the word "new".
- contrib/mail-wait's log line "NEW MAIL for ...: N message(s)." counts the new ids correctly and is not typed. Left as it is.

The typed line is visible in the Mayor's window, so a HOW TO CHECK IT section follows.

HOW TO CHECK IT, for the Governor
1. On a host that runs mw-mail-notify, with the Mayor's window idle and its input line empty, send the Mayor one message.
2. Wait for the next notifier tick.
3. Read the Mayor's window. Right: "New mail for mayor: 1 new message(s). Run bd mail inbox." The number counts the messages that arrived since the last announcement, not the inbox total.
4. Not checked here: this replay ran in a scratch worktree, so step 3 has not been seen on a live host.
5. Known gap: after the notifier's `announced` file is deleted, the line will show the whole unread inbox as "new". That is the reset path, left as it is above.

What's new: the Mayor's window line says "N new message(s)", and a test pins the typed count to the new ids (233 listed, 2 new, says 2).

For the rig memory:
- The notifier's count is the new ids: `count` is wc of `$new` in contrib/mail-notify (~line 327). A whole-inbox count is typed only on the first run or after the `announced` file is deleted (`new=$current`). Check that path before hunting a count bug.
- The Millhand nudge (application/millhandtick.go MillhandNudgeFormat) counts all unread mail and says "message(s)"; its comment and features/millhand_tick.feature say it matches the Mayor's line, which now says "new".
