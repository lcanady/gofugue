package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
)

// validateScriptPath rejects paths that look obviously dangerous for
// file-loading /commands (/load, /js, /py, /save).
//
// It enforces two cheap guards:
//
//  1. The path must not contain a NUL byte.
//  2. If allowedExts is non-empty, the file extension (case-insensitive)
//     must match one of them. This prevents using a script-loading
//     /command as a primitive for reading arbitrary files like
//     /etc/passwd or ~/.ssh/id_rsa via the parser's error messages.
//
// The check is intentionally extension-based (not allowlist-by-dir) so
// users can keep their script collections wherever they like.
func validateScriptPath(path string, allowedExts ...string) error {
	if strings.ContainsRune(path, 0) {
		return fmt.Errorf("invalid path: NUL byte")
	}
	// Reject `..` segments after normalisation. A relative path that
	// escapes its starting directory (e.g. `../pwn.json`, `a/../../b`)
	// could let an injected /save or /load command clobber or exfil
	// files outside the user's working area. Absolute paths are fine —
	// filepath.Clean strips `..` segments above `/`.
	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		for _, seg := range strings.Split(cleaned, string(filepath.Separator)) {
			if seg == ".." {
				return fmt.Errorf("refusing path %q: parent traversal not allowed", path)
			}
		}
	}
	if len(allowedExts) == 0 {
		return nil
	}
	ext := strings.ToLower(filepath.Ext(path))
	for _, want := range allowedExts {
		if ext == strings.ToLower(want) {
			return nil
		}
	}
	return fmt.Errorf("refusing path %q: extension must be one of %s",
		path, strings.Join(allowedExts, ", "))
}
