// Package config loads clauditor's TOML configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Repos         []string `toml:"repos"`
	WorkspaceDirs []string `toml:"workspace_dirs"`

	Poll     Poll     `toml:"poll"`
	Git      Git      `toml:"git"`
	Tmux     Tmux     `toml:"tmux"`
	Serve    Serve    `toml:"serve"`
	Access   Access   `toml:"access"`
	Actions  Actions  `toml:"actions"`
	Dispatch Dispatch `toml:"dispatch"`
	Links    Links    `toml:"links"`
	Notify   Notify   `toml:"notify"`
}

type Poll struct {
	ClaudeSeconds int `toml:"claude_seconds"`
	TmuxSeconds   int `toml:"tmux_seconds"`
	GitSeconds    int `toml:"git_seconds"`
}

type Git struct {
	DirtyCheck  bool `toml:"dirty_check"`
	AheadBehind bool `toml:"ahead_behind"`
}

type Tmux struct {
	Heuristics bool `toml:"heuristics"`
}

type Serve struct {
	Listen       string `toml:"listen"`
	SnapshotFile string `toml:"snapshot_file"`
}

type Access struct {
	TeamDomain string `toml:"team_domain"`
	PolicyAUD  string `toml:"policy_aud"`
}

type Actions struct {
	Enabled           bool `toml:"enabled"`
	ExperimentalReply bool `toml:"experimental_reply"`
}

type Dispatch struct {
	WorktreeBase string `toml:"worktree_base"`
}

type Links struct {
	WorktreeURLTemplate string `toml:"worktree_url_template"`
}

type Notify struct {
	DebounceSeconds int `toml:"debounce_seconds"`
}

// Default returns the built-in defaults (SPEC §15).
func Default() *Config {
	return &Config{
		Poll:   Poll{ClaudeSeconds: 5, TmuxSeconds: 10, GitSeconds: 20},
		Git:    Git{DirtyCheck: true, AheadBehind: false},
		Serve:  Serve{Listen: "127.0.0.1:8790"},
		Notify: Notify{DebounceSeconds: 30},
	}
}

// Load reads config from path, or the default locations when path is empty:
// $XDG_CONFIG_HOME/clauditor/config.toml, else ~/.config/clauditor/config.toml.
// A missing file yields defaults; a malformed file is an error.
func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		path = defaultPath()
	}
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}

func defaultPath() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "clauditor", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "clauditor", "config.toml")
}

// StateDir returns ~/.local/state/clauditor (created on demand).
func StateDir() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	dir := filepath.Join(base, "clauditor")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}
