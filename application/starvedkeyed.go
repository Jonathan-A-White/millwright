package application

// holdIsNotThisHosts says a note's hold is the host stepping aside for another
// host, not a block of the rig: a story that needs a grist key is passed over by
// a host with none (the host that has one takes it), and a story pinned to one
// host is passed over by every other (mw-gq6.1659). A hold of the host's own
// fault with the rig (not checked out, a skip, a metered network) is not one.
func holdIsNotThisHosts(r StarvedRig) bool {
	return r.Hold == StarvedHolds["no-grist-key"] || r.Hold == StarvedHolds["other-host"]
}

// hostsStandingAside splits a rig's holds by host: those of the hosts whose every
// hold steps aside for another host, and the rest. It reports a split only when a
// host stands aside and every other host is held by a wait for its room (no room
// or the cap): then the stepping aside is not the block, the waiting host's room
// is, and the alarm names that alone (mw-gq6.1659). Otherwise it reports none, and
// every host's holds are named as before (mw-gq6.1399).
func hostsStandingAside(held []StarvedRig) (waiting []StarvedRig, ok bool) {
	hosts := map[string][]StarvedRig{}
	for _, r := range held {
		hosts[r.Host] = append(hosts[r.Host], r)
	}
	aside := false
	for _, own := range hosts {
		stands, waits := true, true
		for _, r := range own {
			stands = stands && holdIsNotThisHosts(r)
			waits = waits && holdIsAWait(r)
		}
		switch {
		case stands:
			aside = true
		case waits:
			waiting = append(waiting, own...)
		default:
			return nil, false
		}
	}
	return waiting, aside && len(waiting) > 0
}

// waitsForWords is the alarm's words for a rig whose block is the room of the
// hosts that could take it: "waits for laptop: no room — ...". holdsWords leaves
// the host off when one host holds the rig, and "waits for" has to say whose room.
func waitsForWords(waiting []StarvedRig) string {
	if waiting[0].Host != "" && len(hostsOf(waiting)) == 1 {
		return "waits for " + waiting[0].Host + ": " + holdsWords(waiting)
	}
	return "waits for " + holdsWords(waiting)
}

// hostsOf is the hosts that wrote these notes.
func hostsOf(held []StarvedRig) map[string]bool {
	hosts := map[string]bool{}
	for _, r := range held {
		hosts[r.Host] = true
	}
	return hosts
}
