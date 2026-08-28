package transcript

import (
	"os"
	"strings"
	"testing"

	"grasshopper/internal/bundle"
)

// The format is a log of edits, so nothing here is asserted from a single line:
// every expectation below is the state after the whole file has been applied.
func TestJSONLPatch(t *testing.T) {
	f, err := os.Open("testdata/patch.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	turns, err := JSONLPatch(f)
	if err != nil {
		t.Fatalf("JSONLPatch: %v", err)
	}

	want := []bundle.Turn{
		{Who: bundle.Me, Text: "Why is the build red?"},
		{Who: bundle.Agent, Text: "Because the fixture says so."},
		{Who: bundle.Me, Text: "Fix it."},
		{Who: bundle.Agent, Text: "Fixed."},
		{Who: bundle.Me, Text: "And again?"},
		{Who: bundle.Agent, Text: "One. Two."},
	}
	if len(turns) != len(want) {
		t.Fatalf("got %d turns, want %d: %v", len(turns), len(want), turns)
	}
	for i := range want {
		if turns[i] != want[i] {
			t.Errorf("turn %d = %+v, want %+v", i, turns[i], want[i])
		}
	}
}

// An answer arrives in pieces, and the pieces that are machinery are the majority
// of them. Dropping the wrong ones is silent, so it is asserted by name.
func TestJSONLPatchDropsMachinery(t *testing.T) {
	f, _ := os.Open("testdata/patch.jsonl")
	defer f.Close()
	turns, err := JSONLPatch(f)
	if err != nil {
		t.Fatal(err)
	}
	joined := dump(turns)
	for _, leaked := range []string{"reasoning that must not travel", "mcpServersStarting", "toolInvocationSerialized", "a draft nobody sent"} {
		if strings.Contains(joined, leaked) {
			t.Errorf("%q reached the bundle", leaked)
		}
	}
}

// An append carries a bare element in one line and a list in the next, for the
// same path. Both shapes are in the fixture; this pins the behaviour so a reader
// that only handles one of them fails here rather than in somebody's terminal.
func TestJSONLPatchAcceptsBothAppendShapes(t *testing.T) {
	f, _ := os.Open("testdata/patch.jsonl")
	defer f.Close()
	turns, _ := JSONLPatch(f)
	if got := turns[1].Text; got != "Because the fixture says so." {
		t.Errorf("joined answer = %q", got)
	}
}

// A kind 2 with an index re-sends a window that overlaps what is already there.
// Appending it instead of splicing repeats whole paragraphs, and the repeat reads
// like the model stuttering rather than like a bug — which is why it is pinned.
func TestJSONLPatchSplicesOverlappingWindows(t *testing.T) {
	f, _ := os.Open("testdata/patch.jsonl")
	defer f.Close()
	turns, err := JSONLPatch(f)
	if err != nil {
		t.Fatal(err)
	}
	last := turns[len(turns)-1]
	if last.Text != "One. Two." {
		t.Errorf("answer = %q, want the window spliced in rather than appended", last.Text)
	}
	if strings.Count(dump(turns), "One.") != 1 {
		t.Errorf("the overlapping window was repeated:\n%s", last.Text)
	}
}

func TestPeekPatch(t *testing.T) {
	f, err := os.Open("testdata/patch.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	p, err := Peek("jsonl-patch", f)
	if err != nil {
		t.Fatalf("Peek: %v", err)
	}
	if p.Title != "Red build" {
		t.Errorf("title = %q, want the one set at the end of the file", p.Title)
	}
	if p.Opening != "Why is the build red?" {
		t.Errorf("opening = %q", p.Opening)
	}
	if len(p.Dirs) != 1 || p.Dirs[0] != "/Users/you/code/api" {
		t.Errorf("dirs = %v, want the folder carried on the answer parts", p.Dirs)
	}
}
