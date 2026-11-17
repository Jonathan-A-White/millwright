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
	"runtime"
	"strings"
	"sync"
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
// TS or JS file its import path, the path without extension. A test file counts
// when it calls one of them:
//   - Go: it says pkg.Name, pkg being the declaring file's package, or it is in
//     the same directory (the same package) and says Name not after a dot, so
//     that a field called Status is not a call of a type called Status;
//   - TS or JS: it imports the path (from, import, require or import() of
//     '.../<stem>', or vi.mock or jest.mock of it, by any relative or aliased
//     path; a side-effect import and export ... from count too). The file's exported names
//     as words are not looked for: a name like Reader or Settings is said in
//     strings and comments of hundreds of tests that never import the file
//     (mw-6ww.175.4).
//
// The file's base name as a word is not looked for: a stem like status or
// doctor is in hundreds of tests that never call the file. A named file that is
// itself a test declares nothing others call, one that is not there declares
// nothing, and a named file is never its own caller.
func (Files) TestCallers(rigDir string, named []string) (int, error) {
	tests, err := readTestFiles(rigDir)
	if err != nil {
		return 0, err
	}
	return countCallers(rigDir, tests, [][]string{named})[0], nil
}

// originBranchLimit is how long asking the origin itself may take.
const originBranchLimit = 20 * time.Second

// BranchOnOrigin implements application.RigFiles: whether the branch is one of
// the origin of the checkout at rigDir, by its remote-tracking ref or, when the
// checkout does not have that, by what the origin lists; and the branch the
// origin's HEAD is (mw-gq6.1416). An error is a checkout git cannot read or an
// origin that cannot be asked.
func (Files) BranchOnOrigin(rigDir, branch string) (bool, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), originBranchLimit)
	defer cancel()
	git := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", rigDir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
	if _, err := git("rev-parse", "--git-dir"); err != nil {
		return false, "", fmt.Errorf("reading the checkout at %s: %w", rigDir, err)
	}
	head := ""
	if out, err := git("symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		head = strings.TrimPrefix(out, "origin/")
	}
	if _, err := git("rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch); err == nil {
		return true, head, nil
	}
	listed, err := git("ls-remote", "--heads", "origin", "refs/heads/"+branch)
	if err != nil {
		return false, head, fmt.Errorf("asking the origin of %s: %w", rigDir, err)
	}
	if listed != "" {
		return true, head, nil
	}
	if head == "" {
		if out, err := git("ls-remote", "--symref", "origin", "HEAD"); err == nil {
			for _, line := range strings.Split(out, "\n") {
				if ref, ok := strings.CutPrefix(line, "ref: refs/heads/"); ok {
					head, _, _ = strings.Cut(ref, "\t")
					break
				}
			}
		}
	}
	return false, head, nil
}

// testFile is a tracked test file of a checkout and what it says.
type testFile struct {
	name string
	text []byte
}

// readTestFiles lists the tracked test files of the checkout at rigDir (those
// under features/ or tests/, and any *_test.go) and reads each once: the one
// reader of the test files, for Files.TestCallers (one story's files together)
// and Shape.TestCallersOf (each of many files alone).
func readTestFiles(rigDir string) ([]testFile, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callerSearchLimit)
	defer cancel()
	args := []string{"-C", rigDir, "ls-files", "-z", "--", "features", "tests", ":(glob)**/*_test.go"}
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("counting the test callers in %s: %w", rigDir, err)
	}
	var tests []testFile
	for _, name := range strings.Split(string(out), "\x00") {
		if name == "" {
			continue
		}
		if text, err := os.ReadFile(filepath.Join(rigDir, filepath.FromSlash(name))); err == nil {
			tests = append(tests, testFile{name, text})
		}
	}
	return tests, nil
}

// countCallers counts, for each group of named files, the tests that call
// something the group declares; a named file is never its own caller. The
// groups are counted side by side over the one read of the tests.
func countCallers(rigDir string, tests []testFile, groups [][]string) []int {
	counts := make([]int, len(groups))
	next := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				matcher := newCallerMatcher(rigDir, groups[i])
				own := map[string]bool{}
				for _, file := range groups[i] {
					own[path.Clean(file)] = true
				}
				for _, test := range tests {
					if !own[test.name] && matcher.calls(path.Dir(test.name), test.text) {
						counts[i]++
					}
				}
			}
		}()
	}
	for i := range groups {
		next <- i
	}
	close(next)
	wg.Wait()
	return counts
}

// callerMatcher says whether a test file calls what a story's files declare.
type callerMatcher struct {
	// anywhere matches in any test file: a TS or JS import, and a Go name
	// qualified by its package.
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
			anywhere = append(anywhere, `(?:from|import|require\(|import\(|(?:vi|jest)\.(?:mock|doMock)\()\s*['"](?:[^'"\n]*/)?`+regexp.QuoteMeta(d.importStem)+`(?:\.[cm]?[jt]sx?)?['"]`)
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

// declared is what a file offers its callers: its exported names and the
// package they are qualified by (a Go file), or the stem it is imported by (a
// TS or JS file).
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
		if info, err := os.Stat(file); err != nil || info.IsDir() {
			return declared{}
		}
		return declared{importStem: stem}
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
