package vault

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/Jonathan-A-White/millwright/application"
)

// Vault is the adapter behind application.SubscribeFiles as well: the seats'
// seats/<seat>/subscribe.toml.
var _ application.SubscribeFiles = (*Vault)(nil)

// SubscribedSeats implements application.SubscribeFiles: the seats whose
// directory holds a subscribe file, by name.
func (v *Vault) SubscribedSeats(context.Context) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(v.dir, SeatsDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing the seats: %w", err)
	}
	var seats []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(v.dir, SeatsDir, e.Name(), application.SubscribeFileName)); err == nil {
			seats = append(seats, e.Name())
		}
	}
	sort.Strings(seats)
	return seats, nil
}

// SubscribeFile implements application.SubscribeFiles.
func (v *Vault) SubscribeFile(_ context.Context, seat string) (string, bool, error) {
	if err := safeName("seat", seat); err != nil {
		return "", false, err
	}
	data, err := os.ReadFile(filepath.Join(v.dir, SeatsDir, seat, application.SubscribeFileName))
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("reading the %s seat's subscribe file: %w", seat, err)
	}
	return string(data), true, nil
}
