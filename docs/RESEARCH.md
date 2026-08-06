# RESEARCH.md — Phase 0 recon

Studied 2026-08-06. Reference clones live in `references/` (read-only,
gitignored). File:line pointers below are relative to each repo's root at
the commit cloned that day.

## 1. License table

| Repo | License | Copyright (verbatim) | Code porting? |
|---|---|---|---|
| craftzdog/tmux-claude-session-manager | MIT | `Copyright (c) 2026 Takuya Matsuyama` | ✔ with NOTICE attribution |
| nielsgroen/claude-tmux | **AGPL-3.0-only** | `Copyright (C) 2026 Niels Groeneveld` | ✘ — **ideas only**, re-expressed from scratch |
| formkit/dmux | MIT | `Copyright (c) 2025 Justin Schroeder` | ✔ with NOTICE attribution |
| asheshgoplani/agent-deck | MIT | `Copyright (c) 2025 Ashesh Goplani` | ✔ with NOTICE attribution |

Attributions for anything actually ported are collected in `/NOTICE`.

## 2. Steal / adapt / avoid (condensed; full study notes preserved below each)

### 2.1 tmux-claude-session-manager (MIT, 325 lines of bash — closest prior art to our collector)

| Verdict | Technique | Pointer |
|---|---|---|
| STEAL | `claude agents --json` as sole source of truth, zero hooks | `scripts/agents.sh:3-7,20` |
| STEAL | Never identify Claude by `pane_current_command` (macOS reports the parent shell); use **pid→tty→pane** join instead | `scripts/agents.sh:6-7,32-40` |
| STEAL | One batched `tmux list-panes -a -F` with compound format → O(1) subprocesses per cycle | `scripts/agents.sh:34` |
| STEAL | Attention-first ordering: waiting(0) > idle(1) > unknown(2) > busy(3), then recency | `scripts/agents.sh:46-49,60` |
| STEAL | `capture-pane -e -p` ANSI-preserving preview | `scripts/picker.sh:40` |
| STEAL | Last-activity via transcript mtime glob `${CLAUDE_CONFIG_DIR:-~/.claude}/projects/*/<sessionId>.jsonl` (JSON has only `startedAt`) | `scripts/helpers.sh:41-58` |
| STEAL | Supervisor eventual-consistency tolerance: a killed pid lingers in `--json` ~hundreds of ms (`sleep 0.3` before reload) | `scripts/picker.sh:35-37,41` |
| ADAPT | It filters to `kind=="interactive"` only and ignores `name`, `startedAt`, `--all`, `--cwd` — clauditor uses all of them | `scripts/agents.sh:22` |
| ADAPT | Agents outside tmux are dropped — clauditor renders them as detached instead | `scripts/agents.sh:43-44` |
| AVOID | Silent failure everywhere (`2>/dev/null … \|\| exit 0`) — undiagnosable for a daemon | `scripts/agents.sh:20-23` |
| AVOID | Sorting formatted display strings; ANSI baked into data rows | `scripts/agents.sh:57,60,46-49` |
| AVOID | Bare `kill {3}` on a pid from a display column, no pid-reuse guard | `scripts/picker.sh:41` |
| AVOID | Hard dependency on `~/.claude/projects` internals — best-effort enricher only | `scripts/helpers.sh:46-48` |

### 2.2 claude-tmux (AGPL — **ideas only**, nothing ported)

| Verdict | Idea | Pointer |
|---|---|---|
| IDEA✔ | Tab-joined `-F` format + split + arity guard + tolerant parse; "no server running" stderr ⇒ empty set, not error | `src/tmux.rs:16-43,25-32` |
| IDEA✔ | `%pane_id` as the canonical handle — `switch-client -t %42` resolves the whole hierarchy, survives renumbering | `src/session.rs:97-111` |
| IDEA✔ | Git-context-driven action gating (offer Push only when ahead, PR only when upstream+gh) — port the *predicate table* concept to server-computed allowed-actions | `src/app/mod.rs:309-385` |
| IDEA✔ | Partial-failure reporting for composite ops; stderr sniffing for actionable hints | `src/app/mod.rs:539-556`, `src/git/worktree.rs:157-172` |
| AVOID | `pane_current_command.contains("claude")` detection — misses npm/wrapper installs (`node`/`bash`), false-positives on `claude-tmux` itself | `src/tmux.rs:49-52` |
| AVOID | Screen-scraping `❯`/`[y/n]` as primary state (Claude prompts are numbered choices now — already rotted) | `src/detection.rs:7-55` |
| AVOID | Synchronous un-timed subprocesses (incl. network `gh pr view`) on the render path | `src/main.rs:44-62` |
| AVOID | `send-keys` without `-l`/`--` (key-name injection); `delete_worktree(force=true)` inside a merge macro | `src/tmux.rs:242-244`, `src/app/mod.rs:539` |

### 2.3 dmux (MIT — worktree lifecycle authority)

| Verdict | Technique | Pointer |
|---|---|---|
| STEAL | The full `newWorktree` recipe: `worktree prune` → `rev-parse --verify --end-of-options <base>` → path triage (exists+`.git` ⇒ idempotent ok) → `show-ref` branch-exists probe → `worktree add <dir> <branch>` or `worktree add <dir> -b <branch> [<base>]` | `src/utils/paneBootstrapRunner.ts:458-509` |
| STEAL | Branch-name validator as a pure predicate before any git call (`^[a-zA-Z0-9._/-]*$`, no `..`, `@{`, leading `-`, `.lock` segments…) | `src/utils/git.ts:41-69` |
| STEAL | argv exec, never shell strings; prompt via 0600 temp file (or `load-buffer`+`paste-buffer`), never interpolated into command lines | `WorktreeCleanupService.ts:202-236`, `promptStore.ts:35-53` |
| STEAL | Readiness by polling `#{pane_current_command}` with a shell allowlist, not screen scraping | `src/utils/agentPromptDispatch.ts:6-78` |
| STEAL | Kill-and-verify tmux pane before mutating state; refuse worktree deletion while siblings reference it | `closeAction.ts:237-253,267-278` |
| ADAPT | Slug generator (stop-words + scoring, 48-char word-boundary truncation) — deterministic fallback only, LLM path never on the request path | `src/utils/slug.ts:82-198` |
| ADAPT | Hook contract: executable file + env vars, 3-tier resolution — good T2 material | `src/utils/hooks.ts:69-134` |
| AVOID | `execSync("git merge \"…\"")` shell-interpolation; conflict detection by English-prose matching; `process.exit(1)` from action handlers | `utils/mergeExecution.ts:26,36,142-155`, `useWorktreeActions.ts:77-91` |
| AVOID | Auto-editing user `.gitignore`; interactive prompts on the startup path (daemon must never need a TTY) | `index.ts:1141-1159,235-239` |
| AVOID | JSON file + fs-watcher + poller as the state layer (race-patch pattern: `pauseConfigWatcher`, "CRITICAL:" ordering comments) — clauditor's store is a single-writer | `closeAction.ts:219-230` |

### 2.4 agent-deck (MIT — richest TUI; WebUI/security prior art)

| Verdict | Technique | Pointer |
|---|---|---|
| STEAL | **The snapshot is the product**: one flattened, pre-ordered, annotated snapshot type consumed by every surface (TUI/web/CLI) | `internal/web/session_data_service.go:26-56` |
| STEAL | One shared status-derivation function in its own package (their package doc is a post-mortem of drift across 3 surfaces) | `internal/sessionstatus/sessionstatus.go:1-30` |
| STEAL | SSE for state, WebSocket only for the PTY; fingerprint-diff + 15s heartbeat | `server.go:256-257`, `handlers_events.go:16-80` |
| STEAL | Bind/auth posture: refuse non-loopback bind without auth (3-option error), header-only bearer, CSRF fail-closed, fail-safe on unclassifiable hostnames | `internal/web/bind.go:17-59`, `auth.go:9-35`, `csrf.go:23-60` |
| STEAL | Remote-send safety: server-authoritative target allowlist, single-argv exec, rate limit, audit log of `correlationId+target+sha256(text)` never raw text | `handlers_command_center.go:545-616` |
| STEAL | Accessibility: bold for active + underline for error so state survives colorblindness | `internal/ui/home.go:16396-16406` |
| STEAL | Server-side transition diffing → `recentlyCompleted` in snapshot (reconnecting phones don't re-fire toasts) | `handlers_command_center.go:59-125` |
| ADAPT | Web Push + VAPID with focus-presence gating — T2 roadmap, not T1 | `push_service.go:18-25,536,725` |
| ADAPT | `substate` ("Honest Status v2": `auth-401`, `model-unavailable`) — coarse status lies; roadmap once observable | `session_data_service.go:68-73` |
| ADAPT | Global transcript search with jump-or-adopt resolution — T2 item 5 | `internal/ui/global_search.go:39-60` |
| AVOID | 19,724-line `home.go` — the argument for clauditor's minimal TUI | `internal/ui/home.go` |
| AVOID | Empty allowlist = allow everyone (Slack path) — every clauditor allowlist defaults to deny | `conductor_bridge.py:1844-1856` |
| AVOID | Shelling out to your own CLI as RPC; embedded Python bridge; badge-density rows (6 badges don't fit a phone) | `conductor_bridge.py:356-393`, `home.go:16453-16570` |
| AVOID | Hooks-injection status detection as primary — clauditor's premise is the supervisor now ships this natively | `internal/session/claude_hooks.go:40-66` |

## 3. What clauditor deliberately does NOT rebuild

- **Session persistence, supervision, and status detection.** Claude Code's
  supervisor daemon (`claude agents`, v2.1.139+) already provides durable
  background sessions and authoritative `state`/`waitingFor`. agent-deck's
  five-layer hook-injection status stack (~thousands of lines) is what this
  feature made obsolete; clauditor polls `--json` and trusts it.
- **TUI richness.** agent-deck exists, is good, and is MIT. clauditor's TUI
  is a thin fleet view (SPEC §11); anyone wanting groups, fork, cost
  dashboards, MCP management should run agent-deck alongside.
- **Chat-platform bridges.** agent-deck's Telegram/Slack/Discord conductor
  is a second control plane with its own auth model. clauditor's phone story
  is one authenticated origin (Cloudflare Access → WebUI).
- **Worktree merge/cleanup.** dmux does this well; it's deliberately absent
  from T1 (roadmap item 7) because deletion has side effects that deserve a
  human at a real terminal.

## 4. Empirical answers (§14) — verified live on this machine

Environment: Ubuntu (kernel 6.8), Claude Code **2.1.223**, tmux **3.4**,
git **2.54.0**. Full transcripts condensed; fixtures in
`internal/collect/testdata/`.

### Q1 — does `--json` list interactive sessions? **YES.**

`claude agents --json` returned 6 `kind: "interactive"` sessions (every
live claude in tmux panes and terminals, including the one running this
build). Help text confirms: *"Print active sessions (interactive and
background) as a JSON array"*. Interactive entries carry
`pid, cwd, kind, startedAt, sessionId, name, status(idle|busy)` — **no
`state`, no `id`**. Consequence: the tmux scanner's job shrinks to
(a) mapping sessions to panes and (b) catching claude processes the
supervisor misses; it is not the primary discovery path for interactive
sessions.

### Q2 — exact JSON shape on 2.1.223. Captured.

Fixtures: `agents_v2.1.223.json`, `agents_all_v2.1.223.json` (via
`scripts/capture-fixtures.sh`, names redacted), plus
`agents_bg_states_v2.1.223.json` (hand-assembled from live-observed probe
output covering all states). Union of observed keys:

```
pid        number   present while process alive (absent on done/stopped)
id         string   short id — background sessions only
cwd        string   always
kind       string   "interactive" | "background"
startedAt  number   Unix ms, always
sessionId  string   full UUID, always
name       string   auto-generated summary or --name value; RENAMED by the
                    supervisor after first turn (observed: prompt text →
                    topic summary) — treat as mutable display text, not identity
status     string   "idle" | "busy" | "waiting" — while process alive; null/absent after
state      string   background only: working|blocked|done|failed|stopped
waitingFor string   background only, while blocked: e.g. "input needed";
                    observed to clear to null once input consumed even
                    while state stays "blocked" at an open prompt
```

Live-observed transitions during the probe: `working/busy` →
`blocked/waiting/waitingFor:"input needed"` → (reply delivered) →
`blocked/idle/waitingFor:null` → (claude stop) → `stopped`, pid/status
gone. A finished session: `done`, `status:"idle"` while attached, then
status/pid absent.

### Q3 — `claude logs` tail flag? **NO.**

`claude logs --help`: `Usage: claude logs <id>` — no flags at all. Output
is a **raw ANSI terminal replay** (full-screen escape sequences, cursor
addressing), not clean text. Clauditor must cap bytes server-side and
strip/interpret ANSI per client.

### Q4 — PR field in `--json`? **NO.**

Observed key union (Q2) contains nothing PR-related. PR badges go to the
roadmap (derive from `gh pr view` on demand, T2).

### Q5 — does scripted `claude attach` + send-keys work? **YES.**

Full transcript in `docs/REPLY.md`. Summary: attach in a detached tmux
window rendered a numbered-choice prompt within ~8s; `send-keys -l "1"` +
`Enter` delivered the answer; transcript advanced ("User answered Claude's
questions: … → Blue"). Delivery must be verified by transcript/pane
advancement, **not** by state flip (state stays `blocked` at the next open
prompt). Experimental-reply ships gated off by default.

### Q6 — this machine's repos/worktrees. Mapped.

Umbrella workspace `~/projects/monorepo` (~12 repos: stables-core-app,
stables-core-web, stables-ops, …) plus loose repos under `~/projects`
(clauditor, stables-planning, stables.xyz). Worktrees: main checkouts only
at recon time (each repo one worktree, various feature branches).
`config.example.toml` populated accordingly (`workspace_dirs` =
`~/projects` + `~/projects/monorepo`, depth-2 scan).

Bonus finding: `pane_current_command` **is** `claude` for native installs
here (12 panes scanned, 5 claude) — but one repo had a claude session whose
pane showed `bash` (nested under a shell), confirming the pid-subtree walk
is still required. The supervisor pid from `--json` is the join value.

### Q7 — licenses. See §1.

### Extra findings recorded for later tiers

- Daemon internals (research only, **not** built on — see `docs/REPLY.md`):
  transient supervisor, `control.sock` under `/tmp/cc-daemon-$UID/<hash>/`,
  `~/.claude/daemon/{auth/,control.key,dispatch/,roster.json}`,
  `~/.claude/jobs/<id>/state.json` with rich fields (`needs`, `block`,
  `tokens`, `intent`, …). Fragile research-preview internals.
- `claude agents` also accepts `--dangerously-skip-permissions` /
  `--allow-dangerously-skip-permissions` for *dispatched* sessions —
  clauditor's dispatch deny-list must cover these too, not just the
  top-level flags.
- The supervisor renames sessions after the first turn (prompt → topic
  summary). `name` is display text; identity is `sessionId`.
