// Package store writes a bundle out as a file.
//
// Only bundles live here. A transcript is never copied: its own agent already
// keeps it, and a second copy is a second thing that can be wrong. What this
// directory holds is the rendered document, so a new session can be handed a path
// instead of a hundred kilobytes of command-line argument.
package store

import (
	"fmt"
	"os"
	"path/filepath"

	"grasshopper/internal/bundle"
	"grasshopper/internal/registry"
)

// A bundle is a conversation: whatever was said in a working session, including
// whatever was pasted into it. It gets the permissions the agents give the
// transcripts it came from, not the friendlier default.
const (
	dirMode  = 0o700
	fileMode = 0o600
)

func Dir() string { return filepath.Join(registry.Home(), "bundles") }

// Write renders a bundle to a file named after its code, and returns the path.
// Writing the same bundle twice is the same file: the code is a fingerprint of
// the content, so there is nothing to collide with.
func Write(b bundle.Bundle) (string, error) {
	if err := os.MkdirAll(Dir(), dirMode); err != nil {
		return "", err
	}
	path := filepath.Join(Dir(), b.Code+".md")
	if err := os.WriteFile(path, []byte(bundle.Render(b)), fileMode); err != nil {
		return "", fmt.Errorf("writing the bundle: %w", err)
	}
	return path, nil
}
