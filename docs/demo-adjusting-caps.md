# Demo: caps that adjust themselves, on his phone

The last story (mw-t0z3fu.3) of the epic mw-t0z3fu. With both hosts busy, the
Governor reads each host's status on his phone and sees how many of its cap it
is using, why it is holding a story back, and each host's usual gate time beside
the latest, then says `Looks good`. This page is the script. The Governor does
the numbered steps; the Mayor does the things marked *Mayor* before he starts.

What the epic built, which this demo shows: `mw dispatch` starts a story on a
host only while the host has room (its 1-minute load under its core count, and
at least `room_min_free_mb` of memory available; the cap in config stays the
ceiling) (mw-t0z3fu.1); every close-out records its gate time and the host's
load, and `mw status` sets each host's usual gate time against its latest
(mw-t0z3fu.2); while he is using a tutor (grist in flight or recent) the home
starts fewer stories, and none while tutor answers are slower than their par
(mw-t0z3fu.4, mw-t0z3fu.5).

Run it while both hosts are working stories, so there is a cap line with
something in it. A hold-back reason shows only while a host really has no room;
a quiet host shows none, which is also right (step 4 says what to do then).

## Before the Governor starts (Mayor)

1. Run `mw status` on the Laptop and on the Desktop (each host only reports
   itself). Post the top of each, down to and including the `grist:` line, and
   each host's lines under `BENCHMARKS`, into the Factory channel as one
   message headed with the host's name. Copy them as printed: every line is at
   most 60 columns, so they fit a phone.
2. If neither host is holding a story back at that moment, say so in the
   message and tell him step 4 will show the host line alone. Do not lower a cap
   or a threshold to make one appear.
3. Send him a Postern message that the demo is ready, with this page's steps.

## The demo (Governor, on the phone)

1. Open the **Postern** app, then the **Factory** channel, and open the
   Mayor's newest status message (one block per host: Laptop, Desktop).

2. Find each host's line `host: N of cap M`.
   Right looks like: for each host, N is how many stories it is running now and
   M is its cap, for example `host: 4 of cap 4` on a busy host. N never exceeds
   M.

3. Under `host: N of cap M`, find the `held back, no room:` lines, if any.
   Right looks like: one line a reason, in plain figures, for example
   `held back, no room: load 35.0 of 20 cores` (the 1-minute load against the
   host's core count) or `held back, no room: 1800 MB free, under 2048 MB`
   (the memory available against the floor). A host showing one starts no new
   story until it clears; stories already running are not touched.

4. If neither host shows a `held back` line, the Mayor says so in the message.
   Right looks like: the `host: N of cap M` line stands alone under the host's
   name. A host with room has nothing more to say.

5. Under `BENCHMARKS`, find a line `<rig> on <host>: gate usual 5m10s, latest 9m02s`
   for each host that landed a story lately.
   Right looks like: both hosts have such a line for a rig they landed on. The
   usual time is the middle of that host's last ten test gates for the rig and
   the latest is the most recent gate. A latest far above the usual means the
   host was busier than usual when the gate ran.

6. On the host that runs the tutor, find the `grist:` line, if the Mayor posted
   one.
   Right looks like: `grist: quiet (none in 10 min)` when no one is using a
   tutor. While one is in use, `grist: active (3 in 10 min), median 14 s
   (par 10 s)` and under it `grist first: 2 stories at most while it lasts`,
   so the home starts no new story past that number. If tutor answers are
   slower than their par the line reads `grist: slow`, and a `held back, no
   room: slow grist: ...` line beneath says the home starts none until they
   recover.

7. Reply in the bead mw-t0z3fu.3's channel with the words `Looks good`.
   Right looks like: the demo stays open until you say it, and then closes with
   a comment quoting you (only that exact text, any case, one final `.` or `!`,
   closes a demo; anything else is a plain comment).

## If something is wrong

- A host's status shows no `host: N of cap M` line: that host's config has no
  `cap`, or the post was copied from an old binary. Tell the Mayor; he checks
  `mw status` on the host itself.
- N is above M: tell the Mayor with the host and the time. The cap is the
  ceiling and should never be passed.
- A `held back` line stays on a quiet host after the load has fallen: tell the
  Mayor. The line reads the load at the moment of the status, so a stale one is
  a fault.
- No `gate usual ... latest ...` line for a host that landed stories since
  2026-10-09 23:48Z: the close-out did not record its gate. Tell the Mayor.
