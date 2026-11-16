package browser

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Fake is a Launcher for tests. It starts nothing: a program it "starts"
// listens on the port its arguments name (Xtigervnc's -rfbport, Chromium's
// --remote-debugging-port), Chromium's answering /json/version and /json/list
// with one tab at the URL it was opened at, and writes one line to its Log.
type Fake struct {
	mu      sync.Mutex
	next    int
	started []Program
	procs   map[int]*fakeProc
	silent  map[string]bool
}

type fakeProc struct {
	p       Program
	ln      net.Listener
	tabs    []Tab
	running bool
}

// NewFake is a Fake with nothing started.
func NewFake() *Fake {
	return &Fake{next: 4000, procs: map[int]*fakeProc{}, silent: map[string]bool{}}
}

// Silent makes the program named name start but never listen.
func (f *Fake) Silent(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.silent[name] = true
}

// Started is every program started, in order.
func (f *Fake) Started() []Program {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Program(nil), f.started...)
}

func argAfter(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(a, flag+"="); ok {
			return v
		}
	}
	return ""
}

// Start records p, writes its log line and listens as p would.
func (f *Fake) Start(p Program) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := os.WriteFile(p.Log, []byte("fake "+filepath.Base(p.Path)+" started\n"), 0o600); err != nil {
		return 0, err
	}
	f.next++
	pid := f.next
	proc := &fakeProc{p: p, running: true}
	f.started = append(f.started, p)
	f.procs[pid] = proc
	if f.silent[p.Name] {
		return pid, nil
	}
	port := argAfter(p.Args, "-rfbport")
	if p.Name == ChromiumName {
		port = argAfter(p.Args, "--remote-debugging-port")
		proc.tabs = []Tab{{Title: "", URL: p.Args[len(p.Args)-1]}}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return 0, fmt.Errorf("the fake %s cannot listen on %s: %w", p.Name, port, err)
	}
	proc.ln = ln
	if p.Name == ChromiumName {
		mux := http.NewServeMux()
		mux.HandleFunc("/json/version", func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]string{"Browser": "FakeChrome/1", "webSocketDebuggerUrl": "ws://" + ln.Addr().String() + "/devtools/browser/fake"})
		})
		mux.HandleFunc("/json/list", func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			var list []map[string]string
			for _, t := range proc.tabs {
				list = append(list, map[string]string{"type": "page", "title": t.Title, "url": t.URL})
			}
			f.mu.Unlock()
			list = append(list, map[string]string{"type": "service_worker", "url": "https://worker.example/sw.js"})
			_ = json.NewEncoder(w).Encode(list)
		})
		go func() { _ = http.Serve(ln, mux) }()
	} else {
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				_ = c.Close()
			}
		}()
	}
	return pid, nil
}

// Running is whether pid was started, still runs and carries marker in its
// path or arguments.
func (f *Fake) Running(pid int, marker string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	proc, ok := f.procs[pid]
	if !ok || !proc.running {
		return false
	}
	return strings.Contains(proc.p.Path+" "+strings.Join(proc.p.Args, " "), marker)
}

// Stop ends pid.
func (f *Fake) Stop(pid int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	proc, ok := f.procs[pid]
	if !ok {
		return fmt.Errorf("no pid %d", pid)
	}
	proc.end()
	return nil
}

func (p *fakeProc) end() {
	p.running = false
	if p.ln != nil {
		_ = p.ln.Close()
		p.ln = nil
	}
}

// Kill ends every running program named name, as when he closes its window.
func (f *Fake) Kill(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, proc := range f.procs {
		if proc.p.Name == name {
			proc.end()
		}
	}
}

// StopAll ends every program, for a test's cleanup.
func (f *Fake) StopAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, proc := range f.procs {
		proc.end()
	}
}

// Up is whether a program named name still runs.
func (f *Fake) Up(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, proc := range f.procs {
		if proc.p.Name == name && proc.running {
			return true
		}
	}
	return false
}

// AddTab opens another tab at url in the running Chromium.
func (f *Fake) AddTab(url string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, proc := range f.procs {
		if proc.p.Name == ChromiumName && proc.running {
			proc.tabs = append(proc.tabs, Tab{URL: url})
			return nil
		}
	}
	return fmt.Errorf("no Chromium runs")
}
