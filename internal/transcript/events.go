package transcript

import (
	"fmt"
	"io"
	"strings"

	"grasshopper/internal/bundle"
)

// JSONLEvents reads a transcript that is a flat stream of typed events, each line
// an envelope with a kind and a payload.
//
// It is a different shape from a tree: there is no parent link and no abandoned
// branch to walk around, because an edit starts a new file rather than a new
// branch. So the whole job is picking the envelopes that carry a message and
// ignoring the rest — reasoning, searches, tool calls, token counts, and whatever
// kinds the format grows next.
func JSONLEvents(r io.ReadSeeker) ([]bundle.Turn, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	var turns []bundle.Turn
	lines := eachJSON(r, false, func(e event) {
		if turn, ok := e.turn(); ok {
			turns = append(turns, turn)
		}
	})
	if lines == 0 {
		return nil, fmt.Errorf("no parseable JSON lines")
	}
	if len(turns) == 0 {
		return nil, fmt.Errorf("%w: %d lines, but nothing was said", ErrNothingSaid, lines)
	}
	return turns, nil
}

// event is everything grasshopper reads from a line. Every other field in the
// format is ignored on purpose.
type event struct {
	Type    string `json:"type"`
	Payload struct {
		Kind string `json:"type"`
		Role string `json:"role"`

		// A message's content is a list of typed parts; only the text ones are
		// kept, exactly as with any other format.
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`

		// session_meta carries where the session was working and which surface
		// started it.
		Dir        string `json:"cwd"`
		Originator string `json:"originator"`
	} `json:"payload"`
}

// turn reports the turn a line contributes, if any.
//
// The "developer" role is dropped along with everything else that is not a person
// or the agent: it carries the host's own preamble, injected into the conversation
// wearing a role of its own.
func (e event) turn() (bundle.Turn, bool) {
	if e.Type != "response_item" || e.Payload.Kind != "message" {
		return bundle.Turn{}, false
	}
	var who bundle.Speaker
	switch e.Payload.Role {
	case "user":
		who = bundle.Me
	case "assistant":
		who = bundle.Agent
	default:
		return bundle.Turn{}, false
	}

	var parts []string
	for _, part := range e.Payload.Content {
		if !strings.HasSuffix(part.Type, "_text") || strings.TrimSpace(part.Text) == "" {
			continue
		}
		// Whole-block, not element-by-element: this format packs the host's own
		// context into the same turn as the person's words, one block each.
		if IsInjected(part.Text) {
			continue
		}
		parts = append(parts, part.Text)
	}
	text := strings.TrimSpace(stripEnvelopes(strings.Join(parts, "\n\n")))
	if text == "" {
		return bundle.Turn{}, false
	}
	return bundle.Turn{Who: who, Text: text}, true
}

// peekEvents is the cheap read a listing needs. This format states its working
// directory and its surface once, in the first line, so there is nothing to
// search for.
func peekEvents(r io.ReadSeeker) (Preview, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return Preview{}, err
	}

	var preview Preview
	var dirs []string
	eachJSON(io.LimitReader(r, peekBytes), false, func(e event) {
		if e.Payload.Dir != "" {
			dirs = append(dirs, e.Payload.Dir)
		}
		if e.Payload.Originator != "" && preview.Surface == "" {
			preview.Surface = e.Payload.Originator
		}
		if preview.Opening == "" {
			if turn, ok := e.turn(); ok && turn.Who == bundle.Me {
				preview.Opening = firstLine(turn.Text)
			}
		}
	})
	preview.Dirs = mostRecentFirst(dirs)
	return preview, nil
}

// titlesFromIndex reads a list of sessions and their names.
//
// This format keeps its titles beside the transcripts rather than inside them, so
// a listing has to read one more file — one file for all of them, which is
// cheaper than the alternative.
func titlesFromIndex(r io.Reader) map[string]string {
	type entry struct {
		ID   string `json:"id"`
		Name string `json:"thread_name"`
	}
	titles := map[string]string{}
	eachJSON(r, false, func(e entry) {
		if e.ID != "" && e.Name != "" {
			// Later lines win: the name is revised as a session finds its subject.
			titles[e.ID] = e.Name
		}
	})
	return titles
}
