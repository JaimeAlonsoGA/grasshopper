package transcript

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"grasshopper/internal/bundle"

	_ "modernc.org/sqlite"
)

// A fixture database in the shape the editor writes, built here rather than
// checked in: a binary fixture nobody can read in a diff is a fixture nobody
// maintains.
func cursorFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.vscdb")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value BLOB)`); err != nil {
		t.Fatal(err)
	}
	put := func(k, v string) {
		if _, err := db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, k, v); err != nil {
			t.Fatal(err)
		}
	}
	put("composerData:c1", `{"name":"Red build","createdAt":1787913278019,
		"fullConversationHeadersOnly":[{"bubbleId":"b1"},{"bubbleId":"b2"},{"bubbleId":"b3"}]}`)
	put("bubbleId:c1:b1", `{"type":1,"text":"Why is the build red?"}`)
	put("bubbleId:c1:b2", `{"type":2,"text":"Because the fixture says so."}`)
	put("bubbleId:c1:b3", `{"type":2,"text":""}`)
	// Not in the header list, and typed like a person's turn: a subagent prompt.
	put("bubbleId:c1:ghost", `{"type":1,"text":"words nobody typed","subagentSpawnTaskToolCallId":"call-1"}`)
	// A second conversation, so the two must not collide.
	put("composerData:c2", `{"name":"Green build","createdAt":1787913299000,
		"fullConversationHeadersOnly":[{"bubbleId":"b1"}]}`)
	put("bubbleId:c2:b1", `{"type":1,"text":"And this one?"}`)
	// A draft the person never sent has no headers and is not a conversation.
	put("composerData:empty-state-draft", `{"name":"","fullConversationHeadersOnly":[]}`)
	return path
}

func TestCursorLists(t *testing.T) {
	path := cursorFixture(t)
	inside, err := Inside("sqlite-cursor", path)
	if err != nil {
		t.Fatalf("Inside: %v", err)
	}
	if len(inside) != 2 {
		t.Fatalf("got %d conversations, want 2 (the draft is not one): %+v", len(inside), inside)
	}
	if inside[0].Title != "Red build" || inside[1].Title != "Green build" {
		t.Errorf("titles = %q, %q — and oldest first", inside[0].Title, inside[1].Title)
	}
	if inside[0].Key == inside[1].Key {
		t.Error("two conversations share a key, so they would share a handle")
	}
}

func TestCursorReadsOne(t *testing.T) {
	path := cursorFixture(t)
	turns, err := One("sqlite-cursor", path, "c1")
	if err != nil {
		t.Fatalf("One: %v", err)
	}
	want := []bundle.Turn{
		{Who: bundle.Me, Text: "Why is the build red?"},
		{Who: bundle.Agent, Text: "Because the fixture says so."},
	}
	if len(turns) != len(want) {
		t.Fatalf("got %d turns, want %d: %+v", len(turns), len(want), turns)
	}
	for i := range want {
		if turns[i] != want[i] {
			t.Errorf("turn %d = %+v, want %+v", i, turns[i], want[i])
		}
	}
}

// The header list is the only thing that says which bubbles were the
// conversation. A bubble that is merely in the table, keyed to the same
// conversation and typed like a person, was not said by one.
func TestCursorIgnoresBubblesOutsideTheConversation(t *testing.T) {
	path := cursorFixture(t)
	turns, _ := One("sqlite-cursor", path, "c1")
	for _, turn := range turns {
		if turn.Text == "words nobody typed" {
			t.Fatal("a subagent's prompt was read as the person's turn")
		}
	}
}

// grasshopper reads somebody else's database while their app may be holding it.
// Opening it any way that writes — a journal, a lock, a recovered page — is
// writing into another app's state, which this tool does not do.
func TestContainerOpensWithoutWriting(t *testing.T) {
	path := cursorFixture(t)
	dir := filepath.Dir(path)
	before, _ := os.ReadDir(dir)
	if _, err := Inside("sqlite-cursor", path); err != nil {
		t.Fatal(err)
	}
	if _, err := One("sqlite-cursor", path, "c1"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadDir(dir)
	if len(after) != len(before) {
		var names []string
		for _, e := range after {
			names = append(names, e.Name())
		}
		t.Errorf("reading left files behind: %v", names)
	}
}

func TestUnknownDatabaseIsQuiet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "other.sqlite")
	db, _ := sql.Open("sqlite", path)
	db.Exec(`CREATE TABLE something_else (x TEXT)`)
	db.Close()
	if _, err := Inside("sqlite-cursor", path); err == nil {
		t.Error("a database that is not ours should say so rather than return nothing quietly")
	}
}
