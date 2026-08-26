package transcript

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"grasshopper/internal/bundle"
)

// A4. The fixture is synthetic — it carries no real conversation — but every
// shape in it was measured in a live transcript first: unknown record types,
// records that link the tree without holding a message, a subagent sidechain, a
// branch abandoned by an edit, and a line truncated by a crash.
func TestJSONLTreeFollowsTheLivePath(t *testing.T) {
	turns := parseFixture(t, "testdata/tree.jsonl")

	want := []bundle.Turn{
		{Who: bundle.Me, Text: "Build plan resolution."},
		{Who: bundle.Agent, Text: "Implemented ResolvePlan."},
		{Who: bundle.Me, Text: "That breaks for trials."},
		{Who: bundle.Agent, Text: "Reworked to precedence rules."},
		{Who: bundle.Me, Text: "/commit\n\n-m wip"},
		{Who: bundle.Me, Text: "Now write the test."},
		{Who: bundle.Agent, Text: "Committed."},
	}
	if len(turns) != len(want) {
		t.Fatalf("got %d turns, want %d:\n%s", len(turns), len(want), dump(turns))
	}
	for i := range want {
		if turns[i] != want[i] {
			t.Errorf("turn %d = %#v\n     want %#v", i, turns[i], want[i])
		}
	}
}

// Everything the filter is supposed to drop is marked in the fixture, so this
// stays honest even when the fixture grows.
func TestJSONLTreeDropsEverythingElse(t *testing.T) {
	joined := dump(parseFixture(t, "testdata/tree.jsonl"))
	if strings.Contains(joined, "MUST-NOT-APPEAR") {
		t.Errorf("filtered content survived:\n%s", joined)
	}
	for _, fragment := range []string{"tool_use", "tool_result", "thinking", "ide_opened_file", "task-notification", "Request interrupted", "{", "}"} {
		if strings.Contains(joined, fragment) {
			t.Errorf("output contains %q:\n%s", fragment, joined)
		}
	}
}

func TestJSONLTreeFailsLoudly(t *testing.T) {
	cases := []struct{ name, path, wantIn string }{
		{"malformed", "testdata/malformed.jsonl", "no parseable JSON lines"},
		{"no conversation", "testdata/no-conversation.jsonl", "is a conversation record"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, err := os.Open(c.path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			turns, err := JSONLTree(f)
			if err == nil {
				t.Fatalf("want an error, got %d turns", len(turns))
			}
			if !strings.Contains(err.Error(), c.wantIn) {
				t.Errorf("error %q does not mention %q", err, c.wantIn)
			}
			if turns != nil {
				t.Error("turns returned alongside an error")
			}
		})
	}
}

func TestJSONLTreeEmptyInput(t *testing.T) {
	if _, err := JSONLTree(strings.NewReader("")); err == nil {
		t.Error("empty input should be an error, not an empty handoff")
	}
}

func TestGet(t *testing.T) {
	if _, err := Get("jsonl-tree"); err != nil {
		t.Errorf("jsonl-tree: %v", err)
	}
	if _, err := Get(""); err == nil {
		t.Error("an empty format must be an error, not a default")
	}
	err := func() error { _, e := Get("nope"); return e }()
	if err == nil || !strings.Contains(err.Error(), "jsonl-tree") {
		t.Errorf("unknown format error should list what is known, got %v", err)
	}
}

func TestStripEnvelopes(t *testing.T) {
	cases := []struct{ in, want string }{
		{"<system-reminder>noise</system-reminder>real", "real"},
		{"before<system-reminder>a</system-reminder>mid<system-reminder>b</system-reminder>after", "beforemidafter"},
		{"kept<system-reminder>truncated by the host", "kept"},
		{"<command-name>/x</command-name>", "/x"},
		{"a\n[Request interrupted by user]\nb", "a\nb"},
		{"plain text", "plain text"},
	}
	for _, c := range cases {
		if got := stripEnvelopes(c.in); got != c.want {
			t.Errorf("stripEnvelopes(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func parseFixture(t *testing.T, path string) []bundle.Turn {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	turns, err := JSONLTree(f)
	if err != nil {
		t.Fatal(err)
	}
	return turns
}

func dump(turns []bundle.Turn) string {
	var b strings.Builder
	for _, turn := range turns {
		b.WriteString(string(turn.Who) + ": " + turn.Text + "\n")
	}
	return b.String()
}

// The reader has no line-length cap, and it needs none: a transcript line holds
// whatever was pasted into the session, which can be a whole file.
func TestJSONLTreeReadsAMegabyteLine(t *testing.T) {
	huge := strings.Repeat("x", 2<<20)
	doc := `{"type":"user","uuid":"a","message":{"role":"user","content":"` + huge + `"}}` + "\n" +
		`{"type":"assistant","uuid":"b","parentUuid":"a","message":{"role":"assistant","content":[{"type":"text","text":"ok"}]}}` + "\n"

	turns, err := JSONLTree(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 || len(turns[0].Text) != len(huge) {
		t.Fatalf("got %d turns, first %d bytes of %d", len(turns), len(turns[0].Text), len(huge))
	}
}

// A cycle cannot happen in a well-formed transcript. It must not hang if it does.
func TestJSONLTreeSurvivesACycle(t *testing.T) {
	doc := `{"type":"user","uuid":"a","parentUuid":"b","message":{"role":"user","content":"one"}}` + "\n" +
		`{"type":"assistant","uuid":"b","parentUuid":"a","message":{"role":"assistant","content":[{"type":"text","text":"two"}]}}` + "\n"

	turns, err := JSONLTree(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 {
		t.Errorf("got %d turns, want both records once each", len(turns))
	}
}

// A transcript whose only records are a subagent's is not a conversation hop can
// hand over, and saying so beats handing over the subagent's.
func TestJSONLTreeRefusesASidechainOnlyFile(t *testing.T) {
	doc := `{"type":"assistant","uuid":"s","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"x"}]}}` + "\n"
	if _, err := JSONLTree(strings.NewReader(doc)); err == nil {
		t.Error("want an error rather than a subagent's transcript")
	}
}

// Peek is what makes a UUID recognisable in a listing. It must be cheap: the
// opening exchange is at the top, and the sixty megabytes underneath it are not
// a listing's business.
func TestPeek(t *testing.T) {
	doc := `{"type":"user","uuid":"a","cwd":"/w/api","message":{"role":"user","content":"<ide_opened_file>noise</ide_opened_file>\nresolve billing plans"}}` + "\n" +
		`{"type":"assistant","uuid":"b","parentUuid":"a","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}` + "\n"

	got, err := Peek("jsonl-tree", strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if got.Opening != "resolve billing plans" {
		t.Errorf("Opening = %q", got.Opening)
	}
	if len(got.Dirs) != 1 || got.Dirs[0] != "/w/api" {
		t.Errorf("Dirs = %v", got.Dirs)
	}
}

// A session moves between projects. Trusting the first directory it mentioned is
// how a handoff got written into an unrelated repository; the caller needs all of
// them, latest first, so it can ask.
func TestPeekReportsEveryDirectoryLatestFirst(t *testing.T) {
	var b strings.Builder
	for i, dir := range []string{"/w/first", "/w/first", "/w/second", "/w/second", "/w/third"} {
		fmt.Fprintf(&b, `{"type":"user","uuid":"u%d","cwd":%q,"message":{"role":"user","content":"x"}}`+"\n", i, dir)
	}
	got, err := Peek("jsonl-tree", strings.NewReader(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/w/third", "/w/second", "/w/first"}
	if strings.Join(got.Dirs, ",") != strings.Join(want, ",") {
		t.Errorf("Dirs = %v, want %v", got.Dirs, want)
	}
}

// The directory a session ended in lives at the end of the file, past anything a
// head-only read would reach.
func TestPeekFindsTheDirectoryASessionEndedIn(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"type":"user","uuid":"a","cwd":"/w/started-here","message":{"role":"user","content":"the question"}}` + "\n")
	filler := `{"type":"user","uuid":"f","cwd":"/w/started-here","message":{"role":"user","content":"` + strings.Repeat("x", 900) + `"}}` + "\n"
	for b.Len() < 3*peekBytes {
		b.WriteString(filler)
	}
	b.WriteString(`{"type":"user","uuid":"z","cwd":"/w/ended-here","message":{"role":"user","content":"last"}}` + "\n")

	got, err := Peek("jsonl-tree", strings.NewReader(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Dirs) == 0 || got.Dirs[0] != "/w/ended-here" {
		t.Errorf("Dirs = %v, want the last directory first", got.Dirs)
	}
	// And the opening question still comes from the head.
	if got.Opening != "the question" {
		t.Errorf("Opening = %q", got.Opening)
	}
}

func TestPeekStopsEarly(t *testing.T) {
	// Everything after the opening exchange must go unread.
	head := `{"type":"user","uuid":"a","cwd":"/w","message":{"role":"user","content":"the question"}}` + "\n"
	tail := strings.Repeat(`{"type":"user","uuid":"z","message":{"role":"user","content":"later"}}`+"\n", 50000)
	r := &countingReader{inner: strings.NewReader(head + tail)}

	got, err := Peek("jsonl-tree", r)
	if err != nil {
		t.Fatal(err)
	}
	if got.Opening != "the question" {
		t.Errorf("got %q", got.Opening)
	}
	// Head plus tail, and nothing in between.
	if r.read > 2*peekBytes {
		t.Errorf("read %d bytes, over the %d cap for both ends", r.read, 2*peekBytes)
	}
}

func TestPeekIgnoresToolResultsWhenLookingForTheOpening(t *testing.T) {
	doc := `{"type":"user","uuid":"a","cwd":"/w","toolUseResult":{"x":1},"message":{"role":"user","content":[{"type":"tool_result","content":"NOT-THE-OPENING"}]}}` + "\n" +
		`{"type":"user","uuid":"b","parentUuid":"a","message":{"role":"user","content":"the real opening"}}` + "\n"
	got, err := Peek("jsonl-tree", strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if got.Opening != "the real opening" {
		t.Errorf("got %q", got.Opening)
	}
}

func TestPeekUnknownFormat(t *testing.T) {
	if _, err := Peek("", strings.NewReader("")); err == nil {
		t.Error("an agent with no format must be an error, not an empty preview")
	}
	// A format grasshopper has no preview for is not a failure; there is simply
	// less to show about its sessions.
	if got, err := Peek("other", strings.NewReader("x")); err != nil || got.Opening != "" {
		t.Errorf("got %+v, %v", got, err)
	}
}

// countingReader measures how much of a transcript a preview actually touches.
type countingReader struct {
	inner *strings.Reader
	read  int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.inner.Read(p)
	c.read += n
	return n, err
}

func (c *countingReader) Seek(offset int64, whence int) (int64, error) {
	return c.inner.Seek(offset, whence)
}
