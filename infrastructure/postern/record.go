package postern

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"

	"github.com/bsv-blockchain/go-sdk/script"
)

// AnchorAddress is the postern message channel's own testnet anchor
// (docs/protocol.md section 3, github.com/Jonathan-A-White/postern): every
// record transaction pays it 1 satoshi, and the backend's poller watches it.
const AnchorAddress = "mt6vaAWeFxu2qC6pPs7bsqTNvv87dCwMW5"

// recordProtocolID is the push every postern record script carries, the same
// one the nftgate framing uses (spell-forge's src/bsv/record.ts):
// `OP_FALSE OP_RETURN <protocol id> <version> <payload>`.
const recordProtocolID = "nftgate"

// recordVersion is version 1 of the nftgate payload: plaintext/JSON, the one
// a message's payload travels as. Version 2 is the License-contract format
// and is none of postern's business.
const recordVersion = 0x01

// RecordScript is the locking script of a record carrying payload, laid out
// byte for byte as spell-forge's encodeRecordScript writes it: OP_FALSE
// OP_RETURN, then three pushes — 'nftgate', the version as a one-byte data
// push (`01 01`, never OP_1), and payload — each with the shortest pushdata
// prefix that fits.
func RecordScript(payload []byte) (*script.Script, error) {
	s := &script.Script{}
	if err := s.AppendOpcodes(script.OpFALSE, script.OpRETURN); err != nil {
		return nil, err
	}
	if err := s.AppendPushDataArray([][]byte{[]byte(recordProtocolID), {recordVersion}, payload}); err != nil {
		return nil, err
	}
	return s, nil
}

// DecodeRecordScript reads a locking script, as hex, back to the payload of
// the version-1 record it carries. ok is false for anything else: not hex,
// not OP_FALSE OP_RETURN, not three pushes, not 'nftgate', another version.
func DecodeRecordScript(scriptHex string) (payload []byte, ok bool) {
	raw, err := hex.DecodeString(scriptHex)
	if err != nil || len(raw) < 2 || raw[0] != script.OpFALSE || raw[1] != script.OpRETURN {
		return nil, false
	}
	pushes, ok := splitPushes(raw[2:])
	if !ok || len(pushes) != 3 {
		return nil, false
	}
	if !bytes.Equal(pushes[0], []byte(recordProtocolID)) || !bytes.Equal(pushes[1], []byte{recordVersion}) {
		return nil, false
	}
	return pushes[2], true
}

// splitPushes reads raw as nothing but data pushes (a literal length, or
// OP_PUSHDATA1, 2 or 4), the way record.ts's parsePushDataSequence does.
func splitPushes(raw []byte) ([][]byte, bool) {
	var pushes [][]byte
	for i := 0; i < len(raw); {
		op := raw[i]
		i++
		var n int
		switch {
		case op < script.OpPUSHDATA1:
			n = int(op)
		case op == script.OpPUSHDATA1 && i+1 <= len(raw):
			n = int(raw[i])
			i++
		case op == script.OpPUSHDATA2 && i+2 <= len(raw):
			n = int(binary.LittleEndian.Uint16(raw[i:]))
			i += 2
		case op == script.OpPUSHDATA4 && i+4 <= len(raw):
			n = int(binary.LittleEndian.Uint32(raw[i:]))
			i += 4
		default:
			return nil, false
		}
		if n > len(raw)-i {
			return nil, false
		}
		pushes = append(pushes, raw[i:i+n])
		i += n
	}
	return pushes, true
}
