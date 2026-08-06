# SPEC.md — **clauditor**: a tmux/worktree-native fleet manager for Claude Code sessions

> **How to use this file:** create an empty git repo on the Ubuntu dev box (not the Mac — you want real `claude`, `tmux`, and git repos available), save this file as `SPEC.md` in the repo root, run `claude`, and say:
> *"Read SPEC.md in full. Then begin Phase 0 in plan mode. Work milestone by milestone. Commit at every milestone with a conventional-commit message. Do not skip the recon phase."*
>
> The name: **clauditor** = Claude + auditor — it doesn't do the work, it watches the work and flags who's blocked. Binary and CLI: `clauditor`.

---

## 1. Mission

Build a small, self-hosted system that gives one developer real-time visibility and control over **many Claude Code sessions running across multiple git repos and multiple git worktrees on a single Ubuntu server**, accessible from:

1. the server itself (CLI + TUI inside tmux),
2. a Mac over SSH (notifications), and
3. a phone or any browser through an existing **Cloudflare Tunnel + Cloudflare Access** setup (WebUI).

The core insight this build rests on: **Claude Code now ships the hard parts natively.** The `claude agents` background-session system (research preview, requires Claude Code **v2.1.139+**) provides a per-user supervisor daemon, durable background sessions, authoritative per-session state, and a machine-readable interface: `claude agents --json`. Do **not** rebuild session persistence, status detection via hook injection, or process supervision. Clauditor is a thin, honest layer that *aggregates, correlates, notifies, and exposes* — it composes `claude agents --json` + `tmux` + `git worktree` into one model.

**Deliverables, in order:**
- **Tier 0** — `clauditor notify`: a state-change notifier streamable over SSH to a Mac.
- **Tier 1** — `clauditor serve`: a daemon with a JSON/SSE API, an embedded mobile-first WebUI, action endpoints (dispatch/stop/respawn/open-in-tmux), plus `clauditor status` (CLI table) and a minimal `clauditor tui`.
- **Tier 2** — a written roadmap (`docs/ROADMAP.md`) for full interactive terminals in the browser. Design interfaces so T2 slots in without rework, but write no T2 terminal code now.

---

## 2. Environment & constraints (assume these; verify with `clauditor doctor`)

- Ubuntu server (systemd available), accessed via SSH from a Mac. SSH already traverses Cloudflare Access via `ProxyCommand cloudflared access ssh` in `~/.ssh/config` — clauditor never needs to care; it's transparent to plain `ssh`.
- `tmux` ≥ 3.x installed and in daily use. Dev servers, log tails, and interactive Claude sessions live in tmux.
- `claude` (Claude Code) installed for the login user, **v2.1.139+** (some features used need v2.1.212+; `clauditor doctor` must check and report both thresholds).
- Multiple git repos on disk, including an "umbrella" workspace directory containing several repos, and multiple **linked git worktrees** per repo (some mapped to wildcard dev subdomains via Traefik — see `links.worktree_url_template` in config).
- Cloudflare Tunnel (`cloudflared`) and Cloudflare Access (Google Workspace SSO) already exist. Clauditor binds to **127.0.0.1 only**; cloudflared fronts it. Clauditor must validate the Access JWT itself (defense in depth).
- Single user. No multi-tenancy. No Windows. No macOS server support (the Mac is a client only).
- The machine holds sensitive credentials (cloud keys, secret-manager service accounts). Treat every mutating endpoint as if it were `rm -rf` — see §9.

---

## 3. Phase 0 — Recon (mandatory; produces `docs/RESEARCH.md`)

Clone these four repos into `./references/` (treat as **read-only**; never edit them; add `references/` to `.gitignore`):

| Repo | What to extract |
|---|---|
| `https://github.com/craftzdog/tmux-claude-session-manager` | The reference implementation for consuming `claude agents --json` with **no hooks**. Study: exact invocation and JSON parsing; how agents map to tmux windows/panes; how "needs attention" sorts to top; the live preview; the `$CLAUDE_PICKER` reload trick. This is the closest prior art to clauditor's collector. |
| `https://github.com/nielsgroen/claude-tmux` | Rust TUI (ratatui) managing Claude inside tmux. Study: the tmux command-wrapper design; **pane-based detection of interactive Claude sessions** (panes running `claude` that the supervisor doesn't list); live preview via `capture-pane` with ANSI; worktree + `gh` PR integration. |
| `https://github.com/formkit/dmux` | Node/tmux tool pairing worktrees with panes. Study: worktree lifecycle (create → branch naming → merge back → cleanup), lifecycle **hooks**, project isolation (one tmux session per project), how prompts are fed to agents at launch. |
| `https://github.com/asheshgoplani/agent-deck` | The richest TUI in this space (groups, search, fork, cost tracking, and a **phone-controlled "conductor"** — the closest prior art to clauditor's WebUI). Study its information architecture, keybindings, and any TUI↔remote protocol. |

`docs/RESEARCH.md` must contain:
1. A **"steal / adapt / avoid"** table per repo, with file/line pointers into `references/`.
2. A **license table** (read each LICENSE file). Rule: port code only from MIT/Apache-2.0/BSD sources, with attribution collected in a top-level `NOTICE` file. From anything else (or anything unlicensed), take *ideas only*, re-expressed from scratch.
3. A short "what clauditor deliberately does NOT rebuild" section naming agent-deck's TUI richness and Claude Code's supervisor as things we lean on rather than clone.
4. Answers to the **empirical questions** in §14 (run them on this machine; if a sandbox prevents it, list them in `docs/VERIFY.md` for the human to run and mark the answers `UNVERIFIED`).

**Milestone M0 acceptance:** `docs/RESEARCH.md` + `NOTICE` committed; empirical questions answered or logged in `docs/VERIFY.md`.

---

## 4. Language, layout, and toolchain

- **Go 1.23+**, no cgo, single static binary named `clauditor` with subcommands: `serve`, `notify`, `status`, `tui`, `dispatch`, `doctor`, `version`. Rationale: one artifact, trivial systemd deployment, `go:embed` for the WebUI, strong stdlib HTTP/SSE.
- Suggested deps (substitutes fine, justify in ARCHITECTURE.md): `charmbracelet/bubbletea|bubbles|lipgloss` (TUI), `golang-jwt/jwt/v5` + a JWKS helper (Access JWT), `fsnotify/fsnotify` (optional push-trigger), `BurntSushi/toml` (config). Vendor nothing at runtime from the network.
- Repo layout:

```
clauditor/
  cmd/clauditor/main.go
  internal/
    collect/        # collectors: claudejson.go, tmux.go, gitwt.go (+ testdata/ fixtures)
    model/          # unified types + correlation logic
    store/          # in-memory snapshot, version counter, event bus
    api/            # HTTP handlers, SSE, Access-JWT middleware
    actions/        # dispatch, stop, respawn, open-in-tmux, reply strategies
    notify/         # tier-0 engine (also used by `serve` for webhooks later)
    tui/            # bubbletea app (minimal — see §11)
    doctor/
  web/              # embedded WebUI (see §10)
  scripts/          # mac-notify.sh, capture-fixtures.sh
  deploy/           # systemd/, cloudflared/, docker-compose.example.yml, ACCESS.md
  docs/             # RESEARCH.md, ARCHITECTURE.md, ROADMAP.md, VERIFY.md, REPLY.md, DEMOLOG.md
  test/stubbin/     # fake `claude` and `tmux` executables for tests
  Makefile  NOTICE  README.md  SPEC.md
```

- Quality bar: `golangci-lint` clean; table-driven unit tests for every parser; `make build test lint` green; structured logging via `log/slog`; `--log-level` flag; version stamped via `-ldflags`.

---

## 5. Data sources (collectors) — exact contracts

All collectors run behind interfaces so tests can substitute `test/stubbin/`. All external commands get contexts with timeouts (default 5s) and are executed with explicit `Dir` where relevant.

### 5.1 Claude supervisor collector (`internal/collect/claudejson.go`)
- Poll `claude agents --json` (default every **5s**, jittered ±20%). Optionally also `--all` on a slower cadence (30s) to pick up recently completed sessions.
- **Version-tolerant parsing is a hard requirement**: the feature is a research preview and the schema moves. Unknown fields ignored; every field optional; a parse failure of one array element must not drop the whole snapshot. Keep the schema in ONE file with fixtures.
- Fields to map (verified against docs as of 2026-08-06; re-verify in Phase 0): `cwd`, `kind` (`interactive`|`background`), `startedAt` (Unix ms) always; `id` (short id for attach/logs/stop), `state` (`working`|`blocked`|`done`|`failed`|`stopped`) on background sessions; `pid`, `status` while the process is alive; `waitingFor` when waiting (`permission prompt`, `input needed`, `sandbox request`, `worker request`, `dialog open`); `sessionId` (full UUID, usable with `claude --resume`) and `name` when set.
- On-demand only (never on the poll loop): `claude logs <id>` for a session's recent output, truncated server-side to a configurable byte cap.
- **Optional accelerator:** fsnotify-watch `~/.claude/jobs/*/state.json` and `~/.claude/daemon/roster.json` purely as a *trigger to re-poll immediately*. Never parse these files as a source of truth (documented location, undocumented stability). Honor `CLAUDE_CONFIG_DIR` if set.

### 5.2 tmux collector (`internal/collect/tmux.go`)
- `tmux list-panes -a -F '#{session_name}\t#{window_index}\t#{pane_index}\t#{pane_id}\t#{pane_pid}\t#{pane_current_command}\t#{pane_current_path}\t#{window_name}\t#{session_attached}'` every 10s.
- Detect **interactive** Claude sessions: walk each pane's pid subtree (`ps -e -o pid=,ppid=,command=` parsed once per cycle) looking for a `claude` process. These are sessions the supervisor may not list (a `claude` started in a terminal is tied to that terminal until backgrounded). Emit them as `kind=tmux-interactive`, `state=unknown`.
- Preview on demand: `tmux capture-pane -p -e -t <pane_id> -S -40` (the `-e` keeps ANSI; strip or keep per client).
- **Heuristic state inference from captured pane text (e.g., regexes for permission prompts) is allowed only as a clearly-labeled fallback**, quarantined in one file (`tmux_heuristics.go`), off by default (`tmux.heuristics = false`). Claude Code's TUI changes; heuristics rot. Never let a heuristic override supervisor-reported state.
- If `tmux` isn't running, the collector returns an empty set, not an error.

### 5.3 Git/worktree collector (`internal/collect/gitwt.go`)
- Repo discovery: union of config `repos = [...]` (explicit paths) and `workspace_dirs = [...]` scanned to depth 2 for `.git` (dir or file — a `.git` *file* means linked worktree; resolve to its main repo and dedupe).
- Per repo, every 20s: `git -C <repo> worktree list --porcelain` → worktrees with path, HEAD, branch.
- Per worktree, gated by config `git.dirty_check` (default true) with a 2s per-repo timeout (skip & mark `dirty=unknown` on timeout): `git -C <wt> --no-optional-locks status --porcelain -unormal` (only need "empty or not"). Ahead/behind counts via `git rev-list --left-right --count @{upstream}...HEAD` behind `git.ahead_behind` (default false).
- Note and surface (read-only) worktrees under `<repo>/.claude/worktrees/` — these are created by Claude Code's own background-session isolation. Label them `managed_by=claude-code`. **Clauditor never deletes these**; deletion semantics belong to `claude rm`/agent view (which can remove a session's worktree — warn about this in the UI, don't wrap it).

### 5.4 Correlation (`internal/model`)
Unified model:

```
Repo { name, path, worktrees[] }
Worktree { path, branch, head, dirty (true|false|unknown), managedBy (user|claude-code), url? , sessions[] }
Session {
  key,                       // stable synthetic key
  kind: supervisor-bg | supervisor-interactive | tmux-interactive,
  id?, sessionId?, name, state, waitingFor?, startedAt, ageSeconds,
  cwd, repo?, worktree?, pid?,
  tmuxTarget?                // "session:window.pane" when the session is visible in a pane
}
```

Correlation rules: session `cwd` prefix-matches a worktree path → bind to that worktree; else prefix-matches a repo → bind to repo root; else goes in a synthetic "(loose)" group. A supervisor session that is *attached* in a tmux pane will also be found by the tmux scanner — **dedupe** (match by pid subtree containing the supervisor pid, or by cwd+command match) and keep one Session with `tmuxTarget` filled. Every snapshot carries a monotonically increasing `version` and `generatedAt`.

---

## 6. Tier 0 — `clauditor notify` (Milestone M1)

A standalone subcommand using only the collectors (no HTTP server).

- Modes: `--stream` (long-running; emits an event per state transition) and `--once` (single diff against a state file under `~/.local/state/clauditor/`, for cron).
- Events (diff between consecutive snapshots): `needs_input` (any → blocked, or waitingFor appears), `completed` (working → done), `failed` (any → failed), `session_new`, `session_gone`, `interactive_claude_appeared` (tmux scanner). Debounce flapping (no duplicate event for the same session+type within 30s).
- Output formats: `--format text` (one human line: `[needs_input] stables-core-app/feat-kms · "kms rotation" · waiting: permission prompt · 4m`), `--format json` (one JSON object per line: `{event, session:{...}, ts}`), `--exec CMD` (run CMD per event with `CLAUDITOR_EVENT`, `CLAUDITOR_SESSION_NAME`, `CLAUDITOR_REPO`, `CLAUDITOR_WAITING_FOR`, `CLAUDITOR_STATE`, `CLAUDITOR_ID` in env).
- **Mac client:** `scripts/mac-notify.sh` — a loop with backoff that runs `ssh -o ServerAliveInterval=30 -o ServerAliveCountMax=3 <host> 'clauditor notify --stream --format json'`, parses with `jq`, and posts macOS notifications via `terminal-notifier` if present else `osascript -e 'display notification ...'`. Must survive laptop sleep (reconnect loop), must not stack duplicate instances (pidfile), and needs zero config beyond `CLAUDITOR_HOST`. Works unchanged through a `ProxyCommand cloudflared access ssh` SSH config.

**M1 acceptance:** unit tests drive the differ with fixture snapshots; `test/stubbin/claude` (a shell script that prints fixture JSON and records its argv to a log) lets `clauditor notify --once` run in CI with the stub prepended to PATH; `scripts/mac-notify.sh` passes `shellcheck`; demo commands recorded in `docs/DEMOLOG.md`.

---

## 7. Tier 1 — the daemon: `clauditor serve` (Milestones M2–M5)

### 7.1 Store & events (M2)
In-memory current snapshot + event bus. No database. Optional `--snapshot-file` writing the latest snapshot as JSON atomically (rename) for debugging. `clauditor status` renders the snapshot as a grouped table (repo → worktree → sessions) with state glyphs; `--json` flag prints the raw snapshot. **M2 acceptance:** `clauditor status` correct against stub fixtures covering: 2 repos, 3 worktrees, supervisor sessions in all five states, one tmux-interactive session, one loose session, one dedupe case.

### 7.2 HTTP API (M3)
Bind `127.0.0.1:<port>` (default 8790). All JSON. Consistent error envelope `{error:{code,message}}`.

Read:
- `GET /api/v1/state` → full snapshot.
- `GET /api/v1/sessions/{key}` → one session.
- `GET /api/v1/sessions/{key}/logs?lines=200` → `claude logs` output (supervisor sessions) or `capture-pane` (tmux sessions), text/plain, capped.
- `GET /api/v1/events` → SSE. Send `event: snapshot` (full snapshot, throttled to ≤1/s), heartbeat comment every 15s, `Last-Event-ID` ignored (snapshots are self-contained).
- `GET /healthz` (no auth) → `{ok, version, collectors:{claude,tmux,git}: lastSuccessAgoSeconds}`.

Actions (see §9 for gating):
- `POST /api/v1/dispatch` `{target: {cwd} | {repo, worktree} | {repo, newWorktree:{branch, base?}}, prompt, name?, model?, agent?}` → runs `claude --bg [--name N] [--model M] [--agent A] "prompt"` with `Dir` set to the resolved path. For `newWorktree`: `git worktree add -b <branch> <base_dir>/<slug> <base|HEAD>` first (worktree base dir from config, default `<repo>/../<repo>-worktrees/`), then dispatch **inside** it — Claude Code detects it is already in a linked worktree and skips creating its own. Never pass `--dangerously-skip-permissions` or `--permission-mode bypassPermissions`; reject requests containing them.
- `POST /api/v1/sessions/{key}/stop` → `claude stop <id>`.
- `POST /api/v1/sessions/{key}/respawn` → `claude respawn <id>`.
- `POST /api/v1/sessions/{key}/open-in-tmux` → ensure tmux session `clauditor` exists (`tmux new-session -d -s clauditor` if not), then `tmux new-window -t clauditor -n <shortname> -c <cwd> "claude attach <id>"`; respond with the tmux target so the UI can say "attached in `clauditor:<n>` — `tmux attach -t clauditor`".
- `POST /api/v1/sessions/{key}/reply` `{text}` → **experimental**, see §8; returns 501 with a helpful message unless `actions.experimental_reply = true`.
- Deliberately absent in T1: `claude rm`, worktree deletion, merge. (Roadmap items; deletion has worktree-removal side effects that deserve a human at a real terminal.)

**M3 acceptance:** curl transcript in `docs/DEMOLOG.md`; middleware tests (§9) green; action handlers tested against stubbins asserting exact argv and working directory.

### 7.3 WebUI (M4 read-only, M5 actions)
See §10.

### 7.4 Reply experiment report (M5)
See §8; produces `docs/REPLY.md`.

---

## 8. The reply problem (investigate honestly, then pick)

There is **no documented CLI** to send a reply to a background session without attaching (documented shell surface: `agents`, `attach`, `logs`, `stop`, `respawn`, `rm`, `daemon status`). The docs do say an undeliverable reply is queued and delivered when the session's process next starts — which hints at plumbing, but nothing public. Prototype in this order and write up findings in `docs/REPLY.md`:

1. **tmux injection (expected default if anything ships):** in the hidden `clauditor` tmux session, `new-window "claude attach <id>"`, wait for readiness (poll `capture-pane` for the prompt footer; timeout 15s), `tmux send-keys -t <target> -l <text>` then `Enter`, verify the transcript advanced via `claude logs`, then detach/kill the window. Handle: numbered-choice questions (send just the digit), permission prompts (send-keys the visible choice — or refuse and direct the human to attach, configurable), fullscreen rendering timing. Ship behind `actions.experimental_reply = true`, default **false**.
2. **Daemon socket spelunking (research only):** inspect `~/.claude/daemon/` and `claude daemon status` output for a local socket/protocol. If one exists, document it in REPLY.md but do **not** build on it in T1 — undocumented internals of a research preview. If used later, isolate behind the same interface with tmux injection as fallback.
3. **Fallback that always works (ship regardless):** `open-in-tmux` — the human replies in a real terminal. The WebUI's "Reply" button, when experimental reply is off, becomes "Open in tmux" plus copy-paste instructions.

---

## 9. Security requirements (non-negotiable)

- **Bind loopback only.** Refuse to start if configured to bind a non-loopback address without `--i-know-this-is-exposed`.
- **Cloudflare Access JWT validation** in middleware on every `/api/*` route (and the UI shell): read `Cf-Access-Jwt-Assertion` header (fallback: `CF_Authorization` cookie); validate signature against JWKS fetched from `https://<team_domain>/cdn-cgi/access/certs` (cache with refresh, tolerate rotation), `iss` == team domain, `aud` contains configured `policy_aud`, `exp`/`iat` sane. Config keys: `access.team_domain`, `access.policy_aud`. Log the authenticated email claim at `info` on mutating requests.
- **Dev bypass** only via explicit `--dev-insecure-local` AND remote addr is loopback; log a loud warning every 60s while active.
- **Mutating requests**: additionally require header `X-Clauditor-Action: 1` (blunts CSRF risk from ambient Access cookies), enforce `Content-Type: application/json`, per-route rate limit (e.g., 10/min), and global config gate `actions.enabled = true` (default **false** — first deploy is read-only).
- Never log prompt bodies at `info` (they may contain secrets); `debug` only, and say so in README.
- Never invoke `claude` with permission-bypass flags; strip/deny-list them in dispatch.
- Systemd hardening in the unit file: `NoNewPrivileges=yes`, `PrivateTmp=yes` (see caveat in §12 about needing the user's tmux socket and `~/.claude` — this must run as the login user, so use a **user service**, not a locked-down system service).
- `deploy/ACCESS.md`: step-by-step for adding hostname `clauditor.<domain>` to the existing tunnel's ingress → `http://127.0.0.1:8790`, creating the Access self-hosted app (existing Google IdP), and finding the AUD tag for config.

---

## 10. WebUI (the differentiator — this is the phone view)

Constraints: **no runtime CDN dependencies** (everything vendored + `go:embed`), no heavyweight build step (plain ES modules; vendored `preact` + `htm` as static files is acceptable, or vanilla JS — builder's choice, justify in ARCHITECTURE.md). Mobile-first, dark theme default, must be pleasant on a ~390px viewport.

**M4 (read-only):**
- **Fleet board**: sections in priority order — *Needs input* (always on top), *Working*, *Idle/Interactive*, *Done/Failed/Stopped (collapsed)*. Within sections, group rows by repo → worktree. Each session row: state chip, name, one-line context (`repo · branch`), age, `waitingFor` text when blocked, tmux badge when it has a pane.
- Header counters: needs-input / working / done — tap to filter (this is craftzdog's & VelaTerm's best idea; keep it).
- **Session drawer** (tap a row): metadata, last N log lines (fetched on open, manual refresh button — no log polling), copy buttons for `claude attach <id>`, `ssh <host> -t tmux attach -t clauditor`, and `sessionId` for `claude --resume`.
- Worktree rows show branch, dirty dot, `managed_by` tag, and an optional external link built from `links.worktree_url_template` (e.g. `https://{branch}.dev.example.com`) — supports `{branch}` and `{slug}` placeholders.
- Live via SSE with auto-reconnect + "stale since Xs" banner when disconnected.

**M5 (actions, rendered only when `actions.enabled`):**
- Dispatch sheet: repo picker → worktree picker (or "new worktree": branch name + base), prompt textarea, optional name/model. Submit → toast with the new session once it appears in a snapshot.
- Per-session: Stop (confirm), Respawn, Open-in-tmux, Reply (experimental gate per §8; otherwise shows the open-in-tmux path).
- A visible count of currently-working sessions near the dispatch button (quota awareness — parallel sessions burn subscription quota linearly).

**Explicit UI non-goals for T1:** no terminal emulator, no file browser, no diff viewer, no transcript search. Leave obvious seams (routes, component slots) for T2's xterm.js pane.

---

## 11. TUI (`clauditor tui`) — deliberately minimal (M6)

agent-deck already exists and is good; **do not rebuild it**. Clauditor's TUI is a thin fleet view for when you're already SSH'd in:

- One bubbletea screen: the same grouped list as the WebUI board; `/` filter; `s` cycle state filter; `enter` = open-in-tmux (new window in the `clauditor` tmux session) — if running *inside* tmux, also switch to it (`tmux switch-client -t <target>`); `l` = logs peek (pager overlay); `d` = dispatch (prompt input, target = highlighted repo/worktree); `x` = stop (confirm); `q` quit.
- It talks to the local daemon over HTTP if `serve` is running (loopback, no auth needed via `--dev-insecure-local`-equivalent local socket or a `local_token` file in the state dir — pick one, document it), else falls back to running collectors in-process.
- **M6 acceptance:** usable over a 200ms-latency SSH link (no per-keystroke network round trips), renders correctly in a 100×30 tmux pane, and README contains an honest "when to use clauditor tui vs agent-deck" paragraph.

---

## 12. Deployment (`deploy/`, M7)

- **systemd user service** (`deploy/systemd/clauditor.service` + README steps): `systemctl --user enable --now clauditor`, plus `loginctl enable-linger <user>` so it survives logout. Rationale (document it): clauditor must run *as the login user* to share `~/.claude` (credentials/supervisor), the user's tmux server socket, and repo permissions; and env for dispatched sessions flows sanely. `Environment=PATH=...` must include wherever `claude` lives.
- `deploy/cloudflared/ingress-snippet.yml` and `deploy/ACCESS.md` per §9.
- `deploy/docker-compose.example.yml` **as a documented anti-recommendation**: show what it would take (mounting `~/.claude`, the tmux socket, every repo path, matching UID) and state plainly that host systemd is the supported path. Clauditor differs from typical stack services here because its entire job is touching host-user state.
- `clauditor doctor`: checks `claude` present + `--version` ≥ 2.1.139 (warn < 2.1.212 with the list of degraded features), `claude agents --json` executes and parses, supervisor reachable (`claude daemon status`), `tmux -V` ≥ 3.0, `git --version` ≥ 2.20, config parses, repos/workspace dirs exist and are git repos, Access JWKS fetchable (when serve config present), and prints a PASS/WARN/FAIL table.

**M7 acceptance:** fresh-clone quickstart in README verified end-to-end (`make build`, `clauditor doctor`, `clauditor notify --once`, `clauditor serve` + curl, user-service install), `docs/ROADMAP.md` complete per §13, `docs/VERIFY.md` lists anything not runnable in the build environment.

---

## 13. Tier 2 roadmap (`docs/ROADMAP.md` — write the doc, not the code)

Must cover, with enough specificity that a future session could start implementing from it:

1. **Interactive terminal in the browser**: Go pty (`creack/pty`) spawning `claude attach <id>` or `tmux attach -t <target>`, WebSocket transport, xterm.js (vendored) front end; resize protocol; reconnect with server-side replay buffer (ring buffer per pty, replay on reconnect); idle timeout and hard cap on concurrent ptys.
2. **The multi-client sizing problem**: document VelaTerm's owner/mirror model and tmux's native answer (`aggressive-resize`, per-client sessions via grouped sessions); pick a policy (recommended: one pty per browser client attached to tmux, let tmux mediate).
3. **Security escalation**: an attached terminal is shell-equivalent access (Claude Code's `!` runs bash). Requirements: separate Access policy/AUD for the terminal route, per-connection re-auth, audit log line per pty open/close, kill-switch config.
4. **First-class reply** if/when Claude Code exposes one (upgrade path from §8's interface).
5. **Transcript search**: investigate reading `~/.claude/projects/**` JSONL transcripts read-only; index with SQLite FTS5; cite VelaTerm's global-search UX as the bar.
6. **Usage/cost panel**: investigate available surfaces (in-app `/usage` has no documented CLI twin; OTel metrics env vars exist) — list options with effort estimates.
7. **Worktree merge-back helper** (dmux-inspired): explicit-confirmation merge flow, never auto-commit user work, conflicts left in place; plus safe worktree deletion (refuse when dirty or unpushed without `--force`).
8. Mobile input key bar (Esc/Tab/Ctrl/arrows) for the terminal view.

---

## 14. Empirical questions to answer during Phase 0 (on this machine)

Record answers with command transcripts in `docs/RESEARCH.md` (or `docs/VERIFY.md` if not runnable here):

1. Does `claude agents --json` list the **interactive** session it's invoked next to, or any interactive sessions at all (`kind: "interactive"`), or strictly background ones? (Docs imply both kinds exist in the schema, but agent view hides interactive sessions in other terminals until backgrounded.) This decides how much the tmux scanner must carry.
2. Exact JSON shape on THIS installed version: capture real output (redact prompts) into `internal/collect/testdata/agents_v<version>.json` via `scripts/capture-fixtures.sh`.
3. Does `claude logs <id>` support a lines/tail flag on this version, or must clauditor truncate?
4. Is there a PR field in the JSON (agent view shows PR badges), or is PR linkage invisible to `--json`? UI shows PR badges only if cheaply available; otherwise roadmap it.
5. Does `claude attach` inside a scripted tmux window render usably for send-keys injection (§8 experiment)?
6. Where do this machine's repos/worktrees live (populate a real `config.example.toml`)?
7. Licenses of the four reference repos.

---

## 15. Configuration (`~/.config/clauditor/config.toml`)

```toml
# clauditor config — all keys shown with defaults where they exist

repos = ["/home/user/code/stables-core-app"]        # explicit repo roots
workspace_dirs = ["/home/user/code"]                 # scanned depth<=2 for repos

[poll]
claude_seconds = 5
tmux_seconds   = 10
git_seconds    = 20

[git]
dirty_check  = true
ahead_behind = false

[tmux]
heuristics = false            # pane-text state guessing (fallback only)

[serve]
listen = "127.0.0.1:8790"
snapshot_file = ""            # optional debug snapshot path

[access]                       # Cloudflare Access JWT validation
team_domain = "yourteam.cloudflareaccess.com"
policy_aud  = ""               # AUD tag from the Access app

[actions]
enabled = false                # first deploy is read-only
experimental_reply = false

[dispatch]
worktree_base = ""             # default: <repo>/../<repo>-worktrees

[links]
worktree_url_template = ""     # e.g. "https://{branch}.dev.example.com"

[notify]
debounce_seconds = 30
```

`clauditor` loads config from `--config`, else `$XDG_CONFIG_HOME/clauditor/config.toml`, else `~/.config/clauditor/config.toml`. Ship `config.example.toml` populated from Phase-0 findings.

---

## 16. Testing strategy

- **Stub binaries** in `test/stubbin/`: `claude` and `tmux` as shell scripts that (a) print fixtures selected by argv, (b) append their argv + cwd to a call log the tests assert on. Tests prepend `test/stubbin` to PATH. This makes notify, collectors, and every action handler testable in CI with no real claude/tmux.
- **Fixtures**: real captured outputs (redacted) under `internal/collect/testdata/`, one file per source per version; parser tests are table-driven over all fixture versions; add a deliberately-mangled fixture to prove tolerance (unknown fields, missing fields, one corrupt array element).
- **Differ tests** (notify): snapshot pairs → expected event lists, including debounce.
- **Middleware tests**: forged/expired/wrong-aud JWTs rejected; valid accepted (generate a test JWKS + signer in-test); mutating route without `X-Clauditor-Action` rejected; `actions.enabled=false` returns 403 with a clear message.
- **Correlation tests**: the M2 fixture scenario (§7.1).
- `make test` runs everything; no test touches the network.

---

## 17. Working agreements for the build

1. **Plan mode at each phase boundary** (0→1→2 within this spec's milestones); present the plan, then execute.
2. **Commit per milestone** minimum (conventional commits: `feat(collect): ...`); keep commits reviewable.
3. Maintain `docs/DEMOLOG.md`: after each milestone, the exact commands a human can run to see it work.
4. When a fork in the road appears (e.g., reply strategy results, WebUI vanilla vs preact), write the options + recommendation in the relevant doc, pick one, and proceed — don't stall.
5. Anything you could not verify in this environment goes in `docs/VERIFY.md` as a checklist for the human, with exact commands.
6. Treat `references/` as read-only; respect the §3 license rule; attribute ports in `NOTICE`.
7. Never weaken §9. Never add a permission-bypass path "for convenience."
8. If `claude agents` behavior contradicts this spec (research preview drift), trust the machine, update the fixtures, and note the divergence in ARCHITECTURE.md.

---

## Appendix A — Verified facts about Claude Code you may rely on (as of 2026-08-06; re-verify in Phase 0)

- Agent view / background sessions: research preview, **requires v2.1.139+**; some behaviors (e.g., `/fork` → background copy, `/resume` picker in agent view) need **v2.1.212+**.
- Shell surface: `claude agents [--json] [--all] [--cwd <path>]`, `claude attach <id>`, `claude logs <id>`, `claude stop <id>`, `claude respawn <id> | --all`, `claude rm <id>`, `claude daemon status`, `claude daemon stop --any [--keep-workers]`, `claude --bg "prompt"` with `--name`, `--agent`, `--model`, and `--exec` for PTY-backed shell jobs (shell-job output is memory-only and cleaned ~5 min after exit).
- `--json` states: `working | blocked | done | failed | stopped`; `waitingFor`: `permission prompt | input needed | sandbox request | worker request | dialog open`; `kind`: `interactive | background`; `sessionId` works with `claude --resume`.
- Supervisor: per-user daemon; sessions survive terminal close and machine sleep (reconnect on wake); machine shutdown stops running sessions (they show failed; attach/peek/reply restarts them from where they left off); idle unattached sessions are stopped after ~1h and restarted on demand; state under `~/.claude/jobs/<id>/state.json`, roster at `~/.claude/daemon/roster.json`, log at `~/.claude/daemon.log`; `CLAUDE_CONFIG_DIR` relocates all of it.
- Worktree isolation: background sessions auto-move into `.claude/worktrees/` before editing; **skipped when the session is already inside a linked worktree** (clauditor's `newWorktree` dispatch exploits this); disable per-repo via `.claude/settings.json` → `{"worktree": {"bgIsolation": "none"}}`. Deleting sessions (agent view / `claude rm`) can remove Claude-created worktrees — clauditor stays away from `rm` in T1.
- Interactive sessions in other terminals are not shown in agent view until backgrounded (`/bg`, `←`) — the basis for clauditor's tmux scanner. (Whether they appear in `--json` output is Phase-0 question #1.)
- Quota: background sessions consume subscription usage like interactive ones; N parallel ≈ N× burn. Surface working-count in the UI.
- Docs to consult when uncertain: `https://code.claude.com/docs/en/agent-view`, `.../worktrees`, `.../cli-reference`, index at `https://code.claude.com/docs/llms.txt`.

## Appendix B — Reference repos

- `https://github.com/craftzdog/tmux-claude-session-manager`
- `https://github.com/nielsgroen/claude-tmux`
- `https://github.com/formkit/dmux`
- `https://github.com/asheshgoplani/agent-deck`

*End of SPEC.md*
