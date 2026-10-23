# Fuel knobs

The dials that move fuel today, between a Max month (let it rip) and a Pro
month (make it last). Carved from the grilling mw-6ww.11 as mw-gq6.431.
Every dial row has the same five columns: the dial, what turns it, its value
now on each host, which way is cheaper, and what that way loses.

Where the values come from. cloud3's values were read from its
~/.config/mw/config.toml on 2026-10-17. The laptop's (the home) and the
desktop's are as the Mayor last recorded them, with the date and the bead or
vault file, and were not read on those hosts for this page: `mw config get cap`
(or any key here) on the host prints what it holds now. "Default" means the
config does not say, so mw's own default applies. Cloud hosts (cloud1 to
cloud4) are made from one snapshot and run alike, so cloud3 stands for them.

Any key here is changed with mw config set, the key and then its value (a
dated backup first, the way back printed): `mw config set cap <n>`. A key in
a table is written dotted: `mw config set grist.daily_limit 250`. The one
exception is a clock window, an array of tables, whose TOML the README shows
to paste.

## Sessions at once

| Dial | Turned by | Now, by host | Cheaper | What is lost |
|-|-|-|-|-|
| Session cap: Builder and Tester sessions a host runs at once | `cap` (`mw config set cap <n>`); `MW_CAP` answers first | laptop 5 (2026-10-09, his hands step on mw-6ww.11); desktop 2 by day (2026-10-15); cloud3 3 (read); default 1 | Lower: fewer sessions burn at once, so the week lasts longer | Landings a day; ready stories wait. Past about 5 on the laptop, load flakes refuse finished work and its fuel is spent twice |
| Clock window: the cap (and builder_jobs) inside set hours | `[[dispatch.windows]] cap`, `[[dispatch.windows]] builder_jobs`; checked by `mw doctor dispatch-windows` | desktop: night 23:00 to 06:00 America/New_York, cap 4 (mw-gq6.429, 2026-10-15); laptop none recorded; cloud3 none (read) | A shorter window, or none: the root cap holds all day | The night's extra sessions, when nobody else is on the desktop |
| Test workers a Builder's tests and the gate start at once | `builder_jobs` (`mw config set builder_jobs <n>`); `MW_BUILDER_JOBS` | desktop 4 (since 2026-10-13, for Grace's browsing); laptop none recorded; cloud3 unset, no limit (read) | Not a fuel dial by itself: it shares CPU. Unset or higher is the cheaper side in fuel, since a starved gate times out and refuses work already paid for | The host's CPU for whoever else uses it |
| The landing gate runs only the tests related to a story's files | `[related_tests] <rig>` (a command with {files} in it; delete the line to go back), in the home's config.toml (mw-gq6.601) | none recorded set (PROVISIONAL: the Mayor sets lampas's after it lands, to measure for 14 days) | A rig without the line runs its whole [tests] line; with it the gate is shorter, so a landing waits less and a refused one is run again sooner | A test the gate skips is run by the nightly suite (`mw suite <rig>`), a day later, as a held bug |
| Sessions at once while a tutor is in use | `[dispatch] grist_cap` | default (the cap less 2) on every host; none recorded set | Lower | Landings while someone is reading with a tutor |
| Cloud hosts: how many, how big, how dear | `[cloud] max_boxes` (`mw config set cloud.max_boxes <n>`), `[cloud] box_cap`, `[cloud] monthly_cap_usd`, `[cloud] idle_minutes` | home (laptop): max_boxes 4 (PROVISIONAL), box_cap 3, monthly_cap_usd 75, idle_minutes 30 (vault hosts/vultr/cloud-table.toml, 2026-10-15); four up on 2026-10-17, each cap 3; [cloud] is the home's only (cloud3 has none, read) | max_boxes 0 makes no new one; those up go down once idle | Each cloud host's sessions, and its dollars: the Vultr bill and fuel both stop |
| The home's count of each host's sessions, for the cloud | `[cloud.caps] <host>` | home: laptop 5, desktop 6 (cloud-table.toml); the cloud's count only, not the host's own cap | Higher counts make fewer cloud hosts | Cloud hosts made while the home and desktop really have no room |
| Builders at all on a host | `systemctl --user disable --now mw-dispatch.timer` | enabled on every host (cloud3 read). On the home the follower also starts mw-dispatch.service when a bead opens or lands, so there the timer alone does not stop it: lower the cap | Off on a host: no new story starts there; one running finishes | That host's landings |

## Model and effort, per kind of story

| Dial | Turned by | Now, by host | Cheaper | What is lost |
|-|-|-|-|-|
| Builder story: model and effort | `mw path set <story> model=<model> effort=<effort>`, before it starts | The rule (Mayor charter; his "sonnet default" 2026-09-21): Sonnet at high by default; Opus or Fable when the work warrants it, the reason written on the bead, and back down when the reason ends. Now: the quota push of 2026-10-15 21:57Z moved ready build stories to Opus, and he kept it on 2026-10-16 13:39Z. Path keys live on the story, not a host | Sonnet: about $1.37 a landing against Opus's $4.24 (health report, 7 days to 2026-10-17) | Opus landed first try 86% against Sonnet's 80%, with about 30% fewer escaped bugs a landing (`mw report` MODELS, 2026-10-16) |
| Tester story: model and effort | `mw path set <story> model=haiku effort=max`; `[tester] model` and `[tester] effort` for the Tester stories the trial springs | Tester stories on haiku at max since 2026-10-15 (PROVISIONAL, mw-2zh33g); [tester] is the home's only, its defaults sonnet and high | Haiku | Findings a stronger model would see; one Haiku Tester ran an hour of nothing on the desktop (mw-gq6.545) |
| Turns a Builder session may take | `[next] max_turns`; one story: `mw path set <story> max_turns=<n>` | default 120 on every host (mw-1ljpo9.6); cloud3 unset (read) | Lower | A story stopped at its cap is given back unfinished; with retry_from_branch its work is kept |
| Attempts a story gets | `max_attempts`; `MW_MAX_ATTEMPTS` | default 3; cloud3 unset (read) | Lower | A story that needed one more try stops for the Mayor instead |
| A refused attempt's work kept for the next | `[next] retry_from_branch` | default true; cloud3 unset (read) | true: the next attempt starts from the work, not from nothing | false starts clean; it is the way back when a kept branch is bad |
| Batch landing and its one fixer | `rigs/<rig>.toml landing`, `rigs/<rig>.toml batch_fixer_model`, `rigs/<rig>.toml batch_fixer_effort`, `rigs/<rig>.toml batch_fixer_turns` (vault files, every host alike) | lampas: batch, fixer sonnet at high, 60 turns (2026-10-15); every other rig each | batch: one gate run a batch and one fixer, not a fresh Builder for each conflict | A landing waits for its batch (batch_every, 30m); docs/land-rush.md |
| Rig memory every Builder reads at boot | `rig_memory_bytes` | default 8000 on every host; cloud3 unset (read) | Lower: `mw status` asks for a prune sooner, so each boot reads less | Facts a later Builder would have used |

## Grist: a tutor's answers

The mill grinds only on the home (the laptop), which holds the mill key;
cloud3 has no [grist] table (read), and the desktop's is not recorded here.

| Dial | Turned by | Now, by host | Cheaper | What is lost |
|-|-|-|-|-|
| Grist a day from one key | `[grist] daily_limit` (`mw config set grist.daily_limit <n>`) | home 250 (his "bump it to 250", 2026-10-12 23:38Z, mw-6ww.11); default 50; the factory's own keys are exempt | Lower | A reader past the limit gets no answer until the next day |
| Grist a day from one self-registered person | `[grist] person_cap` (`mw config set grist.person_cap false`) | default true on every host (mw-cvwm1u.6); a person's device keys count together at the person's own cap from the postern backend (50), not at `[grist] daily_limit`; a key the backend does not know, or a backend that cannot be reached, keeps the per-key rule; the Governor's and the factory's own keys keep the home setting | false: every key alone at `[grist] daily_limit` | A person's second and third phones each get a full day instead of sharing one |
| The models and efforts a grist may ask for | `[grist] models`, `[grist] efforts` | home none recorded, so the defaults: haiku, sonnet, opus and low, medium, high | Shorter lists, the dear ones left out | Answers a grind would have run on a stronger model |
| One grind's model and effort | `grinds/<kind>.json model`, `grinds/<kind>.json effort`, in the app's rig | per app and kind, in that rig's grinds/ (not a host's) | A cheaper model or a lower effort, tried first with `mw grist eval` | Answer quality on that kind |
| One grind's turns | `grinds/<kind>.json maxTurns` | per grind; 0 leaves it to the harness's own limit | A cap | A grind that needs more turns fails instead of answering |
| Photos a grist may carry | `[grist] max_attachments`, `[grist] max_attachment_bytes` | home none recorded: 4 photos of at most 8 MB | Fewer or smaller | What the model could read off a missing photo |
| How long a grind may run | `[grist] timeout` | home none recorded: 10m | Shorter | A slow grind is cut off with no answer, its fuel spent |
| How long a talk's session grows | `[grist] talk_context_cap`, `[grist] talk_max_turns` | home none recorded: 80000 context tokens, 40 turns (mw-pxji7y.2) | Lower | The session is ended sooner and the next turn starts from its key points (one extra turn at each end), so less of the talk is read afresh each turn but older detail is only as good as the key points |

Not fuel: `[grist] pool_quiet` and `[grist] pool_max_age` keep warm sessions
waiting for a grist, and a warm session sends the model nothing until its grist
comes, so they spend memory, not fuel. `[grist] concurrency` is speed only.

## Seats that can be parked

| Dial | Turned by | Now, by host | Cheaper | What is lost |
|-|-|-|-|-|
| Millhand review wakes, twice a day (07:30, 19:30) | `systemctl --user disable --now mw-millhand-review.timer`; or `millhand_review_model` | the review timer is not recorded for the laptop or the desktop; cloud3 has none (read); model default opus | Parked, or the review on sonnet | The twice-daily look at what went wrong on the host |
| Millhand routine wakes, hourly at :07 when something needs it | `systemctl --user disable --now mw-millhand-tick.timer`; or `millhand_routine_model` | the tick timer is listed for the laptop and the desktop in the vault's host notes (2026-09-29 and before); cloud3 none (read); model default sonnet. The follower also starts mw-millhand-tick.service on an alarm where it is installed | Parked, or a cheaper model | Alarms on the host wait for the Mayor |
| Deputy | `deputy_model`; it is brought up by `mw deputy`, not by a clock | default sonnet, effort high (fixed); no seat is marked spring = true in the vault's seats/<seat>/subscribe.toml, so none is started by the follower | Not brought up | Its work falls to the Mayor, who runs on Opus: parking it can burn more fuel |
| Tester trial: Tester stories sprung on landings | `[tester] rigs`, `[tester] until` | the home's only; not recorded here | An until in the past, or no [tester] table, springs none | The Tester's findings |
| Sleep pass: one fresh agent a night | `[sleep] nightly_model`, `[sleep] nightly_effort`, `[sleep] sunday_model`, `[sleep] sunday_effort` | home none recorded: opus at medium nightly, fable at xhigh on Sunday; it has no off switch | Cheaper models | The day's priorities, maps and improvements it writes |
| To do rank: one pass over his To do board a night | `[todo] rank_model`, `[todo] rank_effort` | home none recorded: sonnet at medium; no off switch | A cheaper model | The order of his To do board |
| When a seat's session hands off | `handoff_at`; `MW_HANDOFF_AT` | default 180000 tokens; the Deputy and the Millhand 200000 unless a [seats.<name>] table says | Near 180K is the cheaper point: each call re-reads the whole context (the Mayor's estimate on mw-6ww.11, 2026-09-19) | Handing off earlier pays the boot more often |

The Mayor's own model and effort are not an mw dial: they are chosen when the
Mayor is respawned, by the vault's respawn script, and the Mayor runs on Opus
at high today (handoffs of 2026-10-16 and 2026-10-17).

## Pausing the factory

The quickest way to spend nothing until the week resets (mw-gq6.707). The hold is the usage-limit
hold note, host.<host>.harness_hold, on every host: a session already running finishes, nothing
new is claimed, and no cap is set to 0 and no unit is masked. The paused set is kept in
plans/pause-<date>/paused.json in the vault.

| Dial | Turned by | Now, by host | Cheaper | What is lost |
|-|-|-|-|-|
| Pause every host at the Governor's word | `mw pause --reason <words>`, on the home; ended by `mw resume` | not paused on any host (nothing recorded; `mw resume` says "not paused" when so) | Paused: every host holds new claims, each Builder checks its work in to its branch and says PAUSED, cloud boxes go down | Landings while it lasts; a cloud box's stories start again from their pushed branches, not from the live session |
| When the pause ends | `mw pause --until <RFC3339>`; with none, computed from the usage reported and the burn (PROVISIONAL); `mw resume` ends it at once | a week, open-ended, when no usage was reported | Longer: the quota left is not spent on a Builder that cannot finish | The week's last hours, if the time is set too late |
| The factory pauses itself at the reserve | `mw usage set <percent>`: the Governor's reading, from his screenshot (PROVISIONAL), against 100% less R(t), 10% of the quota with a week left to 0% with an hour left | no reading kept | A higher reserve is not a dial: it is the Governor's rule (10% to 0%) | Nothing, until the usage is reported: nothing reads it by itself yet |
| The burn at full throttle the resume time is sized to | `mw pause --rate <percent>`, a percent of the quota an hour; else measured from the seat ledgers and the readings | measured; one reading cannot measure it | A lower rate resumes earlier | A rate too low runs the quota out before the reset |

## Ideas only

| Dial | Turned by | Now, by host | Cheaper | What is lost |
|-|-|-|-|-|
| Ideas only: he is away on purpose | He sets it on his own key in the Postern app; `mw ideas-only` reads it and `mw ideas-only release` gives the held ideas back | Not one host's: the postern backend holds it. It was on until 21:00Z on 2026-10-16 | On: each record of his is kept at zero tokens and wakes no seat | His words wait until it ends; the factory runs on what it already has |

## Seeing the burn

`mw report` shows quality and dollars a landing by rig and by model (MODELS),
`mw fuel` the fuel of the model passes (sleep, To do rank) by kind, and
`mw status` each host's cap in force and why.
