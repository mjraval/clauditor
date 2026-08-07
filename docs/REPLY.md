# REPLY.md — replying to a background session without attaching

**Status: tmux injection WORKS (verified live 2026-08-06, Claude Code 2.1.223).**
Shipped behind `actions.experimental_reply = true` (default **false**).
The always-works fallback is `open-in-tmux`.

## The problem

There is no documented CLI to send a reply to a background session
(documented surface: `agents`, `attach`, `logs`, `stop`, `respawn`, `rm`,
`daemon status`). Three strategies were investigated in SPEC §8 order.

## 1. tmux injection — verified working ✔

Live experiment transcript (condensed; run on this machine):

```sh
# a bg session blocked on input:
$ claude agents --json | jq '.[] | select(.id=="f290fb7a") | {state, waitingFor}'
{"state": "blocked", "waitingFor": "input needed"}

# attach it inside a scripted, detached tmux window:
$ tmux new-session -d -s clauditor-probe
$ tmux new-window -t clauditor-probe -n reply-test "claude attach f290fb7a"
$ sleep 8   # render settle
$ tmux capture-pane -p -t clauditor-probe:reply-test -S -15
❯ 1. Blue
  2. Green
  ...
  5. Type something.
Enter to select · ↑/↓ to navigate · Esc to cancel

# numbered-choice question → send just the digit, then Enter:
$ tmux send-keys -t clauditor-probe:reply-test -l "1"
$ tmux send-keys -t clauditor-probe:reply-test Enter

# verify the transcript advanced:
$ tmux capture-pane -p -t clauditor-probe:reply-test | tail
● User answered Claude's questions:
  ⎿  · What is your favorite color? → Blue
● Thanks — blue it is! ...
```

Observations that shape the implementation:

- **Attach renders usably in a scripted window.** ~8s settle was more than
  enough on this box; the implementation polls `capture-pane` for the
  prompt footer (`❯` line) with a 15s timeout instead of sleeping blind.
- **Numbered-choice prompts**: send only the choice number (`send-keys -l "3"`, 1–2 digits),
  then `Enter`. Free-text prompts: `send-keys -l -- <text>` then `Enter`
  — always `-l` (literal) and `--` so text starting with `-` or containing
  key names (`Enter`, `Space`, `;`) can't be interpreted (a bug observed
  in prior art). For long text, `tmux load-buffer` from a 0600 temp file +
  `paste-buffer` avoids both quoting and `ps` exposure (idea from dmux, MIT).
- **`waitingFor` clears quickly after input is consumed**, but `state`
  stays `blocked` while the (now-idle) prompt is open — verify delivery by
  transcript advancement (`claude logs` byte-length change or capture-pane
  diff), not by state flip.
- **Permission prompts**: clauditor **refuses** to answer these remotely by
  default and directs the human to attach — answering a permission prompt
  from a phone defeats the point of permission prompts. (Configurable
  refusal is deliberate; there is no "allow" path in T1.)
- Cleanup: kill only the window clauditor created (`kill-window`), never
  the user's windows; the bg session itself keeps running detached.

## 2. Daemon socket spelunking — documented, NOT built on

Findings on this machine (research only, per SPEC §8):

- `claude daemon status` reveals a control socket:
  `sock dir: /tmp/cc-daemon-$UID/<hash>/`, `control.sock` inside it.
- `~/.claude/daemon/` contains `auth/` (dir), `control.key`, `dispatch/`,
  `roster.json` (`{proto, supervisorPid, updatedAt, workers}`).
- `~/.claude/jobs/<id>/state.json` is rich: `state`, `needs`, `block`,
  `intent`, `tokens`, `inFlight`, `output`, `resumeSessionId`,
  `respawnFlags`, `tempo`, `updatedAt`, … — clearly the supervisor's own
  store, and clearly *not* a stable public interface.
- The daemon is transient ("started on-demand by `claude --bg`") and
  idle-exits; a socket protocol reverse-engineered today would break
  silently on any Claude Code release.

**Decision:** do not touch `control.sock` in T1. If Claude Code ships a
documented reply command, it slots in behind the same `actions.Replier`
interface with tmux injection demoted to fallback (T2 roadmap item 4).

## 3. Fallback that always works — `open-in-tmux` ✔ (ships regardless)

`POST /api/v1/sessions/{key}/open-in-tmux` creates a window in the
`clauditor` tmux session running `claude attach <id>`; the UI shows
"attached in clauditor:<n> — `tmux attach -t clauditor`". When
`experimental_reply` is off, the WebUI Reply button becomes this.

## Implementation notes (internal/actions/reply.go)

Sequence, all steps with context timeouts:

1. Ensure hidden session: `tmux new-session -d -s clauditor` (idempotent).
2. `tmux new-window -d -t clauditor -n reply-<id> "claude attach <id>"`.
3. Poll `capture-pane -p` every 500ms (timeout 15s) until a prompt footer
   line is visible; classify the screen: free-text prompt / numbered
   choice / permission prompt.
4. Permission prompt → abort with `409 permission_prompt` and the
   open-in-tmux hint. Numbered choice → require the reply text to be a
   1–2 digit choice number, else `422`. Free text → send.
5. `tmux send-keys -t <win> -l -- <text>`, then `send-keys Enter`.
6. Verify: capture-pane diff (or `claude logs` growth) within 10s, else
   report `502 delivery_unverified` (the text may still have landed —
   say so in the message).
7. `tmux kill-window -t <win>` (always, deferred).

Risks (why this stays experimental): Claude Code's TUI layout is not a
contract; multi-page dialogs, fullscreen renders, and future redesigns can
break classification. The blast radius is bounded: worst case the text
lands in the composer of an attached session and a human sees it on attach.

## M5 update — verified through the full API path (2026-08-06)

`POST /api/v1/sessions/{key}/reply` with `{"text":"a red panda"}` against a
live blocked session (`waitingFor: input needed`, free-text question):
delivered and verified in **1.9s**; `claude logs` shows the question, the
injected answer, and Claude's response. The endpoint stays gated behind
`actions.experimental_reply = true`.
