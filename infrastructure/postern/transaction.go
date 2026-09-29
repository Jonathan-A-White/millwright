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

// outpoint is u's txid:vout, the key a spent output is known by.
func outpoint(u application.PosternUtxo) string {
	return fmt.Sprintf("%s:%d", u.Txid, u.Vout)
}

// spareCoinTarget is how many spendable coins a send leaves the key holding,
// change included, when its balance allows: with one coin a send makes the
// next wait for a block to confirm the change, so a send splits its change to
// leave enough for the sends that follow.
const spareCoinTarget = 4

// dustLimitSatoshis is the smallest output a change split makes, before the
// fee a later send spending it must pay.
const dustLimitSatoshis = 546

// buildRecordTransaction builds and signs a record transaction the way
// postern's src/services/send.ts does (docs/protocol.md section 4): inputs
// from priv's own utxos, never a 1-satoshi one (that is a token, not fee
// money); output 0 the record carrying payload, 0 satoshis; output 1 one
// satoshi to AnchorAddress; output 2 the change back to priv's own address,
// the fee taken at 1 sat/kB. An outpoint listed twice is spent once: the
// listing lists one twice while a block confirms it.
//
// It spends utxos in the order given, no more than it takes to cover the
// anchor and the fee, so the coins it leaves stay free for later sends. When
// the coins left plus the change would be fewer than spareCoinTarget, it
// splits the change into that many outputs (outputs 2 and on), each at least
// dustLimitSatoshis plus the fee.
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

	var coins []application.PosternUtxo
	seen := map[string]bool{}
	for _, u := range utxos {
		if u.Satoshis == 1 || seen[outpoint(u)] {
			continue
		}
		seen[outpoint(u)] = true
		coins = append(coins, u)
	}
	if len(coins) == 0 {
		return nil, fmt.Errorf("no spendable coins at %s: fund the postern key before sending", own.AddressString)
	}

	build := func(inputs, changeOutputs int) (*transaction.Transaction, error) {
		tx := transaction.NewTransaction()
		for _, u := range coins[:inputs] {
			if err := tx.AddInputFrom(u.Txid, uint32(u.Vout), ownLock.String(), uint64(u.Satoshis), unlocker); err != nil {
				return nil, fmt.Errorf("adding %s:%d as an input: %w", u.Txid, u.Vout, err)
			}
		}
		tx.AddOutput(&transaction.TransactionOutput{LockingScript: record, Satoshis: 0})
		tx.AddOutput(&transaction.TransactionOutput{LockingScript: anchorLock, Satoshis: anchorSatoshis})
		for i := 0; i < changeOutputs; i++ {
			tx.AddOutput(&transaction.TransactionOutput{LockingScript: ownLock, Change: true})
		}
		err := tx.Fee(&feemodel.SatoshisPerKilobyte{Satoshis: recordFeeRateSatPerKB}, transaction.ChangeDistributionEqual)
		if errors.Is(err, transaction.ErrInsufficientInputs) || (err == nil && len(tx.Outputs) < 3) {
			return nil, transaction.ErrInsufficientInputs
		}
		if err != nil {
			return nil, fmt.Errorf("computing the record transaction's fee: %w", err)
		}
		return tx, nil
	}

	var tx *transaction.Transaction
	for inputs := 1; inputs <= len(coins) && tx == nil; inputs++ {
		single, err := build(inputs, 1)
		if errors.Is(err, transaction.ErrInsufficientInputs) {
			continue
		}
		if err != nil {
			return nil, err
		}
		// The fee is the same whatever the split, near enough: one change
		// output tells how much there is to split.
		tx = single
		for want := max(1, spareCoinTarget-(len(coins)-inputs)); want > 1; want-- {
			split, err := build(inputs, want)
			if err == nil && len(split.Outputs) == 2+want && smallestChange(split) >= dustLimitSatoshis+feeOf(split, coins[:inputs]) {
				tx = split
				break
			}
		}
	}
	if tx == nil {
		return nil, fmt.Errorf("not enough satoshis at %s to cover the anchor output and the fee", own.AddressString)
	}
	if err := tx.Sign(); err != nil {
		return nil, fmt.Errorf("signing the record transaction: %w", err)
	}
	return tx, nil
}

// feeOf is what tx pays in fee: what its inputs (coins) hold less what its
// outputs carry.
func feeOf(tx *transaction.Transaction, coins []application.PosternUtxo) uint64 {
	var in, out uint64
	for _, u := range coins {
		in += uint64(u.Satoshis)
	}
	for _, o := range tx.Outputs {
		out += o.Satoshis
	}
	return in - out
}

// smallestChange is the least any change output of tx (output 2 and on)
// holds.
func smallestChange(tx *transaction.Transaction) uint64 {
	least := tx.Outputs[2].Satoshis
	for _, o := range tx.Outputs[3:] {
		least = min(least, o.Satoshis)
	}
	return least
}
