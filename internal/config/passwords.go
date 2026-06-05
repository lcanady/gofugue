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
		// Split into argv to avoid passing the command through a shell
		// interpreter. Shell metacharacters (;, |, $, `, &&, etc.) in
		// PasswordCmd or its arguments are passed literally to the
		// subprocess rather than interpreted, preventing injection.
		argv := strings.Fields(cmd)
		if len(argv) == 0 {
			return "", fmt.Errorf("password_cmd: empty after trimming")
		}
		out, err := exec.Command(argv[0], argv[1:]...).Output()
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
//
// The file-permission check runs unconditionally: even if the TOML parse
// fails (returning a partial config), permission warnings are still
// emitted so the user is informed of the security issue regardless of
// syntax errors in the file.
func LoadWithWarnings(path string) (Config, []string, error) {
	cfg := Defaults()
	var parseErr error
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		if os.IsNotExist(err) {
			return cfg, nil, nil
		}
		parseErr = err
	}
	for name, wc := range cfg.Worlds {
		if wc.Name == "" {
			wc.Name = name
			cfg.Worlds[name] = wc
		}
	}

	// Permission check runs unconditionally — even on parse errors — so
	// the user is warned about world-readable credentials regardless of
	// whether the rest of the config loaded cleanly.
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
	return cfg, warnings, parseErr
}
