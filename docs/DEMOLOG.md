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
