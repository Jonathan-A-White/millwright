package apptest

import (
	"context"
	"fmt"
	"sort"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.SecretStore = (*FakeSecretStore)(nil)

// FakeSecretStore keeps secrets in memory, in the clear: a stand-in for the
// sops store where no encryption is under test. Calls counts every method
// called; Err, when set, is what each returns.
type FakeSecretStore struct {
	Values map[string][]byte
	Calls  int
	Err    error
}

// Put implements application.SecretStore.
func (f *FakeSecretStore) Put(_ context.Context, name string, value []byte) error {
	f.Calls++
	if f.Err != nil {
		return f.Err
	}
	if f.Values == nil {
		f.Values = map[string][]byte{}
	}
	f.Values[name] = append([]byte(nil), value...)
	return nil
}

// Get implements application.SecretStore.
func (f *FakeSecretStore) Get(_ context.Context, name string) ([]byte, error) {
	f.Calls++
	if f.Err != nil {
		return nil, f.Err
	}
	value, ok := f.Values[name]
	if !ok {
		return nil, fmt.Errorf("%s: %w", name, application.ErrNoSecret)
	}
	return value, nil
}

// Names implements application.SecretStore.
func (f *FakeSecretStore) Names(context.Context) ([]string, error) {
	f.Calls++
	if f.Err != nil {
		return nil, f.Err
	}
	names := make([]string, 0, len(f.Values))
	for name := range f.Values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
