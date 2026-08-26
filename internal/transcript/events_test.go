package transcript

import (
	"fmt"
	"strings"
	"testing"

	"grasshopper/internal/bundle"
)

// The fixture is synthetic, but every shape in it was measured in a real
// transcript first: reasoning, web searches, tool calls, token counts, and a first
// turn that packs the host's own context in beside the person's words.
const events = `{"type":"session_meta","payload":{"session_id":"s","cwd":"/w/api","originator":"Some Desktop"}}
{"type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}
{"type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"<app-context>\nDESCRIBING ITSELF\n</app-context>"}]}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<recommended_plugins>\nPLUGIN CATALOGUE\n</recommended_plugins>"},{"type":"input_text","text":"# AGENTS.md instructions for /w/api\n\n<INSTRUCTIONS>\nPROJECT RULES\n</INSTRUCTIONS>"},{"type":"input_text","text":"<environment_context>\n<cwd>/w/api</cwd>\n</environment_context>"},{"type":"input_text","text":"resolve the billing plans"}]}}
{"type":"response_item","payload":{"type":"reasoning","summary":[{"type":"summary_text","text":"SECRET REASONING"}]}}
{"type":"response_item","payload":{"type":"web_search_call","action":{"type":"search","query":"SEARCH QUERY"}}}
{"type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"TOOL PAYLOAD\"}"}}
{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":9}}}}
{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Reworked as precedence rules."}]}}
{"type":"world_state","payload":{"full":true,"state":{"agents_md":{"text":"WORLD STATE"}}}}
{"truncated by a crash
{"type":"response_item","payload":{"type":"message","role":"user","cwd":"/w/web","content":[{"type":"input_text","text":"now the tests"}]}}
`

func TestJSONLEventsKeepsOnlyWhatWasSaid(t *testing.T) {
	turns, err := JSONLEvents(strings.NewReader(events))
	if err != nil {
		t.Fatal(err)
	}
	want := []bundle.Turn{
		{Who: bundle.Me, Text: "resolve the billing plans"},
		{Who: bundle.Agent, Text: "Reworked as precedence rules."},
		{Who: bundle.Me, Text: "now the tests"},
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

// This format packs the host's own context into the same turn as the person's
// first words — a plugin catalogue, the project's rules file, a description of the
// shell — so blocks have to be judged one at a time.
func TestJSONLEventsDropsInjectedBlocksNotWholeTurns(t *testing.T) {
	turns, err := JSONLEvents(strings.NewReader(events))
	if err != nil {
		t.Fatal(err)
	}
	joined := dump(turns)
	for _, gone := range []string{
		"PLUGIN CATALOGUE", "PROJECT RULES", "AGENTS.md instructions",
		"DESCRIBING ITSELF", "SECRET REASONING", "SEARCH QUERY",
		"TOOL PAYLOAD", "WORLD STATE", "token_count",
	} {
		if strings.Contains(joined, gone) {
			t.Errorf("%q survived:\n%s", gone, joined)
		}
	}
	// And the words that were beside them did not go with them.
	if !strings.Contains(joined, "resolve the billing plans") {
		t.Error("the person's own words were dropped with the injected blocks")
	}
}

func TestJSONLEventsFailsLoudly(t *testing.T) {
	if _, err := JSONLEvents(strings.NewReader("not json at all\n")); err == nil {
		t.Error("a file that is not this format must be an error")
	}
	// A session that was opened and closed is not a failure, and says which.
	_, err := JSONLEvents(strings.NewReader(`{"type":"session_meta","payload":{"cwd":"/w"}}` + "\n"))
	if err == nil {
		t.Fatal("want an error for a session with nothing in it")
	}
	if !strings.Contains(err.Error(), ErrNothingSaid.Error()) {
		t.Errorf("error %q is not classified as an empty session", err)
	}
}

func TestPeekEvents(t *testing.T) {
	got, err := Peek("jsonl-events", strings.NewReader(events))
	if err != nil {
		t.Fatal(err)
	}
	if got.Opening != "resolve the billing plans" {
		t.Errorf("Opening = %q", got.Opening)
	}
	if got.Surface != "Some Desktop" {
		t.Errorf("Surface = %q", got.Surface)
	}
	// Most recent directory first, as with any other format.
	if len(got.Dirs) == 0 || got.Dirs[0] != "/w/web" {
		t.Errorf("Dirs = %v", got.Dirs)
	}
}

// This format keeps its titles beside the transcripts rather than inside them.
func TestTitlesFromIndex(t *testing.T) {
	index := `{"id":"aaaa","thread_name":"Billing resolver","updated_at":"2026-08-26T15:25:21Z"}
{"id":"bbbb","thread_name":"Monorepo cleanup"}
{"id":"aaaa","thread_name":"Billing, renamed"}
not json
{"id":"cccc"}
`
	got := Titles("jsonl-events", strings.NewReader(index))
	if got["bbbb"] != "Monorepo cleanup" {
		t.Errorf("got %q", got["bbbb"])
	}
	// A later line wins: the name is revised as a session finds its subject.
	if got["aaaa"] != "Billing, renamed" {
		t.Errorf("got %q, want the revised name", got["aaaa"])
	}
	if _, ok := got["cccc"]; ok {
		t.Error("an entry with no name became a title")
	}
	// A format that keeps its titles internally has no index to read.
	if Titles("jsonl-tree", strings.NewReader(index)) != nil {
		t.Error("asked the wrong format for an index")
	}
}

func TestJSONLEventsReadsALongLine(t *testing.T) {
	huge := strings.Repeat("x", 2<<20)
	doc := fmt.Sprintf(`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":%q}]}}`+"\n", huge)
	turns, err := JSONLEvents(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || len(turns[0].Text) != len(huge) {
		t.Fatalf("got %d turns, first %d bytes", len(turns), len(turns[0].Text))
	}
}
