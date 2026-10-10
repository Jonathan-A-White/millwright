# Demo: the Builder's memory of a rig as typed facts, on his phone

The last story (mw-rz2dak.8) of the epic mw-rz2dak. The Governor reads lampas's
migrated memory on his phone: a count of its facts by status, one fact file, a
superseded pair, and the `RIG MEMORY` part of `mw status`, then says
`Looks good`. This page is the script. The Governor does the numbered steps; the
Mayor does the things marked *Mayor* before he starts.

What the epic built, which this demo shows: a rig can be kept as facts, not one
memory file: a folder `seats/builder/rigs/<rig>/` with an `about.md` and
`facts/<slug>.md`, one typed fact a file (a sentence, a source and a status), which a
Builder boots from, rendered compactly (mw-rz2dak.1); `mw memory add`, `supersede`, `retire` and `recheck` change them
and delete none, so a replaced or retired fact keeps its sentence and why
(mw-rz2dak.2); `mw memory list` and `query` read them; `mw memory migrate` turns a rig's one file
into facts, once; `mw memory demote` keeps a fact out of boot; and a Builder
reads only the current facts, so the memory stays small as it grows
(mw-rz2dak.3 to mw-rz2dak.9).

## Before the Governor starts (Mayor)

1. Migrate lampas, once: `mw memory migrate lampas --dry-run` first, then
   `mw memory migrate lampas`, then commit and push the vault so GitHub has the
   files.
2. Pick the superseded pair (step 3 below). The old fact must carry a
   `reason:` line, because `supersede` keeps the old fact's reason but does not
   write one. If no superseded fact in lampas has one, take a current fact that
   really is out of date, run `mw memory recheck lampas <slug> --why "<what
   changed>"`, then `mw memory supersede lampas <slug> --source mayor:<date>
   "<the new sentence>"`. Commit and push the vault again. Do not invent a fact:
   use one that is true.
3. Run `mw memory list lampas` and post the output into this bead's channel
   exactly as printed (it fits a phone: one fact a line). Run `mw status` on the
   Laptop and post the top and the `RIG MEMORY` part if there is one, and if
   there is not, say so in the message (step 4 explains).
4. Post the GitHub address of one fact file and of the superseded pair (the
   old and the new), under the vault's repository
   `https://github.com/Jonathan-A-White/millwright-vault`, in the paths below.
5. Send him a Postern message that the demo is ready, with this page's steps.

## The demo (Governor, on the phone)

1. Open the **Postern** app, then this bead's channel (mw-rz2dak.8), and open
   the Mayor's paste of `mw memory list lampas`.
   Right looks like: one line a fact, in the form
   `slug  status  kind  [subject]  since  source`, for example
   `some-slug  current  gotcha  [bd]  2026-10-10  mw-123`, and a last line
   `render N/8000 bytes`. Current facts come first, then any in doubt
   (`recheck`), then `superseded`, then `retired`. Count the lines by their
   second word: the Mayor's message says how many are `current`, `superseded`
   and `retired`, and your count matches. N is under 8000: that is what a
   Builder reads about lampas, and it fits the budget.

2. Open one fact on GitHub: the Mayor's link, to a file under
   `seats/builder/rigs/lampas/facts/` in the `millwright-vault` repository.
   Right looks like: a short block between two `---` lines, then one sentence:

   ```
   ---
   subject: <what it is about>
   kind: gotcha
   status: current
   source: <a bead, or mayor:<date>>
   since: 2026-10-10
   ---

   <one sentence>
   ```

   There is a sentence, a `source:` that says where it came from, and a
   `status:` of `current`. `kind:` is `gotcha` or `decision`. Beside the `facts/` folder, in
   `seats/builder/rigs/lampas/`, is `about.md`, saying what lampas is.

3. Open the superseded pair: the Mayor's two links, the old fact and the new,
   both in `seats/builder/rigs/lampas/facts/`.
   Right looks like: the old fact has `status: superseded` and
   `superseded-by: <the new slug>`, and a `reason:` line still there with its
   sentence below it, unchanged. The new fact has `status: current` and
   `supersedes: <the old slug>`. The two name each other, and the old one was
   not deleted.

4. Find the `RIG MEMORY` part of the Mayor's `mw status`, or its absence.
   Right looks like: lampas is under budget, so it is not named. `RIG MEMORY`
   is a warning section: it lists only a rig whose memory has outgrown its
   budget, as `lampas N/8000 bytes: retire or supersede (Mayor)`, or one whose
   eval fails, as `lampas eval failing: 1 of 5`. With lampas fine, the section
   is not there at all, or names other rigs and not lampas. The `render N/8000
   bytes` line from step 1 is the same size, so N under 8000 says the same
   thing.

5. Reply in the bead mw-rz2dak.8's channel with the words `Looks good`.
   Right looks like: the demo stays open until you say it, and then closes with
   a comment quoting you (only that exact text, any case, one final `.` or `!`,
   closes a demo; anything else is a plain comment). That closes the epic.

## If something is wrong

- A line in step 1 has no status word, or a `skipped:` line follows the facts:
  a fact file is not whole (a key missing, a date that is not a date). Tell the
  Mayor; `skipped:` names the file and why, and a bad file never stops a
  Builder's boot.
- N is 8000 or more in step 1, or `lampas N/8000 bytes` is under `RIG MEMORY`:
  lampas's memory is over budget. Tell the Mayor; he retires or supersedes
  facts until it fits, which deletes none.
- The old fact in step 3 has no `reason:` line, or no `superseded-by:`: the
  pair was not picked as the page says, or the link goes to a different pair.
  Tell the Mayor.
- A fact has no `source:` or its sentence runs over several lines: tell the
  Mayor with the file name.
