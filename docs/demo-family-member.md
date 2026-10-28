# Demo: a member's factory, as the parent sees it

The page of the family-member epic's demo (see [family.md](family.md)). Each story of the
epic that the holder can see adds its step here; the demo story owns the rest.

## The parent's note (mw-9rp4hu.22)

Once a day, at 7:00 local, the member's factory posts one note to the holder's key on the
member's own Postern backend: the holder reads it in their second Postern account. It is a few
plain sentences, for oversight and not for debugging: how many areas of work moved, roughly
how much was finished (nothing, a little, a fair amount, a lot), whether some work did not
pass its checks and will be tried again, and how many things wait on a decision. It carries no
bead id, no story title, no project, model or fuel figure, and no chat. A day with nothing
moved is one line: `Nothing moved in Luke's factory since yesterday.`

Set up, as the member's user (`mw member init` does not):

1. In `~/.config/mw/config.toml` give the note its words and, if 7:00 is not wanted, its hour:

   ```toml
   [report]
   name = "Luke"
   parent_hour = 7
   ```

   `postern_governor_key` there is the holder's key on the member's backend (family.md, step 8).

2. Try it without posting: `mw report send --class parent --dry-run` prints the note.
   `mw report send --class parent --force` posts one at any hour.

3. Install the hourly pair, which posts only in its own local hour and once a local day:

   ```sh
   cp contrib/systemd/mw-parent-note.service contrib/systemd/mw-parent-note.timer ~/.config/systemd/user/
   systemctl --user daemon-reload && systemctl --user enable --now mw-parent-note.timer
   ```

   The service runs `~/.local/bin/mw`, as `mw-report.service` does.

### HOW TO CHECK IT, for the Governor

1. At 7:00 local, open your second Postern account (the one that is the cockpit key on the
   member's backend).
2. Right looks like one new note in the **Factory** channel, from the member's Mayor, of
   at most four short lines in plain words, such as: `Luke's factory, 2026-10-29`,
   `Luke worked on 2 areas of work and finished a fair amount.` and `1 thing is waiting on a decision.`
3. Read it for story titles, ids, project names or token and dollar figures: there are none.
4. On a day nothing moved, the note is the one line saying so.
5. It comes once: nothing more arrives that day.
