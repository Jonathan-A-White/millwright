# Laptop inventory prompt

Paste everything in the block below into Claude Code running in WSL on the Laptop. It is read-only: it installs nothing and changes nothing. It resolves the ticket **Inventory the Laptop**.

```text
I'm building a personal software factory (Go, Beads, tmux/Herdr, Obsidian, Claude Code). Another
Claude session on my always-on VPS is designing it and needs to know what this laptop can do. This
laptop (Windows + WSL2 Ubuntu 24.04) will be the main host: it runs the Mayor session I talk to and
most parallel story sessions. The VPS takes long-running work when this laptop is asleep.

Your job: produce a factual inventory report. STRICT RULES:
- Read-only. Do not install, upgrade, configure, create or delete anything except the one report file.
- Never print secrets: no tokens, keys, passwords, env var values, or file contents from ~/.ssh,
  ~/.config/gh, ~/.claude.json, or any .env. Report existence and names only.
- If a command is missing or fails, write "not found" / the error and move on. Don't guess.
- Facts only, no recommendations, except the final section.

Gather, then write the report to ~/laptop-inventory.md and also print it:

1. HARDWARE (Windows side, via powershell.exe from WSL): laptop model, CPU model, physical cores and
   logical processors, total RAM, GPU(s), disk model/size/free, battery present and design capacity.
2. WSL: `wsl.exe --version`, distro and kernel, contents of %UserProfile%\.wslconfig (memory,
   processors, swap, networkingMode, autoMemoryReclaim) and /etc/wsl.conf (systemd? automount?),
   what WSL actually sees (nproc, free -h, df -h / and /mnt/c), whether systemd is PID 1.
3. POWER AND SLEEP: Windows power plan, lid-close action and sleep timeouts on battery and on AC
   (powercfg /query or /a), Modern Standby (S0) vs S3. I need to know what happens to running WSL
   processes when I close the lid on the train.
4. NETWORK: WSL networking mode (NAT or mirrored), is sshd installed/running in WSL, is Tailscale
   (or any VPN/mesh) installed on Windows or in WSL, Host aliases defined in ~/.ssh/config (names and
   HostName only, no keys), whether mosh or autossh is installed.
5. TOOLCHAIN, with versions and paths: go, git, gh (and `gh auth status` account name only), tmux,
   claude (Claude Code version), bd (beads), dolt, herdr, node/npm, python3, docker (and whether
   Docker Desktop or native), make, gcc, ripgrep, fd, jq, direnv, mise/asdf. Note anything installed
   on the Windows side that matters: Obsidian (and vault locations if discoverable), Windows
   Terminal, VS Code, Emacs.
6. CLAUDE CODE SETUP: ~/.claude/settings.json keys (not secret values), names of commands, skills,
   agents, plugins and MCP servers configured, which plan/login method is in use if `claude` reports
   it, and the default model.
7. REPOS: where my git repos live (list directories under ~ and any obvious code dir, depth 2, that
   contain .git), whether any live under /mnt/c (slow path), and any existing .beads directories.
8. HEADROOM TEST (cheap, non-destructive): time `go build` of a tiny hello-world in a temp dir if go
   exists; report current load average and free memory with nothing else running.
9. OPEN QUESTIONS FOR THE VPS SESSION: a short list of anything surprising, broken, or ambiguous
   you noticed that would affect running 4-8 parallel Claude Code sessions plus Go builds here.

Format the report as markdown with those nine headings. Keep it under ~250 lines.
```

Then get the report to the VPS, either way:

- from the Laptop: `scp ~/laptop-inventory.md <vps>:/root/millwright-vault/hosts/laptop-inventory.md`
- or paste it into the VPS session.
