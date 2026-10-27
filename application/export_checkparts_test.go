package application

// RecordedGateForTest lets the tests of package application_test read a split
// gate's records as the last call of mw check does, at a given tip.
func RecordedGateForTest(records CheckRecords, storyID string, attempt int, parts []GatePart, tip string) Checks {
	return recordedGate{records: records, storyID: storyID, attempt: attempt, parts: parts, tip: tip}
}
