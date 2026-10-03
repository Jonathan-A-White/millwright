package doctor

import "strconv"

// An alarm to the Governor rides the emergency lane of the event log and is
// given the seq it was written at. A check keeps that seq in its own state
// beside its latch, and when its condition ends names it in the event that
// says so, in the `clears` field, so the app takes the emergency as resolved.
// A seq of 0 is no seq: the emergency event could not be written, or the
// latch was kept before seqs were.

// seqAt reads paths[i] as a recorded seq: 0 where it is missing or not one.
func seqAt(paths []string, i int) uint64 {
	if i >= len(paths) {
		return 0
	}
	seq, err := strconv.ParseUint(paths[i], 10, 64)
	if err != nil {
		return 0
	}
	return seq
}

// withSeq is paths with seq added last, or paths as they are for no seq.
func withSeq(paths []string, seq uint64) []string {
	if seq == 0 {
		return paths
	}
	return append(paths, strconv.FormatUint(seq, 10))
}
