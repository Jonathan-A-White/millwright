package postern

import (
	"errors"
	"fmt"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	feemodel "github.com/bsv-blockchain/go-sdk/transaction/fee_model"
	"github.com/bsv-blockchain/go-sdk/transaction/template/p2pkh"

	"github.com/Jonathan-A-White/millwright/application"
)

// recordFeeRateSatPerKB is the fee rate a record transaction is built at:
// spell-forge's chainConfig.feeRateSatPerKb, 1 sat/kB on testnet.
const recordFeeRateSatPerKB = 1

// anchorSatoshis is what every record transaction pays the anchor.
const anchorSatoshis = 1

// buildRecordTransaction builds and signs a record transaction the way
// postern's src/services/send.ts does (docs/protocol.md section 4): one P2PKH
// input per utxo of priv's own but a 1-satoshi one (that is a token, not fee
// money); output 0 the record carrying payload, 0 satoshis; output 1 one
// satoshi to AnchorAddress; output 2 the change back to priv's own address,
// the fee taken at 1 sat/kB.
func buildRecordTransaction(priv *ec.PrivateKey, utxos []application.PosternUtxo, payload []byte) (*transaction.Transaction, error) {
	own, err := script.NewAddressFromPublicKey(priv.PubKey(), false)
	if err != nil {
		return nil, fmt.Errorf("deriving the postern key's testnet address: %w", err)
	}
	ownLock, err := p2pkh.Lock(own)
	if err != nil {
		return nil, fmt.Errorf("building the postern key's own locking script: %w", err)
	}
	anchor, err := script.NewAddressFromString(AnchorAddress)
	if err != nil {
		return nil, fmt.Errorf("reading the postern anchor address: %w", err)
	}
	anchorLock, err := p2pkh.Lock(anchor)
	if err != nil {
		return nil, fmt.Errorf("building the postern anchor's locking script: %w", err)
	}
	unlocker, err := p2pkh.Unlock(priv, nil)
	if err != nil {
		return nil, fmt.Errorf("building the postern key's own unlocking template: %w", err)
	}
	record, err := RecordScript(payload)
	if err != nil {
		return nil, fmt.Errorf("building the record's output: %w", err)
	}

	tx := transaction.NewTransaction()
	for _, u := range utxos {
		if u.Satoshis == 1 {
			continue
		}
		if err := tx.AddInputFrom(u.Txid, uint32(u.Vout), ownLock.String(), uint64(u.Satoshis), unlocker); err != nil {
			return nil, fmt.Errorf("adding %s:%d as an input: %w", u.Txid, u.Vout, err)
		}
	}
	if len(tx.Inputs) == 0 {
		return nil, fmt.Errorf("no spendable coins at %s: fund the postern key before sending", own.AddressString)
	}
	tx.AddOutput(&transaction.TransactionOutput{LockingScript: record, Satoshis: 0})
	tx.AddOutput(&transaction.TransactionOutput{LockingScript: anchorLock, Satoshis: anchorSatoshis})
	tx.AddOutput(&transaction.TransactionOutput{LockingScript: ownLock, Change: true})
	err = tx.Fee(&feemodel.SatoshisPerKilobyte{Satoshis: recordFeeRateSatPerKB}, transaction.ChangeDistributionEqual)
	if errors.Is(err, transaction.ErrInsufficientInputs) || (err == nil && len(tx.Outputs) < 3) {
		return nil, fmt.Errorf("not enough satoshis at %s to cover the anchor output and the fee", own.AddressString)
	}
	if err != nil {
		return nil, fmt.Errorf("computing the record transaction's fee: %w", err)
	}
	if err := tx.Sign(); err != nil {
		return nil, fmt.Errorf("signing the record transaction: %w", err)
	}
	return tx, nil
}
