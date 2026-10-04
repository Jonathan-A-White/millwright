package domain_test

import (
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

const stampCommit = "7b4430c1f2d9a8e3b5c6d7e8f9a0b1c2d3e4f506"

// stampVector is sha256sum of "millwright\n" + stampCommit, worked out outside
// the code so the test does not share the code's mistakes.
const stampVector = "16523a05ba1bb8ec551fd0f26b398070b67355d21608f31c4d8fd0a73c034bb0"

func sampleStamp() domain.Stamp {
	return domain.Stamp{
		Rig:    "millwright",
		Branch: "main",
		Commit: stampCommit,
		Story:  "mw-oowxzy.1",
		Title:  "a chain stamp record",
		Host:   "laptop",
		At:     time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC),
	}
}

func TestCommitmentIsTheHexSHA256OfRigNewlineCommit(t *testing.T) {
	if got := sampleStamp().Commitment(); got != stampVector {
		t.Fatalf("Commitment() = %s, want %s", got, stampVector)
	}
}

func TestCommitmentIgnoresTheRestOfTheBody(t *testing.T) {
	s := sampleStamp()
	s.Branch, s.Story, s.Title, s.Host, s.At = "other", "mw-x", "other title", "vps", time.Time{}
	if got := s.Commitment(); got != stampVector {
		t.Fatalf("Commitment() = %s, want %s", got, stampVector)
	}
}

func TestVerifyAcceptsTheStampsOwnCommitment(t *testing.T) {
	if !sampleStamp().Verify(stampVector) {
		t.Fatal("Verify refused the right commitment")
	}
}

func TestVerifyRefusesOneByteOfThePreimageChanged(t *testing.T) {
	rig := sampleStamp()
	rig.Rig = "millwrigh7"
	commit := sampleStamp()
	commit.Commit = stampCommit[:len(stampCommit)-1] + "7"
	for name, s := range map[string]domain.Stamp{"rig": rig, "commit": commit} {
		if s.Verify(stampVector) {
			t.Errorf("Verify accepted a stamp whose %s differs by one byte", name)
		}
	}
}

func TestVerifyRefusesAWrongOrMalformedCommitment(t *testing.T) {
	for _, c := range []string{"", "zz", stampVector[:63], stampVector + "0"} {
		if sampleStamp().Verify(c) {
			t.Errorf("Verify accepted %q", c)
		}
	}
}

func TestVerifyIsCaseInsensitiveOnHex(t *testing.T) {
	upper := []byte(stampVector)
	for i, c := range upper {
		if c >= 'a' && c <= 'f' {
			upper[i] = c - 32
		}
	}
	if !sampleStamp().Verify(string(upper)) {
		t.Fatal("Verify refused an upper-case rendering of the right commitment")
	}
}
