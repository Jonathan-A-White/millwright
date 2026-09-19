# mw acts under its own name, mw@<host>

*Provisional until the Governor has read it.*

Everything `mw` writes to the tracker on its own account — the claims `mw dispatch` makes, the closes and run states `mw next` writes, the comments either leaves — is signed `mw@<host>`, the `mw` seat on the host named in the config file, and `mw` passes that name to `bd` explicitly with `--actor` on every call rather than inheriting `$BEADS_ACTOR` from whatever shell ran it. A session's own writes stay signed by its seat, `<seat>@<host>`, which the harness sets. We chose this because the same act is run from several environments that carry several names — a claim from the dispatcher's shell or a timer, the close from inside the Builder session that worked the story — and `bd` lets only the actor that claimed a story close it, so a claim and a close under two names is a story that lands and cannot be closed; a name `mw` supplies itself is the same in every environment. We rejected signing as `mayor@<host>`: what `dispatch` and `next` write down was decided by the machinery, not by a seat, and a factory whose history cannot tell a seat's act from the tooling's act cannot be read afterwards. `mw` is neither the Mayor nor the Builder on purpose.

## Consequences

A host with no `host` in its config has no name to sign under, so `mw` refuses plainly before any `bd` is started. A story claimed under some other name — by hand, or before this decision, when claims carried whatever name the shell had — cannot be closed by `mw`; `mw next` says whose name holds the claim, and the story is closed by hand under that name, as root if need be: `bd --actor root close <story> --reason "landed by hand"`, or `bd reclaim <story>` to take it over first. See *Who mw writes as* in the README.

The vault's git history is signed the same way: the commit `mw next` makes in the vault is authored and committed as `mw@<host>`, passed to that `git commit` with `-c user.name` and `-c user.email` rather than written into the clone's config, so a commit made by hand in the same clone keeps the clone's own identity.
