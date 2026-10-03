# Research: Postern's manifest shortcuts and share target

Answers mw-6ww.81, for the grilling mw-6ww.73. The Governor, 2026-10-03 ~11:25Z, by postern: "/later
there should be widgets on the phone for key shortcuts like /later. I want to be able to reduce the
friction to go from idea to getting it down. Also, there should be shortcuts for power users of the
app. With a way of setting and learning what the shortcuts are (long presses, ...). But really
consider ergonomics because power users are most likely to get fatigue." Read-only research: no
product code changed. Paths below are in the `postern` repo at `e7381cd`.

## What exists today

**A share target, but no shortcuts.** `pwa-manifest.ts:14-24` declares `share_target`: `POST
/share-target`, multipart, fields `title`, `text`, `url`, and files (`image/*`, `audio/*`,
`application/pdf`, `text/plain`). There is no `shortcuts` member (`pwa-manifest.ts:3-39`). The
manifest's only test checks name and display (`tests/unit/manifest.test.ts:4-7`), so the share
target is untested. `vite.config.ts:48-56` feeds the manifest to vite-plugin-pwa (`injectManifest`).

**What a share does.** `src/sw.ts:192-207` (`parkShare`) joins title, text and url with newlines,
parks them with any files in IndexedDB and answers 303 to `/?v=share&s=<id>`; `src/sw.ts:209-214`
catches the POST. `ShareScreen.tsx:136-192` shows what arrived, then "Where to": New channel, the
last-used channel (`ShareScreen.tsx:107-109`), recent channels, a bead search. `sendTo`
(`ShareScreen.tsx:111-117`) hands the text and files to `shareInbox.ts`, and opens that channel;
the composer takes them once (`Composer.tsx:89-95`), the text only if the box is empty. So a share
costs: share, pick Postern, pick a place, then type `/later` before the text yourself.

**A way to open the composer saying `/later `, already built.** The `talk` route takes a `prefill`
(`nav/route.ts:19`, parsed at `:103`, written at `:156`), so
`/?v=talk&t=general&p=%2Flater%20` opens Factory with the box holding `/later ` and the caret
after it (`TalkScreen.tsx:244-248`, `Composer.tsx:58-59, 80-86`). Prompts → Run uses it
(`PromptsScreen.tsx:33`). Prefill is never saved as a draft (`useDraft.ts:13`). Locked, the app
shows Unlock in place without changing the route (`App.tsx:145-153`), so the prefill survives
unlock. Both `/later` and `/do` exist on the backend today, each with one required text option
(`mw prompt list`).

## What the platform allows (Android Chrome, installed app)

- **Manifest `shortcuts`:** the long-press menu on the app icon. Each entry is a name and a URL
  inside `scope` (`/`, `pwa-manifest.ts:9`). The launcher decides how many show, about four or
  five. The list is fixed by the manifest: Chrome has no dynamic-shortcut API for web apps, so
  the Governor cannot edit it in the app.
- Chrome re-reads the manifest at most daily; a changed `shortcuts` or `share_target` makes it
  rebuild the installed app, which lands once its windows are closed. Expect a day's delay.
- Shortcut icons are optional. Android does not take SVG for them, so icons would need PNGs;
  `public/` holds only `icon.svg`.
- Most Android launchers (the Pixel's included) let him drag a shortcut out of the long-press
  menu onto the home screen as its own one-tap icon. That is the nearest a web app gets to a
  widget. It is a launcher feature, not ours.
- **Share target:** text shares need no change; this is working as built. Android lists Postern
  once it is installed.
- **A real home-screen widget** needs a native app (Android's widget API): not available to a
  web app.
- **Unknown, to test on his phone:** whether a launch from a shortcut raises the keyboard. The
  composer focuses itself (`Composer.tsx:80-86`), but Chrome often will not show the soft
  keyboard for a focus that no tap caused. Prompts → Run works because a tap came first.

## Options

**A. Manifest shortcuts (recommended).** Add four entries: `/later` and `/do` (the prefill
URLs above), New message (`/?v=talk&t=general`), Talk line (`/?v=line`). No server change, no
new route. Moving parts: `pwa-manifest.ts`, a test of the entries, optionally four PNGs and
their precache lines (`pwa-precache.ts`). One small story, one deploy; no new store, no sync.

**B. Share-sheet `/later` and `/do`.** On the Share screen, put two buttons above "Where to":
"As /later" and "As /do". Each opens Factory with `/later ` plus the shared text. Moving parts:
`ShareScreen.tsx` and `shareInbox.ts` (carry a prefix); `Composer.tsx:93` (the shared text
today yields to a prefill, so it must be appended after it). One small story, no manifest
change and no wait for the app to update. This is the path from "I read something worth keeping" to
parked, in two taps after Share.

**C. In-app shortcut chips he can set.** A row of chips above the composer for the prompts he
uses most, picked by him, stored in `settings`. Moving parts: a settings key, a row in
`Composer.tsx`, an editor on Me. Medium story. Only this option lets him set the list; every tap
is inside the app, so it does nothing for "from the home screen".

**D. A native wrapper with a real widget.** An Android project (Gradle, SDK, signing, install by
hand) holding a widget that opens the same URLs as A. It adds a home-screen tile, and A's
drag-out gives that already. A second app also means a second place that must open the same
store and key. A new rig and the highest fuel by far, for little beyond A. Not recommended.

Ergonomics: the fatigue the Governor names comes from long presses and holds. A on its own costs
a long press plus a tap; dragged to the home screen, it is one tap. B is taps only. None of A, B
or C needs a hold; keep it so. Postern already asks for a long press in one place, the grey Send
(`Composer.tsx:194-199, 365`), and should not add more.

## Recommendation

Do A then B, as two stories, in that order. A gives the long-press menu and, by drag, home-screen
one-taps; B covers the other direction, from the idea sitting in another app. Hold C and D until
he has lived with A and B for a week. For learning: one short note on Me listing the four
shortcuts, how to drag one to the home screen, and the share path. It is static text, in the first story.

## Questions only the Governor can answer

1. **Which four shortcuts?** Recommended: `/later`, `/do`, New message, Talk line. A fifth may be
   cut by his launcher.
2. **If a shortcut opens the box but not the keyboard, is one extra tap on the box fine?**
   Recommended: yes. The first story's HOW TO CHECK IT will show which he gets.
3. **Which launcher is on his phone, and does he want `/later` dragged to the home screen?**
   Recommended: yes for `/later` only, one icon; the Pixel launcher allows it.
4. **Is a fixed list right, changed only by asking the Mayor?** Recommended: yes. If he wants to
   set them himself, that is C, later, not now.
5. **On the Share screen, may "As /later" and "As /do" sit above "Where to"?** Recommended: yes,
   `/later` first, both into Factory.
6. **Does a real widget still matter once A and B are in?** Recommended: no; ask again after a week.
