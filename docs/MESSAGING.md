# MESSAGING.md — cross-session messaging, presence, and coordination

Investigation (2026-08-09, Claude Code 2.1.226) of three surfaces the user
asked about, and how clauditor should — and shouldn't — use each:
Claude Code's built-in [cross-session messaging], YoanWai/**mux**, and
asutekku/**crewmates**.

[cross-session messaging]: https://code.claude.com/docs/en/cross-session-messaging

## 1. What Claude Code now ships natively (v2.1.224+)

Cross-session messaging is **on by default** where available (macOS/Linux,
non-Bedrock/Vertex/Foundry, telemetry not disabled). The model:

- Two **tools**, used *by a Claude session*, never by an external process:
  `ListAgents` (discover reachable sessions) and `SendMessage` (deliver one
  line of text to a named session). `/list-agents` (alias `/peers`) shows
  the roster; `/status` shows this session's own `Peer address`.
- A message is **plain text only** — never files or history. Delivery is
  between-turns (never interrupts a running tool); an idle receiver starts a
  new turn. Delivered messages count as usage like a typed prompt.
- **Inbound controls** (`crossSessionInbound`: accept|hold|refuse) and a
  permission-class default decide delivery. Cross-machine is **reply-only**
  and travels through Anthropic servers via Remote Control.

### The on-disk surfaces (verified live on this machine)

Two are directly useful to clauditor, and both are **safe to read**:

1. **Session registry** — `~/.claude/sessions/<pid>.json`, one per live
   session. Observed schema (2.1.226):

   ```json
   {
     "pid": 3335357, "sessionId": "0ce5be82-…", "jobId": "0ce5be82",
     "cwd": "/home/rishi/projects/monorepo/stables-core-app",
     "kind": "bg", "entrypoint": "cli", "version": "2.1.225",
     "name": "Resolve stream route auth conflict…", "nameSource": "…",
     "status": "idle", "peerProtocol": 1,
     "messagingSocketPath": "/run/user/1000/cc-socks/3335357.sock",
     "startedAt": 1786232726236, "procStart": "295429270",
     "updatedAt": …, "statusUpdatedAt": …
   }
   ```

   This is richer than `claude agents --json` in two ways clauditor wants:
   it is **file-based** (fsnotify-watchable → instant state, the SPEC §5.1
   "accelerator" slot), and it carries `peerProtocol` +
   `messagingSocketPath` — i.e. *which sessions are messaging-reachable*.
   It lacks the background `state`/`waitingFor` that `agents --json` gives,
   so it enriches and accelerates rather than replaces the supervisor poll.

2. **Inbox socket** — `$CLAUDE_CODE_MESSAGING_SOCKET`, path also in the
   registry file's `messagingSocketPath`, under
   `/run/user/<uid>/cc-socks/<pid>.sock`. Verified: a `SOCK_STREAM` unix
   socket, OS-user-restricted. Messages an external process posts here go
   through the same inbound controls; **own-child** posts (a hook/Bash
   command posting to *its own* session's socket) are auto-delivered, which
   is the documented mechanism for hooks-post-to-self.

### The wire protocol is undocumented — do NOT build on it

`peerProtocol: 1` is a version marker for an **undocumented** framing. A
careful probe against a throwaway session (never a real one) got no greeting
and no response to newline-JSON guesses; the length-prefixed-frame probe was
(correctly) refused by the harness as raw-IPC fuzzing. Per SPEC §8's rule on
research-preview internals: **document, don't build on.** clauditor will not
speak the raw socket protocol.

### What this means for clauditor's reply feature

`docs/REPLY.md` reserved an upgrade path (roadmap item 4) for "first-class
reply if/when Claude Code exposes one." That is now **partially** here — but
as *tools inside a session*, not an external CLI verb or a documented socket
API. So:

- **Today:** the experimental tmux-injection reply
  (`actions.experimental_reply`) remains the mechanism. It is unchanged.
- **The clean upgrade, when it lands:** if Claude Code ships a
  `claude message <session> <text>`-style CLI verb (the natural external
  surface for `SendMessage`), clauditor's `actions.Reply` swaps its
  implementation behind the existing interface — tmux injection demoted to
  fallback. Until that verb exists, raw-socket delivery stays off-limits.
- **What clauditor CAN do now, safely:** *surface reachability*. Reading the
  registry, the cockpit can mark which sessions are peer-reachable
  (`peerProtocol` present + live socket), so a human knows a session can be
  messaged from another Claude session — without clauditor sending anything.

## 2. mux (MIT — Copyright (c) 2026 mux contributors)

A tmux session switcher with live preview and AI-CLI detection. Convergent
with clauditor's cockpit; two features worth taking:

- **Token/cost from the transcript** (`tmux/claude.go`): reads per-turn
  `message.usage` (`input_tokens`, `output_tokens`, `cache_read_input_tokens`,
  `cache_creation_input_tokens`) from the same `~/.claude/projects/*.jsonl`
  clauditor already parses for previews, and prices it. **Cheap** — the data
  is already in hand; only a versioned, dated pricing table needs care (the
  agent-deck lesson: surface staleness). → **building now** (§4).
- AI-CLI detection badges (claude/codex/aider/gemini): clauditor is
  Claude-only by design, so not applicable.
- Popup overlay: already documented in the README (`display-popup -E`).

## 3. crewmates (MIT — Copyright (c) 2026 Aku Mäkelä)

The most interesting of the three, and explicitly *complementary* to
messaging: crewmates is "the coordination layer that decides there is
anything worth saying." It runs ~15 Claude Code **hooks** and stores a
per-repo presence DB in `~/.claude/agent-presence/` (SQLite) plus a
`<repo>/.claude/crew.json`. It gives sessions in one checkout: a roster with
roles, each agent's self-declared `doing`, per-file open-warnings before your
Edit lands, `ask`/`answer` obligation tracking, `promise`/`handoff`/`breaks`
commitments, and `note`s that outlive the session.

**How clauditor relates:** crewmates is *write* coordination between agents;
clauditor is *read* observability over the fleet. They stack cleanly and
neither needs the other. Two integration options, both **roadmap, not now**
(reading another tool's SQLite schema couples us to its internals):

- **Surface, if present:** when `~/.claude/agent-presence/*.db` exists, the
  cockpit could read each session's `doing` line and open-file set and show
  them as enrichment (a far better row subtitle than a truncated name).
  Read-only, best-effort, degrades to nothing when crewmates isn't installed.
- **Don't reimplement:** clauditor should not grow its own promises/notes/
  obligations layer — that is crewmates' job, done well. If a user wants
  coordination, they install crewmates; clauditor shows its output.

## 4. What ships in this wave (safe + documented only)

1. **Presence-registry enrichment** (`~/.claude/sessions/*.json`): read as a
   supplementary collector to (a) mark peer-reachable sessions in the cockpit
   and (b) serve as an optional fsnotify accelerator that pokes an immediate
   re-poll on session status change. Never a state authority — the
   supervisor `agents --json` stays authoritative; the registry only
   accelerates and enriches.
2. **Token/cost readout** from transcript `message.usage`, with a versioned
   dated pricing table and an explicit staleness note. Surfaced as a
   per-session figure and a fleet total (quota awareness — SPEC's original
   "N parallel ≈ N× burn" concern).

## 5. Roadmap (added to docs/ROADMAP.md)

- **First-class reply** via a Claude Code messaging CLI verb when it ships
  (upgrade path from `actions.Reply`; tmux injection → fallback).
- **crewmates surfacing**: optional read of `~/.claude/agent-presence` to
  show each session's `doing` line + open files as row enrichment.
- **fsnotify accelerator** graduation: if the registry-poke proves valuable,
  extend it to `~/.claude/jobs/*/state.json` for sub-second bg-state changes.
