// Package transcript turns an agent's own session files into domain turns.
//
// This is the only code coupled to somebody else's file format, and it is kept
// deliberately starved. Its entire obligation is who spoke and what plain text
// they said. It reads no tokens, no usage, no tool names, no per-turn timestamps,
// no model identity — measured across seventeen real transcripts, those two
// fields are the only ones that survive an upstream change.
//
// Readers are keyed by the format they read, never by the product that happens to
// write it. The registry maps a product to a format; a second tool emitting the
// same shape costs no code at all.
package transcript

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"grasshopper/internal/bundle"
)

// ErrNothingSaid marks a file grasshopper read and understood completely, which
// simply held no conversation — a session opened and closed without an exchange.
// It is not a failure, and treating it as one puts an error in front of somebody
// who did nothing wrong.
var ErrNothingSaid = errors.New("no conversation in this session")

type Reader func(io.ReadSeeker) ([]bundle.Turn, error)

var readers = map[string]Reader{
	"jsonl-tree": JSONLTree,
}

// Get looks a reader up by format key. An unknown key is an error and never a
// quiet fall back to something that happens to parse: a bundle assembled by the
// wrong reader is exactly the plausible-but-wrong artefact this tool exists to
// avoid.
func Get(name string) (Reader, error) {
	if name == "" {
		return nil, errors.New("no transcript format configured for this agent")
	}
	r, ok := readers[name]
	if !ok {
		return nil, fmt.Errorf("unknown transcript format %q (known: %s)", name, strings.Join(Names(), ", "))
	}
	return r, nil
}

func Names() []string {
	names := make([]string, 0, len(readers))
	for name := range readers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
