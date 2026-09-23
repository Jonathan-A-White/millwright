# A local model on the Laptop's GPU/NPU: what fits, what it could do, how it would be wired

Research date: 2026-09-23. Read-only: nothing installed, nothing downloaded, no config changed.
Resolves mw-6ww.41, parked from mw-6ww.40/mw-i80dx by the Governor's "yes to 41" (2026-09-23
~23:00Z) after asking whether his laptop's NPU/GPU are useful for the factory.

## What this means for the factory (read this first)

The Laptop has one GPU (an integrated Radeon 890M, no discrete card, no dedicated VRAM — it shares
system RAM) and one NPU (AMD XDNA2, 50 TOPS), on a chip whose real bottleneck for language models is
memory bandwidth, not compute. **Today, right now, with nothing installed**: the NPU cannot usefully
run a 7B+ chat model — mainstream runtimes (ollama, llama.cpp, LM Studio) don't even target it, and
AMD's own NPU tooling tops out around 4B parameters; the GPU *can* run a 7B–14B Qwen model via
Vulkan at read-a-paragraph speed (~13–20 tok/s on AC power), and this now works out of the box on
recent ollama/llama.cpp without ROCm. Nothing needed for that is installed on this machine (verified
below): no ollama, no ROCm, no AMD Software/Adrenalin package, no ONNX DirectML runtime. WSL2 (where
millwright, `mw`, and every Claude Code session actually run) is a real handicap: DirectML — and
therefore the NPU path and one whole class of GPU acceleration — does not work from inside WSL2 at
all, only from Windows natively; only the Vulkan-via-`/dev/dxg` GPU path and (as of very recently)
ROCm work from WSL2, and neither is installed. The realistic first move, if the Governor wants one,
is a single research/prototype session that installs ollama in WSL2, pulls one ~5 GB Qwen model, and
benchmarks it against three or four real Clerk-grade prompts drawn from this factory's own artifacts
— not general wiring work, and not anything touching the doctor or the Millhand charter.

## 1. Hardware

| Item | Value | Source |
|---|---|---|
| Model | Lenovo ThinkPad P14s Gen 6 AMD (Copilot+) | `hosts/laptop-inventory.md` §1 (vault, surveyed 2026-09-18) |
| CPU | AMD Ryzen AI 9 HX PRO 370, 12 cores / 24 threads, 2.0 GHz base | `powershell.exe Get-CimInstance Win32_Processor` (run today, 2026-09-23) |
| RAM | 64 GB physical; WSL2 is capped to 47 GiB by `.wslconfig` | `(Get-CimInstance Win32_PhysicalMemory | Measure-Object Capacity -Sum).Sum` = 68719476736 bytes (today); `free -h` inside WSL2 (today) = 47Gi |
| GPU | AMD Radeon 890M (integrated only — **no discrete GPU**), reported 4 GB "dedicated" adapter RAM, driver 32.0.22024.19001 | `Get-CimInstance Win32_VideoController` (run today, 2026-09-23) |
| NPU | "NPU Compute Accelerator Device", PCI `VEN_1022&DEV_17F0` (AMD), driver 32.0.203.329, status OK | `Get-PnpDevice \| Where FriendlyName -like '*NPU*'` and `Get-PnpDeviceProperty` (run today, 2026-09-23) |
| NPU spec | XDNA 2, up to 50 TOPS (INT8); AMD's combined "up to 80 TOPS" figure is CPU+iGPU+NPU together, not the NPU alone | AMD's published Ryzen AI 9 HX 370 specs, cross-checked across cpu-monkey.com and acemagic.com, 2026-09 |
| WSL2 sees the GPU? | Yes, as `/dev/dxg` (the paravirtualized DirectX device) — **no** `/dev/dri` (no native DRM render node) | `ls -la /dev/dxg` (present), `ls /dev/dri` (does not exist), `ls /usr/lib/wsl/lib` → `libd3d12.so`, `libd3d12core.so`, `libdxcore.so` (run today) |
| WSL2 sees the NPU? | No device node for it inside WSL2 was found; nothing under `/dev` names it, and no NPU-specific library ships in `/usr/lib/wsl/lib` | `ls /usr/lib/wsl/lib` (today) — only the D3D12/dxcore triplet above |
| Already installed for local inference? | **Nothing.** No ollama, no ROCm packages, no AMD Software/Adrenalin suite, no `/dev/dri`-capable Vulkan path | `which ollama` → not found; `dpkg -l \| grep -iE 'rocm\|cuda\|ollama'` → empty; two separate `Get-ItemProperty` sweeps of both Windows uninstall registry hives for `AMD\|Radeon\|Adrenalin` → empty (run today) |

The important shape here: this is a UMA (unified-memory) laptop APU, not a discrete-GPU machine. There
is no separate VRAM pool to size a model against — the ceiling is a slice of the 64 GB system RAM
(minus whatever WSL2's `.wslconfig` and Windows itself are holding), and the wall you hit first is
memory bandwidth (see §3), not memory capacity.

## 2. Runtimes

**GPU, from WSL2 (where millwright actually runs):**
- `/dev/dxg` is present, so the D3D12 paravirtualization path exists, but the only Vulkan ICD
  installed (`radeon_icd.json`, mesa's native RADV driver) needs a real `/dev/dri` render node,
  which WSL2 does not expose — confirmed by `cat` of the ICD file plus the missing `/dev/dri`
  above. The driver WSL2 actually needs for GPU-accelerated Vulkan without a DRM device is Mesa's
  **Dozen (`dzn`)**, a Vulkan-on-D3D12 translation layer Microsoft merged into Mesa specifically for
  WSLg (Phoronix, "'Dozen' Merged Into Mesa For Implementing Vulkan On Direct3D 12"; `microsoft/wslg`
  issue #1340, "Status and Roadmap for Vulkan 1.3 Compliance in D3D12-based Dzn Driver for WSLg"). No
  `dzn` ICD is installed on this box today (only `radeon_icd`/`lvp_icd`/etc. from the stock
  `mesa-vulkan-drivers` apt package) — it would need to be added (a WSL-specific Mesa build, or
  `GALLIUM_DRIVER=d3d12` wiring) before Vulkan-accelerated inference works from WSL2 on this GPU.
  A working case for the *same class* of problem is documented for an Intel iGPU on WSL2 via dzn +
  `/dev/dxg` (`ggml-org/llama.cpp` discussion #26729, "Vulkan on Intel iGPU inside WSL2 works via
  Mesa dzn — works, at 2/3 of native"), and the dzn driver is vendor-agnostic, so the AMD case should
  work the same way once the right Mesa build is in place — **not verified on this exact GPU**.
- **ROCm** now officially supports this exact chip family on WSL2, very recently: "ROCm 7.2.1
  introduces support for Ryzen APUs, specifically including Ryzen Strix and Strix Halo processors"
  (this laptop's Ryzen AI 9 HX PRO 370 is Strix Point), requiring "AMD Software: Adrenalin Edition
  26.1.1 for WSL2" (AMD ROCm docs, `rocm.docs.amd.com/projects/radeon-ryzen`, read 2026-09-23). That
  Adrenalin package is **not installed** on this laptop (checked both Windows uninstall registry
  hives today, empty) — only the bare Lenovo/Windows Update graphics driver is present. ROCm-on-WSL
  is documented as reaching only "beta" maturity as recently as ROCm 6.1.3 and matured through the
  7.x line — young, but real for this exact chip as of this quarter.
- **Native Windows** (outside WSL2) is the well-trodden path: ollama and llama.cpp both run there
  directly against the AMD Vulkan driver with no translation layer needed, and an unofficial
  ROCm/HIP-enabled Windows ollama build already exists by name for this exact GPU
  (`CubeLink-oss/ollama-windows-amd-radeon-890m-gfx1150-rocm`, GitHub). This sidesteps every WSL2
  wrinkle above, at the cost of the model server living outside the Linux environment mw/Claude Code
  run in (a network hop to `localhost` either way, since WSL2 and Windows share a loopback).
- **Ollama's own support matrix**, independent of the above: as of the 0.30.x era ollama's official
  build supports gfx1150/gfx1151 (this GPU) directly under ROCm, and separately ships an
  **experimental Vulkan backend** (introduced 0.12.6-rc0) specifically to cover AMD/Intel GPUs where
  ROCm isn't set up — this is likely the easiest path on Linux/WSL2 whenever the dzn gap above is
  closed, since it doesn't need ROCm or ICD wrangling in the way RADV does — the Vulkan backend
  question is really "does dzn work for AMD in WSL2," not "does ollama support this GPU."

**NPU, today:**
- **DirectML does not work from WSL2 at all.** Multiple independent reports confirm `DmlExecutionProvider`
  will not load inside WSL2 "because DirectML is not exposed to the Linux kernel" — and this matches
  what's on disk here: `/usr/lib/wsl/lib` carries only `libd3d12*.so`/`libdxcore.so` (the raw D3D12
  surface), no DirectML runtime library. Since AMD's own NPU execution path (Windows ML / Ryzen AI
  Software / the Vitis AI EP) runs on top of DirectML/WinML, **the NPU is reachable only from native
  Windows, never from WSL2**, at least as things stand today.
- On native Windows, Microsoft's **Windows AI Foundry / Foundry Local** is the current story:
  it auto-detects hardware and picks an execution provider (QNN for Qualcomm NPUs, WinML/DirectML
  for any DX12 GPU, CPU fallback), and AMD has a matching first-party article, "Windows Local AI: AI
  model deployment using Windows ML on AMD NPU" (amd.com, 2026), describing NPU deployment via
  Windows ML. **Model sizes it targets are small**: AMD's own Ryzen AI Software model table lists
  Phi-3-mini-128k and similar; a 2026 survey states plainly that "NPU-optimized models on compatible
  systems top out around 4 billion parameters, including Llama-3B, Phi4-mini, and Qwen3-4B," and that
  "as of mid-2026, popular LLM runtimes like Ollama, llama.cpp, and LM Studio route LLM workloads to
  the GPU rather than the NPU, while the NPU handles video upscaling and image classification."
- One purpose-built exception exists: **FastFlowLM** (`ROCm/FastFlowLM` on GitHub, now under AMD's
  ROCm GitHub org), a runtime written specifically to drive AMD's XDNA NPU for LLM inference, claims
  up to ~80 tok/s on small models and reports running GPT-OSS-20B at ~19 tok/s on the NPU alone. It
  is Windows-native; a community Docker wrapper for Linux exists (`hpenedones/fastflowlm-docker`) but
  is unofficial and unverified here. This is real but niche — not integrated with ollama/llama.cpp,
  and its Linux/WSL2 story is not production-grade.

**Plain answer to "is the NPU worth anything for a 7B+ chat model today": no.** Every source agrees
the NPU's practical ceiling for mainstream tooling is roughly 4B parameters, and no path from WSL2 to
the NPU exists at all (DirectML is Windows-only). The GPU is the only viable accelerator for a
7B–14B-class Qwen model, and even that needs either a WSL2 Mesa/dzn addition or ROCm's very recent
Strix-on-WSL2 support (which itself needs an Adrenalin package this machine doesn't have) — or
running the model server on native Windows instead of inside WSL2.

## 3. Models

The chip's own governing constraint, stated directly by a benchmark run on the *identical* CPU/iGPU
(Framework 13, Ryzen AI 9 HX 370 / Radeon 890M) via llama.cpp+Vulkan: "memory bandwidth determines
performance — not compute capacity," on a 128-bit DDR5 bus with a theoretical 89.6 GB/s ceiling
(`msf.github.io`, local LLM performance blog post, fetched 2026-09-23). Measured numbers from that
post, same GPU family as this laptop:

| Model (Q4_K_M) | Size | Prompt processing | Token generation |
|---|---|---|---|
| Qwen3-8B | 4.68 GiB | 146–322 tok/s (battery → AC) | **9.9–13.4 tok/s** |
| GPT-OSS-20B (MoE) | 11.27 GiB | 234–390 tok/s | 17.4–23.4 tok/s (MoE only activates a fraction of weights per token, so it decodes faster than its size implies) |

Using that post's own rule (tok/s ≈ bandwidth ÷ bytes read per token, i.e. roughly bandwidth ÷ model
size for a dense model) as a back-of-envelope scale, calibrated against the 8B number above (~13
tok/s on AC): a dense **14B** Q4 model (~8 GB) should land near **8–11 tok/s**, and a dense **32B**
Q4 model (~18–20 GB) near **4–6 tok/s** — all comfortably inside this laptop's 47 GiB WSL2 RAM
budget on capacity alone; speed, not capacity, is what narrows the choice. This laptop's own memory
bandwidth wasn't independently measured (would need `mbw`/STREAM inside WSL2, not run — read-only),
so treat these as same-chip-family estimates, not measurements of this exact machine.

**Sizing against the two task classes the story asks about:**
- **Clerk-grade checks** (does a diff carry an attribution trailer, does it contain an obviously
  risky string, summarize one Landed mail, draft one ledger line) are short, well-specified,
  low-reasoning tasks with small outputs. **Qwen3-8B or Qwen2.5-7B-Instruct at Q4** is a reasonable
  fit: ~10–13 tok/s means a 200-token verdict lands in 15–20 seconds, and the task doesn't demand
  deep reasoning — it demands narrow, literal pattern-following, which the Mayor should still spot-check.
- **Diagnosis** (reading a doctor log / failed-probe reasons and proposing what's wrong) asks for
  more world-knowledge and multi-step reasoning than a Clerk check. A **14B** model is a sensible
  middle ground (~8–11 tok/s, still interactive); a **32B** model would judge better at the cost of
  a much longer wait (~5 tok/s) — tolerable for something invoked rarely and offline, where the
  alternative is "nobody looks at it until the Millhand can," not "the Mayor is blocked."

No model was downloaded to confirm these numbers directly on this hardware — they are read across
from a same-CPU-family published benchmark, per the ticket's no-download rule.

## 4. Wiring

**Claude Code itself already supports pointing at a different endpoint**, with no millwright code
involved: `ANTHROPIC_BASE_URL` redirects all of Claude Code's own API traffic, `ANTHROPIC_AUTH_TOKEN`
carries the (dummy, for a local server) key, and `ANTHROPIC_MODEL` / `ANTHROPIC_SMALL_FAST_MODEL`
remap the `sonnet`/`opus`/`haiku` names Claude Code hardcodes internally. A local Qwen model doesn't
speak the Anthropic Messages API natively, so a translating proxy sits in between — **LiteLLM** is
the standard choice (`docs.litellm.ai`, "Claude Code (CLI)" page), forwarding to ollama's own local
API. Caution for whoever picks this up: Anthropic's own docs flag that LiteLLM PyPI versions
1.82.7–1.82.8 shipped credential-stealing malware (`BerriAI/litellm#24518`) — pin a version outside
that range and never install it unattended.

This is **entirely a Claude Code / environment-variable concern, not a millwright port**. Look at
`application/harness.go`: `Harness` turns a `Launch` (a story, a seat, a host, a worktree, a boot
file) into a `SessionSpec` — it exists to launch a *whole agentic session* that works a story end to
end. Pointing that whole session's model at a local Qwen server would mean setting
`ANTHROPIC_BASE_URL`/`ANTHROPIC_AUTH_TOKEN` in the environment `infrastructure/claude/claude.go`'s
`Session()` builds (it already builds a `--settings` JSON blob and an env for the `claude` command,
per `SessionSettings` in that file) — no second `Harness` implementation needed for that case, since
it is still the same `claude` binary, just told to talk to a different server. The smallest change
there is a couple of new fields on `Launch`/env, gated behind a `Path` flag, not a new adapter.

**But that's not the shape the factory's actual candidate tasks need.** A Clerk-grade check or an
offline diagnosis is a single narrow question with a short answer — not a multi-turn coding session
that needs tool use, file edits, or `bd` access. Standing up a full Claude Code session (with its
own settings, permission modes, and startup cost) to ask "does this diff have a Co-Authored-By line"
is the wrong tool even before considering fuel: **a single HTTP POST to ollama's local API
(`localhost:11434/api/generate` or its OpenAI-compatible `/v1/chat/completions`) from a small Go
helper or a one-line `curl`, with a fixed prompt template, is the actually-smallest change** — no
`Harness`, no `SessionSpec`, no new adapter, no Claude Code process at all for this narrow case. This
only becomes a `Harness`-relevant question if some future task needs the model driving tools/shell
across multiple turns, which is a materially bigger and riskier thing to wire (see §5).

## 5. Which factory tasks fit, ranked

| Task | Fuel saved | Risk | Verdict |
|---|---|---|---|
| Mayor's Clerk-grade checks (diff attribution scan, mail summary, ledger line draft) | Real, per-use: each one today is either the Mayor's own tokens or a subagent call. Narrow, high-frequency, cheap to try. | Low — read-only, the Mayor still reviews the output before acting on it; a wrong answer costs a re-check, not a bad commit. | **Best fit.** Smallest wiring (§4), lowest blast radius, easiest to fall back to the Mayor if quality disappoints. |
| Night triage of `Refused:` reports (a first-pass read of why a landing was refused, before a human/Mayor looks) | Real — this is exactly the kind of thing that piles up overnight with nobody watching. | Low-medium — read-only summarization, but a wrong triage could mis-prioritize what gets looked at first; still not destructive. | **Second.** Same wiring as Clerk checks (§4); worth trying once Clerk checks are proven. |
| Doctor's offline diagnosis | Would matter most exactly when it's needed (no internet, no Millhand) — but mw-i80dx's own design (Q4, Governor-approved 2026-09-23) is explicit: **"the doctor never calls AI, mail or push itself."** Any local-model diagnosis is not the doctor calling AI — it would have to be a separate, human- or Millhand-invoked tool that reads the doctor's log and proposes a cure **by name only** from a fixed list, never free shell. | Highest of the three — it exists specifically for the moment things are already broken and nobody is watching; a bad suggestion executed unattended is the failure mode the Governor's own words in mw-6ww.40 called out ("would act with shell rights unattended"). | **Third, and only as a constrained, name-only suggestion tool — never wired into the doctor itself, never given shell.** Matches mw-6ww.40's own Mayor recommendation to revisit only if the doctor meets an offline fault it can't cure. |
| Anything that edits product code, runs `bd` writes, or drives multi-step tool use | — | High — no path evaluated here gives a local 7B–32B Qwen model the judgment this factory currently trusts to a full Claude Code session; the whole reason Claude models are used for building is quality of multi-step reasoning and tool use that a laptop-class local model does not match. | **Must not go local.** Nothing here proposes changing this. |

## 6. Recommendation

**Not yet — one small research/prototype session, not wiring, is the next step if the Governor wants
to keep exploring.** The honest state today: nothing is installed, the WSL2 GPU path has a real gap
(no `dzn` Vulkan driver, ROCm-on-WSL needs an Adrenalin package that isn't here), and no one has
measured accuracy on this factory's actual Clerk-grade prompts — only speed, from a same-chip-family
benchmark. Three stories, each sized to one session, each strictly bounded, for the Governor to
approve or refuse individually (do not bundle: each should stand or fall on its own):

1. **Install ollama + one Qwen model, benchmark against real Clerk-grade prompts, write a doc.**
   Rig: millwright (or a scratch space — no product code touched). Files: a new
   `docs/research/local-model-benchmark.md`; the story installs ollama in WSL2 (`curl -fsSL
   https://ollama.com/install.sh | sh`) and pulls one model (`ollama pull qwen3:8b`, ~5 GB) — both
   commands need the Governor's own go-ahead per this ticket's >1 GB download rule, so the story
   should ask again at that step rather than assume standing approval. Acceptance: a table of at
   least four real prompts (drawn from an actual diff, an actual Landed mail, an actual ledger
   entry) with the model's answer, a human verdict (right/wrong/close), and measured tokens/sec on
   this exact machine — replacing the estimates in §3 with real numbers.
2. **If (1) shows Clerk-grade quality is good enough: a narrow Go helper that shells one fixed
   prompt to ollama's local HTTP API and returns a structured verdict**, used only as a first-pass
   filter the Mayor still reviews — never autonomous. Files: one new small package under
   `infrastructure/` (not `application/Harness` — see §4), a unit test against a fake local HTTP
   server, no change to any existing use case. Acceptance: the helper correctly flags a
   known-attribution-carrying diff and a known-clean one, with a timeout and a "server unreachable"
   error path that degrades to "skip the local check" rather than blocking anything.
3. **Hold, don't file yet: a name-only diagnosis suggestion tool for the doctor's uncured faults.**
   Not a story until mw-i80dx's doctor has actually met an offline fault more than once that it
   cannot cure and the Millhand also could not reach — a real occurrence, not a hypothetical one. It
   would also need its own explicit line in the Millhand's charter (Governor-approved, per how
   mw-i80dx.5's escalation note is already charter-gated) before any code, and would never be given
   shell — only a choice from the doctor's own fixed cure-name list.

**Reopen condition if the answer is "not yet" on all three**: reopen when either (a) the doctor
(mw-i80dx epic) logs a real offline fault it could not cure and the Millhand could not reach either
— the exact scenario mw-6ww.40 was written from — or (b) the Mayor's Clerk/subagent fuel spend on
read-only checks becomes large enough that a half-day prototype clearly pays for itself, or (c) ROCm
or Vulkan support for this exact WSL2/GPU combination matures past what's documented today (§2), or
this laptop is upgraded to have a real gap to close (an Adrenalin install, closing the ROCm-on-WSL
gap named above, would itself be a small standing-alone story if the Governor ever wants item 1
without downloading a second time).

## Gaps / couldn't verify

- No model was downloaded, so §3's tokens/sec are read across from a same-CPU-family published
  benchmark (Framework 13, identical Ryzen AI 9 HX 370 / Radeon 890M), not measured on this exact
  laptop. This laptop's actual RAM bandwidth was not independently measured.
- Whether Mesa's `dzn` Vulkan driver actually works for this AMD iGPU under WSL2 specifically was
  not confirmed — the working case found (`llama.cpp` discussion #26729) is for an Intel iGPU; `dzn`
  is described as vendor-agnostic but the AMD case wasn't found demonstrated anywhere.
- Whether ollama's new experimental Vulkan backend (0.12.6-rc0+) works through `dzn` inside WSL2, as
  opposed to natively on Linux with a real `/dev/dri` node, was not directly confirmed.
- FastFlowLM's Linux/WSL2 support (`hpenedones/fastflowlm-docker`) is a community wrapper, not
  official — its actual functionality on this hardware is unverified.
- Accuracy/quality of any Qwen size on this factory's real Clerk-grade prompts is entirely unmeasured
  — §3 and §5 reason from published speed numbers and general model-capability discussion, not from
  running anything.
