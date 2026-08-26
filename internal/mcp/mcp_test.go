package mcp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func server() *Server {
	return &Server{
		Name: "grasshopper", Version: "1.0",
		Tools: []Tool{
			{Name: "second", Description: "b", Call: func(json.RawMessage) (string, error) { return "B", nil }},
			{Name: "first", Description: "a", Call: func(args json.RawMessage) (string, error) {
				return "A:" + string(args), nil
			}},
			{Name: "broken", Description: "c", Call: func(json.RawMessage) (string, error) {
				return "", errors.New("it did not work")
			}},
		},
	}
}

func call(t *testing.T, in string) []map[string]any {
	t.Helper()
	var out strings.Builder
	if err := server().Serve(strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	var replies []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var reply map[string]any
		if err := json.Unmarshal([]byte(line), &reply); err != nil {
			t.Fatalf("emitted a line that is not JSON: %q", line)
		}
		replies = append(replies, reply)
	}
	return replies
}

func TestInitialize(t *testing.T) {
	replies := call(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`+"\n")
	if len(replies) != 1 {
		t.Fatalf("got %d replies", len(replies))
	}
	result := replies[0]["result"].(map[string]any)
	if result["protocolVersion"] != protocolVersion {
		t.Errorf("protocolVersion = %v", result["protocolVersion"])
	}
	if result["capabilities"].(map[string]any)["tools"] == nil {
		t.Error("did not advertise tools")
	}
}

func TestToolsListIsSortedAndSchemad(t *testing.T) {
	replies := call(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`+"\n")
	tools := replies[0]["result"].(map[string]any)["tools"].([]any)

	var names []string
	for _, raw := range tools {
		tool := raw.(map[string]any)
		names = append(names, tool["name"].(string))
		// A tool with no schema still needs one, or the calling agent has nothing
		// to validate its arguments against.
		if tool["inputSchema"] == nil {
			t.Errorf("%s has no inputSchema", tool["name"])
		}
	}
	if strings.Join(names, ",") != "broken,first,second" {
		t.Errorf("names = %v, want sorted", names)
	}
}

func TestToolsCall(t *testing.T) {
	replies := call(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"first","arguments":{"x":1}}}`+"\n")
	content := replies[0]["result"].(map[string]any)["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if text != `A:{"x":1}` {
		t.Errorf("text = %q", text)
	}
}

// A tool that fails reports it as a result, not as a protocol error: the call
// arrived correctly and failed on its own terms, and the agent that made it
// should be told why in words it can act on.
func TestFailingToolIsAResultNotAProtocolError(t *testing.T) {
	replies := call(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"broken","arguments":{}}}`+"\n")
	if replies[0]["error"] != nil {
		t.Errorf("reported a protocol error: %v", replies[0]["error"])
	}
	result := replies[0]["result"].(map[string]any)
	if result["isError"] != true {
		t.Error("did not mark the result as an error")
	}
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "it did not work") {
		t.Errorf("text = %q", text)
	}
}

func TestUnknownToolAndMethod(t *testing.T) {
	for _, in := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nope","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"frobnicate"}`,
	} {
		replies := call(t, in+"\n")
		if replies[0]["error"] == nil {
			t.Errorf("no error for %s", in)
		}
	}
}

// Notifications carry no id and must be answered with silence, not with a reply
// the host is not expecting.
func TestNotificationsAreSilent(t *testing.T) {
	replies := call(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n")
	if len(replies) != 0 {
		t.Errorf("replied to a notification: %v", replies)
	}
}

// One bad frame must not take down a server the host depends on.
func TestMalformedFrameDoesNotStopTheServer(t *testing.T) {
	replies := call(t, "not json\n"+`{"jsonrpc":"2.0","id":2,"method":"ping"}`+"\n")
	if len(replies) != 2 {
		t.Fatalf("got %d replies, want an error and then the ping", len(replies))
	}
	if replies[0]["error"] == nil {
		t.Error("the bad frame was not reported")
	}
	if replies[1]["result"] == nil {
		t.Error("the server stopped answering after a bad frame")
	}
}

// Every reply is exactly one line: the transport is newline-delimited, so an
// embedded newline is a corrupt frame to the host.
func TestRepliesAreOneLineEach(t *testing.T) {
	var out strings.Builder
	in := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"first","arguments":{"s":"a\nb"}}}` + "\n"
	if err := server().Serve(strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(strings.TrimRight(out.String(), "\n"), "\n"); n != 0 {
		t.Errorf("one reply spanned %d lines:\n%s", n+1, out.String())
	}
}
