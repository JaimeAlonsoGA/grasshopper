package transcript

import (
	"os"
	"strings"
	"testing"

	"grasshopper/internal/bundle"
)

// Most lines typed "user" in this format were not typed by a user. The fixture
// carries all four kinds — a host preamble, an injected reminder, and two real
// questions — because telling them apart is the entire reader.
func TestJSONLGrok(t *testing.T) {
	f, err := os.Open("testdata/grok.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	turns, err := JSONLGrok(f)
	if err != nil {
		t.Fatalf("JSONLGrok: %v", err)
	}
	want := []bundle.Turn{
		{Who: bundle.Me, Text: "Why is the build red?"},
		{Who: bundle.Agent, Text: "Because the fixture says so."},
		{Who: bundle.Me, Text: "Fix it."},
		{Who: bundle.Agent, Text: "Fixed."},
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

func TestJSONLGrokDropsWhatWasNotSaid(t *testing.T) {
	f, _ := os.Open("testdata/grok.jsonl")
	defer f.Close()
	turns, err := JSONLGrok(f)
	if err != nil {
		t.Fatal(err)
	}
	joined := dump(turns)
	for _, leaked := range []string{"OS Version", "system-reminder", "thinking that must not travel", "exit status 1", "user_query"} {
		if strings.Contains(joined, leaked) {
			t.Errorf("%q reached the bundle", leaked)
		}
	}
}

// The checkpoint is the agent summarising the conversation back to itself. It
// reads like something that was said and was not, which is the one thing a hop
// must never carry.
func TestJSONLSteps(t *testing.T) {
	f, err := os.Open("testdata/steps.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	turns, err := JSONLSteps(f)
	if err != nil {
		t.Fatalf("JSONLSteps: %v", err)
	}
	want := []bundle.Turn{
		{Who: bundle.Me, Text: "Why is the build red?"},
		{Who: bundle.Agent, Text: "Because the fixture says so."},
		{Who: bundle.Me, Text: "Fix it."},
		{Who: bundle.Agent, Text: "Fixed."},
	}
	if len(turns) != len(want) {
		t.Fatalf("got %d turns, want %d: %+v", len(turns), len(want), turns)
	}
	for i := range want {
		if turns[i] != want[i] {
			t.Errorf("turn %d = %+v, want %+v", i, turns[i], want[i])
		}
	}
	joined := dump(turns)
	for _, leaked := range []string{"CHECKPOINT", "thinking that must not travel", "ADDITIONAL_METADATA", "File Path"} {
		if strings.Contains(joined, leaked) {
			t.Errorf("%q reached the bundle", leaked)
		}
	}
}

func TestPeekNewFormats(t *testing.T) {
	for _, c := range []struct{ format, path, opening string }{
		{"jsonl-grok", "testdata/grok.jsonl", "Why is the build red?"},
		{"jsonl-steps", "testdata/steps.jsonl", "Why is the build red?"},
	} {
		f, err := os.Open(c.path)
		if err != nil {
			t.Fatal(err)
		}
		p, err := Peek(c.format, f)
		f.Close()
		if err != nil {
			t.Fatalf("Peek(%s): %v", c.format, err)
		}
		if p.Opening != c.opening {
			t.Errorf("%s opening = %q, want %q", c.format, p.Opening, c.opening)
		}
	}
}
