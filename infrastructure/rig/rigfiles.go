package rig

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// Files reads the checkouts of the rigs on this host from disk.
type Files struct{}

var _ application.RigFiles = Files{}

// Exists implements application.RigFiles: whether the pattern, a file, a
// directory or a glob of a story's files key, is in the checkout at rigDir. A
// glob with ** is found when the directory it starts from is there, since path
// has no way to say any depth.
func (Files) Exists(rigDir, pattern string) bool {
	if !strings.ContainsAny(pattern, "*?[") {
		_, err := os.Stat(filepath.Join(rigDir, filepath.FromSlash(pattern)))
		return err == nil
	}
	if strings.Contains(pattern, "**") {
		start := strings.SplitN(pattern, "**", 2)[0]
		if i := strings.LastIndex(start, "/"); i >= 0 {
			start = start[:i]
		} else {
			start = ""
		}
		info, err := os.Stat(filepath.Join(rigDir, filepath.FromSlash(start)))
		return err == nil && info.IsDir()
	}
	found, err := filepath.Glob(filepath.Join(rigDir, filepath.FromSlash(pattern)))
	return err == nil && len(found) > 0
}

// FilesIn implements application.RigFiles: the files under dir in the checkout
// at rigDir, counted no further than one past most. What git and a package
// manager keep there (.git and the other dot directories, node_modules) is not
// counted: a story does not change it.
func (Files) FilesIn(rigDir, dir string, most int) (int, bool) {
	root := filepath.Join(rigDir, filepath.FromSlash(dir))
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return 0, false
	}
	var n int
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if n++; n > most {
			return filepath.SkipAll
		}
		return nil
	})
	return n, true
}

// Matching implements application.RigFiles: the files in the checkout at rigDir
// that the glob covers, counted no further than one past most. The walk starts
// at the directory the glob names before its first wildcard, and skips what
// FilesIn does not count.
func (Files) Matching(rigDir, pattern string, most int) int {
	start := ""
	for _, part := range strings.Split(pattern, "/") {
		if strings.ContainsAny(part, "*?[") {
			break
		}
		start = path.Join(start, part)
	}
	root := filepath.Join(rigDir, filepath.FromSlash(start))
	var n int
	_ = filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if file != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(rigDir, file)
		if err != nil || !domain.Covers(pattern, filepath.ToSlash(rel)) {
			return nil
		}
		if n++; n > most {
			return filepath.SkipAll
		}
		return nil
	})
	return n
}

// callerSearchLimit is how long listing the rig's tracked files may take.
const callerSearchLimit = 20 * time.Second

// TestCallers implements application.RigFiles: the tracked test files of the
// checkout at rigDir (those under features/ or tests/, and any *_test.go) that
// name something the named files declare (mw-gq6.661, mw-gq6.856). The config's
// [tests] is a command, not a list of globs, so those three are the test files
// of every rig.
//
// What a file declares is, for a Go file, its top-level exported identifiers
// (func, type, var and const; a method is reached through its type), and for a
// TS or JS file its exported names and its import path, the path without
// extension. A test file counts when it calls one of them:
//   - Go: it says pkg.Name, pkg being the declaring file's package, or it is in
//     the same directory (the same package) and says Name not after a dot, so
//     that a field called Status is not a call of a type called Status;
//   - TS or JS: it says an exported name as a whole word, or imports the path
//     (from, require or import() of '.../<stem>').
//
// The file's base name as a word is not looked for: a stem like status or
// doctor is in hundreds of tests that never call the file. A named file that is
// itself a test declares nothing others call, one that is not there declares
// nothing, and a named file is never its own caller.
func (Files) TestCallers(rigDir string, named []string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callerSearchLimit)
	defer cancel()
	args := []string{"-C", rigDir, "ls-files", "-z", "--", "features", "tests", ":(glob)**/*_test.go"}
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		return 0, fmt.Errorf("counting the test callers in %s: %w", rigDir, err)
	}
	matcher := newCallerMatcher(rigDir, named)
	own := map[string]bool{}
	for _, file := range named {
		own[path.Clean(file)] = true
	}
	var n int
	for _, test := range strings.Split(string(out), "\x00") {
		if test == "" || own[test] {
			continue
		}
		text, err := os.ReadFile(filepath.Join(rigDir, filepath.FromSlash(test)))
		if err == nil && matcher.calls(path.Dir(test), text) {
			n++
		}
	}
	return n, nil
}

// callerMatcher says whether a test file calls what a story's files declare.
type callerMatcher struct {
	// anywhere matches in any test file: a TS or JS name or import, and a Go
	// name qualified by its package.
	anywhere *regexp.Regexp
	// sameDir matches in the test files of one directory: the bare Go names of
	// the files declared there.
	sameDir map[string]*regexp.Regexp
}

func newCallerMatcher(rigDir string, named []string) callerMatcher {
	var anywhere []string
	bare := map[string][]string{}
	for _, file := range named {
		d := declaredIn(rigDir, file)
		if len(d.names) == 0 && d.importStem == "" {
			continue
		}
		quoted := make([]string, len(d.names))
		for i, name := range d.names {
			quoted[i] = regexp.QuoteMeta(name)
		}
		names := strings.Join(quoted, "|")
		switch {
		case d.pkg != "" && names != "":
			anywhere = append(anywhere, `\b`+regexp.QuoteMeta(d.pkg)+`\.(?:`+names+`)\b`)
			bare[path.Dir(file)] = append(bare[path.Dir(file)], names)
		case names != "":
			anywhere = append(anywhere, `\b(?:`+names+`)\b`)
		}
		if d.importStem != "" {
			anywhere = append(anywhere, `(?:from|require\(|import\()\s*['"](?:[^'"\n]*/)?`+regexp.QuoteMeta(d.importStem)+`(?:\.[A-Za-z]+)?['"]`)
		}
	}
	m := callerMatcher{sameDir: map[string]*regexp.Regexp{}}
	if len(anywhere) > 0 {
		m.anywhere = regexp.MustCompile(strings.Join(anywhere, "|"))
	}
	for dir, names := range bare {
		m.sameDir[dir] = regexp.MustCompile(`(?:^|[^.\w])(?:` + strings.Join(names, "|") + `)(?:[^:\w]|:=|$)`)
	}
	return m
}

func (m callerMatcher) calls(dir string, text []byte) bool {
	if m.anywhere != nil && m.anywhere.Match(text) {
		return true
	}
	same := m.sameDir[dir]
	return same != nil && same.Match(text)
}

// What a TS or JS file exports: export function, class, const, let, var, type,
// interface and enum, and the names in export { ... }.
var (
	exportedDeclaration = regexp.MustCompile(`(?m)^\s*export\s+(?:declare\s+)?(?:default\s+)?(?:abstract\s+)?(?:async\s+)?(?:function\*?|class|const|let|var|type|interface|enum)\s+([A-Za-z_$][\w$]*)`)
	exportedList        = regexp.MustCompile(`(?m)^\s*export\s*(?:type\s*)?\{([^}]*)\}`)
)

// declared is what a file offers its callers: its exported names, the package
// they are qualified by (a Go file) and the stem it is imported by (a TS or JS
// file).
type declared struct {
	names      []string
	pkg        string
	importStem string
}

// declaredIn reads what the file at rel in the checkout declares for others to
// use; a file of another kind, or one that is not there, declares nothing.
func declaredIn(rigDir, rel string) declared {
	file := filepath.Join(rigDir, filepath.FromSlash(rel))
	base := path.Base(rel)
	switch ext := path.Ext(rel); ext {
	case ".go":
		if strings.HasSuffix(base, "_test.go") {
			return declared{}
		}
		parsed, _ := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
		if parsed == nil {
			return declared{}
		}
		d := declared{pkg: parsed.Name.Name}
		for _, decl := range parsed.Decls {
			switch x := decl.(type) {
			case *ast.FuncDecl:
				if x.Recv == nil && x.Name.IsExported() {
					d.names = append(d.names, x.Name.Name)
				}
			case *ast.GenDecl:
				for _, spec := range x.Specs {
					switch sp := spec.(type) {
					case *ast.TypeSpec:
						if sp.Name.IsExported() {
							d.names = append(d.names, sp.Name.Name)
						}
					case *ast.ValueSpec:
						for _, id := range sp.Names {
							if id.IsExported() {
								d.names = append(d.names, id.Name)
							}
						}
					}
				}
			}
		}
		return d
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		stem := strings.TrimSuffix(base, ext)
		if strings.HasSuffix(stem, ".test") || strings.HasSuffix(stem, ".spec") {
			return declared{}
		}
		text, err := os.ReadFile(file)
		if err != nil {
			return declared{}
		}
		d := declared{importStem: stem}
		for _, m := range exportedDeclaration.FindAllSubmatch(text, -1) {
			d.names = append(d.names, string(m[1]))
		}
		for _, m := range exportedList.FindAllSubmatch(text, -1) {
			for _, item := range strings.Split(string(m[1]), ",") {
				fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(item), "type "))
				if len(fields) > 0 {
					d.names = append(d.names, fields[len(fields)-1])
				}
			}
		}
		return d
	}
	return declared{}
}

// Docs reads one file of a rig's checkout on this host, for a seat's boot
// manifest entry that names a rig: the adapter behind application.RigDocs.
// Rigs is where each rig is checked out, by name, as the config's [rigs] says.
type Docs struct {
	Rigs map[string]string
}

var _ application.RigDocs = Docs{}

// RigFile implements application.RigDocs.
func (d Docs) RigFile(_ context.Context, rig, rel string) (string, error) {
	dir, here := d.Rigs[rig]
	if !here || dir == "" {
		return "", fmt.Errorf("the rig %s is not checked out on this host (the config's [rigs] does not name it)", rig)
	}
	switch {
	case rel == "":
		return "", fmt.Errorf("a path in the rig %s is needed", rig)
	case strings.HasPrefix(rel, "/"), strings.Contains(rel, `\`), strings.Contains(rel, ".."):
		return "", fmt.Errorf("%q would reach outside the rig %s", rel, rig)
	}
	text, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		return "", fmt.Errorf("reading %s of the rig %s: %w", rel, rig, err)
	}
	return string(text), nil
}
