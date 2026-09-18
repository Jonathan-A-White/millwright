# One story, one fresh session

Every story is worked by a brand-new session that boots into a seat, does that story, and ends; stories are sized to fit comfortably inside a 200K context window. Long-lived worker sessions that pick up story after story would save boot cost, but price grows roughly quadratically with context length and fuel is the binding constraint (one Max plan now, a Pro plan in steady state), so we pay a small fixed priming cost per story instead of an unbounded context cost. Continuity lives in the seat and the beads, never in a session's context.

## Consequences

Seat priming must be cheap and on-demand, because it is paid on every story. A story that cannot fit is a sign it should be split, not a reason to extend a session.
