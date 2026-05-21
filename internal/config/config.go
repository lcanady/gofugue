// Package config loads and manages GoFugue configuration (TOML) and world profiles.
package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config is the top-level configuration structure, loaded from
// ~/.config/gofugue/config.toml (or the path given by --config).
type Config struct {
	// DefaultWorld is connected on startup if set.
	DefaultWorld string `toml:"default_world"`

	// StartupScript path to a .tf or GoFugue script run at startup.
	StartupScript string `toml:"startup_script"`

	// WrapWidth is the default line-wrap column (0 = terminal width).
	WrapWidth int `toml:"wrap_width"`

	// ScrollbackLines is the per-world scrollback buffer depth.
	ScrollbackLines int `toml:"scrollback_lines"`

	// IPC server settings.
	IPC IPCConfig `toml:"ipc"`

	// Theme controls TUI colours.
	Theme ThemeConfig `toml:"theme"`

	// Worlds is the map of named world profiles.
	Worlds map[string]WorldConfig `toml:"world"`
}

// ThemeConfig holds colour settings for the TUI status bar and input bar.
// Colour names are standard terminal names ("navy", "white", "black", "red",
// "green", "yellow", "blue", "magenta", "cyan", "silver", "gray", …) or
// empty/omitted for the terminal default.
// The default theme uses plain reverse-video with no explicit palette colours.
type ThemeConfig struct {
	// Status bar colours and attributes.
	StatusFG      string `toml:"status_fg"`
	StatusBG      string `toml:"status_bg"`
	StatusBold    bool   `toml:"status_bold"`
	StatusReverse bool   `toml:"status_reverse"`

	// Input bar colours and attributes.
	InputFG      string `toml:"input_fg"`
	InputBG      string `toml:"input_bg"`
	InputReverse bool   `toml:"input_reverse"`
}

// IPCConfig controls the IPC server listeners.
type IPCConfig struct {
	// SocketPath for Unix domain socket (empty = ~/.config/gofugue/gofugue.sock).
	SocketPath string `toml:"socket_path"`

	// TCPPort for raw TCP JSON-RPC listener (0 = disabled).
	TCPPort int `toml:"tcp_port"`

	// WSPort for WebSocket JSON-RPC listener (0 = disabled).
	WSPort int `toml:"ws_port"`
}

// WorldConfig holds a saved world profile.
type WorldConfig struct {
	URL      string `toml:"url"`      // transport URL (mud://, ws://, wt://, …)
	Name     string `toml:"name"`     // display name (defaults to map key)
	Charset  string `toml:"charset"`  // e.g. "utf-8", "latin-1"
	Login    string `toml:"login"`    // autologin string (sent on CONNECT hook)
	Password string `toml:"password"` // stored password (consider keychain integration later)
	Char     string `toml:"char"`     // character name (for /addworld)
	Pass     string `toml:"pass"`     // password short-form alias (for /addworld)

	TelnetEnabled *bool `toml:"telnet_enabled"` // nil = auto-detect from scheme

	// KeepaliveInterval is how often (in seconds) to send a Telnet NOP to the
	// server and check for inactivity. If no data is received from the server
	// within 2× this interval, the connection is closed. 0 = disabled.
	KeepaliveInterval int `toml:"keepalive_interval"`
}

// Defaults returns a Config populated with sensible defaults.
func Defaults() Config {
	return Config{
		ScrollbackLines: 5000,
		WrapWidth:       0,
		IPC: IPCConfig{
			TCPPort: 7878,
			WSPort:  7879,
		},
		Theme: ThemeConfig{}, // all terminal defaults; set in [theme] to add colour
		Worlds: make(map[string]WorldConfig),
	}
}

// Load reads a TOML config file from path, merging it over Defaults().
// A missing file is silently ignored (returns Defaults()).
func Load(path string) (Config, error) {
	cfg := Defaults()
	_, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	// Ensure world names are filled from their map key.
	for name, wc := range cfg.Worlds {
		if wc.Name == "" {
			wc.Name = name
			cfg.Worlds[name] = wc
		}
	}
	return cfg, nil
}

// DefaultPath returns the platform default config file path.
func DefaultPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "gofugue", "config.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "gofugue", "config.toml")
}

// DefaultSocketPath returns the default Unix domain socket path.
func DefaultSocketPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "gofugue", "gofugue.sock")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "gofugue", "gofugue.sock")
}
