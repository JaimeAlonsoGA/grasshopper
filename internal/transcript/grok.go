package transcript

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"grasshopper/internal/bundle"
)

// JSONLGrok reads a transcript that is a flat list of chat messages, where the
// person's own words are the exception rather than the rule.
//
// Most lines typed as "user" were not typed by a user: the host's preamble, the
// skills it advertises, the servers it connected, a reminder it injected
// mid-conversation. The format marks those, and this reader trusts the marks —
// a line is a person's turn when it carries the prompt index that the host
// stamps on what somebody actually sent, and never otherwise.
func JSONLGrok(r io.ReadSeeker) ([]bundle.Turn, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	var turns []bundle.Turn
	lines := 0
	eachLine(r, false, func(raw []byte) {
		var m grokMessage
		if json.Unmarshal(raw, &m) != nil {
			return
		}
		lines++
		if turn, ok := m.turn(); ok {
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

// grokMessage is a line of the chat history. Content is a bare string on the
// agent's side and a list of typed parts on the person's, so it is decoded late.
type grokMessage struct {
	Type    string          `json:"type"`
	Content json.RawMessage `json:"content"`

	// PromptIndex is present only on a message somebody sent, which is what
	// separates a question from the scaffolding wearing the same role.
	PromptIndex *int `json:"prompt_index"`

	// SyntheticReason is present on a message the host wrote in the person's
	// name. Belt and braces: a line with a reason is never a turn even if a
	// future version also stamps it with an index.
	SyntheticReason string `json:"synthetic_reason"`
}

func (m grokMessage) turn() (bundle.Turn, bool) {
	switch {
	case m.Type == "assistant":
		text := strings.TrimSpace(unwrap(m.text()))
		if text == "" {
			return bundle.Turn{}, false
		}
		return bundle.Turn{Who: bundle.Agent, Text: text}, true

	case m.Type == "user" && m.PromptIndex != nil && m.SyntheticReason == "":
		text := strings.TrimSpace(unwrap(m.text()))
		if text == "" || IsInjected(text) {
			return bundle.Turn{}, false
		}
		return bundle.Turn{Who: bundle.Me, Text: text}, true
	}
	return bundle.Turn{}, false
}

// text flattens whichever of the two shapes content arrived in.
func (m grokMessage) text() string {
	var plain string
	if json.Unmarshal(m.Content, &plain) == nil {
		return plain
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(m.Content, &parts) != nil {
		return ""
	}
	var said []string
	for _, part := range parts {
		if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
			said = append(said, part.Text)
		}
	}
	return strings.Join(said, "\n")
}

// unwrap removes the tag the host puts around what somebody typed. The tag is
// the host talking about the message, not part of it, and carrying it into
// another agent's context hands that agent a delimiter it did not write.
func unwrap(text string) string {
	const open, close = "<user_query>", "</user_query>"
	start := strings.Index(text, open)
	if start < 0 {
		return text
	}
	end := strings.Index(text[start:], close)
	if end < 0 {
		return text
	}
	return text[start+len(open) : start+end]
}

func peekGrok(r io.ReadSeeker) (Preview, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return Preview{}, err
	}
	var preview Preview
	eachLine(io.LimitReader(r, peekBytes), false, func(raw []byte) {
		if preview.Opening != "" {
			return
		}
		var m grokMessage
		if json.Unmarshal(raw, &m) != nil {
			return
		}
		if turn, ok := m.turn(); ok && turn.Who == bundle.Me {
			preview.Opening = firstLine(turn.Text)
		}
	})
	return preview, nil
}
