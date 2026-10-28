package doctor

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Defaults of the sampler: ten looks a second apart. A bd lives a fraction of a
// second, so a look sees only some of them; the counts rank the clients, they do
// not total the connections.
const (
	bdParentsRounds   = 10
	bdParentsInterval = time.Second
)

// BdParents samples which commands start bd processes, the way the Millhand
// did by hand for mw-gq6.825: look at the process table a few times, and for
// each bd not seen before name its parent's command.
type BdParents struct {
	// Ps returns the process table, one process a line as `pid ppid args`; nil
	// is ps.
	Ps func(ctx context.Context) (string, error)
	// Rounds is how many looks are taken; zero is ten.
	Rounds int
	// Wait pauses between looks; nil sleeps the interval, ending early with ctx.
	Wait func(ctx context.Context, d time.Duration)
}

// Sample implements the Clients function of BeadsChurn: the commands whose
// children were bd processes, most first, each bd counted once however many
// looks saw it.
func (s *BdParents) Sample(ctx context.Context) ([]ChurnClient, error) {
	rounds := s.Rounds
	if rounds <= 0 {
		rounds = bdParentsRounds
	}
	seen := map[int]bool{}
	counts := map[string]int{}
	for round := 0; round < rounds; round++ {
		if round > 0 {
			s.wait(ctx, bdParentsInterval)
		}
		if ctx.Err() != nil {
			break
		}
		table, err := s.ps(ctx)
		if err != nil {
			return nil, err
		}
		for pid, command := range bdChildren(table) {
			if !seen[pid] {
				seen[pid] = true
				counts[command]++
			}
		}
	}
	clients := make([]ChurnClient, 0, len(counts))
	for command, n := range counts {
		clients = append(clients, ChurnClient{Command: command, Count: n})
	}
	sort.Slice(clients, func(i, j int) bool {
		if clients[i].Count != clients[j].Count {
			return clients[i].Count > clients[j].Count
		}
		return clients[i].Command < clients[j].Command
	})
	return clients, nil
}

func (s *BdParents) ps(ctx context.Context) (string, error) {
	if s.Ps != nil {
		return s.Ps(ctx)
	}
	out, err := exec.CommandContext(ctx, "ps", "-eo", "pid=,ppid=,args=").Output()
	if err != nil {
		return "", fmt.Errorf("ps: %w", err)
	}
	return string(out), nil
}

func (s *BdParents) wait(ctx context.Context, d time.Duration) {
	if s.Wait != nil {
		s.Wait(ctx, d)
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

type psLine struct {
	ppid int
	args []string
}

// bdChildren is each bd process of a process table, by pid, with the name of
// its parent's command (the parent's own pid when the parent has left).
func bdChildren(table string) map[int]string {
	procs := map[int]psLine{}
	for _, line := range strings.Split(table, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		procs[pid] = psLine{ppid: ppid, args: fields[2:]}
	}
	children := map[int]string{}
	for pid, p := range procs {
		if filepath.Base(p.args[0]) != "bd" {
			continue
		}
		if parent, ok := procs[p.ppid]; ok {
			children[pid] = commandName(parent.args)
		} else {
			children[pid] = "pid " + strconv.Itoa(p.ppid)
		}
	}
	return children
}

// shells are the interpreters whose script, not themselves, is the client.
var shells = map[string]bool{"bash": true, "sh": true, "zsh": true, "dash": true, "python": true, "python3": true, "node": true}

// commandName names a command by its program and the subcommand words after
// it, up to three: `/usr/bin/mw postern bead --vault x` is `mw postern bead`,
// and `/bin/bash /opt/seat-loop.sh` is `seat-loop.sh`.
func commandName(args []string) string {
	program := filepath.Base(args[0])
	rest := args[1:]
	if shells[program] {
		for len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
			rest = rest[1:]
		}
		if len(rest) == 0 {
			return program
		}
		return filepath.Base(rest[0])
	}
	name := []string{program}
	for _, arg := range rest {
		if len(name) == 3 || strings.HasPrefix(arg, "-") || strings.ContainsAny(arg, "/=:.") {
			break
		}
		name = append(name, arg)
	}
	return strings.Join(name, " ")
}
