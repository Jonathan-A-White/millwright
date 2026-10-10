package vault

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

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

// ReadRigEval implements application.RigFactFiles.
func (v *Vault) ReadRigEval(_ context.Context, seat, rig string) (string, bool, error) {
	if err := safeName("seat", seat); err != nil {
		return "", false, err
	}
	if err := safeName("rig", rig); err != nil {
		return "", false, err
	}
	text, err := os.ReadFile(filepath.Join(v.dir, SeatsDir, seat, RigsDir, rig, application.EvalFile))
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("reading the %s seat's eval for the rig %s: %w", seat, rig, err)
	}
	return string(text), true, nil
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

// ReadRigMemoryFiles implements application.RigLegacyFiles: the text of the
// seat's one memory file for the rig (seats/<seat>/rigs/<rig>.md) and of the
// archive it is pruned into, each only when it is there, and whether the rig
// already has a facts folder.
func (v *Vault) ReadRigMemoryFiles(_ context.Context, seat, rig string) (application.RigMemoryFiles, error) {
	var got application.RigMemoryFiles
	if err := checkRigNames(seat, rig); err != nil {
		return got, err
	}
	rigs := filepath.Join(v.dir, SeatsDir, seat, RigsDir)
	for _, f := range []struct {
		name string
		text *string
		has  *bool
	}{
		{rig + application.MemoryExt, &got.Memory, &got.HasMemory},
		{rig + application.ArchiveSuffix + application.MemoryExt, &got.Archive, &got.HasArchive},
	} {
		text, err := os.ReadFile(filepath.Join(rigs, f.name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return got, fmt.Errorf("reading %s: %w", filepath.Join(rigs, f.name), err)
		}
		*f.text, *f.has = string(text), true
	}
	if _, err := os.Stat(filepath.Join(rigs, rig, application.FactsDir)); err == nil {
		got.HasFacts = true
	} else if !os.IsNotExist(err) && !errors.Is(err, syscall.ENOTDIR) {
		return got, fmt.Errorf("looking for the facts folder of the rig %s: %w", rig, err)
	}
	return got, nil
}

// WriteRigAbout implements application.RigLegacyFiles: seats/<seat>/rigs/<rig>/about.md.
func (v *Vault) WriteRigAbout(_ context.Context, seat, rig, text string) (string, error) {
	if err := checkRigNames(seat, rig); err != nil {
		return "", err
	}
	dir := filepath.Join(v.dir, SeatsDir, seat, RigsDir, rig)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("making the folder of the rig %s: %w", rig, err)
	}
	path := filepath.Join(dir, application.AboutFile)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}

// RemoveRigMemoryFiles implements application.RigLegacyFiles: it removes the
// rig's one memory file and its archive, the two files migrate has just turned
// into facts, and nothing else. A file that is not there is not an error.
func (v *Vault) RemoveRigMemoryFiles(_ context.Context, seat, rig string) error {
	if err := checkRigNames(seat, rig); err != nil {
		return err
	}
	rigs := filepath.Join(v.dir, SeatsDir, seat, RigsDir)
	for _, name := range []string{rig + application.MemoryExt, rig + application.ArchiveSuffix + application.MemoryExt} {
		if err := os.Remove(filepath.Join(rigs, name)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing %s: %w", filepath.Join(rigs, name), err)
		}
	}
	return nil
}

func checkRigNames(seat, rig string) error {
	for _, name := range [][2]string{{"seat", seat}, {"rig", rig}} {
		if err := safeName(name[0], name[1]); err != nil {
			return err
		}
	}
	return nil
}
