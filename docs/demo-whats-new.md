# Demo: every app says what's new

The last story (mw-s061bg.5) of the epic mw-s061bg. After a Postern landing and a
Lampas landing that each carry a "What's new" note, the Governor sees the note
on his phone, from the **Update ready** banner to the GitHub page, and says
`Looks good`. This page is the script. The Governor does the numbered steps;
the Mayor does the things marked *Mayor* before he starts.

Run it only after both apps have a landing whose closing comment had a
`What's new: New: ...` or `What's new: Fixed: ...` line (not `none`), mw next has
written it into the app's changelog, and the deploy has gone out.

## Before the Governor starts (Mayor)

1. Pick the landing for each app: one Postern landing and one Lampas landing
   that carry a note, the newest of each. Check that `public/changelog.json` and
   `CHANGELOG.md` in each repo show the note under its `## X.Y.Z` heading, and
   that the deployed app is that version.
2. The banner appears only when the phone is on an *older* build than the
   deployed one. So tell him, in a Postern message, which two versions to expect
   (the version of the older build his phone is on, and the new one), and name
   the note each should show, in the words of the `What's new:` line. If his
   phone is already on the newest build, wait for the next landing that carries
   a note.
3. Send the steps below (or this page's path) with the message. Do not tell him
   what the banner will say beyond the two versions: he reads it on the screen.

## The demo (Governor, on the phone)

### Postern

1. Open the **Postern** app and leave it open on any screen for a moment (it
   checks for a new build on start, when you come back to it, and every 30
   minutes). To ask at once: **Me**, then **About and credits**, then under
   **What's new** tap **Check for updates**.
   Right looks like: a bar across the top of every screen reading
   **Update ready, tap to reload**. Under it, a line like
   `0.5.28 · 1 new · What's new` (the new version, how many **New** and **Fixed**
   lines it has, and a **What's new** button). From **Check for updates** the
   button first says **Checking…**, then **Update ready, tap to reload** appears
   beside it.

2. Tap **What's new** in that line (not the bar itself).
   Right looks like: a sheet titled **What's new** with the waiting versions,
   newest first, each with its date and its lines marked **New** or **Fixed**.
   The note the Mayor named is there, in plain words with no file names. Tap
   **Close**. The banner is still there: opening the sheet does not update.

3. Tap **Update ready, tap to reload**.
   Right looks like: the bar says **Updating…**, the page reloads, the bar is
   gone, and a sheet titled **What's new** opens by itself with every version
   since the last one you saw, the Mayor's note among them. Tap **Close**.
   Close the app and open it again: the sheet does not come back, it is shown
   once.

4. Go to **Me**, then **About and credits**, and find **What's new** (the
   section after the credits' opening lines).
   Right looks like: `Version` and the number of this build as a link, the
   **Check for updates** button, and below them every version, newest first, with
   its date and its **New** and **Fixed** lines. The top version is the one you
   just updated to.

5. Tap the version number.
   Right looks like: your browser opens **github.com/Jonathan-A-White/postern**
   at `CHANGELOG.md`, scrolled to that version's heading (`## 0.5.28` or
   whichever it is), and its line is the same as in the app.

6. Tap **Check for updates** once more.
   Right looks like: **Checking…**, then **Up to date**.

### Lampas

7. Open **Lampas** and wait a moment on the reader, or tap the gear
   (**Settings**), then **About**, and under **What's new** tap
   **Check for updates**.
   Right looks like: a bar across the top reading
   **Update ready, tap to reload**, and under it a line like
   `0.1.34 · 1 fixed · What's new`, the same shape as Postern's. (From **Check
   for updates** an **Update now** button also appears beside it; it does the same
   as the bar.)

8. Tap **What's new** in that line, read the sheet, and tap **Close**. Then tap
   **Update ready, tap to reload**.
   Right looks like: the same as Postern's steps 2 and 3: the sheet lists the
   waiting versions with **New** and **Fixed** lines; after the update the bar
   says **Updating…**, the page reloads, and **What's new** opens once by itself
   with the Mayor's note.

9. Open the gear (**Settings**), then **About**, and find **What's new**; then
   below it tap **What changed in X.Y.Z, on GitHub** (X.Y.Z is this build).
   Right looks like: **What's new** lists every version with its **New** and
   **Fixed** lines, **Check for updates** says **Checking…** then **Up to date**,
   and the link opens **github.com/Jonathan-A-White/lampas** at `CHANGELOG.md`,
   scrolled to that version's heading.

10. Say `Looks good` in the bead mw-s061bg.5's channel (or tell the Mayor what
    is wrong).
    Right looks like: the demo closes, with a comment quoting him, and the Mayor
    is mailed `Closed: mw-s061bg.5 on his Looks good` (only that exact text, any
    case, one final `.` or `!`, closes a demo; anything else is a plain
    comment). The Mayor then files one adopt story per other app, each with its
    `changelog_files` key.

## If something is wrong

- No bar and no **What's new** line: the phone is already on the newest build, or
  the app has not checked yet. Tap **Check for updates**; if it says
  **Up to date**, the Mayor waits for the next landing with a note.
- The bar shows but there is no line under it: the waiting build's changelog had
  nothing after the running version, or it could not be read (offline). Tell the
  Mayor; the update itself still works.
- **Couldn't check** after **Check for updates**: no network or no service
  worker (the app is open in a browser tab, not installed). Say so; install it
  to the home screen and try again.
- The version number opens the list instead of GitHub: that app's repo is private
  (both are public today). Tell the Mayor.
- GitHub opens `CHANGELOG.md` but at the top, not at the version: the heading is
  missing from the file. Tell the Mayor which version.
