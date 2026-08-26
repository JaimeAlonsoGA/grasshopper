package transcript

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"grasshopper/internal/bundle"
)

// record is everything hop reads from a transcript line. Every other field in
// the format is ignored on purpose — see the package comment.
type record struct {
	UUID        string `json:"uuid"`
	ParentUUID  string `json:"parentUuid"`
	IsSidechain bool   `json:"isSidechain"`

	// ToolUseResult marks a line that exists only to carry a tool's output back
	// to the model. It has role "user" but no human wrote it.
	ToolUseResult json.RawMessage `json:"toolUseResult"`

	Message *struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// JSONLTree reads a JSON Lines transcript whose records are linked into a tree by
// parentUuid.
//
// The tree is the whole reason this is not a twenty-line function. Editing a
// message or retrying a tool call leaves the abandoned branch in the file, so a
// flat read produces a conversation that never happened — in one real transcript
// measured here, 483 of 1959 records were off the live path. The live
// conversation is the chain from the last record back to the root.
func JSONLTree(r io.ReadSeeker) ([]bundle.Turn, error) {
	records, lines, err := readRecords(r)
	if err != nil {
		return nil, err
	}
	if lines == 0 {
		return nil, errors.New("no parseable JSON lines")
	}

	// Records without a uuid (titles, latches, modes) are bookkeeping that sits
	// outside the conversation tree, and sidechain records belong to a subagent.
	// Neither can be the leaf and neither can be walked through.
	byUUID := make(map[string]int, len(records))
	leaf := -1
	for i, rec := range records {
		if rec.UUID == "" || rec.IsSidechain {
			continue
		}
		byUUID[rec.UUID] = i
		leaf = i
	}
	if leaf < 0 {
		return nil, fmt.Errorf("%w: none of %d lines is a conversation record", ErrNothingSaid, lines)
	}

	var path []int
	seen := make(map[string]bool, len(records))
	for i := leaf; ; {
		if seen[records[i].UUID] {
			break // a cycle cannot happen, but it must not hang if it does
		}
		seen[records[i].UUID] = true
		path = append(path, i)

		parent, ok := byUUID[records[i].ParentUUID]
		if !ok {
			break
		}
		i = parent
	}

	turns := make([]bundle.Turn, 0, len(path))
	for i := len(path) - 1; i >= 0; i-- {
		if turn, ok := turnOf(records[path[i]]); ok {
			turns = append(turns, turn)
		}
	}
	if len(turns) == 0 {
		return nil, fmt.Errorf("%w: %d lines, but nothing was said on the live path", ErrNothingSaid, lines)
	}
	return turns, nil
}

// turnOf reports the turn a record contributes, if any. Records that carry no
// message at all — attachments, hook notices, mode changes, and whatever the
// format grows next — are links in the chain and nothing more. Skipping every
// type we do not recognise is the design, not a shortcut.
func turnOf(rec record) (bundle.Turn, bool) {
	if rec.Message == nil || len(rec.ToolUseResult) > 0 {
		return bundle.Turn{}, false
	}
	var who bundle.Speaker
	switch rec.Message.Role {
	case "user":
		who = bundle.Me
	case "assistant":
		who = bundle.Agent
	default:
		return bundle.Turn{}, false
	}

	text := strings.TrimSpace(stripEnvelopes(plainText(rec.Message.Content)))
	if text == "" {
		return bundle.Turn{}, false
	}
	return bundle.Turn{Who: who, Text: text}, true
}

// plainText keeps text blocks and nothing else. Thinking, tool calls and tool
// results are dropped whole rather than summarised: hop has no way to summarise
// them honestly, and a list of tool names is noise the receiving agent has to
// read past.
func plainText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var kept []string
	for _, b := range blocks {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			kept = append(kept, b.Text)
		}
	}
	return strings.Join(kept, "\n\n")
}

// readRecords parses line by line rather than streaming one decoder over the
// file, so that a transcript truncated mid-write by a crash still yields
// everything before the damage. Lines are read without a size cap: a single
// pasted file in a transcript can run to megabytes.
func readRecords(r io.Reader) (records []record, lines int, err error) {
	br := bufio.NewReaderSize(r, 1<<16)
	for {
		line, readErr := br.ReadString('\n')
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			var rec record
			if json.Unmarshal([]byte(trimmed), &rec) == nil {
				lines++
				records = append(records, rec)
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return records, lines, nil
			}
			return nil, 0, readErr
		}
	}
}
