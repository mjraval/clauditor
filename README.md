# clauditor

**The cockpit for your Claude Code fleet.** Run `clauditor` — no config, no
setup — and you get one screen showing every Claude Code session across all
your repos and worktrees: who's blocked, who's working, who's done. Attach to
any of them, reply to a blocked one, or dispatch a new one, without leaving
the view.

clauditor is Claude + auditor: it doesn't do the work, it watches the work and
flags who's blocked. It's a thin, honest aggregation layer over three things
that already exist and already do the hard part: Claude Code's own
background-session supervisor (`claude agents --json`), tmux, and git
worktrees. It polls all three, correlates sessions to repos/worktrees/tmux
panes into one snapshot, and gives you visibility and light control over a
fleet of Claude Code sessions on one Ubuntu box — from the box itself, from a
Mac over SSH, or (optionally) from a phone. It deliberately does not rebuild
session persistence, status detection, or process supervision — Claude Code's
supervisor already does that.

## Quickstart

```
make setup     # one-time: Go toolchain, golangci-lint, shellcheck into ~/.local
make build     # -> ./bin/clauditor
./bin/clauditor    # launch the cockpit — that's it, no config required
```

Running `clauditor` with no arguments launches the cockpit. It finds your
sessions from the supervisor, then figures out which repo each one lives in
by reading the live sessions' working directories — so it correlates
sessions to repos and worktrees with **zero configuration**. (Configure
`repos`/`workspace_dirs` only if you want repos with no live session to show
up too; see the config reference below.)

## Cockpit

The default view. On a wide terminal (≥110 cols) it's a split: the session
list on the left, a live preview of the selected session on the right. On a
narrow terminal the list is full-width and `tab` toggles a full-screen
preview.

The header shows, at a glance: sessions needing input (yellow ◐), sessions
working (green ●, with a spinner while any are), the total, the data source
(`in-process` or `daemon`), and how stale the view is. Sessions are grouped
by state (needs-input first), then repo, then worktree.

| Key | Action |
|---|---|
| `↑`/`↓` or `k`/`j`, `g`/`G`, `ctrl+d`/`ctrl+u` | move the selection / jump to first (the top blocked session) or last / half-page |
| `enter` | **attach** — `claude attach` a supervisor session, or jump to a tmux-pane session (switch-client inside tmux, else `tmux attach`). **Coming back:** press `←` at the session's empty prompt (backgrounds it, cockpit resumes automatically); after a tmux jump, `prefix + L` returns to the cockpit's pane |
| `r` | **reply** inline to a session waiting on input (only when it's blocked and has a background id) |
| `D` | **make durable** — on a `⌁bare` session (interactive, not in tmux: dies with its SSH connection) opens a sheet with the honest options; on durable sessions it tells you why you're fine |
| `o` | open the session in a tmux window **without** switching to it |
| `d` | dispatch a new background session into the selected repo/worktree |
| `x` | stop the selected session (with confirm) |
| `R` | respawn a stopped/failed session |
| `l` | full-screen logs pager for the selection |
| `/` | filter by substring (live, as you type) |
| `1` `2` `3` `4` | show only needs-input / working / idle / done+failed+stopped (same key again clears; `s` still cycles) |
| `tab` | toggle the preview (narrow terminals) |
| `?` | help overlay — every key, plus live collector health |
| `esc` | clear the topmost thing (overlay → filter → state filter); never quits |
| `q` | quit — instant, no questions |

The full design rationale — frozen keys, color semantics, the durability
mechanics, and the v1.1/v2 roadmap (new-session key, resume picker, command
palette) — lives in `docs/TUI-DESIGN.md`.

The live preview reads a tmux pane directly (`capture-pane`) when the session
has one, otherwise it tails `claude logs`. It refreshes on its own 2-second
tick, independent of the snapshot poll, and only while it's visible.

**Reply trust:** the cockpit's `r` reply is *not* gated behind
`actions.experimental_reply` — a user sitting at the keyboard has exactly the
same trust as one who would attach and type the answer themselves, so the
in-process path lets them reply directly. (The daemon path stays gated and
returns a 501 when the flag is off — see "Advanced" and `docs/REPLY.md`.)

Config lives at `--config PATH`, else `$XDG_CONFIG_HOME/clauditor/config.toml`,
else `~/.config/clauditor/config.toml`. Copy `config.example.toml` there and
adjust — a missing file falls back to defaults, and the cockpit still works
fully via zero-config discovery.

## Config reference

All keys from `config.example.toml`, with their defaults:

| Key | Default | Meaning |
|---|---|---|
| `repos` | `[]` | Explicit repo roots, always included, never scanned |
| `workspace_dirs` | `[]` | Directories scanned to depth 2 for `.git` (dir or file); linked worktrees resolve to their main repo and dedupe |
| `[poll].claude_seconds` | `5` | `claude agents --json` poll interval |
| `[poll].tmux_seconds` | `10` | tmux pane-scan interval |
| `[poll].git_seconds` | `20` | git worktree-list interval |
| `[git].dirty_check` | `true` | Run `git status --porcelain` per worktree (2s timeout; `dirty=unknown` on timeout) |
| `[git].ahead_behind` | `false` | Compute ahead/behind counts vs upstream |
| `[tmux].heuristics` | `false` | Reserved for a pane-text state-guessing fallback. **Not implemented** — supervisor state proved sufficient; the key is parsed and ignored (see docs/ARCHITECTURE.md) |
| `[serve].listen` | `127.0.0.1:8790` | HTTP bind address (loopback-only unless `--i-know-this-is-exposed`) |
| `[serve].snapshot_file` | `""` | Optional path to atomically write the latest snapshot as JSON, for debugging |
| `[access].team_domain` | `""` (example shows a placeholder) | Cloudflare Access team domain, e.g. `yourteam.cloudflareaccess.com` |
| `[access].policy_aud` | `""` | AUD tag from the Access application (`deploy/ACCESS.md`) |
| `[actions].enabled` | `false` | Master gate for mutating endpoints (dispatch/stop/respawn/open-in-tmux/reply) — first deploy is read-only |
| `[actions].experimental_reply` | `false` | tmux-injection reply strategy (`docs/REPLY.md`); 501 when off |
| `[dispatch].worktree_base` | `""` | Base dir for new worktrees created via dispatch; default `<repo>/../<repo>-worktrees` |
| `[links].worktree_url_template` | `""` | e.g. `https://{branch}.dev.example.com`, supports `{branch}`/`{slug}` |
| `[notify].debounce_seconds` | `30` | Suppress duplicate events for the same session+type within this window |

## Advanced: phone + notifications

The cockpit is the whole product for "I'm at the box (or SSH'd in) and want to
see my fleet." Everything below is optional and only for reaching the fleet
from elsewhere — a phone, a Mac, a cron job.

**The daemon (`serve`).** A long-running process that serves the same snapshot
over HTTP + SSE and hosts the WebUI. The cockpit auto-detects it (via
`<state>/local_token`) and, when present, shows `[daemon]` instead of
`[in-process]` and routes actions through it.

```
./bin/clauditor serve            # binds 127.0.0.1:8790 by default
curl http://127.0.0.1:8790/healthz
# {"ok":true,"version":"...","collectors":{"claude":0,"tmux":0,"git":0}}
```

**Notifications.** `clauditor notify` emits state-change events (e.g. a session
just became blocked). Stream them to a Mac over SSH, or run `--once` from cron
for a single diff against persisted state.

```
./bin/clauditor notify --stream          # long-lived event stream
./bin/clauditor notify --once            # one diff, for cron
```

**Persistent install + phone access.** For a daemon that survives logout and
reboot, see `deploy/systemd/clauditor.service` — a systemd **user** service,
not a system service (clauditor needs the login user's `~/.claude`, tmux
socket, and repo permissions; see that file's header comment for the full
rationale). `deploy/cloudflared/` and `deploy/ACCESS.md` cover putting the
WebUI behind your existing Cloudflare Tunnel + Access so you can reach it from
a phone.

## Security model

(Full requirements: SPEC.md §9; enforced in `internal/api/middleware.go`.)

- **Loopback bind only.** `serve` refuses to start bound to a non-loopback
  address unless you pass `--i-know-this-is-exposed`. Cloudflare Tunnel
  fronts it; clauditor never listens on a public interface itself.
- **`--dev-insecure-local` must never run behind an active cloudflared
  ingress.** cloudflared connects over loopback, so the flag's loopback
  check cannot tell a local `curl` from a proxied internet request — with
  both active, every tunneled request bypasses Access validation. Use it
  only when the tunnel ingress for this hostname is absent/disabled
  (see deploy/ACCESS.md).
- **Cloudflare Access JWT validated independently**, in middleware, on
  every `/api/*` route — not just trusted because the tunnel let the
  request through. Reads `Cf-Access-Jwt-Assertion` (falls back to the
  `CF_Authorization` cookie), validates against your team's JWKS,
  checks `iss`/`aud`/`exp`/`iat`. The authenticated email is logged at
  `info` on every mutating request.
- **Actions off by default.** `actions.enabled = false` — the first deploy
  is read-only. Mutating requests additionally require the
  `X-Clauditor-Action: 1` header (blunts CSRF from ambient Access
  cookies), `Content-Type: application/json`, and are per-route rate
  limited.
- **No permission-bypass path.** `--dangerously-skip-permissions` and
  `--permission-mode bypassPermissions` (in any spelling) are rejected
  wherever they could be smuggled into dispatch — this is a hard deny, not
  a default that can be turned off.
- **Prompt bodies are never logged**, at any level. The only thing logged
  on a mutating request is the route name and the authenticated email
  (`internal/api/middleware.go:142`) — request bodies, including dispatch
  prompts and reply text, don't pass through `slog` anywhere in this
  codebase. (SPEC §9 asks for "debug only, never info"; the actual
  implementation is stricter than that — there is no logging statement
  for prompt content at all, checked by grepping every `slog.*` call site
  in `internal/api` and `internal/actions`.)

## clauditor vs agent-deck

[agent-deck](https://github.com/asheshgoplani/agent-deck) already exists, is
good, and is MIT-licensed — clauditor doesn't rebuild it. They solve
overlapping problems differently:

- **agent-deck is a multi-tool orchestrator.** It manages agents across
  several coding tools, with groups, fork, and cost tracking, and its own
  session lifecycle. If that's what you want, run agent-deck.
- **clauditor is a Claude-only cockpit.** It's backed by Claude Code's own
  supervisor rather than reimplementing session state — zero hooks, nothing
  to install into your Claude config, no shadow lifecycle. It reads what the
  supervisor already knows and correlates it to tmux and git worktrees. The
  scope is narrower on purpose: watch a Claude fleet, and attach / reply /
  dispatch from one screen.

The cockpit does live-preview, inline reply, and attach right from the list.
clauditor's other value-add over agent-deck's on-box TUI is the optional
phone/notify layer (see "Advanced"): `clauditor notify` streaming
state-change events, and a WebUI reachable from your phone through Cloudflare
Access.

## Further reading

| Doc | What's in it |
|---|---|
| `docs/RESEARCH.md` | Phase 0 recon: steal/adapt/avoid notes on the four reference repos, their license table, and the empirical questions answered on this machine |
| `docs/ARCHITECTURE.md` | The binary's shape — subcommands, layers, and the design trade-offs behind them |
| `docs/REPLY.md` | The reply investigation: why tmux-injection shipped as the strategy, and the always-works `open-in-tmux` fallback |
| `docs/VERIFY.md` | Human checklist for anything that couldn't be fully verified inside the build environment |
| `docs/DEMOLOG.md` | Exact commands to see each milestone work |
| `docs/ROADMAP.md` | Tier 2 — the eight items SPEC deferred, written up with enough detail to start implementing from |
