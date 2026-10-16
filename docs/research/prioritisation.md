# Research: industry prioritisation practice, mapped to a one-person AI factory

Answers mw-1ljpo9.9 (investments.md section 8 of plans/triage-2026-10-15, under A44 on
mw-6ww.119). Read-only research: no product code changed, and nothing in the vault is edited.
The three principle edits at the end are proposals for the Governor; the Mayor files them as a
card.

## The setting the methods are mapped to

Every method below was written for a team of paid people whose hours are the scarce thing and
whose customers number in the thousands. This factory inverts both:

- **One reviewer, and his time is the bottleneck.** Every landing that needs his tap, his demo
  evening, his hands step or his A/B answer joins one queue, and that queue (cards + demos +
  hands steps) is the real constraint: principles.md already says "his time is the scarcest
  resource, not the Builders'". On 2026-10-15 it held seven factory-internal demos and three
  SpellForge demos he had not opened in a week (questions.md Q3, Q4).
- **Builders are elastic.** A Builder is a fresh session with a path; two or ten can run at once
  across the desktop, the Laptop and a boost. What they consume is fuel, and fuel is bounded by
  the plan, not by headcount: ~$100-200 a day (investments.md section 6).
- **Reach is one, or three.** The Governor, his son, a reader. "How many users does this touch"
  is a constant.
- **The arc moves.** A priority "is a claim about this month, not forever" (principles.md); his
  words on 9 Oct set Lampas and Postern first and said so would change.
- **The pass is an agent, not a meeting.** The nightly sleep pass reads `mw triage dump`, the
  runs data and `mw report`, and recommends; the Mayor applies; he overrides. So any method has
  to be one an agent can run from a JSON line, and one whose misses can be counted.

Two consequences run through the whole mapping. First, the denominator of every "value over
cost" formula must be *his* time, and only second the fuel or the size, because Builders can be
added and he cannot. Second, since the factory can start more work than he can accept, the
dampers that limit how much reaches him matter more than the ranking of what starts.

## The methods, one by one

### WSJF and cost of delay

What it is: the Scaled Agile Framework's rule for ordering a backlog: cost of delay (business
value + time criticality + risk reduction, each on a Fibonacci scale) divided by job size, and
the highest ratio goes first (Scaled Agile, "WSJF", https://framework.scaledagile.com/wsjf/).
The economics underneath are Reinertsen's: when jobs differ in delay cost and in duration,
schedule the one with the highest cost-of-delay per unit of duration first (Donald Reinertsen,
*The Principles of Product Development Flow*, Celeritas, 2009).

Verdict: **adapt**. The shape is right and the factory already has half of it: the bang-for-the-
buck line in principles.md is cost of delay over size. What is wrong for this setting is the
numerator's three-part Fibonacci vote (one person, no room full of estimators) and the
denominator (job size, when the scarce resource is not the job). The adapted form: cost of
delay is one of four bands the pass can read off a bead (he hits it this week; it blocks the
arc or main is red; it finishes something nearly done; none), and the denominator is what the
landing will cost *him* (nothing, a tap, a demo, a hands step, a decision) before it is size.
See the scoring section.

### RICE

What it is: Intercom's score for a roadmap item: reach x impact x confidence, divided by effort
in person-months, where reach is how many users it touches in a period (Sean McBride, "RICE:
Simple prioritization for product managers",
https://www.intercom.com/blog/rice-simple-prioritization-for-product-managers/).

Verdict: **drop** the formula, **keep** one word. Reach is a constant here, impact and
confidence are guesses that invite exactly the number-bikeshedding principles.md warns against,
and effort is a Builder's cost, the elastic one. The word worth keeping is *confidence*: RICE's
20/50/80/100 ladder is the factory's maturity ladder (idea, spike, first slice, hardening) in
disguise. The principles already say the engineering bar rises with the stage; a story's stage
is its confidence, and nothing more need be scored.

### Kano

What it is: Noriaki Kano's 1984 classification of a product attribute by how satisfaction
answers its presence: must-be (its absence hurts, its presence is taken for granted),
one-dimensional (more is better), attractive (delights when present, unmissed when absent),
indifferent, reverse ("Attractive Quality and Must-Be Quality", 1984; summary at
https://en.wikipedia.org/wiki/Kano_model).

Verdict: **adapt** as a vocabulary for the bugs-versus-features rule, not as a survey. The
survey (ask many customers two questions per attribute) is pointless with one reader of it, the Governor. The
categories are useful because they name why most Tester findings sort low: a landscape-only
polish is *indifferent* to him; a bug he meets on his phone is *must-be*; a feature on his arc
is *attractive* until he uses it daily, then it is *one-dimensional*. The pass should tag each
new bead with one of the four words, from his words and his use, and treat must-be and
indifferent as the two ends of the P scale. Kano's own observation that attractive qualities
decay into must-be over time is the reason a priority is a claim about this month.

### Eisenhower and Covey

What it is: two axes, urgent and important, four quadrants; the discipline is to spend most time
in quadrant II (important, not urgent) so quadrant I shrinks (Stephen Covey, *The 7 Habits of
Highly Effective People*, habit 3; a summary at
https://blog.dropbox.com/topics/work-culture/eisenhower-matrix).

Verdict: **keep**; principles.md already holds it and the Governor's own 15 Oct words on the To
do board ("priority tree, Covey, due dates bubble up") ask for the same on his side. The one
addition the research suggests: quadrant II needs a *reserved share*, not just a rule that it
must not be crowded out, because urgent polish always has a louder claim. The principles'
"one in five landings is an investment" is that share; it should be enforced by the pass as a
count (investments landed per five landings, from `mw report`'s factory_changes), not left as an
aspiration.

### Kanban WIP limits and flow

What it is: pull work only when there is capacity, limit the work in progress at each stage, and
run the system on four flow measures: WIP, throughput, work item age and cycle time (The Kanban
Guide, May 2025, https://kanbanguides.org/english/). Little's law ties them: average time in a
queue = items in the queue / rate of leaving it, so the fastest way to shorten a wait is to shrink
the queue (Reinertsen, as above).

Verdict: **keep**, and widen it from Builders to him. The factory limits Builders per host and
Tester stories per rig already. The queue that Little's law should be applied to is his: on
2026-10-15 roughly ten demos and cards were open and he answers about four a week (improvements.md:
40 replies in a week, 4 of them verifications), so a new demo filed tonight waits two to three
weeks whatever its priority. Two rules follow. First, *work item age* is the alarm, not count:
`days_ready` and `days_since_update` are already in the dump, and a P1 untouched 5 days or a demo
waiting 7 days (the DAMPERS story, mw-1ljpo9.5) is the age limit. Second, the only lever that
shortens his wait is fewer items reaching him: factory-internal landings verified by the Mayor
and batched into one weekly card (questions.md Q4) is Little's law applied, and the pass should
measure his queue's length and his weekly rate every night.

### Shape Up: appetite and the betting table

What it is: Basecamp's cycle: shape a pitch with a fixed *appetite* (the time we want to spend,
not an estimate), bet on a few pitches at a betting table during cool-down, and let unbet ideas
go rather than keep a backlog (Ryan Singer, *Shape Up*, "The Betting Table",
https://basecamp.com/shapeup/2.2-chapter-08).

Verdict: **adapt**. The six-week cycle and the two-person team do not fit; three of its ideas
do. *Appetite* is the factory's cap on an attempt: max_attempts, the turn cap (mw-1ljpo9.6),
and the T-shirt size at which a story must be sliced; the pass should say the appetite ("S, two
attempts, no demo") when it files, and a story that outruns its appetite is given back, not
extended. The *betting table* is the weekly pass plus his A/B card: a few named bets for the
week, with the Mayor's recommendation first, and nothing else promised. *No backlog* is the
icebox: an idea nobody bet on in two weeks goes to the icebox with its reason, and it is his
words that thaw it, which is exactly Shape Up's "if it is important it will come back".

### Hill charts

What it is: Basecamp's picture of a project's state: a hill whose uphill side is "figuring out
what our approach is" and whose downhill side is execution once all the work can be seen; a dot
per scope, moved by the people doing the work (*Shape Up*, "Show Progress",
https://basecamp.com/shapeup/3.4-chapter-13).

Verdict: **adapt** the idea, **drop** the chart. The factory has the hill already in words: a
map or grilling is uphill (fog, decisions), a sized story is over the top, a running story is
downhill. What the hill chart adds is the diagnosis "a dot that has not moved": a grilling open
seven days with no card is a dot stuck uphill, and the principles already say carve it or ice it.
The roadmap view (mw-1ljpo9's Postern story) should show each epic's position in one of three
words (foggy, carved, running) rather than draw a hill; the pass computes the word from the
dump (a bead of type map or grilling with open children is foggy; with sized ready children,
carved; with a child in progress, running).

### Toyota kata

What it is: Mike Rother's two routines distilled from Toyota: the improvement kata (understand
the direction, grasp the current condition, set the next target condition, experiment toward it
with a prediction per step) and the coaching kata, whose five questions are asked at every
step: what is the target condition, what is the actual condition now, what obstacles are in the
way and which one are you addressing, what is your next step and what do you expect, when can we
see what we learned (Mike Rother, *Toyota Kata*, McGraw-Hill, 2009; the five questions at
https://www.lean.org/the-lean-post/articles/the-five-coaching-kata-questions/).

Verdict: **keep**, as the shape of the learning loop. Of the eight methods this is the only one
about *learning* rather than *ordering*, and it is the one the Governor asked for in his own
words on mw-1ljpo9.12 (Edison's 900 ways that do not work). Every call the pass makes, and every
investment the Mayor files, is one kata step: a target, the condition now, the step, the
prediction, the date to look. The ledger row below is the five questions as fields. The coaching
kata is the Mayor's reading of the pass each morning: he asks the five questions of the
recommendations, and a recommendation that cannot answer "what do you expect" is not applied.

## The table

| Method | Verdict | One line of reason |
|---|---|---|
| WSJF and cost of delay | adapt | Right shape; the denominator must be his time before size, and the numerator four bands, not a Fibonacci vote. |
| RICE | drop (keep "confidence") | Reach is a constant and effort is the elastic thing; confidence is the maturity ladder, already a rule. |
| Kano | adapt | A vocabulary (must-be, one-dimensional, attractive, indifferent) for why Tester taste sorts low; no survey when the Governor is the one asked. |
| Eisenhower and Covey | keep | Already in principles.md and in his To do design; add a counted quadrant II share. |
| Kanban WIP and flow | keep, widen | Apply Little's law to his review queue; age, not count, is the alarm. |
| Shape Up | adapt | Appetite = attempt and turn caps; betting table = the weekly pass and his card; no backlog = the icebox. |
| Hill charts | adapt the idea, drop the chart | A stuck dot is a grilling with no card; three words per epic in the roadmap, computed from the dump. |
| Toyota kata | keep | The learning loop's shape: target, now, step, prediction, date to look. |

## The scoring rule: rules only, with one tie-break inside a band

Proposed: **no composite score.** The P band stays a judgement made by rules (the raise and
lower lists in principles.md), and `mw dispatch` keeps its sort of priority then age. Inside a
band, where today only age decides, add one tie-break: *his cost first, then size, then age*.

Why rules and not a number:

- A score needs inputs nobody will keep honest. Impact and confidence on a bead are guesses by
  the same agent that wants the bead to run; they drift toward whatever gets the bead moving.
  Rules name a fact ("he hit it this week", "the last story of an epic", "found by the Tester")
  that the pass can check against the dump and the mail, and a miss can be traced to one rule.
- The factory's own data is coarse. Five P bands over a few hundred live beads, a dozen
  landings a day; a finer number would rank noise.
- A composite score is bikeshedding in numbers: it is the "debate about a label weight"
  principles.md tells the Mayor to stop.
- His overrides are the ground truth, and an override of a rule is a fix to one sentence; an
  override of a score is a fix to a weight nobody can explain to him.

The tie-break inside a band is WSJF with the right denominator. For two ready stories of the
same P:

1. **His cost**, ascending: none (Internal, lands and is verified by the Mayor) < a tap (one
   card line) < a decision (an A/B card) < a demo (an evening) < a hands step (his keyboard).
   The pass reads it from the story's expect line and its labels (`hitl:*`, demo, hands).
2. **Size**, ascending, from the `size` field.
3. **Age**, oldest first, from `days_ready`.

A worked example from 15 Oct: two P1 stories, the TradeTracker cap bug mw-31qug1.6 (S, lands
Internal, a tap at most) and a SpellForge Tutor demo (S, a demo evening). The rule puts the bug
first, and says why in one line: it finishes a P0 he asked for and costs him nothing to accept.
Today both wait on age alone.

Two rules the research adds to the band judgement, beside the lists already in principles.md:

- **Cost of delay has four bands and the band is the P.** Daily (he meets it on his phone this
  week; main is red): P1. Arc (it blocks a story or demo he is on): P1 or P2 by his cost. Finish
  (the last story of an epic, a thing nearly done): one band up from where it was. None: P3 or
  the icebox.
- **Appetite is written on the story when it is filed**: size, max attempts, the turn cap, and
  whether a demo is owed. A story past its appetite is given back with a note, never extended
  by the pass; only he extends.

## The learning loop

The loop is the improvement kata run nightly by the pass and read each morning by the Mayor.
Each call the pass makes is one experiment with a prediction; at 14 and 30 days the pass scores
its own predictions; three misses of one kind in 30 days change the rule that made them.

### What each pass records

One line per call in `plans/sleep-<date>/calls.jsonl`, written by the pass beside
recommendations.json (which it extends; the field names below are recommendations.json's where
they exist, and the dump's where the pass copies them in, so a 14-day join is on `id` alone):

| Field | From | Meaning |
|---|---|---|
| `pass` | the job | the pass's date, `2026-10-15` |
| `id`, `title` | dump `id`, bead title | the bead |
| `rig`, `type`, `size`, `labels`, `parent` | dump, same names | copied so the call can be read without the dump |
| `his` | dump `his_priority` | true when he set the priority the call touches |
| `call` | the pass | one of `raise`, `lower`, `ice`, `thaw`, `carve`, `question`, `close`, `invest`, `keep` |
| `from`, `to` | recommendations.json | the P before and after (0-4); `ice` writes `to: 4` and `ice: true` as today |
| `rule` | principles.md | the heading, in one word, of the sentence that decided it: `arc`, `raises`, `lowers`, `bugs`, `size`, `wip`, `icing`, `maps`, `bikeshed`, `investment` |
| `kano` | the pass | `must-be`, `one-dimensional`, `attractive`, `indifferent` |
| `cod` | the pass | cost of delay band: `daily`, `arc`, `finish`, `none` |
| `his_cost` | the story's expect line and labels | `none`, `tap`, `decision`, `demo`, `hands` |
| `reason` | recommendations.json | one sentence, as today |
| `predict` | the pass | what should be true at 14 days, in one of the fixed shapes below |
| `days_ready`, `days_since_update` | dump, same names | the ages at the time of the call, for the age alarms |

The fixed prediction shapes, so scoring is mechanical: for `raise`, "landed and verified by day
14"; for `lower` and `ice`, "untouched by him through day 30"; for `thaw`, "started by day 14";
for `carve`, "a slice dispatched by day 14"; for `question`, "answered by day 7"; for `invest`,
the measure, baseline and window from the improvements ledger (mw-1ljpo9.11: `measure {metric,
baseline, window}`); for `keep`, nothing to score.

### Outcomes at 14 and 30 days

`mw sleep outcome <date>` (mw-1ljpo9.11) reads that pass's calls.jsonl, today's dump, the
diffs between, the mail and bead comments since, and `mw report`, and appends one outcome per
call. The fields it fills, with where each comes from:

| Field | From | Meaning |
|---|---|---|
| `scored_at`, `day` | the clock | the date and 14 or 30 |
| `landed` | `mw report --json` `factory_changes[].landed`, or the bead's close | the landing's date, else null |
| `verified` | his reply on the bead, a Postern verify tap, a "Looks good" | `his_word`, `his_use`, `mayor`, or null |
| `overridden` | diff `moved` with a change by his actor, or a thaw of an iced bead | the field he changed, and to what |
| `bitten` | a bug he reported (`his_priority` or his actor) on a lowered or iced bead's app and screen | the bug's id |
| `still_waiting` | dump `days_ready`, `days_since_update` | true when a raised story has not started |
| `stale_again` | diff `newly_stale` | true when a thawed or carved bead went stale |
| `measured` | `mw report` `factory_changes[].before`, `after`, `verdict`, `after_ends` | for `invest` only: the measure's before and after and the report's verdict |
| `hit` | computed | whether `predict` came true |
| `miss` | computed | one of the miss kinds below, else null |

The `invest` outcome is the improvements ledger line (mw-1ljpo9.11, mw-1ljpo9.12) and needs no
second record: `mw report`'s factory_changes already carries expect, before, after and verdict
for a named landing, and the ledger's `result` and `verdict` are read from it. The pass's calls
file should carry the ledger's `id` so the two join.

The field names above are matched to the dump and to recommendations.json as they stand on
2026-10-15; the ledger in investments.md section 6 writes `measured-at` with a hyphen, and that
should become `measured_at` so every file in plans/ reads with one convention.

### Which misses change which rule

Scored at 30 days; a miss is one of these kinds. Three of one kind within 30 days, across
passes, change the named rule; the pass proposes the sentence and the Mayor files a card.

| Miss | What happened | Rule it changes | How |
|---|---|---|---|
| `raised_unlanded` | a raised story is still waiting at day 14 | What raises a priority | the raise reason that recurs is demoted one band, or the WIP cap on ready P1 is lowered |
| `raised_unverified` | it landed but he never verified or used it by day 30 | His cost tie-break and the demo rule | that his_cost kind is counted dearer; a demo of a cool app is filed as Internal with a weekly digest line |
| `lowered_bitten` | he reported a bug on a lowered or iced bead's screen | What lowers a priority; Bugs vs features | the lowering reason (usually "Tester-found" or "polish") is narrowed: it no longer applies to that app while it is in the arc |
| `iced_thawed_by_him` | he opened or named an iced bead | Icing and thawing | the ice reason is removed or its waiting period lengthened from 7 days |
| `overridden` | he moved a priority the pass set, either way | the `rule` named on the call | the sentence is rewritten in his direction the same night, one override is enough; his word is the ground truth |
| `carved_unstarted` | a carved slice sat ready 14 days | Maps and grillings | the map's heat was misread; the signal the pass used (commits, his words) is reweighted |
| `question_unanswered` | a card sat 7 days | The pass | that question kind is batched into the weekly card, not asked alone |
| `invest_missed` | the measure moved the wrong way or not at all at the window's end | Investments that compound | the investment is rolled back as the ledger says; the prediction method (tokens, speed, quality) that was wrong is named in the ledger's `lesson` |
| `wip_breached` | his queue passed 8, or ready P1 passed 10, on a night the pass added to it | WIP limits | the pass refuses to raise or file into a breached queue; the cap is lowered if breached three nights |

Two asymmetries, on purpose. A miss on a raise costs fuel and a slot; a miss on a lowering
costs him a bug on his phone, which is dearer, so `lowered_bitten` changes a rule at three
misses like the others but is reported to him the morning it is seen. And his override changes
a rule at one, not three: there is one of him and his overrides are the only ground truth the
loop has.

What the loop cannot learn from yet, and should say so in its summary: his verification by use
leaves no bead comment (improvements.md: most of it happens in Postern threads and by use), so
`verified` will read null for landings he liked; the weekly deep pass should read the Postern
threads and the app's own usage events before calling a landing unverified.

## Three proposed edits to principles.md

Proposals only; the Mayor files them as one card, and the vault file changes only on the
Governor's word.

### Edit 1. "Size every story": the denominator is his time first

Replace the sentence "Bang for the buck: value divided by size; a high-value S beats a
medium-value L every time. An L in P1 is a smell: slice it or lower it." with:

> Bang for the buck: value divided by what it costs him, then by size. What it costs him is
> one of: nothing (Internal, verified by the Mayor), a tap, a decision (an A/B card), a demo
> (an evening), a hands step (his keyboard); say which when the story is filed. Within one
> priority, the story that costs him least runs first, then the smallest, then the oldest. A
> high-value S beats a medium-value L every time; an L in P1 is a smell: slice it or lower it.
> A story's appetite (size, attempts, turn cap, demo owed or not) is written when it is filed and
> never extended by the pass.

Why: WSJF's lesson applied to the one scarce resource; today two same-P stories are ordered by
age alone, and the pass has no word for what a landing will ask of him.

### Edit 2. "WIP limits": age and his rate, not counts alone

Append to the WIP limits paragraph:

> Age is the alarm, not count: a P1 ready 5 days, a demo waiting 7, a card unanswered 7 are
> each a damper tripped, whatever the counts. His queue's wait is its length divided by what he
> answers in a week (about four, 2026-10-15); when the wait passes seven days, nothing new is
> filed to him: factory-internal landings are verified by the Mayor and listed on one weekly
> card, demos are batched into one evening, and questions wait for the weekly card. The pass
> reports the queue's length, his weekly rate and the wait every night.

Why: Little's law; the queue that decides how fast the factory learns is his, and the only
lever on his wait is how much reaches him.

### Edit 3. "Learning loop": a prediction per call and the miss table

Replace the Learning loop paragraph with:

> Every call the pass makes (raise, lower, ice, thaw, carve, question, invest) is recorded with
> the rule that made it, in one word, and a prediction of what will be true at 14 days. At 14
> and 30 days the pass scores its own calls against the dump, the diff, his replies and mw
> report: landed, verified (by his word or his use), overridden, bitten (a bug he reported on a
> lowered thing), still waiting, measured (for an investment: before, after, verdict). Each miss
> has a kind and each kind names the rule it changes; three misses of one kind in 30 days change
> that rule, and the pass proposes the new sentence. His override changes a rule at one, the
> same night: his word is the ground truth. A bug he hits on a thing the pass lowered is told to
> him the morning it is seen. Write what changed in the rules and why. Failures are lessons as
> much as successes: say what was predicted, what happened, and how the factory got back to
> where it was.

Why: the Toyota kata's five questions as fields; the present paragraph says "three misses of one
kind change a rule" without saying what a miss is, which rule, or that his override is
different in kind.

## Also considered, not proposed

- **MoSCoW** (must, should, could, won't): its four words are the P scale already; nothing to
  add.
- **Buy a feature and dot voting**: need many voters.
- **Opportunity scoring and ICE** (Dan Olsen's importance minus satisfaction; Sean Ellis's
  impact, confidence, ease): the same objections as RICE, and ICE's "ease" is again the Builder's
  cost.
- **A priority that inherits its epic's when unset** and **a hot-rig weight in the start
  order**: investments.md already defers both until the pass has run for a month; this research
  agrees, since each would hide which rule made a call and so blunt the loop.
