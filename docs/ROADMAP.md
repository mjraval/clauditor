# ROADMAP.md — Tier 2

Tier 1 (M1–M7) is the honest, shippable aggregation layer: notify, serve,
status, tui, doctor. Tier 2 is everything SPEC §13 explicitly deferred —
written up here with enough specificity that a future session can start
implementing directly, without rediscovering the seams. Nothing in this
document is implemented; T1 was built to leave room for it (see the
"designed seam" note under each item) but ships no T2 code.

Route table referenced below is the real one, as of M5, in
`internal/api/server.go`'s `Handler()`:

| Method | Path | Handler | Line |
|---|---|---|---|
| GET | `/healthz` | `handleHealthz` (unauthenticated) | server.go:81 |
| GET | `/api/v1/state` | `handleState` | server.go:86 |
| GET | `/api/v1/config` | `handleConfig` | server.go:87 |
| GET | `/api/v1/sessions/{key}` | `handleSession` | server.go:88 |
| GET | `/api/v1/sessions/{key}/logs` | `handleLogs` | server.go:89 |
| GET | `/api/v1/events` | `handleEvents` (SSE) | server.go:90 |
| POST | `/api/v1/dispatch` | `handleDispatch` (guarded) | server.go:92 |
| POST | `/api/v1/sessions/{key}/stop` | `handleStop` (guarded) | server.go:93 |
| POST | `/api/v1/sessions/{key}/respawn` | `handleRespawn` (guarded) | server.go:94 |
| POST | `/api/v1/sessions/{key}/open-in-tmux` | `handleOpenInTmux` (guarded) | server.go:95 |
| POST | `/api/v1/sessions/{key}/reply` | `handleReply` (guarded, 501 unless `actions.experimental_reply`) | server.go:96 |

"guarded" = wrapped in `s.Auth.Auth(...)` (Access JWT, `internal/api/middleware.go:40`)
and `NewMutatingGuard(...).Wrap(...)` (`actions.enabled`, `X-Clauditor-Action`
header, content-type, per-route rate limit — `internal/api/middleware.go:104-145`).

---

## 1. Interactive terminal in the browser

**New route:** `GET /api/v1/sessions/{key}/terminal` — a WebSocket upgrade,
slotted in next to the existing `GET /api/v1/sessions/{key}/logs` route
(server.go:89) since both operate on a resolved session key. It needs its
own entry in `Handler()`, not a repurposed existing route.

**Server side:** Go `creack/pty` spawns either `claude attach <id>` (for a
supervisor background session) or `tmux attach -t <target>` (for a session
that already has a `tmuxTarget`, per `model.Session.TmuxTarget`) inside a
pty, and bridges pty stdout/stdin over the WebSocket. Reuse
`internal/actions`' `Runner`/`ClaudeBin`/`TmuxBin` fields (`internal/actions/actions.go:17-19`)
for the spawn — don't hand-roll a second "how do we invoke claude/tmux"
path.

**Wire protocol:** binary frames for terminal I/O (raw bytes both
directions); a small JSON control frame for resize, e.g.
`{"type":"resize","cols":n,"rows":n}` sent by the client on viewport
change, matching xterm.js's `onResize` callback shape.

**Front end:** vendored xterm.js (static files under `web/`, consistent
with SPEC §10's "no runtime CDN dependencies" constraint that already
governs the M4 WebUI) plus the xterm.js WebSocket addon or a hand-rolled
equivalent (~50 lines).

**Reconnect + replay:** each open pty gets a per-pty ring buffer (fixed
byte cap, e.g. 64KB) of recent output on the server. On reconnect (same
session key, new WebSocket), replay the ring buffer before resuming live
streaming, so a flaky phone connection doesn't lose context. This is the
same shape VelaTerm and `nielsgroen/claude-tmux`'s `capture-pane -S -N`
scrollback-on-attach solve, just server-buffered instead of re-querying
tmux each time.

**Limits:** idle timeout (no client bytes and no pty output for N
minutes → close, matching the supervisor's own ~1h idle-session semantics
in spirit) and a hard cap on concurrent open ptys (config key, e.g.
`terminal.max_concurrent = 4`) — a browser tab left open must not become an
unbounded fleet of live shells.

**Designed seam left in T1:** SPEC §10 explicitly calls this out as a
"leave obvious seams" item — the WebUI's session drawer already has a slot
where a "Reply"/"Open in tmux" button lives; a terminal button drops in
next to it without restructuring the drawer.

## 2. The multi-client sizing problem

Two browser tabs (or a phone + a laptop) attached to the same session at
different viewport sizes can't both dictate the pty's terminal size — a
classic multi-client tty problem.

**VelaTerm's owner/mirror model** (cited per SPEC §13.2): one client is
the "owner" whose resize events actually resize the pty; every other
client is a read-only "mirror" that receives output but can't resize or
type. Simple to implement, but demotes every non-first client to
view-only, which is a real loss for "reply from my phone while the laptop
tab is also open."

**tmux's native answer:** `aggressive-resize` (per-window, sizes to the
smallest attached client showing that window) and grouped sessions (each
client gets its own *session* sharing one set of windows, so tmux itself
mediates size via its own attached-client negotiation instead of clauditor
reinventing it).

**Recommendation: one pty per browser client, each attached to tmux** (not
one shared pty fanned out to N WebSockets). Concretely: `terminal` route
opens `tmux new-session -t <target> -d` — actually, `tmux attach -t
<target>` run inside a *grouped* session (`tmux new-session -t <target>`
without `-d`, per client) — each browser client gets its own tmux client
identity, tmux handles size negotiation the way it already does for two
humans attached to one session from two real terminals, and clauditor's
pty-bridging code stays dumb (byte pipe + resize passthrough) instead of
tracking client rosters and picking a winner. This also means clauditor
gets tmux's existing behavior for free instead of re-implementing
owner/mirror semantics badly.

## 3. Security escalation

An attached terminal is shell-equivalent access — Claude Code's `!` runs
bash, and a tmux-attached pane is a real shell regardless. This is a
different risk tier than every other T1 action (dispatch/stop/respawn
are constrained to specific argv shapes; a terminal is unconstrained).

Requirements before shipping item 1:
- **Separate Access policy + AUD** for the terminal route specifically —
  do not reuse the same `policy_aud` as the rest of `/api/v1/*`. A second
  Cloudflare Access application (`deploy/ACCESS.md`'s flow, repeated) with
  its own AUD, checked by a second `AuthConfig`/`policy_aud` pair
  (`internal/api/middleware.go:20-37`'s `AuthConfig` struct already takes
  `TeamDomain`/`PolicyAUD` as fields — a second instance costs nothing
  structurally).
- **Per-connection re-auth**: WebSockets don't carry the Access cookie the
  same way subsequent fetches do reliably across all browsers/proxies;
  validate the Access JWT again at WebSocket-upgrade time (not just at the
  initial page load), and consider a short re-auth window (e.g. re-check
  every 15 minutes on long-lived sessions) rather than trusting the
  connection indefinitely.
- **Audit log**: one structured log line per pty open and close (session
  key, authenticated email from `api.EmailFrom(ctx)` — `internal/api/middleware.go:98`,
  timestamp, duration on close). This is strictly more than the existing
  `slog.Info("action", ...)` line mutating routes get (`internal/api/middleware.go:142`)
  because a terminal session's duration and close reason matter, not just
  that it was opened.
- **Kill-switch config**: `terminal.enabled = false` default, independent
  of `actions.enabled` — a fleet operator should be able to turn off
  shell-equivalent access without turning off dispatch/stop/respawn, and
  vice versa.

## 4. First-class reply (upgrade path)

Today's reply path is `internal/actions/reply.go`'s `func (a *Actions)
Reply(ctx context.Context, sessionID, text string) error` (reply.go:18) —
tmux-window injection: attach in a hidden window, poll for the prompt
footer, classify the screen (permission/numbered-choice/free-text via
`classifyScreen`, reply.go:120), `send-keys`, verify transcript
advancement by screen length, always tear the window down. It's gated
behind `actions.experimental_reply` (`internal/config/config.go:54`,
checked at `internal/api/handlers.go:193` returning 501 when off) — see
`docs/REPLY.md` for the full investigation and why this shipped as the
default strategy (no documented CLI alternative existed as of Claude Code
2.1.223).

**Upgrade path, if/when Claude Code ships a documented reply primitive**
(a `claude reply <id> <text>` subcommand, or a stable local socket
protocol under `~/.claude/daemon/` — REPLY.md's "research only" section
flags this as unverified and explicitly not built on for T1): implement it
as a second, preferred strategy behind the *same* `Actions.Reply` method
signature. Concretely:
- Keep `Reply(ctx, sessionID, text) error` as the interface `Actions`
  exposes to `handleReply` — callers (the HTTP handler, the TUI's `d`
  dispatch-adjacent reply flow) never need to change.
- Internally, try the documented primitive first; fall back to today's
  tmux-injection implementation when it's unavailable (older Claude Code
  version, primitive not present) rather than a hard cutover — this keeps
  clauditor working across the version spread real users will have.
- Retire tmux-injection as the default (but keep it as the fallback) once
  the documented primitive has been live long enough to trust; downgrade
  `actions.experimental_reply`'s meaning from "use reply at all" to "use
  the tmux-injection fallback even when the primitive is unavailable" or
  similar — write the exact migration in `docs/REPLY.md` when this
  triggers, don't silently change the config key's meaning.

## 5. Transcript search

Claude Code writes per-project transcripts as JSONL under
`~/.claude/projects/**` (documented location, same caveat as
`~/.claude/jobs/*/state.json` in SPEC §5.1 — treat as a trigger/read
surface, not a stable schema to build core functionality on without a
tolerant parser). Investigate:
- Read-only access: tail/scan the JSONL files matching sessions clauditor
  already knows about (correlate by `sessionId`, the field
  `internal/collect/claudejson.go:20` already captures for `--resume`).
- Index with SQLite FTS5 (stdlib-adjacent: `mattn/go-sqlite3` needs cgo,
  which SPEC §4 rules out for the main binary — either accept a cgo build
  for this feature specifically as a documented exception, or evaluate a
  pure-Go FTS-capable alternative, e.g. `modernc.org/sqlite`; write the
  trade-off in ARCHITECTURE.md when this is picked up).
- UX bar: VelaTerm's global search (cited in SPEC §13.5) — a single search
  box that returns matching sessions/transcripts across the whole fleet,
  not per-session grep.

## 6. Usage/cost panel

No documented CLI twin for the in-app `/usage` view as of Claude Code
2.1.223 (SPEC §13.6's framing holds — re-verify on whatever version is
current when this is picked up). Options, roughly S/M/L effort:
- **S — OTel env vars only:** Claude Code exposes OTel metrics env vars
  (Appendix A doesn't enumerate them; re-check `code.claude.com/docs`
  when implementing). If a metrics endpoint is exposed locally, scrape it
  and surface raw counters (token/request counts) with no cost math — cheap,
  but not "cost" in dollars.
  - Effort: S (a few hours) — one more polled/scraped source alongside the
    existing three collectors.
- **M — transcript token fields:** if the JSONL transcripts (item 5) carry
  per-turn token counts, sum them per session/day and multiply by a
  configurable $/token table the user maintains by hand (pricing changes,
  don't hardcode it).
  - Effort: M (a day or two) — depends on item 5's parser existing first.
- **L — full attribution:** per-repo/per-worktree cost breakdown,
  historical trends, budget alerts. Needs a real datastore (SPEC's
  "no database" constraint, §7.1, would need revisiting for this one
  feature) — punt until there's a concrete need, not speculatively.
  - Effort: L (multi-day, plus a design doc for the storage question
    alone).

Recommendation: start at S when picked up, upgrade to M once item 5 exists;
don't build L without a concrete pain point driving it.


**Survey update (2026-08-09, lunemis/mux teardown):** mux proves the
cheap path — per-turn `message.usage` token fields (input/output/cache_read/
cache_creation) sit in the same transcript JSONL `internal/transcript`
already parses for previews. Token counts are effectively free; only the
pricing table needs care (version it, date it, surface staleness — the
agent-deck lesson). Effort estimate drops accordingly.

## 7. Worktree merge-back helper (dmux-inspired)

Explicit-confirmation merge flow: given a worktree clauditor already knows
about (`model.Worktree`, `internal/model/model.go`), offer "merge branch
`<branch>` back into `<base>`" as an action that:
- **Never auto-commits** user work — if the worktree is dirty, refuse and
  say so; this is a hard rule, not a default, mirroring
  `internal/actions/dispatch.go`'s existing "never pass permission-bypass
  flags" hard-deny pattern (`checkDenied`, actions.go:44) — same
  posture, different rule.
- Leaves conflicts in place on merge failure — no auto-abort, no
  auto-resolve; report the conflict and point at the worktree path for the
  human to resolve normally with `git`.
- Pairs with **safe worktree deletion**: refuse when dirty, and refuse
  when the branch has unpushed commits, unless an explicit `--force` (or a
  UI-equivalent "type the branch name to confirm") is passed — deliberately
  higher friction than dispatch/stop, because deletion is destructive and
  T1 already stays away from `claude rm` for the same reason (SPEC §5.3,
  §7.2's "deliberately absent in T1" list).

## 8. Mobile input key bar for the terminal view

Once item 1 exists: a fixed row of buttons above the mobile keyboard for
keys that don't have a natural mobile-soft-keyboard equivalent — `Esc`,
`Tab`, `Ctrl` (as a sticky modifier for the next keystroke, since mobile
keyboards have no physical Ctrl to hold), and arrow keys (for shell
history / tmux copy-mode navigation). This is UI-only on top of item 1's
WebSocket transport — each button just synthesizes the corresponding
byte(s)/escape sequence into the same binary frame the real keyboard
input uses; no new server-side protocol needed.
