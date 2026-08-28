package transcript

import (
	"encoding/json"
	"fmt"
	"strings"

	"grasshopper/internal/bundle"
)

// cursorDB reads the editor that keeps every conversation in one key-value table.
//
// There are no rows for messages and no rows for sessions: there is one table of
// JSON blobs keyed by string, and the structure is in the key names.
// "composerData:<id>" is a conversation, and "bubbleId:<composer>:<bubble>" is a
// message in it. The order of the messages is not the order of the rows — it is
// the list the conversation itself carries, which is also the only thing that
// says which bubbles belong to the conversation at all.
//
// That last point is load-bearing. Bubbles exist in the table that the person
// never saw: a subagent's prompt is typed the same as a person's turn, and taking
// every bubble with a matching prefix puts words in their mouth. The list is the
// authority; a bubble not in it was not part of the conversation.
type cursorDB struct{}

func (cursorDB) List(path string) ([]Contained, error) {
	db, err := openRead(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT key, value FROM cursorDiskKV WHERE key LIKE 'composerData:%'`)
	if err != nil {
		return nil, errNoTable
	}
	defer rows.Close()

	var found []Contained
	for rows.Next() {
		var key string
		var raw []byte
		if rows.Scan(&key, &raw) != nil {
			continue
		}
		var c composer
		if json.Unmarshal(raw, &c) != nil {
			continue
		}
		id := strings.TrimPrefix(key, "composerData:")
		if id == "" || len(c.Headers) == 0 {
			continue
		}
		found = append(found, Contained{
			Key:   id,
			Title: strings.TrimSpace(c.Name),
			// The draft box is not a message and the created stamp is in
			// milliseconds, which is a unit nobody else here uses.
			When: c.CreatedAt / 1000,
		})
	}
	return found, rows.Err()
}

func (cursorDB) Turns(path, key string) ([]bundle.Turn, error) {
	db, err := openRead(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var raw []byte
	err = db.QueryRow(`SELECT value FROM cursorDiskKV WHERE key = ?`, "composerData:"+key).Scan(&raw)
	if err != nil {
		return nil, fmt.Errorf("no conversation %s in this database", key)
	}
	var c composer
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}

	var turns []bundle.Turn
	for _, header := range c.Headers {
		if header.BubbleID == "" {
			continue
		}
		var body []byte
		row := db.QueryRow(`SELECT value FROM cursorDiskKV WHERE key = ?`,
			fmt.Sprintf("bubbleId:%s:%s", key, header.BubbleID))
		if row.Scan(&body) != nil {
			continue
		}
		var b bubble
		if json.Unmarshal(body, &b) != nil {
			continue
		}
		// A bubble spawned for a subagent carries a prompt this person never
		// wrote. It is typed like their turn and must not be read as one.
		if b.SubagentTaskID != "" {
			continue
		}
		var who bundle.Speaker
		switch b.Type {
		case 1:
			who = bundle.Me
		case 2:
			who = bundle.Agent
		default:
			continue
		}
		if turn, ok := speak(who, b.Text); ok {
			turns = append(turns, turn)
		}
	}
	return turns, nil
}

// composer is a conversation. The record carries some sixty other fields —
// attached folders, lint state, diff histories — and none of them is speech.
type composer struct {
	Name      string `json:"name"`
	CreatedAt int64  `json:"createdAt"`
	Headers   []struct {
		BubbleID string `json:"bubbleId"`
	} `json:"fullConversationHeadersOnly"`
}

// bubble is a message. Type 1 is the person, 2 is the agent.
type bubble struct {
	Type           int    `json:"type"`
	Text           string `json:"text"`
	SubagentTaskID string `json:"subagentSpawnTaskToolCallId"`
}
