# Demo: a cloud box made by demand takes a story, and goes when idle

The last story (mw-5gr3k0.5) of the epic mw-5gr3k0. With every host full, the
factory makes a Vultr box by itself, a story runs on it and lands like any
other, the box is destroyed when it has been idle, and the month's spend stays
under the cap. The Governor watches it on his phone and says `Looks good`.
This page is the script. The Mayor does the two parts marked *Mayor*; the
Governor does the numbered steps on his phone.

This demo spends real money (about $0.132 an hour a box, $75 a month at most),
and the real `up` is the Mayor's, run only on the Governor's word. Nothing in
this page runs it for him, and the elastic check never spends until the
`[cloud]` table of the home's `~/.config/mw/config.toml` exists
(`docs/vultr-boost.md`, "Elastic").

## Before the Governor starts (Mayor)

Do these on the home, in order. The first four are once; if they are done
already, check them and go on.

1. **The tokens**, each put from a file, never an argument
   (`docs/secrets.md`): `mw secrets put vultr_api_token < file`, and for a box
   that can work, `boost_github_token`, `boost_claude_token` (from `claude
   setup-token`) and, if the beads server has one, `boost_beads_password`.
   Check with `mw secrets list` (names only) and `mw doctor`: the age-key check
   should be ok on the home and quiet everywhere else.
2. **The settings** in the vault at `hosts/vultr/vultr-boost.tfvars`
   (`region`, `wg_endpoint`, `beads_host`, and the rest of the table in
   `docs/vultr-boost.md`), and the hub able to run `wg-enrol`.
   `contrib/vultr-boost up --dry-run` should list its six steps and change
   nothing.
3. **The ready-made image**, so a box works in about two minutes and not ten:
   `contrib/vultr-boost up --name cloud1`, let the bootstrap finish
   (`~/boost-bootstrap.log` on the box; it shows in `mw status` once it
   dispatches), `contrib/vultr-boost snapshot --name cloud1` and wait for
   Vultr to finish it, then `contrib/vultr-boost down --name cloud1`. Note the
   snapshot id it printed. This is the demo's first real spend, about an hour
   of one box; say so to the Governor before it, and have his word.
4. **The `[cloud]` table** in the home's `~/.config/mw/config.toml`, which is
   the word that lets mw spend (`provider = "vultr"`, `max_boxes = 2`,
   `monthly_cap_usd = 75`, `idle_minutes = 30`, `hourly_usd = 0.132`,
   `snapshot = "<the id>"`, and `[cloud.caps]` listing only the hosts that are
   on, each with the sessions it runs at once). Run `mw cloud check` by hand
   once with nothing waiting: it should do nothing and write no event.
5. **Fill every host.** The demo needs more stories waiting for any host than
   there are sessions free. Take an epic of small stories whose Path is
   `host = auto` (a story pathed to one host makes no box), and `mw release
   <epic>` enough of them to fill the cap of every host in `[cloud.caps]` and
   leave at least two waiting. Check with `mw status` on each host: RUNNING is
   at its cap, READY still lists `host auto` stories. If a host is asleep, it
   still counts as free; wake it or take it out of `[cloud.caps]`.
6. **Send him the message.** Tell the Governor, in a Postern message, that the
   demo is ready and that a box will appear within about a minute of the
   release, then work through the steps below with him. If he is not watching,
   the cloud check runs by itself every minute and the steps still hold; the
   Mayor reads `mw events tail` for him.

## The demo (Governor, on the phone)

1. Open the **Postern** app and go to **Factory** (the map). Look at the row
   of figures **Working**, **Ready**, **Blocked** and **Landed today**, and
   the host chips under it.
   Right looks like: **Working** is as many as the hosts can run at once, and
   **Ready** is not zero. The Laptop and the desktop are both full and
   stories are still waiting. No cloud box is there yet.

2. Wait a few minutes and look at **Factory** again.
   Right looks like: **Working** goes up by one or two, and a host chip named
   **cloud1** appears among the host chips once the box has synced for the
   first time (a box bootstrapped in full takes longer than one made from the
   snapshot). (The Mayor can also post
   `mw status` for you: it has a **CLOUD** section, `CLOUD: $0.13 spent of
   $75.00 this month, 1 of 2 boxes`, then `cloud1  up 2m, working`, then the
   last moves, among them `up cloud1: N stories wait for any host, 0 sessions
   free`.)

3. Open one of the stories running on **cloud1** (the **Working** figure
   opens the map narrowed to what is working).
   Right looks like: the story is claimed by **cloud1**, and its session is
   running there. A box takes at most as many stories as its `box_cap`
   (two), and the second box, **cloud2**, only appears if stories still wait
   with the first one full and the month can pay for it.

4. Wait for that story to finish, then open it again, or open **Landed today**
   in **Factory**.
   Right looks like: it is closed and landed like every other story: its
   closing comment says what was done and how it was checked, and it is in
   **Landed today**. Nothing about it says it ran in the cloud except its
   host, **cloud1**.

5. Leave the queue empty (the Mayor releases nothing more) and look at
   **Factory** again after the idle time, thirty minutes by the table.
   Right looks like: **Working** is back down, **Ready** is zero, and the
   **cloud1** chip stops being renewed: its age grows and, past twenty
   minutes, it shows as stale (the chip of a box that has gone stays, as a
   host that has stopped syncing; a box never comes back under the same chip
   unless it is made again). The Mayor's `mw status` shows
   `cloud1  up 29m, idle 29m` before it, and afterwards the **CLOUD** section
   reads `0 of 2 boxes` with the move `down cloud1: idle 30m` among the last
   five.

6. Ask the Mayor for the month's spend, or read the **CLOUD** line of the
   `mw status` he posts.
   Right looks like: `CLOUD: $X spent of $75.00 this month`, with X well
   under 75 (a box's hour is $0.132, so a demo of a few hours is under a
   dollar or two, and the snapshot hour of Mayor step 3). No line says `cap
   reached`.

7. Reply in the bead mw-5gr3k0.5's channel with the words `Looks good`.
   Right looks like: the demo closes, with a comment quoting him, and the
   Mayor is mailed `Closed: mw-5gr3k0.5 on his Looks good` (only that exact
   text, any case, one final `.` or `!`, closes a demo; anything else is a
   plain comment). The Mayor then closes the epic mw-5gr3k0.

## After it (Mayor)

- Check `contrib/vultr-boost status`: no box left. A box that is still there
  after `down cloud1: idle 30m` is a finding; `contrib/vultr-boost down
  --name cloud1` removes it and its WireGuard peer.
- Check `mw events tail` has the `up`, the `down` and no `failed` move, and that
  Vultr's billing (`contrib/vultr-boost spend`) agrees with the book to within
  an hour's box.
- If he wants the elastic cloud off, delete the `[cloud]` lines from the home's
  config: with no table, mw never spends. Revoke the box tokens at their
  source if a box was lost (`docs/vultr-boost.md`, "Revoking the box's
  tokens"), and delete the snapshot at Vultr when you revoke them.

## If something is wrong

- **No box after several minutes**: `mw cloud check` by hand on the home says
  why. "no `[cloud]` table, or this host is not home" means the table or the
  host is wrong; otherwise `mw events tail` shows a `failed cloud1: ...` or a
  `cap reached`. A host listed in `[cloud.caps]` that is asleep counts as
  free, so list only the hosts that are on.
- **A box up but no story on it**: the box is still bootstrapping (ten minutes
  without a snapshot, two with) or its dispatch timer has not ticked;
  `~/boost-bootstrap.log` on the box says. The check counts its sessions as
  free while it boots, so it will not make a second box for the same wait.
- **A `failed cloud1` event**: the wrapper failed, the box was destroyed and
  tried once more; if that fails too, no box is made for an hour. Run
  `contrib/vultr-boost up --dry-run` and read what it says.
- **The box does not go after idle**: the idle time counts from the first
  check that found it empty, not from its last story; wait `idle_minutes`
  from that check. Any story claimed on it starts the count again.
- **cloud1 is in `mw status` but not on his phone**: the phone's Factory
  lists the hosts that have a last-sync note, so a box shows only after its
  first sync, and the view is as fresh as the home's last mirror; it is a
  finding about the phone, not about the cloud. The Mayor reads
  the `mw status` CLOUD section to him instead and the rest of the steps hold.
