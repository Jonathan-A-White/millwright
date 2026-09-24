package apptest

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakePostern is an in-memory application.Postern: records, utxos and a
// balance held by address, and a canned txid every broadcast returns.
type FakePostern struct {
	mu sync.Mutex

	records   []application.PosternRecord
	utxos     map[string][]application.PosternUtxo
	balance   map[string]int64
	broadcast []string

	// NextTxid is the txid Broadcast reports. "fake-txid" when empty.
	NextTxid string

	// Err, when set, is returned by every method instead of doing the work.
	Err error
}

// FakePostern satisfies the port.
var _ application.Postern = (*FakePostern)(nil)

// NewFakePostern returns an empty postern backend.
func NewFakePostern() *FakePostern {
	return &FakePostern{
		utxos:   map[string][]application.PosternUtxo{},
		balance: map[string]int64{},
	}
}

// AddRecord adds a record to the index, in the order Messages reports it: the
// order records are added in. Its Seq is set to the next sequence number, one
// past what has been added so far, unless it is already set.
func (f *FakePostern) AddRecord(record application.PosternRecord) application.PosternRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	if record.Seq == 0 {
		record.Seq = int64(len(f.records)) + 1
	}
	f.records = append(f.records, record)
	return record
}

// SetUtxos sets what Utxos reports for address.
func (f *FakePostern) SetUtxos(address string, utxos ...application.PosternUtxo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.utxos[address] = utxos
}

// SetBalance sets what Balance reports for address.
func (f *FakePostern) SetBalance(address string, satoshis int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.balance[address] = satoshis
}

// Broadcasts reports the raw transactions Broadcast was asked to send, in
// order.
func (f *FakePostern) Broadcasts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.broadcast...)
}

// Messages implements application.Postern.
func (f *FakePostern) Messages(_ context.Context, since int64) ([]application.PosternRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	var found []application.PosternRecord
	for _, r := range f.records {
		if r.Seq > since {
			found = append(found, r)
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].Seq < found[j].Seq })
	return found, nil
}

// Utxos implements application.Postern.
func (f *FakePostern) Utxos(_ context.Context, address string) ([]application.PosternUtxo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	return append([]application.PosternUtxo(nil), f.utxos[address]...), nil
}

// Balance implements application.Postern.
func (f *FakePostern) Balance(_ context.Context, address string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return 0, f.Err
	}
	return f.balance[address], nil
}

// Broadcast implements application.Postern.
func (f *FakePostern) Broadcast(_ context.Context, rawtx string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", f.Err
	}
	f.broadcast = append(f.broadcast, rawtx)
	if f.NextTxid != "" {
		return f.NextTxid, nil
	}
	return fmt.Sprintf("fake-txid-%d", len(f.broadcast)), nil
}
