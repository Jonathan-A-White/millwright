package domain

import (
	"path"
	"strings"
)

// FileList is a story's files key read as the repo paths and globs it names:
// split at commas, each trimmed, a leading "./" or "/" and a trailing "/" taken
// off, the empty ones dropped.
func FileList(files string) []string {
	var list []string
	for _, name := range strings.Split(files, ",") {
		name = strings.TrimSpace(name)
		name = strings.TrimPrefix(name, "./")
		name = strings.Trim(name, "/")
		if name != "" {
			list = append(list, name)
		}
	}
	return list
}

// SharedFile is the first path, of ours in order, that ours and theirs both
// cover, and whether there is one. Either side may name a file, a directory
// (which covers everything under it) or a glob: * and the rest as path.Match
// has them within a directory, and ** for any number of directories. Where one
// side's glob covers the other's path, the path is the one said; two globs
// meet only when one covers the other as it is written.
func SharedFile(ours, theirs []string) (string, bool) {
	for _, a := range ours {
		for _, b := range theirs {
			switch {
			case covers(a, b):
				return b, true
			case covers(b, a):
				return a, true
			}
		}
	}
	return "", false
}

// covers reports whether the pattern names the path or a directory it is in.
func covers(pattern, name string) bool {
	if pattern == name {
		return true
	}
	pat, parts := strings.Split(pattern, "/"), strings.Split(name, "/")
	for n := 1; n <= len(parts); n++ {
		if globMatch(pat, parts[:n]) {
			return true
		}
	}
	return false
}

// globMatch matches a pattern against a path, one directory at a time, a **
// standing for any number of them, none included.
func globMatch(pat, parts []string) bool {
	if len(pat) == 0 {
		return len(parts) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(parts); i++ {
			if globMatch(pat[1:], parts[i:]) {
				return true
			}
		}
		return false
	}
	if len(parts) == 0 {
		return false
	}
	matched, err := path.Match(pat[0], parts[0])
	return err == nil && matched && globMatch(pat[1:], parts[1:])
}

// NamedDirectory is one name of a files key that may name a directory: as it
// was written, the directory it would cover (Dir, "" for the whole rig), and
// whether its words alone say it covers all of that directory (Whole): "src/",
// "src/*" and "src/**" do, and so do "*" and "**" of the whole rig. A plain
// name with no glob in it, "src" or "src/Talk.tsx", may be a file or a
// directory: only the rig's checkout can say which.
type NamedDirectory struct {
	Written string
	Dir     string
	Whole   bool
}

// Directories is each name of a files key that may name a directory, in
// order: the ones written as a whole directory, and the plain ones. A glob
// narrower than a whole directory, "src/*.tsx", is left out.
func Directories(files string) []NamedDirectory {
	var found []NamedDirectory
	for _, written := range strings.Split(files, ",") {
		written = strings.TrimSpace(written)
		name := strings.TrimPrefix(written, "./")
		if name == "" {
			continue
		}
		whole := strings.HasSuffix(name, "/") || name == "."
		parts := strings.Split(strings.Trim(name, "/"), "/")
		for len(parts) > 0 && (parts[len(parts)-1] == "*" || parts[len(parts)-1] == "**" || parts[len(parts)-1] == ".") {
			parts, whole = parts[:len(parts)-1], true
		}
		dir := strings.Join(parts, "/")
		if strings.ContainsAny(dir, "*?[") {
			continue
		}
		found = append(found, NamedDirectory{Written: written, Dir: dir, Whole: whole})
	}
	return found
}
