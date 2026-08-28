package sessions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setup(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRASSHOPPER_HOME", filepath.Join(home, ".grasshopper"))
	if err := os.MkdirAll(filepath.Join(home, ".grasshopper"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"readable":{"transcripts":"~/.agent/*/*.jsonl","normalize":"jsonl-tree"},` +
		`"cooperative":{"transcripts":"~/.other/*/*.jsonl","normalize":""}}`
	if err := os.WriteFile(filepath.Join(home, ".grasshopper", "registry.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}

// write builds a transcript the way the real ones are built: a title record, a
// working directory on every line, and an exchange.
func write(t *testing.T, home, agentDir, id, title, cwd string, exchange ...string) string {
	t.Helper()
	dir := filepath.Join(home, agentDir, "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if title != "" {
		fmt.Fprintf(&b, `{"type":"ai-title","aiTitle":%q}`+"\n", title)
	}
	for i, text := range exchange {
		role, uuid, parent := "user", fmt.Sprintf("u%d", i), fmt.Sprintf("u%d", i-1)
		if i%2 == 1 {
			role = "assistant"
		}
		if i == 0 {
			fmt.Fprintf(&b, `{"type":%q,"uuid":%q,"cwd":%q,"message":{"role":%q,"content":%q}}`+"\n", role, uuid, cwd, role, text)
			continue
		}
		fmt.Fprintf(&b, `{"type":%q,"uuid":%q,"parentUuid":%q,"cwd":%q,"message":{"role":%q,"content":%q}}`+"\n", role, uuid, parent, cwd, role, text)
	}
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestListUsesTheSessionsOwnTitle(t *testing.T) {
	home := setup(t)
	older := write(t, home, ".agent", "aaaaaaaa1111", "Billing resolver", "/w/api", "build it", "built")
	write(t, home, ".agent", "bbbbbbbb2222", "Cleanup", "/w/web", "clean it", "cleaned")
	// An agent with no transcript format has nothing to read, so its files must
	// not appear as if they could be loaded.
	write(t, home, ".other", "cccccccc3333", "Invisible", "/w/other", "x", "y")

	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(older, past, past); err != nil {
		t.Fatal(err)
	}

	all, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("got %d sessions, want 2: %+v", len(all), all)
	}
	if all[0].Title != "Cleanup" {
		t.Errorf("not newest first, or title missing: %+v", all[0])
	}
	if all[0].Label() != "Cleanup" {
		t.Errorf("Label = %q", all[0].Label())
	}
	if len(all[0].ID) != idLength {
		t.Errorf("ID = %q, want %d characters", all[0].ID, idLength)
	}
	if all[0].Dir() != "/w/web" {
		t.Errorf("Dir = %q", all[0].Dir())
	}
	// Recency is what "still going" actually means: an agent appends to its
	// transcript and closes it, so no open file identifies a live session.
	if !all[0].Active {
		t.Error("a session written just now is not marked active")
	}
	if all[1].Active {
		t.Error("a session from two hours ago is marked active")
	}
}

// A session with no title falls back to the first thing said, because a UUID is
// not something anybody recognises.
func TestLabelFallsBackToTheOpening(t *testing.T) {
	home := setup(t)
	write(t, home, ".agent", "dddddddd4444", "", "/w/api", "resolve the billing plans", "done")

	all, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if all[0].Label() != "resolve the billing plans" {
		t.Errorf("Label = %q", all[0].Label())
	}
}

func TestFind(t *testing.T) {
	home := setup(t)
	path := write(t, home, ".agent", "abcdef111111", "Billing resolver", "/w/api", "build it", "built")
	write(t, home, ".agent", "abcdef222222", "Billing rewrite", "/w/api", "rewrite it", "rewritten")
	write(t, home, ".agent", "fedcba333333", "Cleanup", "/w/web", "clean it", "cleaned")

	t.Run("by id prefix", func(t *testing.T) {
		s, err := Find("fedcba")
		if err != nil || s.Title != "Cleanup" {
			t.Errorf("got %q, %v", s.Title, err)
		}
	})
	t.Run("by title fragment, any case", func(t *testing.T) {
		s, err := Find("cleanup")
		if err != nil || s.Title != "Cleanup" {
			t.Errorf("got %q, %v", s.Title, err)
		}
	})
	t.Run("by path", func(t *testing.T) {
		s, err := Find(path)
		if err != nil || s.Path != path {
			t.Errorf("got %q, %v", s.Path, err)
		}
	})
	// Loading the wrong conversation is the one mistake this package exists to
	// prevent, so an ambiguous answer names the candidates instead of guessing.
	t.Run("ambiguous", func(t *testing.T) {
		_, err := Find("billing")
		if err == nil {
			t.Fatal("want an error")
		}
		for _, want := range []string{"matches 2", "Billing resolver", "Billing rewrite"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention %q", err, want)
			}
		}
	})
	t.Run("no match", func(t *testing.T) {
		if _, err := Find("nothing like this"); err == nil {
			t.Error("want an error")
		}
	})
}

func TestLoadRendersABundleThatPointsAtTheOriginal(t *testing.T) {
	home := setup(t)
	path := write(t, home, ".agent", "eeeeeeee5555", "Billing resolver", "/w/api",
		"a trial outranks a paid plan", "reworked as precedence rules")

	s, err := Find("eeeeeeee")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Load(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Turns) != 2 {
		t.Fatalf("got %d turns", len(b.Turns))
	}
	if b.Source.Title != "Billing resolver" || b.Source.Dir != "/w/api" {
		t.Errorf("source = %+v", b.Source)
	}
	// The original is left where its own agent wrote it, and the bundle says
	// where, so an agent that needs more than was carried can go and read it.
	if b.Source.RawPath != path {
		t.Errorf("RawPath = %q, want %q", b.Source.RawPath, path)
	}
}

// A session that was opened and closed without an exchange is not a failure, and
// it must still be listed rather than vanishing.
func TestEmptySessionIsListedAndSaysSo(t *testing.T) {
	home := setup(t)
	write(t, home, ".agent", "ffffffff6666", "Nothing happened", "/w/api")

	all, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("got %d sessions", len(all))
	}
	if _, err := all[0].Load(0, 0); err == nil {
		t.Error("loading an empty session should say there is nothing in it")
	}
}

// The handle has to survive the file moving. One agent archives a session by
// moving it to another directory, and an id that changed when that happened would
// be an id you could not write down.
func TestIDSurvivesTheFileMoving(t *testing.T) {
	home := setup(t)
	path := write(t, home, ".agent", "abcdef111111", "Billing", "/w/api", "build it", "built")

	before, err := List()
	if err != nil {
		t.Fatal(err)
	}

	archived := filepath.Join(home, ".agent", "archive")
	if err := os.MkdirAll(archived, 0o755); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(archived, filepath.Base(path))
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	// The registry looks in both places, as it does for a real agent that archives.
	registryPath := filepath.Join(home, ".grasshopper", "registry.json")
	body := `{"readable":{"transcripts":"~/.agent/*/*.jsonl,~/.agent/archive/*.jsonl","normalize":"jsonl-tree"}}`
	if err := os.WriteFile(registryPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	after, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 {
		t.Fatalf("got %d sessions after the move", len(after))
	}
	if after[0].ID != before[0].ID {
		t.Errorf("id changed when the file moved: %q became %q", before[0].ID, after[0].ID)
	}
}

// Handles are short, and they lengthen only when two would collide.
func TestIDsAreShortAndUnique(t *testing.T) {
	home := setup(t)
	for i := 0; i < 40; i++ {
		// Identifiers that share a long prefix, the way time-ordered ones do.
		id := fmt.Sprintf("01a03ea6-0d36-79d1-bce2-d2f16f72%04d", i)
		write(t, home, ".agent", id, fmt.Sprintf("Session %d", i), "/w/api", "x", "y")
	}

	all, err := List()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, s := range all {
		if seen[s.ID] {
			t.Fatalf("duplicate id %q", s.ID)
		}
		seen[s.ID] = true
		if len(s.ID) > idMaxLength {
			t.Errorf("id %q is longer than %d", s.ID, idMaxLength)
		}
	}
	if len(seen) != 40 {
		t.Errorf("got %d ids for 40 sessions", len(seen))
	}
	// And every one of them resolves back.
	for _, s := range all {
		got, err := Find(s.ID)
		if err != nil {
			t.Errorf("Find(%q): %v", s.ID, err)
			continue
		}
		if got.Path != s.Path {
			t.Errorf("Find(%q) returned the wrong session", s.ID)
		}
	}
}

// A session grasshopper opened begins with the prompt grasshopper wrote, so its
// raw opening reads as a file path. Naming the hop it came from says what
// happened, and makes a chain of handovers visible in a listing.
func TestLabelNamesTheHopASessionCameFrom(t *testing.T) {
	opening := "Read /Users/someone/.grasshopper/bundles/HOP-K3QZ.md first — it is a record " +
		"of an earlier session, carried here by grasshopper."
	if got := (Session{Opening: opening}).Label(); got != "↳ continued from HOP-K3QZ" {
		t.Errorf("Label = %q", got)
	}

	// And an ordinary opening is left exactly as somebody wrote it.
	for _, plain := range []string{
		"Read the billing resolver and tell me what is wrong",
		"Read /etc/hosts",
		"",
	} {
		got := (Session{Opening: plain}).Label()
		if strings.HasPrefix(got, "↳") {
			t.Errorf("Label(%q) = %q, want it left alone", plain, got)
		}
	}
}
