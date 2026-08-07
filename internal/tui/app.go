package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/rishi/clauditor/internal/actions"
	"github.com/rishi/clauditor/internal/config"
	"github.com/rishi/clauditor/internal/model"
)

const (
	refreshInterval = 3 * time.Second        // snapshot poll
	previewInterval = 2 * time.Second        // live-preview poll (separate tick)
	spinnerInterval = 150 * time.Millisecond // header spinner while any session works
	fetchTimeout    = 4 * time.Second
)

// mode is which input the keyboard is currently routed to.
type mode int

const (
	modeList mode = iota
	modeFilter
	modeDispatch
	modeReply
	modeConfirmStop
	modeLogs
	modeHelp    // `?` help overlay
	modeDurable // make-durable sheet for a bare session
)

// --- tea.Msg types ----------------------------------------------------------

type tickMsg struct{}
type previewTickMsg struct{}
type spinnerTickMsg struct{}

type snapMsg struct {
	snap *model.Snapshot
	err  error
}

type logsMsg struct {
	title string
	text  string
	err   error
}

// previewMsg carries a preview fetch result. key is the session it was fetched
// for, so a result arriving after the selection moved is ignored on render.
type previewMsg struct {
	key  string
	text string
	err  error
	at   time.Time
}

type actionDoneMsg struct {
	text string
	err  error
}

// attachDoneMsg is delivered after an interactive attach (ExecProcess) returns.
type attachDoneMsg struct {
	err error
}

// --- Model -------------------------------------------------------------------

// Model is the bubbletea model for the cockpit: a session list (left) with a
// live-preview pane (right) on wide terminals, or a full-width list with a
// toggleable full-screen preview on narrow ones. Modal-line overlays handle
// filter/dispatch/reply/confirm; a full-screen pager handles logs.
type Model struct {
	ctx     context.Context
	source  Source
	actionC ActionClient
	inTmux  bool

	snap      *model.Snapshot
	lastFetch time.Time
	fetchErr  error

	query       string
	queryPrev   string
	stateFilter StateFilter
	rows        []Row
	cursor      int

	mode mode

	filterInput   textinput.Model
	dispatchInput textinput.Model
	replyInput    textinput.Model
	confirmSess   *model.Session

	logsVP    viewport.Model
	logsTitle string
	logsErr   error

	durableSess *model.Session // session the make-durable sheet is acting on

	// first-blocked flash: r reply renders in accent for the one poll cycle
	// after a blocked session first appears.
	everBlocked  bool
	blockedFlash bool

	// live preview
	showPreview bool // narrow-mode full-screen preview toggle
	previewKey  string
	previewText string
	previewErr  error
	previewAt   time.Time

	// header spinner
	spinnerOn    bool
	spinnerFrame int

	statusMsg string
	statusErr bool

	width, height int
	quitting      bool
}

// NewModel wires a fresh cockpit model around the given source/actionC. inTmux
// controls whether attach uses `tmux switch-client` (already inside tmux) vs
// spawning `tmux attach` for a tmux-pane session.
func NewModel(ctx context.Context, source Source, actionC ActionClient) Model {
	fi := textinput.New()
	fi.Placeholder = "filter (substring)…"
	di := textinput.New()
	di.Placeholder = "dispatch prompt…"
	ri := textinput.New()
	ri.Placeholder = "reply…"
	return Model{
		ctx:           ctx,
		source:        source,
		actionC:       actionC,
		inTmux:        os.Getenv("TMUX") != "",
		filterInput:   fi,
		dispatchInput: di,
		replyInput:    ri,
		logsVP:        viewport.New(80, 20),
		cursor:        -1,
	}
}

// Run starts the cockpit (blocking until the user quits or ctx is canceled).
func Run(ctx context.Context, cfg *config.Config) error {
	source, err := DetectSource(ctx, cfg, 1500*time.Millisecond)
	if err != nil {
		return err
	}
	var actionC ActionClient
	if d, ok := source.(*daemonSource); ok {
		actionC = newDaemonActionClient(d)
	} else {
		actionC = newLocalActionClient(cfg)
	}
	m := NewModel(ctx, source, actionC)
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithAltScreen())
	_, err = p.Run()
	return err
}

// --- tea.Model ---------------------------------------------------------------

func (m Model) Init() tea.Cmd {
	return tea.Batch(fetchCmd(m.ctx, m.source), tickCmd(), previewTickCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

func previewTickCmd() tea.Cmd {
	return tea.Tick(previewInterval, func(time.Time) tea.Msg { return previewTickMsg{} })
}

func spinnerTickCmd() tea.Cmd {
	return tea.Tick(spinnerInterval, func(time.Time) tea.Msg { return spinnerTickMsg{} })
}

func fetchCmd(ctx context.Context, source Source) tea.Cmd {
	return func() tea.Msg {
		fctx, cancel := context.WithTimeout(ctx, fetchTimeout)
		defer cancel()
		snap, err := source.Fetch(fctx)
		return snapMsg{snap: snap, err: err}
	}
}

func logsCmd(ctx context.Context, source Source, sess *model.Session) tea.Cmd {
	return func() tea.Msg {
		lf, ok := source.(LogFetcher)
		if !ok {
			return logsMsg{title: displayName(sess), err: fmt.Errorf("logs unavailable for this data source")}
		}
		fctx, cancel := context.WithTimeout(ctx, fetchTimeout)
		defer cancel()
		text, err := lf.FetchLogs(fctx, sess, 500)
		return logsMsg{title: displayName(sess), text: text, err: err}
	}
}

func previewFetch(ctx context.Context, source Source, sess *model.Session, lines int) tea.Cmd {
	key := sess.Key
	return func() tea.Msg {
		pf, ok := source.(PreviewFetcher)
		if !ok {
			return previewMsg{key: key, err: fmt.Errorf("preview unavailable for this data source"), at: time.Now()}
		}
		fctx, cancel := context.WithTimeout(ctx, fetchTimeout)
		defer cancel()
		text, err := pf.FetchPreview(fctx, sess, lines)
		return previewMsg{key: key, text: lastNLines(text, lines), err: err, at: time.Now()}
	}
}

// previewRefreshCmd fetches the preview for the current selection, or nil when
// the preview isn't visible / nothing is selected.
func (m Model) previewRefreshCmd() tea.Cmd {
	if !m.previewVisible() {
		return nil
	}
	sess := m.selectedSession()
	if sess == nil {
		return nil
	}
	return previewFetch(m.ctx, m.source, sess, m.previewBudget())
}

func (m Model) previewVisible() bool { return wideLayout(m.width) || m.showPreview }

// previewBudget is how many content lines the preview pane can show.
func (m Model) previewBudget() int {
	if m.height <= 0 {
		return 200
	}
	n := bodyHeight(m.height) - 1 // minus the caption line
	if n < 1 {
		n = 1
	}
	return n
}

func openInTmuxCmd(ctx context.Context, actionC ActionClient, sess *model.Session) tea.Cmd {
	return func() tea.Msg {
		fctx, cancel := context.WithTimeout(ctx, fetchTimeout)
		defer cancel()
		res, err := actionC.OpenInTmux(fctx, sess)
		if err != nil {
			return actionDoneMsg{err: fmt.Errorf("open-in-tmux: %w", err)}
		}
		return actionDoneMsg{text: "opened in tmux: " + res.Target + " — " + res.Attach}
	}
}

func switchClientCmd(ctx context.Context, target string) tea.Cmd {
	return func() tea.Msg {
		fctx, cancel := context.WithTimeout(ctx, fetchTimeout)
		defer cancel()
		if err := SwitchTmuxClient(fctx, target); err != nil {
			return actionDoneMsg{err: fmt.Errorf("switch-client: %w", err)}
		}
		return actionDoneMsg{text: "switched to " + target}
	}
}

// attachClaudeCmd suspends the cockpit and execs `claude attach <id>`,
// resuming when the user detaches. Not ctx-bound: attach is long-lived and
// interactive, and the program is suspended for its duration.
func attachClaudeCmd(sess *model.Session) tea.Cmd {
	c := exec.Command("claude", "attach", sess.ID) //nolint:gosec // fixed argv, id validated by supervisor
	return tea.ExecProcess(c, func(err error) tea.Msg { return attachDoneMsg{err: err} })
}

// attachTmuxCmd suspends the cockpit and execs `tmux attach -t <session>`
// (used when the cockpit is NOT already inside tmux).
func attachTmuxCmd(session string) tea.Cmd {
	c := exec.Command("tmux", "attach", "-t", session) //nolint:gosec // fixed argv
	return tea.ExecProcess(c, func(err error) tea.Msg { return attachDoneMsg{err: err} })
}

const hiddenTmuxSession = "clauditor"

// openResumeInTmuxCmd opens a new window in the hidden `clauditor` tmux session
// running `claude --resume <sessionId>` for a bare session, WITHOUT switching to
// it (§6 `t`: park a durable copy, don't jump to it). Like SwitchTmuxClient it
// execs tmux directly, because make-durable acts on the user's own tmux server
// on the same box as the cockpit rather than through the daemon.
func openResumeInTmuxCmd(ctx context.Context, sess *model.Session) tea.Cmd {
	return func() tea.Msg {
		if sess == nil || sess.SessionID == "" {
			return actionDoneMsg{err: fmt.Errorf("make durable: session has no resumable id")}
		}
		if !actions.ValidSessionID(sess.SessionID) {
			return actionDoneMsg{err: fmt.Errorf("make durable: session id has unexpected format")}
		}
		fctx, cancel := context.WithTimeout(ctx, fetchTimeout)
		defer cancel()
		target, err := openResumeWindow(fctx, sess)
		if err != nil {
			return actionDoneMsg{err: fmt.Errorf("make durable: %w", err)}
		}
		return actionDoneMsg{text: fmt.Sprintf("durable copy opened in tmux (%s) — original terminal is now stale", target)}
	}
}

// openResumeWindow ensures the hidden clauditor tmux session exists, then opens
// a window running `claude --resume <sessionId>` in the session's cwd. Argv
// arrays only; the session id is validated by the caller.
func openResumeWindow(ctx context.Context, sess *model.Session) (string, error) {
	if err := exec.CommandContext(ctx, "tmux", "has-session", "-t", hiddenTmuxSession).Run(); err != nil {
		if out, err := exec.CommandContext(ctx, "tmux", "new-session", "-d", "-s", hiddenTmuxSession).CombinedOutput(); err != nil {
			return "", fmt.Errorf("tmux new-session: %v: %s", err, strings.TrimSpace(string(out)))
		}
	}
	args := []string{"new-window", "-t", hiddenTmuxSession, "-n", safeWindowName(sess), "-P", "-F", "#{session_name}:#{window_index}"}
	if sess.CWD != "" {
		args = append(args, "-c", sess.CWD)
	}
	args = append(args, "claude --resume "+sess.SessionID)
	out, err := exec.CommandContext(ctx, "tmux", args...).CombinedOutput() //nolint:gosec // fixed argv; sessionId validated, cwd from supervisor
	if err != nil {
		return "", fmt.Errorf("tmux new-window: %v: %s", err, strings.TrimSpace(string(out)))
	}
	target := strings.TrimSpace(string(out))
	if target == "" {
		target = hiddenTmuxSession
	}
	return target, nil
}

// safeWindowName sanitizes a session name into a tmux window name (mirrors
// actions.shortWindowName, which is unexported).
func safeWindowName(s *model.Session) string {
	n := s.Name
	if n == "" {
		n = s.SessionID
	}
	n = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, n)
	if len(n) > 20 {
		n = n[:20]
	}
	if n == "" {
		n = "session"
	}
	return n
}

func stopCmd(ctx context.Context, actionC ActionClient, sess *model.Session) tea.Cmd {
	return func() tea.Msg {
		fctx, cancel := context.WithTimeout(ctx, fetchTimeout)
		defer cancel()
		if err := actionC.Stop(fctx, sess); err != nil {
			return actionDoneMsg{err: fmt.Errorf("stop: %w", err)}
		}
		return actionDoneMsg{text: "stopped " + displayName(sess)}
	}
}

func respawnCmd(ctx context.Context, actionC ActionClient, sess *model.Session) tea.Cmd {
	return func() tea.Msg {
		fctx, cancel := context.WithTimeout(ctx, fetchTimeout)
		defer cancel()
		if err := actionC.Respawn(fctx, sess); err != nil {
			return actionDoneMsg{err: fmt.Errorf("respawn: %w", err)}
		}
		return actionDoneMsg{text: "respawned " + displayName(sess)}
	}
}

func replyCmd(ctx context.Context, actionC ActionClient, sess *model.Session, text string) tea.Cmd {
	return func() tea.Msg {
		fctx, cancel := context.WithTimeout(ctx, fetchTimeout)
		defer cancel()
		if err := actionC.Reply(fctx, sess, text); err != nil {
			return actionDoneMsg{err: fmt.Errorf("%s", humanReplyErr(err))}
		}
		return actionDoneMsg{text: "reply sent to " + displayName(sess)}
	}
}

func dispatchCmd(ctx context.Context, actionC ActionClient, snap *model.Snapshot, sess *model.Session, prompt string) tea.Cmd {
	return func() tea.Msg {
		fctx, cancel := context.WithTimeout(ctx, fetchTimeout)
		defer cancel()
		req := actions.DispatchRequest{Prompt: prompt}
		if sess != nil {
			req.Target = actions.DispatchTarget{Repo: sess.Repo, Worktree: sess.Worktree}
		}
		res, err := actionC.Dispatch(fctx, snap, req)
		if err != nil {
			return actionDoneMsg{err: fmt.Errorf("dispatch: %w", err)}
		}
		msg := "dispatched in " + res.Dir
		if res.ShortID != "" {
			msg += " · " + res.ShortID
		}
		return actionDoneMsg{text: msg}
	}
}

func (m Model) selectedSession() *model.Session {
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		return m.rows[m.cursor].Session
	}
	return nil
}

func (m *Model) rebuildRows() {
	m.rows = BuildRows(m.snap, m.query, m.stateFilter)
	m.cursor = ClampCursor(m.rows, m.cursor)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.logsVP.Width = msg.Width
		m.logsVP.Height = bodyHeight(msg.Height)
		return m, m.previewRefreshCmd()

	case tickMsg:
		return m, tea.Batch(fetchCmd(m.ctx, m.source), tickCmd())

	case previewTickMsg:
		cmds := []tea.Cmd{previewTickCmd()}
		if c := m.previewRefreshCmd(); c != nil {
			cmds = append(cmds, c)
		}
		return m, tea.Batch(cmds...)

	case spinnerTickMsg:
		if !m.spinnerOn {
			return m, nil
		}
		m.spinnerFrame++
		if !anyWorking(m.snap) {
			m.spinnerOn = false
			return m, nil
		}
		return m, spinnerTickCmd()

	case snapMsg:
		m.fetchErr = msg.err
		if msg.err == nil {
			m.snap = msg.snap
			m.lastFetch = time.Now()
		}
		prevSel := ""
		if s := m.selectedSession(); s != nil {
			prevSel = s.Key
		}
		m.rebuildRows()
		// First-blocked flash: accent the `r reply` hint for exactly the cycle
		// a blocked session first appears.
		if anyBlocked(m.snap) {
			m.blockedFlash = !m.everBlocked
			m.everBlocked = true
		} else {
			m.blockedFlash = false
		}
		var cmds []tea.Cmd
		// Start the spinner when work appears; it self-stops when work ends.
		if anyWorking(m.snap) && !m.spinnerOn {
			m.spinnerOn = true
			cmds = append(cmds, spinnerTickCmd())
		}
		// Fetch a preview when the selection changed or none is loaded yet.
		if sel := m.selectedSession(); sel != nil && sel.Key != prevSel && m.previewVisible() {
			if c := m.previewRefreshCmd(); c != nil {
				cmds = append(cmds, c)
			}
		} else if m.previewKey == "" {
			if c := m.previewRefreshCmd(); c != nil {
				cmds = append(cmds, c)
			}
		}
		return m, tea.Batch(cmds...)

	case previewMsg:
		m.previewKey = msg.key
		m.previewText = msg.text
		m.previewErr = msg.err
		m.previewAt = msg.at
		return m, nil

	case logsMsg:
		m.logsErr = msg.err
		m.logsTitle = msg.title
		m.logsVP.SetContent(msg.text)
		m.logsVP.GotoTop()
		return m, nil

	case attachDoneMsg:
		if msg.err != nil {
			m.statusMsg, m.statusErr = "attach: "+msg.err.Error(), true
		}
		return m, tea.Batch(fetchCmd(m.ctx, m.source), m.previewRefreshCmd())

	case actionDoneMsg:
		if msg.err != nil {
			m.statusMsg, m.statusErr = msg.err.Error(), true
		} else {
			m.statusMsg, m.statusErr = msg.text, false
		}
		return m, fetchCmd(m.ctx, m.source) // refresh promptly after a mutation

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// bodyHeight is the number of rows available to the body (header + status +
// footer take one line each).
func bodyHeight(total int) int {
	h := total - 3
	if h < 1 {
		h = 1
	}
	return h
}

func anyWorking(snap *model.Snapshot) bool {
	if snap == nil {
		return false
	}
	for _, s := range snap.Sessions {
		if s.State == model.StateWorking {
			return true
		}
	}
	return false
}

func anyBlocked(snap *model.Snapshot) bool {
	if snap == nil {
		return false
	}
	for _, s := range snap.Sessions {
		if s.NeedsInput() {
			return true
		}
	}
	return false
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeFilter:
		return m.handleFilterKey(msg)
	case modeDispatch:
		return m.handleDispatchKey(msg)
	case modeReply:
		return m.handleReplyKey(msg)
	case modeConfirmStop:
		return m.handleConfirmKey(msg)
	case modeLogs:
		return m.handleLogsKey(msg)
	case modeHelp:
		return m.handleHelpKey(msg)
	case modeDurable:
		return m.handleDurableKey(msg)
	default:
		return m.handleListKey(msg)
	}
}

func (m Model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit
	case "j", "down":
		if idx := NextSelectable(m.rows, m.cursor); idx >= 0 {
			m.cursor = idx
		}
		return m, m.previewRefreshCmd()
	case "k", "up":
		if idx := PrevSelectable(m.rows, m.cursor); idx >= 0 {
			m.cursor = idx
		}
		return m, m.previewRefreshCmd()
	case "g":
		if idx := FirstSelectable(m.rows); idx >= 0 {
			m.cursor = idx
		}
		return m, m.previewRefreshCmd()
	case "G":
		if idx := PrevSelectable(m.rows, 0); idx >= 0 { // wraps backward → last
			m.cursor = idx
		}
		return m, m.previewRefreshCmd()
	case "ctrl+d":
		return m.halfPage(true)
	case "ctrl+u":
		return m.halfPage(false)
	case "?":
		m.mode = modeHelp
		return m, nil
	case "esc":
		return m.escClear()
	case "1":
		return m.applyStateFilter(FilterNeeds)
	case "2":
		return m.applyStateFilter(FilterWorking)
	case "3":
		return m.applyStateFilter(FilterIdle)
	case "4":
		return m.applyStateFilter(FilterTerminal)
	case "/":
		m.mode = modeFilter
		m.queryPrev = m.query
		m.filterInput.SetValue(m.query)
		m.filterInput.CursorEnd()
		return m, m.filterInput.Focus()
	case "s":
		m.stateFilter = m.stateFilter.Next()
		m.rebuildRows()
		return m, m.previewRefreshCmd()
	case "n", "N", "h", "i", ":":
		// Reserved for v1.1 (new session / new task / resume / inspect /
		// palette). Swallowed in v1 so the features land without stealing a key.
		return m, nil
	case "tab":
		if !wideLayout(m.width) {
			m.showPreview = !m.showPreview
			if m.showPreview {
				return m, m.previewRefreshCmd()
			}
		}
		return m, nil
	case "enter":
		sess := m.selectedSession()
		if sess == nil {
			m.statusMsg, m.statusErr = "no session selected", true
			return m, nil
		}
		return m.attach(sess)
	case "o":
		sess := m.selectedSession()
		if sess == nil {
			m.statusMsg, m.statusErr = "no session selected", true
			return m, nil
		}
		return m, openInTmuxCmd(m.ctx, m.actionC, sess)
	case "r":
		sess := m.selectedSession()
		if !replyEnabled(sess) {
			m.statusMsg, m.statusErr = "reply needs a session waiting on input with a background id (attach for others)", true
			return m, nil
		}
		m.mode = modeReply
		m.replyInput.SetValue("")
		return m, m.replyInput.Focus()
	case "l":
		sess := m.selectedSession()
		if sess == nil {
			m.statusMsg, m.statusErr = "no session selected", true
			return m, nil
		}
		m.mode = modeLogs
		m.logsErr = nil
		m.logsVP.SetContent("loading…")
		return m, logsCmd(m.ctx, m.source, sess)
	case "d":
		m.mode = modeDispatch
		m.dispatchInput.SetValue("")
		return m, m.dispatchInput.Focus()
	case "x":
		sess := m.selectedSession()
		if sess == nil {
			m.statusMsg, m.statusErr = "no session selected", true
			return m, nil
		}
		if sess.ID == "" {
			m.statusMsg, m.statusErr = "session has no background id — can't stop from here", true
			return m, nil
		}
		m.mode = modeConfirmStop
		m.confirmSess = sess
		return m, nil
	case "R":
		sess := m.selectedSession()
		if !respawnEnabled(sess) {
			m.statusMsg, m.statusErr = "respawn only applies to stopped/failed sessions with a background id", true
			return m, nil
		}
		return m, respawnCmd(m.ctx, m.actionC, sess)
	case "D":
		sess := m.selectedSession()
		if sess == nil {
			m.statusMsg, m.statusErr = "no session selected", true
			return m, nil
		}
		toast, openSheet := durabilityAction(sess)
		if openSheet {
			m.mode = modeDurable
			m.durableSess = sess
			return m, nil
		}
		m.statusMsg, m.statusErr = toast, false
		return m, nil
	}
	return m, nil
}

// halfPage moves the cursor a half-body of selectable rows (ctrl+d/ctrl+u),
// clamping at the ends rather than wrapping.
func (m Model) halfPage(down bool) (tea.Model, tea.Cmd) {
	step := bodyHeight(m.height) / 2
	if step < 1 {
		step = 1
	}
	for i := 0; i < step; i++ {
		if down {
			idx := NextSelectable(m.rows, m.cursor)
			if idx <= m.cursor { // wrapped or none: at the bottom
				break
			}
			m.cursor = idx
		} else {
			idx := PrevSelectable(m.rows, m.cursor)
			if idx < 0 || idx >= m.cursor { // wrapped or none: at the top
				break
			}
			m.cursor = idx
		}
	}
	return m, m.previewRefreshCmd()
}

// applyStateFilter sets the 1–4 direct state filter, or clears it when the same
// bucket is pressed again (§4).
func (m Model) applyStateFilter(f StateFilter) (tea.Model, tea.Cmd) {
	if m.stateFilter == f {
		m.stateFilter = FilterAll
	} else {
		m.stateFilter = f
	}
	m.rebuildRows()
	return m, m.previewRefreshCmd()
}

// escClear is the list-mode esc chain: text filter → state filter → nothing.
// esc never quits (overlays are cleared in their own handlers).
func (m Model) escClear() (tea.Model, tea.Cmd) {
	switch {
	case m.query != "":
		m.query, m.queryPrev = "", ""
		m.rebuildRows()
		return m, m.previewRefreshCmd()
	case m.stateFilter != FilterAll:
		m.stateFilter = FilterAll
		m.rebuildRows()
		return m, m.previewRefreshCmd()
	default:
		return m, nil
	}
}

// handleHelpKey: esc or ? closes the help overlay; everything else is swallowed.
func (m Model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "?", "q":
		m.mode = modeList
	}
	return m, nil
}

// handleDurableKey drives the make-durable sheet (§6): t continues in tmux
// (parked, not switched), b performs the normal attach, esc cancels.
func (m Model) handleDurableKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	sess := m.durableSess
	switch msg.String() {
	case "t":
		m.mode = modeList
		m.durableSess = nil
		return m, openResumeInTmuxCmd(m.ctx, sess)
	case "b":
		m.mode = modeList
		m.durableSess = nil
		if sess == nil {
			return m, nil
		}
		return m.attach(sess)
	case "esc":
		m.mode = modeList
		m.durableSess = nil
	}
	return m, nil
}

// attach implements the `enter` action: claude-attach for supervisor sessions
// with an id; for tmux-pane sessions, switch-client when already inside tmux,
// else spawn `tmux attach`.
func (m Model) attach(sess *model.Session) (tea.Model, tea.Cmd) {
	switch {
	case sess.ID != "":
		return m, attachClaudeCmd(sess)
	case sess.TmuxPaneID != "":
		if m.inTmux {
			return m, switchClientCmd(m.ctx, sess.TmuxTarget)
		}
		return m, attachTmuxCmd(tmuxSessionName(sess))
	default:
		m.statusMsg, m.statusErr = "nothing to attach to (no background id or tmux pane)", true
		return m, nil
	}
}

func (m Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.query = m.queryPrev
		m.mode = modeList
		m.filterInput.Blur()
		m.rebuildRows()
		return m, nil
	case "enter":
		m.query = m.filterInput.Value()
		m.mode = modeList
		m.filterInput.Blur()
		m.rebuildRows()
		return m, m.previewRefreshCmd()
	}
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	m.query = m.filterInput.Value()
	m.rebuildRows()
	return m, cmd
}

func (m Model) handleDispatchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.dispatchInput.Blur()
		return m, nil
	case "enter":
		prompt := strings.TrimSpace(m.dispatchInput.Value())
		m.mode = modeList
		m.dispatchInput.Blur()
		if prompt == "" {
			m.statusMsg, m.statusErr = "dispatch canceled: empty prompt", true
			return m, nil
		}
		return m, dispatchCmd(m.ctx, m.actionC, m.snap, m.selectedSession(), prompt)
	}
	var cmd tea.Cmd
	m.dispatchInput, cmd = m.dispatchInput.Update(msg)
	return m, cmd
}

func (m Model) handleReplyKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.replyInput.Blur()
		return m, nil
	case "enter":
		text := strings.TrimSpace(m.replyInput.Value())
		sess := m.selectedSession()
		m.mode = modeList
		m.replyInput.Blur()
		if text == "" {
			m.statusMsg, m.statusErr = "reply canceled: empty text", true
			return m, nil
		}
		if !replyEnabled(sess) {
			m.statusMsg, m.statusErr = "selection no longer accepts a reply", true
			return m, nil
		}
		return m, replyCmd(m.ctx, m.actionC, sess, text)
	}
	var cmd tea.Cmd
	m.replyInput, cmd = m.replyInput.Update(msg)
	return m, cmd
}

func (m Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		sess := m.confirmSess
		m.mode = modeList
		m.confirmSess = nil
		return m, stopCmd(m.ctx, m.actionC, sess)
	case "n", "esc":
		m.mode = modeList
		m.confirmSess = nil
		return m, nil
	}
	return m, nil
}

func (m Model) handleLogsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		m.mode = modeList
		return m, nil
	}
	var cmd tea.Cmd
	m.logsVP, cmd = m.logsVP.Update(msg)
	return m, cmd
}

// --- View --------------------------------------------------------------------

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	width := m.width
	if width <= 0 {
		width = 100
	}

	// The `?` overlay is a full-screen takeover (dim scrim + key crib).
	if m.mode == modeHelp {
		var ages map[string]int64
		if m.snap != nil {
			ages = m.snap.CollectorAges
		}
		return strings.Join(helpLines(width, max0(m.height), ages, m.source.Label()), "\n")
	}

	header := HeaderText(m.snap, m.source.Label(), m.lastFetch, time.Now(), m.stateFilter, m.query, m.spinnerGlyph())
	bodyH := bodyHeight(m.height)

	var body, footer string
	switch m.mode {
	case modeLogs:
		body = m.viewLogs()
		footer = FooterText(footerLogs)
	case modeDurable:
		bg := m.listLinesPlain(width, bodyH)
		body = strings.Join(overlayCenter(bg, makeDurableSheet(m.durableSess), sheetWidth, width, bodyH), "\n")
		footer = styleFooterBar.Render("t continue in tmux · b background it · esc cancel")
	case modeFilter, modeDispatch, modeReply, modeConfirmStop:
		body = m.viewBody(width, bodyH)
		footer = FooterText(footerInput)
	default:
		body = m.viewBody(width, bodyH)
		if !wideLayout(width) && m.showPreview {
			footer = FooterText(footerPreview)
		} else {
			footer = styleFooterList(footerForSelection(m.selectedSession()), m.blockedFlash)
		}
	}

	return strings.Join([]string{header, body, m.viewStatusLine(), footer}, "\n")
}

func (m Model) spinnerGlyph() string {
	if !m.spinnerOn {
		return ""
	}
	return spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
}

// viewBody chooses the layout: split (wide), full-screen preview (narrow +
// toggled), or the full-width list (narrow default).
func (m Model) viewBody(width, height int) string {
	if !wideLayout(width) && m.showPreview {
		return strings.Join(m.previewLines(width, height), "\n")
	}
	if wideLayout(width) {
		return m.viewSplit(width, height)
	}
	return m.viewList(width, height)
}

func (m Model) viewSplit(width, height int) string {
	const sep = " │ "
	sepW := len([]rune(sep))
	// List width = clamp(38, 42% of width, 64): below 38 the row anatomy can't
	// hold a name + waitingFor; above 64 a session row is pure padding while the
	// preview — the pane that shows actual work — starves (§5).
	listW := width * 42 / 100
	if listW < 38 {
		listW = 38
	}
	if listW > 64 {
		listW = 64
	}
	previewW := width - listW - sepW
	if previewW < 16 { // too cramped to split — fall back to list only
		return m.viewList(width, height)
	}
	left := m.listLines(listW, height)
	right := m.previewLines(previewW, height)
	var b strings.Builder
	for i := 0; i < height; i++ {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(left[i])
		b.WriteString(styleSep.Render(sep))
		b.WriteString(right[i])
	}
	return b.String()
}

// listLines renders the session list as exactly height padded lines, or the
// appropriate empty/first-run/no-matches state (§5) when there are no rows.
func (m Model) listLines(width, height int) []string {
	if len(m.rows) == 0 {
		return centerLines(m.emptyState(width), width, height)
	}
	out := make([]string, 0, height)
	start, end := VisibleWindow(len(m.rows), max0(m.cursor), height)
	for i := start; i < end; i++ {
		out = append(out, RenderRow(m.rows[i], width, i == m.cursor))
	}
	for len(out) < height {
		out = append(out, padTrunc("", width))
	}
	return out[:height]
}

// listLinesPlain renders the list as plain (unstyled) width-padded lines, used
// as the dimmable background behind the make-durable sheet.
func (m Model) listLinesPlain(width, height int) []string {
	out := make([]string, 0, height)
	if len(m.rows) > 0 {
		start, end := VisibleWindow(len(m.rows), max0(m.cursor), height)
		for i := start; i < end; i++ {
			out = append(out, padTrunc(rowLine(m.rows[i], width, i == m.cursor), width))
		}
	}
	for len(out) < height {
		out = append(out, strings.Repeat(" ", max0(width)))
	}
	return out[:max0(height)]
}

// emptyState returns the styled, horizontally-centered lines for whichever
// no-rows condition holds (§5, exact copy).
func (m Model) emptyState(width int) []string {
	switch {
	case m.snap == nil: // first fetch not back yet
		return []string{styleDim.Render(center(m.spinnerGlyph()+" connecting to the Claude supervisor…", width))}
	case len(m.snap.Sessions) > 0: // sessions exist, filter/query hides them
		msg := fmt.Sprintf("no %s sessions — esc clears", m.stateFilter.Label())
		if m.query != "" {
			msg = fmt.Sprintf("no matches for %q — esc clears", m.query)
		}
		return []string{styleDim.Render(center(msg, width))}
	}
	// No sessions anywhere: the welcoming empty state.
	scan := "just now"
	if !m.lastFetch.IsZero() {
		scan = humanDur(time.Since(m.lastFetch)) + " ago"
	}
	lines := []string{
		"No Claude sessions anywhere on this box.",
		fmt.Sprintf("supervisor + tmux scanned %s — a new session appears here within 5s.", scan),
		"",
		"  d   dispatch a background task from here",
		"  or run `claude` in any repo — it shows up live.",
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = styleDim.Render(center(l, width))
	}
	return out
}

// center pads s with equal blanks to sit centered within width.
func center(s string, width int) string {
	if width <= 0 {
		return s
	}
	r := runeLen(s)
	if r >= width {
		return truncate(s, width)
	}
	left := (width - r) / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", width-r-left)
}

// centerLines vertically centers a styled content block within height, padding
// above and below with dim blank lines.
func centerLines(content []string, width, height int) []string {
	if height <= 0 {
		return nil
	}
	blank := styleDim.Render(strings.Repeat(" ", max0(width)))
	out := make([]string, 0, height)
	top := (height - len(content)) / 2
	if top < 0 {
		top = 0
	}
	for i := 0; i < top; i++ {
		out = append(out, blank)
	}
	out = append(out, content...)
	for len(out) < height {
		out = append(out, blank)
	}
	return out[:height]
}

func (m Model) viewList(width, height int) string {
	return strings.Join(m.listLines(width, height), "\n")
}

// previewLines renders the live-preview pane as exactly height padded lines:
// a caption, then the tailed session output.
func (m Model) previewLines(width, height int) []string {
	out := make([]string, height)
	blank := padTrunc("", width)
	for i := range out {
		out[i] = blank
	}
	if height == 0 {
		return out
	}
	sess := m.selectedSession()
	out[0] = styleCaption.Render(padTrunc(m.previewCaption(sess), width))
	if height == 1 {
		return out
	}
	content := m.previewContent(sess)
	for i, line := range strings.Split(content, "\n") {
		if i+1 >= height {
			break
		}
		out[i+1] = stylePreview.Render(padTrunc(line, width))
	}
	return out
}

func (m Model) previewCaption(sess *model.Session) string {
	switch {
	case sess == nil:
		return "preview · (no selection)"
	case m.previewKey != sess.Key:
		return "preview · loading…"
	case m.previewErr != nil:
		return "preview · error"
	default:
		// Name the source: a live tmux pane and a `claude logs` replay have
		// different fidelity, so the caption says which one (§5).
		src := "logs " + sess.ID
		if previewSourceKind(sess) == previewPane {
			src = "pane " + sess.TmuxTarget
		}
		caption := fmt.Sprintf("preview · %s · %s", displayName(sess), src)
		if !m.previewAt.IsZero() {
			caption += fmt.Sprintf(" · %ds", int(time.Since(m.previewAt).Seconds()))
		}
		return caption
	}
}

func (m Model) previewContent(sess *model.Session) string {
	switch {
	case sess == nil:
		return "no session selected"
	case m.previewKey != sess.Key:
		return "loading…"
	case m.previewErr != nil:
		return "preview error: " + m.previewErr.Error()
	case strings.TrimSpace(m.previewText) == "":
		return "(no output yet)"
	default:
		return m.previewText
	}
}

func (m Model) viewLogs() string {
	title := "logs: " + m.logsTitle
	if m.logsErr != nil {
		title += " (error: " + m.logsErr.Error() + ")"
	}
	return styleRepo.Render(title) + "\n" + m.logsVP.View()
}

func (m Model) viewStatusLine() string {
	switch m.mode {
	case modeFilter:
		return "/ " + m.filterInput.View()
	case modeDispatch:
		target := "(no session selected)"
		if s := m.selectedSession(); s != nil {
			target = s.Repo
			if s.Worktree != "" {
				target += "/" + s.Worktree
			}
		}
		return fmt.Sprintf("dispatch → %s: %s", target, m.dispatchInput.View())
	case modeReply:
		target := "(no session)"
		if s := m.selectedSession(); s != nil {
			target = displayName(s)
		}
		return fmt.Sprintf("reply → %s: %s", target, m.replyInput.View())
	case modeConfirmStop:
		name := ""
		if m.confirmSess != nil {
			name = displayName(m.confirmSess)
		}
		return ErrorText(fmt.Sprintf("stop %q? (y/n)", name))
	default:
		// A fetch outage is the headline while it lasts: keep the last good
		// snapshot on screen (§5) and name the next action in red.
		if m.fetchErr != nil {
			return ErrorText("supervisor unreachable — is claude installed? retrying…")
		}
		if m.statusMsg == "" {
			return ""
		}
		if m.statusErr {
			return ErrorText(m.statusMsg)
		}
		return styleWorking.Render(m.statusMsg)
	}
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
