package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"

	"github.com/rishi/clauditor/internal/actions"
	"github.com/rishi/clauditor/internal/config"
	"github.com/rishi/clauditor/internal/model"
)

const refreshInterval = 3 * time.Second
const fetchTimeout = 4 * time.Second

// mode is which input the keyboard is currently routed to.
type mode int

const (
	modeList mode = iota
	modeFilter
	modeDispatch
	modeConfirmStop
	modeLogs
)

// --- tea.Msg types ----------------------------------------------------------

type tickMsg struct{}

type snapMsg struct {
	snap *model.Snapshot
	err  error
}

type logsMsg struct {
	title string
	text  string
	err   error
}

type actionDoneMsg struct {
	text string
	err  error
}

// --- Model -------------------------------------------------------------------

// Model is the bubbletea model for `clauditor tui` (SPEC §11): a single
// screen showing the grouped fleet list, with modal-line overlays for
// filter/dispatch/confirm and a full-screen pager for logs.
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
	confirmSess   *model.Session

	logsVP    viewport.Model
	logsTitle string
	logsErr   error

	statusMsg string
	statusErr bool

	width, height int
	quitting      bool
}

// NewModel wires a fresh TUI model around the given source/actionC. inTmux
// controls whether `enter` also runs `tmux switch-client` after opening a
// window (SPEC §11).
func NewModel(ctx context.Context, source Source, actionC ActionClient) Model {
	fi := textinput.New()
	fi.Placeholder = "filter (substring)…"
	di := textinput.New()
	di.Placeholder = "dispatch prompt…"
	return Model{
		ctx:           ctx,
		source:        source,
		actionC:       actionC,
		inTmux:        os.Getenv("TMUX") != "",
		filterInput:   fi,
		dispatchInput: di,
		logsVP:        viewport.New(80, 20),
		cursor:        -1,
	}
}

// Run starts the TUI (blocking until the user quits or ctx is canceled).
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
	return tea.Batch(fetchCmd(m.ctx, m.source), tickCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return tickMsg{} })
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

func openInTmuxCmd(ctx context.Context, actionC ActionClient, sess *model.Session, inTmux bool) tea.Cmd {
	return func() tea.Msg {
		fctx, cancel := context.WithTimeout(ctx, fetchTimeout)
		defer cancel()
		res, err := actionC.OpenInTmux(fctx, sess)
		if err != nil {
			return actionDoneMsg{err: fmt.Errorf("open-in-tmux: %w", err)}
		}
		msg := "opened in tmux: " + res.Target
		if inTmux {
			if serr := SwitchTmuxClient(fctx, res.Target); serr != nil {
				msg += fmt.Sprintf(" (switch-client failed: %v)", serr)
			} else {
				msg += " (switched)"
			}
		} else {
			msg += " — " + res.Attach
		}
		return actionDoneMsg{text: msg}
	}
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
		return m, nil

	case tickMsg:
		return m, tea.Batch(fetchCmd(m.ctx, m.source), tickCmd())

	case snapMsg:
		m.fetchErr = msg.err
		if msg.err == nil {
			m.snap = msg.snap
			m.lastFetch = time.Now()
		}
		m.rebuildRows()
		return m, nil

	case logsMsg:
		m.logsErr = msg.err
		m.logsTitle = msg.title
		m.logsVP.SetContent(msg.text)
		m.logsVP.GotoTop()
		return m, nil

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

func bodyHeight(total int) int {
	h := total - 3 // header + status/input line + footer
	if h < 1 {
		h = 1
	}
	return h
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeFilter:
		return m.handleFilterKey(msg)
	case modeDispatch:
		return m.handleDispatchKey(msg)
	case modeConfirmStop:
		return m.handleConfirmKey(msg)
	case modeLogs:
		return m.handleLogsKey(msg)
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
		return m, nil
	case "k", "up":
		if idx := PrevSelectable(m.rows, m.cursor); idx >= 0 {
			m.cursor = idx
		}
		return m, nil
	case "/":
		m.mode = modeFilter
		m.queryPrev = m.query
		m.filterInput.SetValue(m.query)
		m.filterInput.CursorEnd()
		return m, m.filterInput.Focus()
	case "s":
		m.stateFilter = m.stateFilter.Next()
		m.rebuildRows()
		return m, nil
	case "enter":
		sess := m.selectedSession()
		if sess == nil {
			m.statusMsg, m.statusErr = "no session selected", true
			return m, nil
		}
		return m, openInTmuxCmd(m.ctx, m.actionC, sess, m.inTmux)
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
	}
	return m, nil
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
		return m, nil
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
	header := HeaderText(m.snap, m.source.Label(), m.lastFetch, time.Now(), m.stateFilter, m.query)

	var body string
	switch m.mode {
	case modeLogs:
		body = m.viewLogs()
	default:
		body = m.viewList(width, bodyHeight(m.height))
	}

	statusLine := m.viewStatusLine()
	footer := FooterText()
	if m.mode == modeLogs {
		footer = styleFooterBar.Render("j/k↑↓ scroll · pgup/pgdn page · q/esc back")
	}

	return strings.Join([]string{header, body, statusLine, footer}, "\n")
}

func (m Model) viewList(width, height int) string {
	if len(m.rows) == 0 {
		msg := "no sessions"
		if m.fetchErr != nil {
			msg = "fetch error: " + m.fetchErr.Error()
		}
		return ErrorText(msg)
	}
	start, end := VisibleWindow(len(m.rows), max0(m.cursor), height)
	var lines []string
	for i := start; i < end; i++ {
		lines = append(lines, RenderRow(m.rows[i], width, i == m.cursor))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
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
	case modeConfirmStop:
		name := ""
		if m.confirmSess != nil {
			name = displayName(m.confirmSess)
		}
		return ErrorText(fmt.Sprintf("stop %q? (y/n)", name))
	default:
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
