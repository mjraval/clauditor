package actions

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Reply delivers text to a background session by attaching it in a hidden
// tmux window and injecting keys (docs/REPLY.md). EXPERIMENTAL — callers
// gate this behind actions.experimental_reply; the API returns 501 when off.
//
// Sequence: attach window → poll for prompt → classify screen → send →
// verify advancement → kill window (always).
func (a *Actions) Reply(ctx context.Context, sessionID, text string) error {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return errf("bad_request", "reply text is required")
	}
	if !ValidSessionID(sessionID) {
		return errf("bad_target", "session id has unexpected format")
	}
	if err := checkDenied(text); err != nil {
		return err
	}

	// Ensure hidden session.
	if _, err := a.Runner.Run(ctx, "", a.TmuxBin, "has-session", "-t", hiddenSession); err != nil {
		if _, err := a.run(ctx, "", a.TmuxBin, "new-session", "-d", "-s", hiddenSession); err != nil {
			return err
		}
	}
	win := hiddenSession + ":reply-" + sessionID
	_, err := a.run(ctx, "", a.TmuxBin, "new-window", "-d", "-t", hiddenSession,
		"-n", "reply-"+sessionID, fmt.Sprintf("%s attach %s", a.ClaudeBin, sessionID))
	if err != nil {
		return err
	}
	defer func() {
		// Always tear the window down; the bg session keeps running detached.
		kctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = a.Runner.Run(kctx, "", a.TmuxBin, "kill-window", "-t", win)
	}()

	// Poll for readiness: the composer footer line (❯) must be visible.
	screen, err := a.waitForPrompt(ctx, win, 15*time.Second)
	if err != nil {
		return err
	}

	switch classifyScreen(screen) {
	case screenPermission:
		return errf("permission_prompt",
			"session is at a permission prompt; refusing to answer remotely — use open-in-tmux and attach")
	case screenNumberedChoice:
		if !isChoiceNumber(text) {
			return errf("bad_request",
				"session shows a numbered choice; reply with the choice number (or use open-in-tmux)")
		}
	}

	before, beforeErr := captureLen(ctx, a, win)
	if _, err := a.run(ctx, "", a.TmuxBin, "send-keys", "-t", win, "-l", "--", text); err != nil {
		return err
	}
	time.Sleep(300 * time.Millisecond)
	if _, err := a.run(ctx, "", a.TmuxBin, "send-keys", "-t", win, "Enter"); err != nil {
		return err
	}

	// Verify delivery by screen advancement, not state flip (REPLY.md).
	// A capture error is never evidence of advancement (QA finding: the -1
	// sentinel used to compare unequal to a real length and report success).
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(1 * time.Second)
		after, err := captureLen(ctx, a, win)
		if err != nil || beforeErr != nil {
			continue
		}
		if after != before {
			return nil
		}
	}
	return errf("delivery_unverified",
		"reply sent but transcript advancement could not be verified within 10s — attach to confirm")
}

var promptRe = regexp.MustCompile(`(?m)^\s*❯`)

func (a *Actions) waitForPrompt(ctx context.Context, win string, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, errf("timeout", "context cancelled while waiting for prompt")
		case <-time.After(500 * time.Millisecond):
		}
		out, err := a.Runner.Run(ctx, "", a.TmuxBin, "capture-pane", "-p", "-t", win)
		if err != nil {
			continue // window may still be initializing
		}
		if promptRe.Match(out) {
			return out, nil
		}
	}
	return nil, errf("timeout", "attach window never showed a prompt within %s", timeout)
}

type screenKind int

const (
	screenFreeText screenKind = iota
	screenNumberedChoice
	screenPermission
)

var (
	numberedRe   = regexp.MustCompile(`(?m)^\s*(?:❯\s*)?\d+\.\s`)
	permissionRe = regexp.MustCompile(`(?i)permission|allow this|approve`)
)

// classifyScreen decides what kind of input the visible screen expects.
// Deliberately conservative: permission wins over numbered wins over free text.
func classifyScreen(screen []byte) screenKind {
	if numberedRe.Match(screen) {
		if permissionRe.Match(screen) {
			return screenPermission
		}
		return screenNumberedChoice
	}
	return screenFreeText
}

// isChoiceNumber accepts 1–2 digit menu selections (menus can exceed 9).
func isChoiceNumber(s string) bool {
	if len(s) < 1 || len(s) > 2 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func captureLen(ctx context.Context, a *Actions, win string) (int, error) {
	out, err := a.Runner.Run(ctx, "", a.TmuxBin, "capture-pane", "-p", "-t", win)
	if err != nil {
		return 0, err
	}
	return len(bytes.TrimRight(out, " \n")), nil
}
