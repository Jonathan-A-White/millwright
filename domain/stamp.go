package domain

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
	"time"
)

// Stamp is what a chain stamp says: that this Rig's Branch held this Commit,
// landed for this Story, on this Host, at At. Only the Rig and the Commit go
// into its Commitment; the rest is the body a stamp seals.
type Stamp struct {
	Rig    string    `json:"rig"`
	Branch string    `json:"branch"`
	Commit string    `json:"commit"` // the full commit id
	Story  string    `json:"story"`
	Title  string    `json:"title"` // the commit's subject line
	Host   string    `json:"host"`
	At     time.Time `json:"at"` // UTC
}

// Commitment is the stamp's public handle: the lower-case hex SHA-256 of the
// rig, a newline and the commit id. It names neither, yet whoever holds both
// can recompute it, and nobody else can read them back out of it.
func (s Stamp) Commitment() string {
	sum := sha256.Sum256([]byte(s.Rig + "\n" + s.Commit))
	return hex.EncodeToString(sum[:])
}

// Verify reports whether commitment is the one this stamp's Rig and Commit
// make. Hex case does not matter; any other difference, one byte of the rig or
// the commit included, makes it false.
func (s Stamp) Verify(commitment string) bool {
	want := s.Commitment()
	got := strings.ToLower(strings.TrimSpace(commitment))
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
