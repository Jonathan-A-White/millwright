# Research: a floating HOW TO CHECK IT window beside the app under test

Answers mw-6ww.80, for the grilling mw-6ww.57. His want, 2026-10-01: a verify card in the app, and
"a separate pop-up/window so I can see instructions while working with the app". Read-only
research: no product code changed. Postern's files are cited from `~/postern`.

## What exists today

The card is already most of it. Missing is only the steps staying on screen once he leaves the card.

- mw puts the steps on the need: `howToCheck` (`application/posternview.go:904`) cuts the section
  after HOW TO CHECK IT out of the closing comment, 1500 runes at most (`:843`); `needs`
  (`:626`) makes a verify need for each story that landed and waits on VERIFIED.
- Postern shows them: `NeedCard.tsx:154-156,203-208` renders a verify card's text in full under
  the heading "How to check it", images at full width. The button "I checked it: it works"
  (`labels.ts:17`) sends the `verified` action in one tap (`NeedCard.tsx:130`). No server or mw
  change is needed to show steps anywhere in the app.
- Postern is a standalone PWA (`pwa-manifest.ts:7`). Its one shell, `Shell.tsx:203-211`, wraps every
  screen: banners at the top, the screen, the tab bar at the bottom (phone) or a sidebar (wide). A
  piece placed in the shell survives every route change.
- Pieces to reuse: the Screen Wake Lock (`src/services/wakeLock.ts`, used by the Talk screen); local
  state in Dexie (`src/data/db.ts`); notifications with action buttons (`src/push/classOptions.ts:144`,
  the ring, and `src/sw.ts:109-137`).

## What a phone PWA can do (Android Chrome)

- **Document Picture-in-Picture** (an always-on-top window of any HTML): desktop Chrome and Edge 116+
  only. Not on Chrome for Android, and Chromium says Android's windowing cannot host an
  interactive always-on-top window ([browser limits](https://www.rabbitpair.com/en/products/dualpip/faqs/document-pip-browser-limits),
  [Intent to Ship](https://groups.google.com/a/chromium.org/g/blink-dev/c/JTPl7fM64Lc)). Ruled out on the phone.
- **Video Picture-in-Picture**: Chrome on Android floats a `<video>` over other apps
  ([Chrome blog](https://developer.chrome.com/blog/picture-in-picture)). Text could be drawn on a
  canvas and fed to a video, but it is a picture: no scrolling, no button, small, and the video
  must keep playing. I did not test it and would not build on it.
- **Overlay over other apps** (`SYSTEM_ALERT_WINDOW`, chat-head bubbles): native Android only. A PWA
  cannot ask for it. Only a native app could, a new thing to build and keep.
- **Split screen**: Android's own multi-window lets two apps share the screen
  ([Android Central](https://androidcentral.com/heres-neat-chrome-multiwindow-trick-you-probably-didnt-know-about)).
  An installed PWA is an ordinary app to it, so Postern can sit in one half and the app under test in
  the other. No code. From memory, not checked here, and it depends on his phone: whether an
  installed Postern accepts a half, and how small the halves are.
- **Notification**: a notification stays in the shade until dismissed, can carry the steps as its
  body and an action button. On Android Chrome `requireInteraction` is ignored
  ([MDN](https://developer.mozilla.org/docs/Web/API/ServiceWorkerRegistration/showNotification),
  [web.dev](https://web.dev/push-notifications-notification-behaviour/)); it does not float, he pulls the shade
  down to read it, and a long body is cut short unless he expands it.
- **A second device**: the Laptop open on Postern, the phone in his hand. No code, and Document PiP
  works there.

## The options, with cost

Fuel is the budget in CONTEXT.md. "Moving parts" are the places that can break.

| Option | Fuel | Moving parts | Works for | Catch |
|---|---|---|---|---|
| A. Steps sheet in Postern's shell | One Postern story, near the size of mw-tbx1n.15 | 1 component in `Shell.tsx`, 1 Dexie row (open, ticks) | Verifying Postern itself | Covers part of the screen under test; useless for another app |
| B. Split screen | None | 0 | Any app | Phone-dependent, small halves; his gesture each time |
| C. Notification with steps and the VERIFIED action | One Postern story plus a worker handler | Permission, `sw.ts` handler, tag per bead, the signed action from the worker | Any app | Not beside the app; the worker would sign `verified` itself |
| D. Document PiP | Small, on top of A | Feature detect, fall back to A | Desktop Chrome only | Not the phone |
| E. Video PiP of canvas text | Medium | Canvas, video, play state | Any app | Read-only picture; fragile |
| F. Native overlay | Large | A new native app | Any app | A second product to keep |
| G. Laptop beside the phone | None | 0 | Any app | Needs both in hand |

## Recommendation

Build **A**: a steps sheet in Postern's shell that opens from the verify card ("Keep these steps
open"). Closed, it is one line above the tab bar: "Step 3 of 5: <text>" with a small
VERIFIED button; tapped, it grows to the full steps over the lower half of the screen and shrinks
again. It stays as he moves between Places, because it lives in the shell. Ticks are local. It
sends the same `verified` action the card sends, so mw is untouched. It holds the screen awake
with `holdAwake`. This is the cheapest thing that keeps the steps visible while he uses Postern, the app he was
tapping in the screenshot. Meanwhile **B** costs nothing, and for any other app it is the answer he
can try today. Hold **D** until he verifies on the Laptop; it would be a small add-on to A.

Do not build C, E or F now. C does not sit beside the app; E and F cost more than A and give less.

## Questions only the Governor can answer

1. **Which app is usually under test: Postern, or others?** Recommended: Postern most of the time, so
   A. If the others are common, B for those.
2. **Try split screen once now: does your installed Postern take one half, with the app under test in
   the other?** Recommended: yes, a two-minute try, because it costs nothing and may be enough alone.
3. **Where does he verify: the phone, or the Laptop?** Recommended: the phone. If the Laptop,
   add D after A.
4. **Is a tick on each step kept on the bead, or only the final word?** (His question 3 on mw-6ww.57.)
   Recommended: only the final word. A tick is for his eyes; recording it needs a new action and a
   mw change to learn nothing the factory acts on.
5. **Does the sheet start closed to a one-line step, or open?** Recommended: one line, so the app
   under test is visible, one tap to open it in full.
6. **Should his "Walk me through this" chat (the judgment calls added to the steps) be in the
   sheet?** Recommended: no. That is words written by a model per landing, which spends fuel each
   time. Show the closing comment's steps as written; if the steps are hard to follow, fix the
   formula's wording, which is free once.
7. **Does it fold into the live cards of mw-6ww.56 stage 2?** Recommended: not now. The sheet reads
   the verify need's text, which exists today; folding in later changes where the data comes from,
   not the sheet.
