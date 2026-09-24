package application

import (
	"context"
	"fmt"
	"io"
)

// PosternKeyFile is where the Mayor's postern key lives on this host: a
// generated testnet secp256k1 key, kept host-local outside the vault and its
// backups, like .mayor-acting. It never hands back the private key itself.
type PosternKeyFile interface {
	// Path reports where the key file is, for a message a person reads.
	Path() string
	// Exists reports whether the key file is already there.
	Exists() (bool, error)
	// Generate makes a fresh testnet key and writes it to the key file, 0600.
	// It must only be called once Exists has said false.
	Generate() error
	// PublicKey reads the key file and reports its compressed public key, as
	// hex, and its testnet address.
	PublicKey() (pubKeyHex string, address string, err error)
}

// PosternKeyInit makes the Mayor's postern key, once. It refuses to
// overwrite a key that is already there, so a second init changes nothing.
type PosternKeyInit struct {
	Keys PosternKeyFile
	Out  io.Writer
}

// PosternKeyInitReport is what init made.
type PosternKeyInitReport struct {
	Path string
}

func (r PosternKeyInitReport) String() string {
	return "Wrote a testnet postern key to " + r.Path
}

// Run makes the key, refusing when one is already there.
func (i PosternKeyInit) Run(_ context.Context) (PosternKeyInitReport, error) {
	exists, err := i.Keys.Exists()
	if err != nil {
		return PosternKeyInitReport{}, err
	}
	if exists {
		return PosternKeyInitReport{}, fmt.Errorf("a postern key already exists at %s: mw postern key init never overwrites one", i.Keys.Path())
	}
	if err := i.Keys.Generate(); err != nil {
		return PosternKeyInitReport{}, err
	}
	report := PosternKeyInitReport{Path: i.Keys.Path()}
	fmt.Fprintln(i.Out, report.String())
	return report, nil
}

// PosternKeyShow prints the postern key's public half: never the private key.
type PosternKeyShow struct {
	Keys PosternKeyFile
	Out  io.Writer
}

// PosternKeyShowReport is what show printed.
type PosternKeyShowReport struct {
	PublicKey string
	Address   string
}

func (r PosternKeyShowReport) String() string {
	return fmt.Sprintf("public key: %s\ntestnet address: %s", r.PublicKey, r.Address)
}

// Run reports the key's public key and testnet address, refusing when there
// is no key yet.
func (s PosternKeyShow) Run(_ context.Context) (PosternKeyShowReport, error) {
	exists, err := s.Keys.Exists()
	if err != nil {
		return PosternKeyShowReport{}, err
	}
	if !exists {
		return PosternKeyShowReport{}, fmt.Errorf("no postern key at %s: run mw postern key init first", s.Keys.Path())
	}
	pubKeyHex, address, err := s.Keys.PublicKey()
	if err != nil {
		return PosternKeyShowReport{}, err
	}
	report := PosternKeyShowReport{PublicKey: pubKeyHex, Address: address}
	fmt.Fprintln(s.Out, report.String())
	return report, nil
}
