# DEMOLOG.md — see it work

Exact commands a human can run after each milestone. All from the repo root;
`make build` first.

## M1 — Tier 0 notify

```sh
make build

# one-shot diff against persisted state (~/.local/state/clauditor/);
# the very first run is a silent baseline
./bin/clauditor notify --once --format text

# make a change: dispatch a throwaway bg session somewhere
( cd /tmp && claude --bg "reply with the word ok" )

# now the diff reports it
./bin/clauditor notify --once --format text
# [session_new] (loose) · "reply with the word ok" · 5s
# …and when it finishes, a later run prints:
# [completed] (loose) · "…" · 1m

# streaming mode (Ctrl-C to stop): one line per state transition, 5s poll
./bin/clauditor notify --stream --format json

# exec mode: run any command per event
./bin/clauditor notify --stream --exec 'echo "$CLAUDITOR_EVENT $CLAUDITOR_REPO $CLAUDITOR_WAITING_FOR"'

# on the Mac (see docs/VERIFY.md #1):
CLAUDITOR_HOST=devbox ./scripts/mac-notify.sh
```

Verified live on the build box 2026-08-06: two consecutive `--once` runs
against the real fleet (6 interactive sessions) — first silent baseline,
second silent (no transitions), exit 0.

## M0 — recon artifacts

```sh
less docs/RESEARCH.md      # steal/adapt/avoid + empirical answers
less docs/REPLY.md         # the reply experiment transcript
./scripts/capture-fixtures.sh   # refresh fixtures after a claude upgrade
```

## M2 — store + status

```sh
make build

# grouped fleet table (repo → worktree → sessions) with state glyphs
./bin/clauditor status

# raw snapshot JSON (version counter, generatedAt, full correlation)
./bin/clauditor status --json | jq '{version, sessions: (.sessions|length)}'
```

Verified live 2026-08-06: 8 sessions across 6 repos, dirty dots, tmux
targets, working/idle states — including this build session itself.

## M3 — HTTP API

```sh
make build
./bin/clauditor serve --dev-insecure-local &   # loopback dev mode (no Access JWT)

curl -s localhost:8790/healthz | jq .
# {"collectors":{"claude":4,"git":4,"tmux":4},"ok":true,"version":"…"}

curl -s localhost:8790/api/v1/state | jq '{version, sessions: (.sessions|length)}'

KEY=$(curl -s localhost:8790/api/v1/state | jq -r '.sessions[0].key')
curl -s "localhost:8790/api/v1/sessions/$KEY" | jq .
curl -s "localhost:8790/api/v1/sessions/$KEY/logs?lines=20"
curl -sN localhost:8790/api/v1/events | head -3     # SSE snapshots

# security gates (actions default OFF):
curl -s -X POST "localhost:8790/api/v1/sessions/$KEY/stop"
# {"error":{"code":"actions_disabled","message":"… set actions.enabled = true …"}}

# with actions.enabled=true but no CSRF header:
# {"error":{"code":"missing_action_header",…}}   ← X-Clauditor-Action: 1 required

# dispatch from the CLI (same code path as POST /api/v1/dispatch):
./bin/clauditor dispatch --repo clauditor --new-worktree feat/demo "say ok"
```

Verified live 2026-08-06 (transcript in git history of this file's commit).

## M4 — WebUI (read-only board)

```sh
make build
./bin/clauditor serve --dev-insecure-local &
# open http://127.0.0.1:8790/ (or via SSH port-forward from the Mac:
#   ssh -L 8790:127.0.0.1:8790 devbox   → http://localhost:8790)
# Board: needs-input on top, working, idle/interactive, done (collapsed);
# tap a counter to filter; tap a row for the session drawer with logs,
# copy-attach/resume buttons; SSE keeps it live (kill serve → stale banner).
```

## M5 — actions in the WebUI + reply

```sh
# enable actions in config first:
#   [actions]
#   enabled = true
#   experimental_reply = true   # optional
make build && ./bin/clauditor serve --dev-insecure-local &

# dispatch into a NEW worktree via API (the UI's dispatch sheet does this):
curl -s -X POST localhost:8790/api/v1/dispatch \
  -H 'X-Clauditor-Action: 1' -H 'Content-Type: application/json' \
  -d '{"target":{"repo":"demo-repo","newWorktree":{"branch":"feat/reply-demo"}},
       "prompt":"Ask me what my favorite animal is and wait.","name":"reply-demo"}' | jq .

# when it blocks, reply through the API (experimental gate):
curl -s -X POST "localhost:8790/api/v1/sessions/$KEY/reply" \
  -H 'X-Clauditor-Action: 1' -H 'Content-Type: application/json' \
  -d '{"text":"a red panda"}'
# → {"status":"delivered"}   (verified live; transcript in docs/REPLY.md)

# in the browser: + dispatch FAB (with working-count), per-session
# stop / respawn / open-in-tmux / reply buttons in the drawer.
```

## M6 — TUI

```sh
make build
./bin/clauditor tui          # in-process collectors, or auto-connects to a
                             # running `serve` via <state>/local_token
# keys: j/k move · / filter · s cycle state · enter open-in-tmux
#       l logs pager · d dispatch · x stop (confirm) · q quit
```

Verified live 2026-08-06 in a 100×30 tmux pane: grouped buckets, glyphs,
tmux targets, [in-process] source indicator.

## M7 — doctor + deploy

```sh
make build
./bin/clauditor doctor        # PASS/WARN/FAIL table; exit 0 unless FAIL
# deploy as a systemd user service:
mkdir -p ~/.config/systemd/user && cp deploy/systemd/clauditor.service ~/.config/systemd/user/
systemctl --user daemon-reload && systemctl --user enable --now clauditor
loginctl enable-linger $USER
# Cloudflare: deploy/ACCESS.md + deploy/cloudflared/ingress-snippet.yml
```

Verified live 2026-08-07: 8 PASS, 1 WARN (supervisor idle — starts on
demand), 0 FAIL against the real machine.

## Cockpit (bare command)

```sh
make build
./bin/clauditor              # the cockpit — no arguments, no config needed
# ./bin/clauditor tui        # identical (alias)

# Zero-config proof: even pointed at a nonexistent config, sessions still
# correlate to their repos/worktrees (not all "(loose)") because repo roots
# are derived from the live sessions' cwds:
./bin/clauditor --config /nonexistent/nope.toml

# move:   ↑↓/kj select · g/G first/last · ^d/^u half-page
# act:    enter attach · r reply · o open-in-tmux · d dispatch
#         x stop (confirm) · R respawn · l logs · D make durable
# glance: / filter · 1 2 3 4 state (needs/working/idle/done; same key clears)
#         s cycle state (demoted) · tab preview (narrow) · ? help
# esc clears overlay → text filter → state filter → nothing (never quits)
# q / ^c quit instantly · n N h i : reserved (no-op in v1)
```

Layout: at ≥110 cols, a split — session list left (clamp 38–64, ~42%), live
preview of the selection right (tmux capture-pane, else `claude logs`, refreshed
every 2s; caption names its source: `pane <target>` vs `logs <id>`). Below 110
cols the list is full-width and `tab` toggles a full-screen preview. Header:
needs-input (◐) / working (● + spinner) / total counts, source label, bare
snapshot age (red `stale Xs — retrying` past 15s, red `tmux scan ✗ Xs` on
collector failure). Rows drop the state word + short id, carry one badge max
(`⌁bare` risk wins over `⧉` durability), and right-align age (`3d` past 48h).

Durability: bare interactive sessions (supervisor-interactive, no tmux) wear the
accent `⌁bare` badge; `D` opens a centered sheet — `t` parks a durable
`claude --resume` copy in a tmux window (no switch), `b` attaches to background
from the inside. Already-durable kinds answer `D` with a toast. The contextual
footer shows ≤6 hints selected by the selection's state, always ending `? help`;
`?` opens the full key crib whose `sources:` line doubles as the completeness
report. Empty/first-run/stale states carry their own copy.

Verified live 2026-08-07 in a 130×35 tmux pane: split layout rendered with 10
real sessions grouped by state → repo → worktree, animated working spinner,
live preview streaming a session's transcript, and zero-config discovery
correlating every session to its repo (0 "(loose)").

Verified live 2026-08-08 (v1 cockpit delta) via capture-pane at 80×20, 100×30,
140×40: buckets, `⌁bare` badges, right-aligned day ages, contextual footer, the
`?` overlay (with live `sources:` line), the centered make-durable sheet, and
the no-sessions welcome — no horizontal overflow or torn glyphs at any size.

## Cockpit v2 — the overhaul (2026-08-09)

The v1 preview stripped ANSI from `claude logs` and mashed words together; the
list column could overflow into the preview. v2 rewrites both.

Preview: source is chosen per session kind — a tmux pane gets a RAW
`capture-pane -p -e` (colors kept, made safe by the ANSI sanitation kit), and a
pane-less background session renders its transcript tail
(`${CLAUDE_CONFIG_DIR:-~/.claude}/projects/*/<sessionId>.jsonl`, new
`internal/transcript` package) as clean `❯` user / `●` assistant / dim
`⚒ <tool>` lines. The panel title names the source (`pane dev:1.2` vs
`transcript`) and its age. Refresh is adaptive (~300ms while the selected
session works, ~1200ms calm) with a 50ms settle debounce on cursor movement and
a generation counter that discards stale fetches (a held `j` never queues tmux
forks). The `/api/v1/…/logs` endpoint still returns ANSI-stripped text for curl.

Visual: tonal surfaces instead of borders — a `mix()` lerp over three theme
anchors derives the panel/block/rule tones; the list rail is painted with the
panel tone via a `paint()` primitive (exact-width pad + bg re-assert after inner
resets), the preview column stays unpainted, a hairline seam divides them. Both
columns carry a 2-line panel title (bold-accent + full-width rule). The selected
row is an unbroken inverted accent bar with a `▶` prefix. Every frame row is
clamped to the terminal width, so the preview can never bleed into the list.

Density & feel: budget-based row truncation (the name takes the remainder after
glyph/flag/badge/age), two-component ages (`3h 20m`, `2d 5h`), `⋮ +N above/below`
scroll indicators, and responsive empty-state tiers. Action feedback is a toast
spliced over the frame's top-right (zero layout shift); the narrow list footer
gains a live `tab preview` hint when it fits the 6-hint cap; the last frame is
held while suspended into an attach (never a blank frame). Frozen keys
(`enter r d x q`) unchanged.

Verified live 2026-08-09 via capture-pane at 80×20, 100×30, 110×30, 140×40
(`window-size manual`): every row exactly the terminal width, zero overflow at
every size. On 140×40 with a WORKING supervisor session selected, the preview
showed readable live pane content (`PREVIEW · … · pane claudinator:1.1`); the
needs-input session's preview showed clean `❯`/`●`/`⚒` transcript lines — no
word-mash.

## Peer-reachability enrichment (2026-08-09)

`docs/MESSAGING.md` §4.1's presence-registry read: a new `SessionsCollector`
(`internal/collect/sessions.go`) globs `~/.claude/sessions/*.json` each poll
cycle and matches entries to correlated sessions by `sessionId`
(`model.EnrichPeerReachable`), setting `peerReachable: true` when the
registry shows a live messaging socket + `peerProtocol > 0`. Read-only,
supplementary — the supervisor poll stays the sole state authority.

```sh
make build
./bin/clauditor status --json | jq '.sessions[] | {sessionId, name, peerReachable}'
```

Verified live 2026-08-09: of 7 concurrent sessions on this machine, the one
background session with an active `messagingSocketPath`
(`/run/user/1000/cc-socks/2684716.sock`) showed `"peerReachable": true`; the
six interactive sessions (`messagingSocketPath: null` in their registry
files) showed `false`. The TUI's preview caption appends `⇄ peer-reachable`
for the selected session when true — no change to the row's single badge
slot.

## Token/cost readout (2026-08-09)

`docs/MESSAGING.md` §4.2's cost estimator: a new `internal/usage` package
sums per-turn `message.usage` (input/output/cache-read/cache-creation
tokens, keyed by model — a session can switch models mid-conversation) out
of the whole transcript file (`internal/transcript.Resolve`, capped at 32MB,
tail-truncated past that with `Truncated` set), then prices it against a
dated table (`PricingAsOf`, currently 2026-08-09) covering the Opus/Sonnet/
Haiku/Fable/Mythos families. An unpriced model reports `CostKnown: false`
rather than a guessed $0; cache read/write rates are derived from
Anthropic's published fixed ratio to the input rate (0.1×/1.25×), since
there's no separate per-model cache price to look up. Cost is int64
microdollars end to end, formatted to `$X.XX` only at the display edge.

Gated behind `[usage].track_cost` (default off — extra disk IO) and cached
per `(sessionID, transcript file size+mtime)` in `store.Poller.enrichUsage`,
a post-correlation step mirroring `EnrichPeerReachable`, so a poll tick with
no transcript changes never re-reads a session's file. `model.Session`
carries `Tokens`/`CostMicroUSD`/`CostKnown` (so `/api/v1/state` and `status
--json` get it for free); the cockpit shows the selected session's tokens +
cost in the preview caption and a dim fleet-total in the header, both only
when `track_cost` is on and the session's own `CostKnown` is true; `status
--cost` (or the config flag) adds the same to the CLI table.

```sh
./bin/clauditor status --cost --config <cfg-with-usage.track_cost=true>
```

Verified live 2026-08-09 against real `~/.claude/projects/*.jsonl`
transcripts on this machine: 7 sessions all reported `costKnown: true`
(models `claude-opus-5`/`claude-opus-4-8`/`claude-fable-5`, no unpriced
turns encountered), tokens ranging 317.6k–1.2M and costs $35.47–$466.16,
fleet total `$1256.25 working` in the header. Cross-checked one session
(881,985 tokens) against an independent Python re-implementation of the
same pricing math reading the same file: totals agreed exactly, cost
agreed to within $0.000001 (per-turn vs. summed-then-rounded microdollar
rounding). tmux capture-pane at 80×20, 100×30, and 140×40 showed zero
row/column overflow with cost segments rendered; at 220×40 the preview
caption's `<tok> tok · $<cost>` fragment was visible in full alongside the
`⇄ peer-reachable` mention.
