# TUI-DESIGN.md — the clauditor cockpit design specification

Status: definitive spec for the cockpit TUI (`internal/tui/`). Supersedes the
draft charter where they conflict; every overrule of the charter or the
shipped implementation is marked **Overrule** with a reason. Produced from a
dedicated design study of claude-dashboard, workmux, aven, k9s, gh-dash, and
selected awesome-tuis entries (lazygit, atuin) on 2026-08-07.

---

## 1. Product thesis

clauditor is the room you stand in while a fleet of Claude Code sessions
works for you: one glance tells you exactly what needs a human, and one
keystroke acts on it. It wins not by doing more than the terminal but by
never making you hunt — the supervisor's truth, tmux, and git are correlated
into a single attention-sorted list where the blocked session is always on
top and always yellow. It becomes "the only way developers interact with
Claude Code" the same way k9s became the only way people touch clusters:
start from *watching* everything perfectly, then absorb *starting*,
*resuming*, and *finishing* sessions until leaving the cockpit feels like a
regression.

---

## 2. Reference study

| # | Reference | Pattern | Verdict | Why |
|---|---|---|---|---|
| 1 | claude-dashboard | Status glyph language (`●` active / `◎` waiting / `○` idle) with per-state color | **Adopt** (already shipped) | Same conclusion independently reached; validates `◐/●/○` glyph set |
| | | `n` = create new session, auto-named from directory | **Adopt (v1.1)** | The single biggest gap vs. "only tool you need"; auto-naming removes a prompt |
| | | Logs viewer reads `~/.claude/projects/*.jsonl` without attaching | **Adapt (v1.1)** | Powers the resume picker: history browsing must not require a live session |
| | | CPU/RAM/uptime columns in the main list; 2s full refresh | **Reject** | Resource metrics answer a question nobody asks in this product ("what needs me?"); they cost columns and calm. `top` exists |
| | | `Ctrl+K` kill-all-idle | **Reject** | A one-chord mass-destructive action is a footgun; clauditor's destructive verbs are always per-session + confirmed |
| | | Managed-session prefix (`cd-`) / shadow lifecycle | **Reject** | clauditor's whole bet is *no shadow lifecycle* — the supervisor is the truth (ARCHITECTURE.md) |
| 2 | workmux | `add` = worktree + tmux window + agent launched + client auto-switched, one command | **Adopt (v2)** | This is the interactive-worktree-per-task flow; the recipe (create → setup → window → switch) is proven |
| | | `open --continue` — reopen a worktree's window and resume the agent conversation | **Adopt (v1.1)** | Exactly the mechanics the durability sheet and resume picker need (`claude --resume` in a fresh tmux window) |
| | | Agent status icons injected into tmux `window-status-format` | **Adapt (v2, optional)** | Nice ambient signal, but it mutates user tmux config — must be opt-in |
| | | YAML pane-layout config as a prerequisite | **Reject** | Violates promise #1 (zero to cockpit in one word). Default layout must be hardcoded-good |
| 3 | aven | Command palette inside a keyboard-first TUI | **Adopt (v1.1)** | Confirms palette + single-key coexistence works for power users |
| | | CLI output designed separately for agents vs. TUI for humans | **Adopt** (already true: `status`/`notify` vs cockpit) | Keep the separation sacred; never let TUI needs leak into `status --json` |
| | | Task-manager scope: priorities, epics, labels, due dates | **Reject** | Curation is upkeep, and upkeep kills daily tools. Ordering stays computed, never manual |
| 4 | k9s | `:` command mode for switching views/resources | **Adopt (v1.1)** | The scaling path: when views multiply, the palette absorbs them without new top-level keys |
| | | Contextual key hints that change with selection | **Adopt (v1, hardened)** | See §4 footer rules |
| | | Enter = drill down, Esc = up, everywhere | **Adapt** | Keep *Esc = up* universally. Reject *Enter = drill*: in a cockpit the unit of intent is *action*, not exploration — Enter attaches. Detail inspection gets its own key (`i`, v1.2) |
| | | 4–6 line header block (context, logo, hotkey crib) | **Reject** | k9s spends ~20% of a 30-row terminal on chrome. clauditor's header is 1 line; the crib lives in `?` |
| | | Skins/YAML theming | **Adopt (v2)** | Cheap love once the palette is centralized; not before the semantics are frozen |
| 5 | gh-dash | Split list + rich preview pane as the core layout | **Adopt** (already shipped) | Validates the ≥110-col split |
| | | Config-defined sections/filters per user | **Adapt (v2)** | gh-dash *requires* config to be useful — clauditor must be excellent with zero config, config only ever tunes |
| | | Everything-through-YAML keybindings | **Reject (until v2)** | Rebindable keys before the default keymap is beloved = fragmented muscle memory |
| 6 | awesome-tuis picks | **lazygit**: confirm prompts that state the consequence, not just "y/n" | **Adopt (v1)** | Every clauditor confirm names the object and the consequence (§6) |
| | | **atuin**: instant-as-you-type filtering, zero modes, Esc restores | **Adopt** (already shipped in `/`) | Keep it substring-only and forgiving; no regex modes ever |
| | | **lazygit**: 5-panel grid with per-panel focus cycling | **Reject** | Multi-panel focus is the main thing newcomers fumble. One list + one read-only preview; exactly one focus |

---

## 3. Information architecture

**One home screen. Transient overlays. Esc always walks home. No nested
navigation, ever.**

```
                    ┌──────────────┐
      ?  ──────────▶│ Help overlay │──── esc/? ─┐
                    └──────────────┘            ▼
   ┌─────────────────────────────────────────────────────────┐
   │  HOME: attention-sorted session list (+ preview ≥110c)  │◀── esc from anything
   └─────────────────────────────────────────────────────────┘
      │ l           │ / d r          │ x D            │ enter
      ▼             ▼                ▼                ▼
   Logs pager    modal input line   confirm sheet   suspend → attach
   (fullscreen)  (bottom, 1 line)   (centered)      (ExecProcess, returns here)
   v1.1 adds:  : palette (bottom line) · h resume picker (fullscreen list)
```

- **Home** — the bucketed list: `NEEDS INPUT → WORKING → IDLE / INTERACTIVE
  → DONE / FAILED / STOPPED`, each bucket grouped repo → worktree.
  Attention-first ordering is computed and non-negotiable.
- **Preview** — right pane at ≥110 cols; fullscreen toggle via `tab` below.
  Read-only live tail. It is a *window*, not a terminal — interaction
  happens by attaching.
- **Modal line inputs** (filter / dispatch / reply) occupy the status line,
  never a popup — the list stays visible so context is never lost.
- **Confirm sheets** (stop, make-durable) are small centered boxes; they
  exist only for consequences.
- **Logs pager** and (v1.1) **resume picker** are the only fullscreen
  takeovers, both one Esc from home.
- **Navigation model: flat, not drill-down.** A Claude fleet is 3–30
  sessions, not thousands of objects. New *views* (resume history, health)
  are siblings reached by palette/keys, not children reached by drilling.

**Communicating completeness.** The user must be able to *trust* that the
screen is the whole fleet. Four mechanisms, all passive:

1. **Header freshness chip**: `3s` (dim) = age of last successful snapshot.
   At >15s it becomes red `stale 22s — retrying`. Never silently frozen.
2. **Collector health**: healthy collectors show nothing (calm is a
   feature). On failure, a red header segment appears: `tmux scan ✗ 40s`.
3. **The empty state asserts coverage** (§5): an empty list that *names its
   search* is proof, not absence of evidence.
4. **`?` overlay footer**: `sources: supervisor ✓ 2s · tmux ✓ 4s · git ✓ 11s`.

---

## 4. Keymap (v1)

**Frozen keys** — may never be rebound or repurposed: `enter` `r` `d` `x`
`q`. They are the muscle-memory core.

### Home (list) mode

| Key | Action | Rationale |
|---|---|---|
| `↑`/`k`, `↓`/`j` | move selection (skips header rows) | vim + arrows, both always |
| `g` / `G` | jump to first / last session | `g` lands on the top blocked session — "take me to what needs me" in one key |
| `ctrl+d` / `ctrl+u` | half-page down / up | fleets of 30+ sessions; vim-standard |
| `enter` | **attach** — the obvious thing per kind: `claude attach <id>` (suspend/resume cockpit) for supervisor sessions; `switch-client` when inside tmux for pane sessions; `tmux attach` outside | one verb, kind-dispatch hidden |
| `r` | **reply** inline to a blocked session (blocked + background id only) | the killer feature |
| `o` | **open in tmux** without switching | "park it over there"; distinct from enter's "take me there" |
| `D` | **make durable** — badge-paired action, see §6 | capital-D pairs visually with the `⌁bare` badge |
| `d` | **dispatch** a background task into the selected repo/worktree | the primary creation verb of v1 |
| `x` | **stop** selected session — confirm sheet | `x` = cross out. Not `ctrl+k` (mass-kill rejected, §2) |
| `R` | **respawn** a stopped/failed session | pairs with `x` as the undo-shaped counterpart; only on terminal states |
| `l` | **logs** pager (fullscreen) | breaks vim `h/l` horizontal nav — accepted, there is no horizontal nav |
| `/` | filter, live substring across name/repo/branch | as-you-type narrowing, enter keeps, esc restores |
| `1` `2` `3` `4` | state filter: needs / working / idle / done+failed+stopped. Same key again clears | direct-jump beats cycling: one press to any bucket. **Overrule of cycling-only `s`** |
| `s` | cycle state filter | kept for continuity; demoted in footer/help |
| `tab` | toggle fullscreen preview (narrow terminals; no-op when split visible) | tab = "the other thing" |
| `?` | help overlay | universal |
| `esc` | clears, in order: open overlay → text filter → state filter → nothing. **Never quits** | esc must always be safe to mash |
| `q`, `ctrl+c` | quit, instantly, no questions | quit is never negotiated |
| `n` `N` `h` `i` `:` | **reserved, no-op in v1** (new session / new task+worktree / history-resume / inspect / palette) | v1.1 features land without stealing anyone's key |

### Modal line inputs (filter / dispatch / reply)
`enter` submit · `esc` cancel (restores prior state). Full readline-style
editing via bubbles textinput.

### Confirm sheets
Stop: `y` / `n`·`esc`. Make-durable: `t` / `b` / `esc` (§6). Single-key
accept, always.

### Logs pager
`j/k` `↑↓` scroll · `pgup/pgdn` page · `g/G` ends · `q`/`esc` back.

### Contextual footer rules

**Overrule of the shipped static 12-hint footer**: a footer that shows
everything teaches nothing. Rules:

1. **Max 6 hints.** Always ends `? help`.
2. Hints are selected by the current selection's state; a verb appears only
   when it would work right now.
3. First hint = the most likely intent for this selection state.

| Selection | Footer (exact string) |
|---|---|
| blocked, has id | `r reply · enter attach · o tmux · l logs · / filter · ? help` |
| bare interactive (fragile) | `enter attach · D make durable · l logs · d dispatch · / filter · ? help` |
| working, durable | `enter attach · o tmux · x stop · d dispatch · / filter · ? help` |
| stopped / failed | `R respawn · l logs · d dispatch · / filter · ? help` |
| idle / unknown | `enter attach · o tmux · x stop · l logs · / filter · ? help` |
| nothing selected / empty | `d dispatch · / filter · ? help · q quit` |
| input modes | `enter submit · esc cancel` |
| logs | `j/k scroll · pgup/pgdn page · q back` |

The first time a blocked session ever appears, the `r reply` hint renders in
accent for one poll cycle.

### The `?` help overlay (fullscreen, dim scrim over the list)

```
  clauditor — keys                                              esc or ? closes
  ─────────────────────────────────────────────────────────────────────────────
  GLANCE                              ACT ON SELECTION
   /       filter as you type          enter  attach (the obvious thing)
   1–4     only needs / working /      r      reply to a blocked session
           idle / done                 o      open in tmux, don't switch
   esc     clear filter / overlay      D      make durable (bare sessions)
   tab     fullscreen preview          d      dispatch background task here
           (narrow terminals)          x      stop… asks first
                                       R      respawn stopped/failed
  MOVE                                 l      logs pager
   j/k ↑↓  select session
   g / G   first / last               COMING (v1.1): n new session ·
   ^d/^u   half page                  h resume a conversation · : commands
   q       quit — instant, no prompt
  ─────────────────────────────────────────────────────────────────────────────
  sources: supervisor ✓ 2s · tmux ✓ 4s · git ✓ 11s          v0.4.0 · in-process
```

Three intent groups (Glance / Act / Move), every line states a consequence,
not a name. This is the only place all keys appear. The sources line doubles
as the completeness report.

---

## 5. Visual system

### Palette

| Color | Hex | Meaning — never violated |
|---|---|---|
| yellow | `#e8b44c` | **needs you.** Used for nothing else, ever |
| green | `#4caf7d` | working (breathing spinner) |
| red | `#e05d5d` | failed / stale / collector down — always underlined or glyph-doubled (colorblind-safe) |
| dim | `#6d7b89` | idle, done, stopped, chrome text — visible but recessive |
| accent | `#d97a4a` | chrome (selection, brand, bucket headers, hints) **and actionable affordance badges** |

**Overrule of the charter's "accent = chrome only":** the durability badge
`⌁bare` renders in accent. The badge is not a state; it is a standing
*invitation to act* (`D`), which is exactly what accent means in the footer
hints. A sixth color would be worse; dim would bury a data-loss risk.

Terminal-default background always; lipgloss downsampling covers low-color
SSH. Selection = one unbroken inverted bar in accent across every row
segment, with a `▶` prefix (v2 decision, superseding the earlier
state-tinted-reverse idea: accent's role IS "where you are"; tinting the
bar by state made the highlight compete with the state glyph).

### Row anatomy

**Overrule of the charter's row spec** ("state glyph · name · repo·branch ·
waitingFor · age"): in the tree layout, repo and branch are *group headers* —
repeating them per-row wastes ~20 cells. Rows inline `repo·branch` only in
flat contexts (filtered results, v1.2).

**Overrule of the shipped `sessionLine`:** drop the state word (the
glyph+color already say it), drop the short id (belongs in preview caption
and inspect view), cap badges at one:

```
[cursor 2][indent 4][glyph 1] [name flex 12–24] ··· [⚑ waitingFor ≤16] [badge ≤6] [age 4, right]
```

Badge priority (one shown, highest wins): `⌁bare` (fragile — you might lose
this) → `⧉` (lives in tmux; target shown only when width allows).

At three widths (list pane of 80 / 58 / 38 columns):

```
80 (narrow terminal, full-width list)
NEEDS INPUT (1)
  clauditor
    feat/cockpit *dirty
>   ◐ auth-flow refactor          ⚑ file write approval    ⧉ dev:1.2      12m
WORKING (2)
  stables
    main
    ● payments-recon                                       ⌁bare        2h10m

58 (list pane at a 140-col split)
NEEDS INPUT (1)
  clauditor
    feat/cockpit *dirty
>   ◐ auth-flow refactor    ⚑ file write approval  ⧉  12m
WORKING (2)
  stables
    main
    ● payments-recon                             ⌁bare  2h10m

38 (list pane at the 110-col split minimum)
NEEDS INPUT (1)
  clauditor
   feat/cockpit *
>  ◐ auth-flow ref… ⚑ approval  ⧉  12m
WORKING (2)
  stables · main
   ● payments-recon      ⌁bare 2h10m
```

Degradation order as width shrinks: tmux target text → bare `⧉` →
waitingFor truncates 16→8 → name truncates 24→14. The glyph, badge, and age
never drop — state, risk, and staleness are the last three facts standing.

### Split geometry

**Overrule of the shipped formula** (`45% min 34`): list width =
**clamp(38, 42% of width, 64)**. Below 38 the row anatomy can't hold a name
+ waitingFor; above 64 a session row is pure padding while the preview — the
pane that shows *actual work* — starves. Threshold stays 110; below it,
full-width list + `tab` preview.

### Header (1 line, always)

```
clauditor · ◐ 2 need input · ⠹ 3 working · 9 total · [in-process] · 3s
```

Segments: brand (accent) · needs count (yellow) · working count (green;
braille spinner while any session works, `●` otherwise) · total (dim) ·
source (dim) · snapshot age (dim; red `stale 22s — retrying` past 15s).
Appended only when active: `filter:needs` chip, `/auth` query chip, and any
red collector segment `tmux scan ✗ 40s`. **Overrule of "refreshed 3s ago"**
→ bare `3s`: the label re-reads 40 times a day; the number alone is the
information.

### Preview pane

Caption line (accent): `preview · auth-flow refactor · pane dev:1.2 · 2s` —
**overrule of the shipped caption** to name its *source* (`pane <target>` vs
`logs <id>`): the two sources have different fidelity and the user deserves
to know which they're looking at.

### Motion rules

The working spinner is the only continuous motion. State transitions flash
the row background once, 150ms (v1.2). No marquees. Every keystroke echoes
within one frame; anything >100ms shows an inline spinner where the result
will land; the event loop never blocks.

### Empty, error, loading states (exact copy)

First fetch: `connecting to the Claude supervisor…` centered, dim, spinner.

No sessions at all:

```
        No Claude sessions anywhere on this box.
        supervisor + tmux scanned 2s ago — a new session appears here within 5s.

          d   dispatch a background task from here
          or run `claude` in any repo — it shows up live.
```

Filter with no matches: `no matches for "kms" — esc clears` (dim, in-list).
Fetch failure: keep rendering the last good snapshot; header goes
`stale 22s — retrying`; status line shows `supervisor unreachable — is
claude installed? retrying…` (red). The cockpit never dies on a collector
failure and never modals an error.

---

## 6. Durability UX

The problem, honestly: over SSH, a Claude session in a bare terminal dies
with the connection. Background sessions are daemon-owned and survive.
Interactive sessions inside tmux survive. Interactive sessions in a bare
terminal do **not**, and Claude Code today offers no external command to
background one — only `←`/`/bg` typed *inside* it, or continuing the
conversation elsewhere via `claude --resume <sessionId>`.

**Badge:** ` ⌁bare` — accent color, shown only on non-durable sessions
(badge priority 1). Not yellow (yellow = needs input, sacred), not red
(nothing has failed — yet).

**Key:** `D` (make **D**urable). Works on any selection; on already-durable
sessions it answers instead of acting.

**Per-kind behavior:**

| Kind | tmux pane? | Durable? | Badge | `D` does |
|---|---|---|---|---|
| supervisor-bg | no | yes (daemon-owned) | — | toast: `already durable — background sessions survive terminal loss · o opens it in tmux` |
| supervisor-bg | yes | yes | `⧉` | toast: `already durable — daemon-owned and visible in tmux (dev:1.2)` |
| supervisor-interactive | yes | yes | `⧉` | toast: `already durable — lives in tmux (dev:1.2)` |
| **supervisor-interactive** | **no** | **NO** | **`⌁bare`** | **opens the sheet below** |
| tmux-interactive | always | yes | `⧉` | toast: `already durable — lives in tmux (dev:1.2)` |

**The sheet** (centered, list dimmed behind; exact copy):

```
┌ Make durable — auth-flow refactor ──────────────────────────────┐
│ This session runs in a bare terminal. If that terminal or its   │
│ SSH connection dies, the session dies with it.                  │
│                                                                 │
│  t   continue in tmux  (recommended)                            │
│      Opens a tmux window running `claude --resume` on this      │
│      conversation. Durable from then on. The original terminal  │
│      keeps a live copy of the old session — exit it when you    │
│      get back to it, and don't type into both.                  │
│                                                                 │
│  b   background it from the inside                              │
│      No external command can background a bare interactive      │
│      session. Attach now (this key does it) and press ← or      │
│      type /bg inside — it becomes a daemon-owned background     │
│      session and survives on its own.                           │
│                                                                 │
│  esc cancel                                                     │
└─────────────────────────────────────────────────────────────────┘
```

- `t` runs open-in-tmux with `claude --resume <SessionID>` as the window
  command, then toasts `durable copy opened in tmux (dev:3.1) — original
  terminal is now stale`. It does **not** auto-switch (parking, not jumping).
- `b` immediately performs the normal `enter` attach so the user is *inside*
  the session, one `←` from done.
- The copy never pretends. When Claude ships a first-class external
  backgrounding API (v2 watch item), option b becomes a single silent action.

---

## 7. First-run and day-2

**Minute one.** `clauditor`. No config, no wizard. Either the fleet appears
(yellow on top, footer showing exactly the 6 things worth pressing) or the
welcoming empty state teaches the two ways to make a session exist. The
first blocked session makes `r reply` glow once. That is the entire
tutorial; there is no tour to dismiss.

**Day 2.** The user has learned three keys without noticing: `enter` (go
there), `r` (answer it), `q` (leave). `?` fills gaps in context.

**Day 30 — the power user's loop:**
`clauditor` → glance (yellow count) → `g` (top = most blocked) → `r`, type
answer, `enter` → `j` `j` → `enter` to attach where judgment is needed,
detach, cockpit is back → `d`, type a task, `enter` → sees `⌁bare` on a
session started from a laptop terminal → `D` `t` → `q`. Ten seconds of
attention triage, zero mouse, zero window hunting. With v1.1: `n` starts new
work and `h` resurrects yesterday's conversation — at which point there is
no remaining reason to leave.

---

## 8. Phased roadmap

Each phase ships whole and alone.

**v1 — this PR: the trustworthy cockpit.**
Everything shipped today, plus: `?` overlay · durability badge + `D` + sheet
· contextual 6-hint footer · empty/first-run/stale states with the exact
copy above · collector-failure header segment · `1–4` direct state filters ·
`esc` clear-chain (never quits) · `g/G`, `ctrl+d/u` · row anatomy cleanup
(drop state word + id) · list-width clamp(38, 42%, 64) · preview caption
source honesty · `humanDur` gains days (`3d`). Bar: a stranger answers
"what needs me?" in 3 seconds and can make any session durable in 2 keys.

**v1.1 — the only tool you open.**
- `n` **new interactive session**: creates/uses a tmux session named after
  the selected repo, new window running `claude` in that worktree,
  switch-client (workmux's adopt-and-switch recipe). Without a selection,
  `n` first asks for a repo. Session naming follows sesh's convention
  (git-repo-derived names) so clauditor-created sessions coexist cleanly
  with a sesh/choose-tree navigation stack.
- `h` **resume picker**: fullscreen list of recent conversations (from
  supervisor + `~/.claude` history), newest first, same row language;
  `enter` = resume in a tmux window, `esc` back.
- `:` **command palette** (k9s-style, bottom line, fuzzy): `:needs :working
  :idle :done` (filters), `:resume` (= h), `:health`, `:help :q`.
- Mouse (bubbletea native): click selects, double-click attaches, wheel
  scrolls — never required.
- Reply pre-labeling from waitingFor classification (`reply (free text)` vs
  `choice 1–5`; permission prompts refuse with `attach to answer —
  permission prompts deserve eyes`, offering enter).

**v1.2 — polish that compounds.**
`i` inspect (detail overlay with copyable attach line) · `c` copy attach
command via OSC52 · state-transition row flash (150ms) · filter/selection
persistence across launches · flat result view when filtering · toast queue.

**v2 — the fleet workshop.**
- `N` **new task** = workmux-grade flow: repo → branch → worktree → tmux
  window → interactive `claude` with the task prompt injected. The
  interactive twin of `d`.
- **Finish flow**: on done sessions in claude-managed worktrees, `M`
  merge-and-clean (always confirmed, never automatic).
- Config-tunable keymap + skins — only now, once defaults have earned loyalty.
- First-class backgrounding: if/when Claude Code exposes an external `/bg`
  API, `D` option b becomes one silent action.
- Optional tmux status-line agent icons (opt-in).

---

## 9. Acceptance bar — every future TUI change must pass all ten

1. Renders correctly at 80×20, 100×30, 140×40 (capture-pane verified); no
   torn glyphs, no mid-rune wraps, no horizontal overflow.
2. A newcomer answers "what needs me?" within 3 seconds of open, reading
   nothing but the screen.
3. Every action appears in `?` and appears contextually in the footer; the
   footer never exceeds 6 hints + `? help`.
4. Color semantics hold: yellow only ever means needs-input; red is never
   hue-only; accent never encodes state.
5. No keystroke path blocks the event loop; feedback within one frame;
   >100ms work shows a spinner where the result will land.
6. `esc` never quits; `q` quits instantly and asks nothing.
7. Every destructive or consequential action names its object and
   consequence in its confirm; every confirm accepts with a single key.
8. Degradation is honest: staleness visible, collector failure visible,
   unknown states say `unknown`, fragile sessions wear `⌁bare` — the screen
   never claims more than the collectors know.
9. The frozen keys (`enter r d x q`) keep their meaning; new features spend
   reserved keys (`n N h i :`) or the palette, never a frozen one.
10. Every error string names the next action; no error is a dead end, and
    the cockpit never exits on a collector failure.

---

## 10. Ecosystem positioning (2026-08-09)

The tmux-agent-monitoring space converged fast (tmuxpulse, mux, NTM; sesh
and choose-tree for navigation). Positioning rules derived from surveying
them:

- **Never compete on navigation.** sesh + choose-tree own "take me
  somewhere." Clauditor documents the `display-popup -E` binding (README)
  so the cockpit overlays any workflow, and its `n` flow adopts sesh-style
  git-derived session names rather than inventing a scheme.
- **The load-bearing differentiators** — defend and deepen these, cut
  anything that doesn't serve them: (1) supervisor truth — state and
  `waitingFor` read from Claude Code itself, not inferred from pane-output
  hashing like tmuxpulse/mux, which cannot distinguish "quiet, thinking"
  from "quiet, waiting on you"; (2) reply-without-attach; (3) the SSE/
  mobile layer, which none of the surveyed tools have.
- **Technique to adopt in v2b:** tmuxpulse's render economics — hash each
  capture (FNV), skip re-render when unchanged — composes with the
  control-mode event push for near-zero idle cost.

---
*Where this spec and the code disagree, this spec wins; where this spec and
a user's 3-second glance disagree, the glance wins.*
