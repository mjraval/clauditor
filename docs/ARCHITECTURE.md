# ARCHITECTURE.md

## Shape

One static Go binary, seven subcommands, three layers:

```
collectors (internal/collect)  →  model.Correlate  →  store (snapshot+bus)
     claude agents --json                                  │
     tmux list-panes + ps                    ┌─────────────┼─────────────┐
     git worktree list                     notify        api (HTTP)     tui
                                          (differ)     SSE + actions
```

The **snapshot is the product** (lesson from agent-deck, docs/RESEARCH.md):
one flattened, pre-correlated `model.Snapshot` consumed unchanged by
`status`, `notify`, the HTTP API, the SSE stream, the WebUI, and the TUI.
State derivation happens exactly once, in `model.Correlate`.

## Decisions & rationale

**Supervisor is the source of truth.** `claude agents --json` provides
existence + state for every session (interactive AND background — verified
empirically, RESEARCH.md Q1). The tmux scanner only (a) maps sessions to
panes and (b) catches claude processes the supervisor doesn't list. Pane-text
heuristics exist in prior art and rot; clauditor ships none in the state
path (SPEC allows a quarantined fallback; not needed so far — the config
knob `tmux.heuristics` is reserved).

**Dependencies** (SPEC §4 suggested; actual):
- `BurntSushi/toml` — config, as suggested.
- `golang-jwt/jwt/v5` — Access JWT validation. JWKS fetching is ~100 lines
  of stdlib (internal/api/jwks.go) instead of a JWKS helper dep: the
  Cloudflare document is a plain RSA key list, and owning the cache lets us
  implement rotation-tolerance (serve cached key when refetch fails) exactly
  as §9 asks.
- `charmbracelet/bubbletea + lipgloss` — TUI (M6), as suggested.
- **No fsnotify** in T1: the 5s poll is within the product's latency budget
  and one fewer dep; the accelerator slot (§5.1) remains open, trigger-only.

**WebUI: vanilla ES modules, no framework.** The UI is one list + one
drawer + one form. Vendoring preact+htm buys component abstraction we don't
need at this size and adds a third-party payload to security-review; plain
DOM with two render functions is smaller than preact itself. If T2's
terminal view grows real component needs, revisit (xterm.js will be
vendored then anyway). No build step: files under `web/static` are the
artifact, embedded via `go:embed`.

**Interactive-session states.** The supervisor reports `status busy|idle|
waiting` for interactive sessions (no `state` field). Mapping: busy→working,
waiting→blocked, idle→idle. tmux-only sessions are `unknown` — honesty over
guessing.

**Dedupe rule.** A supervisor session attached in a pane is found by both
collectors. Primary match: pane pid subtree contains the session pid
(one `ps` snapshot per cycle). Fallback: suppress tmux-found claudes whose
cwd equals a *live* (pid>0) supervisor session's cwd — dead sessions don't
suppress, so a stopped bg session and a new interactive claude in the same
directory both render.

**Session keys.** Supervisor sessions: `sup-<sessionId>` (stable across
restarts, survives supervisor renames — `name` is mutable display text,
verified in Phase 0). tmux-only: `tmux-<pane>-<pid>` (a new claude in the
same pane is a new session).

**Actions never bypass permissions.** The deny-list covers the top-level
flags AND the `claude agents` dispatch spellings discovered in Phase 0
(`--allow-dangerously-skip-permissions`). Flag-shaped `name/model/agent`
values are rejected so argv can't be smuggled.

**Worktree creation** follows the dmux-derived recipe (RESEARCH.md §2.3):
prune → verify base with `--end-of-options` → path triage (idempotent when
the dir is already a worktree) → branch-exists probe chooses
`worktree add <dir> <branch>` vs `worktree add <dir> -b <branch> [base]`.
Dispatching inside the new worktree makes Claude Code skip its own bg
isolation (Appendix A) — that's the point.

**Poller cadences.** One loop at the claude interval (5s ±20% jitter);
tmux (10s) and git (20s) re-collect within that loop when their interval
has elapsed, and their last results are reused otherwise, so every snapshot
is complete without cross-goroutine merging.

## Cockpit pivot (2026-08-07)

The product repositioned from "aggregation daemon with a minimal TUI" to
"cockpit-first CLI." Three changes, all additive — the daemon, API, WebUI, and
every subcommand still work unchanged:

- **Bare command is the cockpit.** `clauditor` with no arguments launches the
  TUI (the old `cmdTUI` path); `clauditor tui` remains as an alias. A leading
  `-flag` with no subcommand is treated as cockpit flags, not a bad
  subcommand. `serve` is now documented as "advanced" (phone + notifications),
  not the headline. Wiring: `cmd/clauditor/main.go`.

- **Zero-config repo discovery.** When neither `repos` nor `workspace_dirs` is
  configured, the git collector derives repo roots each cycle from the live
  sessions' cwds (`GitCollector.DiscoverReposAuto`): each distinct cwd →
  `git rev-parse --show-toplevel` (failure tolerated = not a repo) →
  `resolveMainRepo` to fold linked worktrees into their main repo → dedupe.
  Used by both `Fleet.Collect` and `store.Poller`'s git tick (shared helper,
  no duplication). This makes `clauditor` correlate sessions to repos with no
  config; configured mode is unchanged (explicit repos/workspace_dirs still
  win and skip cwd derivation).

- **TUI reply is ungated.** The cockpit's inline `r` reply goes straight
  through `actions.Reply` on the in-process path — deliberately NOT gated
  behind `actions.experimental_reply`. Rationale: a user at the physical
  keyboard has the same trust as one who would `open-in-tmux` and type the
  answer; the gate exists to protect the *remote* (daemon/phone) path from
  a not-yet-trusted injection strategy, and that path stays gated (the daemon
  still returns 501 when the flag is off, surfaced verbatim in the cockpit).
  The permission-prompt refusal inside `Reply` still applies to both paths.

Supporting refactor: the ANSI-strip regex, previously private to
`internal/api`, moved to `internal/collect.StripANSI` so the API's logs
endpoint and the TUI's live-preview pane share one definition.

## Divergences from SPEC assumptions (per §17.8)

- `claude agents --json` **does** list interactive sessions (SPEC hedged on
  this). The tmux scanner is therefore a pane-mapper + safety net, not the
  primary interactive discovery path.
- Observed schema has no `waitingFor` on interactive sessions and drops
  `pid`/`status` on terminal states; the parser treats every field as
  optional anyway.
- `claude logs` has no tail flag and emits a raw ANSI screen replay; the
  API strips ANSI server-side and caps at 256 KiB.
- **`PrivateTmp=no` in the systemd unit** (SPEC §9 says `yes`): clauditor's
  job requires two sockets that live in the shared /tmp — the user's tmux
  server socket (`/tmp/tmux-<uid>/`) and Claude Code's daemon socket
  (`/tmp/cc-daemon-<uid>/`). `PrivateTmp=yes` would sever both, breaking
  open-in-tmux, reply, and (on some versions) supervisor access. The unit
  keeps the rest of the hardening (`NoNewPrivileges=yes`) and documents the
  trade-off inline. Systemd's `BindPaths=` could re-expose just those two
  dirs, but the socket dir names embed the uid and (for cc-daemon) a hash,
  making a static unit fragile — revisit if clauditor ever runs multi-user.
- `GET /api/v1/config` exists beyond SPEC §7.2's route list: the WebUI needs
  the action gates and URL template at boot. Auth-gated like every read route
  and exposes no secrets.
- `tmux.heuristics` and the fsnotify accelerator are parsed-but-reserved:
  supervisor state proved sufficient in practice; the config keys keep the
  upgrade path without a breaking change.
