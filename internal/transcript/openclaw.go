package transcript

import (
	"encoding/json"
	"strings"

	"grasshopper/internal/bundle"
)

// openclawDB reads the agent that keeps one SQLite database per agent id, with
// every session it has ever run inside it.
//
// Two tables matter. session_windows is one row per session — its name, when it
// started, when it was last touched. transcript_events is the conversation, one
// JSON event per row, ordered by a sequence number rather than by a timestamp,
// because the log is append-only and the sequence is what the writer guarantees.
//
// The event shape is not documented as a contract, so this reader treats it as a
// shape that may change: it looks for a role and for text, in the several places
// an event of this kind puts them, and an event it cannot read is skipped rather
// than guessed at.
type openclawDB struct{}

func (openclawDB) List(path string) ([]Contained, error) {
	db, err := openRead(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT session_id,
		       COALESCE(display_name, ''),
		       COALESCE(started_at, 0)
		  FROM session_windows`)
	if err != nil {
		return nil, errNoTable
	}
	defer rows.Close()

	var found []Contained
	for rows.Next() {
		var id, name string
		var started int64
		if rows.Scan(&id, &name, &started) != nil {
			continue
		}
		if id == "" {
			continue
		}
		found = append(found, Contained{Key: id, Title: strings.TrimSpace(name), When: seconds(started)})
	}
	return found, rows.Err()
}

func (openclawDB) Turns(path, key string) ([]bundle.Turn, error) {
	db, err := openRead(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT event_json FROM transcript_events WHERE session_id = ? ORDER BY seq`, key)
	if err != nil {
		return nil, errNoTable
	}
	defer rows.Close()

	var turns []bundle.Turn
	for rows.Next() {
		var raw []byte
		if rows.Scan(&raw) != nil {
			continue
		}
		var e claw
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		var who bundle.Speaker
		switch strings.ToLower(e.role()) {
		case "user", "human":
			who = bundle.Me
		case "assistant", "agent", "model":
			who = bundle.Agent
		default:
			continue
		}
		if turn, ok := speak(who, e.text()); ok {
			turns = append(turns, turn)
		}
	}
	return turns, rows.Err()
}

// claw is one transcript event, read loosely on purpose. The log carries tool
// calls and compaction summaries alongside messages; anything without a role and
// words is not a turn.
type claw struct {
	Role    string `json:"role"`
	Type    string `json:"type"`
	Message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	Content json.RawMessage `json:"content"`
	Text    string          `json:"text"`
}

func (e claw) role() string {
	if e.Role != "" {
		return e.Role
	}
	return e.Message.Role
}

// text flattens whichever of the shapes this log puts words in: a bare string, a
// list of typed parts, or a text field beside them.
func (e claw) text() string {
	for _, raw := range []json.RawMessage{e.Content, e.Message.Content} {
		if len(raw) == 0 {
			continue
		}
		var plain string
		if json.Unmarshal(raw, &plain) == nil && strings.TrimSpace(plain) != "" {
			return plain
		}
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(raw, &parts) == nil {
			var said []string
			for _, part := range parts {
				if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
					said = append(said, part.Text)
				}
			}
			if len(said) > 0 {
				return strings.Join(said, "\n")
			}
		}
	}
	return e.Text
}
