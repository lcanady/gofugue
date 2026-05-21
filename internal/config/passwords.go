package config

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
)

// ResolvePassword returns the world's password, preferring PasswordCmd
// (executed via the shell, stdout trimmed) over the on-disk plaintext
// fields. An empty result with no error means no password is
// configured.
func (wc WorldConfig) ResolvePassword() (string, error) {
	if cmd := strings.TrimSpace(wc.PasswordCmd); cmd != "" {
		shell := "/bin/sh"
		flag := "-c"
		if runtime.GOOS == "windows" {
			shell = "cmd"
			flag = "/C"
		}
		out, err := exec.Command(shell, flag, cmd).Output()
		if err != nil {
			return "", fmt.Errorf("password_cmd: %w", err)
		}
		return strings.TrimRight(string(out), "\r\n\t "), nil
	}
	if wc.Pass != "" {
		return wc.Pass, nil
	}
	return wc.Password, nil
}

// LoadWithWarnings is like Load but also returns advisory warnings —
// most importantly, that plaintext passwords are sitting in a
// group/world-readable config file. Callers should surface these to the
// user (slog, stderr).
func LoadWithWarnings(path string) (Config, []string, error) {
	cfg := Defaults()
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		if os.IsNotExist(err) {
			return cfg, nil, nil
		}
		return cfg, nil, err
	}
	for name, wc := range cfg.Worlds {
		if wc.Name == "" {
			wc.Name = name
			cfg.Worlds[name] = wc
		}
	}

	var warnings []string
	if runtime.GOOS != "windows" {
		if st, err := os.Stat(path); err == nil {
			perm := st.Mode().Perm()
			readableByOthers := perm&0o077 != 0
			writableByOthers := perm&0o022 != 0
			for name, wc := range cfg.Worlds {
				if readableByOthers && (wc.Password != "" || wc.Pass != "") {
					warnings = append(warnings,
						fmt.Sprintf("world %q has a plaintext password in %s (mode %#o); "+
							"tighten with `chmod 600 %s` or migrate to `password_cmd`",
							name, path, perm, path))
				}
				// password_cmd in a group/world-writable file is RCE on
				// every startup: an attacker who appends a line to
				// config.toml runs arbitrary commands as the user.
				if writableByOthers && wc.PasswordCmd != "" {
					warnings = append(warnings,
						fmt.Sprintf("world %q uses password_cmd, but %s is writable by others (mode %#o) — "+
							"this is a startup-RCE risk. Run `chmod 600 %s` immediately.",
							name, path, perm, path))
				}
			}
		}
	}
	return cfg, warnings, nil
}
