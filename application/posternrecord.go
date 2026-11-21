package application

import "time"

// PosternRecord is one message record as the postern backend indexes it from
// the chain: still encrypted, whoever it is addressed to.
type PosternRecord struct {
	Seq        int64
	Txid       string
	Class      string
	From       string // sender's compressed public key, hex, as the payload itself claims — anyone's to write, never trusted alone
	To         string // recipient's compressed public key, hex
	Ts         time.Time
	Ciphertext string // base64, as it travels on chain

	// Signer is the compressed public key, hex, that signed the funding
	// transaction's input, when the postern backend supplies enough to
	// recover it (its scriptSig, or the raw transaction). Empty when it does
	// not, so it is never compared — an unchecked signer is not evidence for
	// or against a record's sender.
	Signer string

	// SignerApps are the apps the signer's licences open, as the backend
	// stamped them when a direct record arrived: postern's docs/protocol.md
	// section 18. The mill trusts this stamp, never a grist's own plaintext,
	// to say whether its sender may use an app's grinds. Empty for a record
	// whose signer holds no app licence, and for every record on chain.
	SignerApps []string

	// Bead and Channel are where the record's message is, as its payload names
	// it in the clear (clearNames: the bead outranks a channel name): both empty
	// for Factory, and for a record that came by chain, which names neither.
	// A record of ours has no other place to say its channel, for what we sealed
	// to him we cannot read.
	Bead    string
	Channel string
}
