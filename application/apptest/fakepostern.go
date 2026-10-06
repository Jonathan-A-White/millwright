package apptest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakePostern is an in-memory application.Postern: records, and the coin
// calls a FakeChain sends through — utxos and a balance held by address, and
// a canned txid every broadcast returns.
type FakePostern struct {
	mu sync.Mutex

	records    []application.PosternRecord
	chainCalls int
	utxos      map[string][]application.PosternUtxo
	balance    map[string]int64
	broadcast  []string
	blobs      map[string][]byte
	delivered  [][]byte
	uploaded   [][]byte
	deleted    []string

	// NextTxid is the txid Broadcast and Deliver report. When empty,
	// Broadcast reports "fake-txid-<n>" and Deliver "direct:fake-<n>".
	NextTxid string

	// Mine is what Me reports.
	Mine application.PosternMe

	// Err, when set, is returned by every method instead of doing the work.
	Err error
	// ChainErr, when set, is returned by Utxos and Broadcast alone: the
	// chain road down while the direct line is up.
	ChainErr error
	// DeliverErr, when set, is returned by Deliver alone: a backend that
	// reads but will not take a record.
	DeliverErr error
}

// FakePostern satisfies the port.
var _ application.Postern = (*FakePostern)(nil)

// NewFakePostern returns an empty postern backend.
func NewFakePostern() *FakePostern {
	return &FakePostern{
		utxos:   map[string][]application.PosternUtxo{},
		balance: map[string]int64{},
		blobs:   map[string][]byte{},
	}
}

// SetBlob sets what Blob reports for hash: a blob store's raw body,
// standing in for the postern backend's GET /api/blobs/{hash}.
func (f *FakePostern) SetBlob(hash string, body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blobs[hash] = append([]byte(nil), body...)
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

// ChainCalls is how many times Utxos or Broadcast was called, failed or not.
func (f *FakePostern) ChainCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.chainCalls
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

// Utxos reports address's unspent outputs: the coin call FakeChain spends
// through, as infrastructure/bsv does postern's /api/utxos.
func (f *FakePostern) Utxos(_ context.Context, address string) ([]application.PosternUtxo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chainCalls++
	if f.Err != nil {
		return nil, f.Err
	}
	if f.ChainErr != nil {
		return nil, f.ChainErr
	}
	return append([]application.PosternUtxo(nil), f.utxos[address]...), nil
}

// Balance reports address's balance, as postern's /api/balance does.
func (f *FakePostern) Balance(_ context.Context, address string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return 0, f.Err
	}
	return f.balance[address], nil
}

// Broadcast takes a raw transaction, as postern's /api/broadcast does.
func (f *FakePostern) Broadcast(_ context.Context, rawtx string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chainCalls++
	if f.Err != nil {
		return "", f.Err
	}
	if f.ChainErr != nil {
		return "", f.ChainErr
	}
	f.broadcast = append(f.broadcast, rawtx)
	if f.NextTxid != "" {
		return f.NextTxid, nil
	}
	return fmt.Sprintf("fake-txid-%d", len(f.broadcast)), nil
}

// Blob implements application.Postern.
func (f *FakePostern) Blob(_ context.Context, hash string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	body, ok := f.blobs[hash]
	if !ok {
		return nil, fmt.Errorf("no blob for hash %s", hash)
	}
	return append([]byte(nil), body...), nil
}

// Deliver implements application.Postern: the payload is kept, in order, for
// Delivered to report.
func (f *FakePostern) Deliver(_ context.Context, payload []byte) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", f.Err
	}
	if f.DeliverErr != nil {
		return "", f.DeliverErr
	}
	f.delivered = append(f.delivered, append([]byte(nil), payload...))
	if f.NextTxid != "" {
		return f.NextTxid, nil
	}
	return fmt.Sprintf("direct:fake-%d", len(f.delivered)), nil
}

// Delivered reports the payloads Deliver was handed, in order.
func (f *FakePostern) Delivered() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]byte, len(f.delivered))
	for i, p := range f.delivered {
		out[i] = append([]byte(nil), p...)
	}
	return out
}

// UploadBlob implements application.Postern: the body is stored under its
// own sha256, where Blob finds it, and kept for Uploaded to report.
func (f *FakePostern) UploadBlob(_ context.Context, body []byte) (string, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", 0, f.Err
	}
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	f.blobs[hash] = append([]byte(nil), body...)
	f.uploaded = append(f.uploaded, append([]byte(nil), body...))
	return hash, int64(len(body)), nil
}

// Uploaded reports the bodies UploadBlob was handed, in order.
func (f *FakePostern) Uploaded() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]byte, len(f.uploaded))
	for i, b := range f.uploaded {
		out[i] = append([]byte(nil), b...)
	}
	return out
}

// DeleteBlob implements application.Postern: the blob is gone, and the hash
// is kept for Deleted to report. A hash not held is already gone.
func (f *FakePostern) DeleteBlob(_ context.Context, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	delete(f.blobs, hash)
	f.deleted = append(f.deleted, hash)
	return nil
}

// Deleted reports the hashes DeleteBlob was asked to delete, in order.
func (f *FakePostern) Deleted() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deleted...)
}

// HasBlob reports whether a blob is still held under hash.
func (f *FakePostern) HasBlob(hash string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.blobs[hash]
	return ok
}

// Me implements application.Postern: Mine, as it is set.
func (f *FakePostern) Me(_ context.Context) (application.PosternMe, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return application.PosternMe{}, f.Err
	}
	return f.Mine, nil
}
