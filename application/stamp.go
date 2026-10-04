package application

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// StampKind is the kind a chain stamp record's payload carries.
const StampKind = "stamp"

// StampPayload is a chain stamp's on-chain payload, in the shape of a postern
// record (PosternPayload, docs/protocol.md section 1) and carried by the same
// nftgate framing: UTF-8 JSON, fields in this order. Everything but Ct is in
// the clear, and the only thing of the stamp in the clear is Commitment
// (domain.Stamp.Commitment), so the chain shows neither the rig nor the commit.
// Ct is the stamp's body, sealed to the Governor's key by the same Cipher a
// message's text is. docs/chain-stamps.md.
type StampPayload struct {
	V          int    `json:"v"`          // always 1
	Kind       string `json:"kind"`       // always StampKind
	To         string `json:"to"`         // the Governor's compressed public key, hex
	From       string `json:"from"`       // sender's compressed public key, hex
	Ts         int64  `json:"ts"`         // Unix seconds, when the sender built it
	Commitment string `json:"commitment"` // hex SHA-256 of rig + "\n" + commit id
	Ct         string `json:"ct"`         // the BRC-78 ciphertext of the stamp's body, base64
}

// SealStamp builds a stamp's payload: the commitment in the clear and the
// stamp's body, as JSON, sealed to governorKey the way PosternSend seals a
// message. It reports the payload and its bytes, ready for a record script.
// It talks to nothing.
func SealStamp(cipher Cipher, governorKey, from string, stamp domain.Stamp, now time.Time) (StampPayload, []byte, error) {
	stamp.At = stamp.At.UTC()
	body, err := json.Marshal(stamp)
	if err != nil {
		return StampPayload{}, nil, fmt.Errorf("building the stamp's body: %w", err)
	}
	ct, err := cipher.Encrypt(governorKey, string(body))
	if err != nil {
		return StampPayload{}, nil, fmt.Errorf("sealing the stamp: %w", err)
	}
	payload := StampPayload{
		V: 1, Kind: StampKind, To: governorKey, From: from, Ts: now.Unix(),
		Commitment: stamp.Commitment(), Ct: ct,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return StampPayload{}, nil, fmt.Errorf("building the stamp's payload: %w", err)
	}
	return payload, raw, nil
}

// OpenStamp reads a stamp's payload and opens its body with privKey (WIF). It
// reports the stamp and the envelope's own sender key (Cipher.Decrypt's
// envelopeFrom). It refuses a payload that is not a stamp, and a body whose
// rig and commit do not make the commitment the payload shows in the clear.
func OpenStamp(cipher Cipher, privKey string, raw []byte) (domain.Stamp, string, error) {
	var payload StampPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return domain.Stamp{}, "", fmt.Errorf("reading the stamp's payload: %w", err)
	}
	if payload.Kind != StampKind {
		return domain.Stamp{}, "", fmt.Errorf("not a stamp: kind %q", payload.Kind)
	}
	text, envelopeFrom, err := cipher.Decrypt(privKey, payload.Ct)
	if err != nil {
		return domain.Stamp{}, "", fmt.Errorf("opening the stamp: %w", err)
	}
	var stamp domain.Stamp
	if err := json.Unmarshal([]byte(text), &stamp); err != nil {
		return domain.Stamp{}, "", fmt.Errorf("reading the stamp's body: %w", err)
	}
	stamp.At = stamp.At.UTC()
	if !stamp.Verify(payload.Commitment) {
		return domain.Stamp{}, "", fmt.Errorf("the stamp's body does not make its commitment %s", payload.Commitment)
	}
	return stamp, envelopeFrom, nil
}
