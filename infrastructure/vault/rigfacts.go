package vault

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// ReadRigFacts implements application.RigFactFiles: the rig's about text and
// every .md file of its facts folder, by file name. A seat with no folder for
// the rig does not know it; a fact file that cannot be read is handed on empty
// so that it is named as not whole, like readFacts does for a boot.
func (v *Vault) ReadRigFacts(_ context.Context, seat, rig string) (string, map[string]string, bool, error) {
	if err := safeName("seat", seat); err != nil {
		return "", nil, false, err
	}
	if err := safeName("rig", rig); err != nil {
		return "", nil, false, err
	}
	rigDir := filepath.Join(v.dir, SeatsDir, seat, RigsDir, rig)
	info, err := os.Stat(rigDir)
	if os.IsNotExist(err) || (err == nil && !info.IsDir()) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, fmt.Errorf("looking for the %s seat's folder for the rig %s: %w", seat, rig, err)
	}
	about, files, ok, err := v.readFacts(seat, rig)
	if err != nil {
		return "", nil, false, err
	}
	if !ok {
		aboutText, err := os.ReadFile(filepath.Join(rigDir, application.AboutFile))
		if err != nil && !os.IsNotExist(err) {
			return "", nil, false, fmt.Errorf("reading the %s seat's about text for the rig %s: %w", seat, rig, err)
		}
		return string(aboutText), map[string]string{}, true, nil
	}
	return about, files, true, nil
}

// WriteRigFact implements application.RigFactFiles.
func (v *Vault) WriteRigFact(_ context.Context, seat, rig, slug, text string) (string, error) {
	for _, name := range [][2]string{{"seat", seat}, {"rig", rig}, {"fact", slug}} {
		if err := safeName(name[0], name[1]); err != nil {
			return "", err
		}
	}
	dir := filepath.Join(v.dir, SeatsDir, seat, RigsDir, rig, application.FactsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("making the facts folder of the rig %s: %w", rig, err)
	}
	path := filepath.Join(dir, strings.TrimSuffix(slug, application.MemoryExt)+application.MemoryExt)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}
