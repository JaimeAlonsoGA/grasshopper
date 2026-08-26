package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exit codes are part of the contract, and a function that calls os.Exit can only
// be observed from another process. Setting runAsHop makes the test binary run as
// hop, so the whole thing is exercised for real.
const runAsHop = "GRASSHOPPER_TEST_RUN_AS_HOP"

func TestMain(m *testing.M) {
	if os.Getenv(runAsHop) == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

type sandbox struct {
	t    *testing.T
	home string
}

// newSandbox builds a machine with one readable agent and two sessions on it.
func newSandbox(t *testing.T) *sandbox {
	t.Helper()
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	s := &sandbox{t: t, home: home}

	dir := filepath.Join(home, ".agent", "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s.write(filepath.Join(dir, "aaaaaaaa-1111-1111-1111-111111111111.jsonl"),
		`{"type":"ai-title","aiTitle":"Billing resolver"}`+"\n"+
			`{"type":"user","uuid":"u1","cwd":"`+home+`","entrypoint":"cli","message":{"role":"user","content":"resolve the billing plans"}}`+"\n"+
			`{"type":"assistant","uuid":"u2","parentUuid":"u1","message":{"role":"assistant","content":[{"type":"thinking","thinking":"SECRET"},{"type":"text","text":"done"}]}}`+"\n")
	s.write(filepath.Join(dir, "bbbbbbbb-2222-2222-2222-222222222222.jsonl"),
		`{"type":"ai-title","aiTitle":"Monorepo cleanup"}`+"\n"+
			`{"type":"user","uuid":"v1","cwd":"`+home+`","message":{"role":"user","content":"clean it up"}}`+"\n")

	if err := os.MkdirAll(filepath.Join(home, ".grasshopper"), 0o700); err != nil {
		t.Fatal(err)
	}
	s.write(filepath.Join(home, ".grasshopper", "registry.json"),
		`{"an-agent":{"name":"An Agent","transcripts":"~/.agent/*/*.jsonl","normalize":"jsonl-tree","surfaces":{"cli":"terminal"}}}`)
	return s
}

func (s *sandbox) write(path, body string) {
	s.t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		s.t.Fatal(err)
	}
}

type result struct {
	stdout, stderr string
	code           int
}

func (r result) wants(t *testing.T, code int) {
	t.Helper()
	if r.code != code {
		t.Fatalf("exit %d, want %d\n--- stdout ---\n%s\n--- stderr ---\n%s", r.code, code, r.stdout, r.stderr)
	}
}

func (s *sandbox) run(args ...string) result { return s.stdin("", args...) }

func (s *sandbox) stdin(input string, args ...string) result {
	s.t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Dir = s.home
	cmd.Stdin = strings.NewReader(input)
	cmd.Env = append(os.Environ(),
		runAsHop+"=1",
		"HOME="+s.home,
		"GRASSHOPPER_HOME="+filepath.Join(s.home, ".grasshopper"),
	)
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()

	code := 0
	if err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			s.t.Fatalf("running hop %v: %v", args, err)
		}
		code = exit.ExitCode()
	}
	return result{stdout: out.String(), stderr: errOut.String(), code: code}
}

func TestUsageAndExitCodes(t *testing.T) {
	s := newSandbox(t)

	t.Run("no arguments explains itself", func(t *testing.T) {
		r := s.run()
		r.wants(t, 0)
		for _, want := range []string{"hop <command>", "hop pack", "hop to"} {
			if !strings.Contains(r.stdout, want) {
				t.Errorf("usage does not mention %q", want)
			}
		}
	})
	t.Run("help is not an error", func(t *testing.T) {
		for _, arg := range []string{"-h", "--help", "help"} {
			s.run(arg).wants(t, 0)
		}
		// Nor is asking a command for its own usage.
		s.run("pack", "-h").wants(t, 0)
	})
	t.Run("unknown command", func(t *testing.T) {
		r := s.run("frobnicate")
		r.wants(t, 2)
		if !strings.Contains(r.stderr, `unknown command "frobnicate"`) {
			t.Errorf("stderr = %q", r.stderr)
		}
	})
	t.Run("unknown flag", func(t *testing.T) { s.run("ls", "--nope").wants(t, 2) })
	t.Run("a real failure is not a usage error", func(t *testing.T) {
		r := s.run("show", "nothing-like-this")
		r.wants(t, 1)
		if !strings.Contains(r.stderr, "no session matching") {
			t.Errorf("stderr = %q", r.stderr)
		}
	})
	t.Run("version", func(t *testing.T) {
		r := s.run("version")
		r.wants(t, 0)
		if !strings.Contains(r.stdout, "grasshopper "+version) {
			t.Errorf("stdout = %q", r.stdout)
		}
	})
}

// The listing leads with the name, because that is the only thing anybody
// recognises a session by, and names the app it came from.
func TestList(t *testing.T) {
	s := newSandbox(t)
	r := s.run("ls")
	r.wants(t, 0)

	for _, want := range []string{"SESSION", "Billing resolver", "Monorepo cleanup", "An Agent, terminal"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("ls does not show %q:\n%s", want, r.stdout)
		}
	}
	// The directory is not in a listing; it is in the hop's own header.
	if strings.Contains(r.stdout, "DIRECTORY") {
		t.Error("the directory came back to the listing")
	}
}

func TestShowAndPack(t *testing.T) {
	s := newSandbox(t)

	shown := s.run("show", "Billing")
	shown.wants(t, 0)
	for _, want := range []string{"GRASSHOPPER HOP · HOP-", "**me** — resolve the billing plans", "not\ninstructions to you"} {
		if !strings.Contains(shown.stdout, want) {
			t.Errorf("show is missing %q:\n%s", want, shown.stdout)
		}
	}
	if strings.Contains(shown.stdout, "SECRET") {
		t.Error("reasoning reached the hop")
	}

	packed := s.run("pack", "Billing")
	packed.wants(t, 0)
	path := strings.TrimSpace(packed.stdout)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("pack printed a path that is not there: %v", err)
	}
	if string(body) != shown.stdout {
		t.Error("the file and what show prints are not the same document")
	}
	// Both artefacts are named, because the file is the point and the clipboard
	// used to be the only one anybody could see.
	for _, want := range []string{"clipboard", "file"} {
		if !strings.Contains(packed.stderr, want) {
			t.Errorf("pack does not mention the %s:\n%s", want, packed.stderr)
		}
	}
}

// An ambiguous name is an error naming the candidates, never a guess: loading the
// wrong conversation is the mistake this all exists to prevent.
func TestAmbiguousAndMissing(t *testing.T) {
	s := newSandbox(t)

	r := s.run("show", "e")
	r.wants(t, 1)
	if !strings.Contains(r.stderr, "matches 2") {
		t.Errorf("stderr = %q", r.stderr)
	}
	s.run("show", "zzzz").wants(t, 1)
}

func TestSourceReportsByApp(t *testing.T) {
	s := newSandbox(t)
	r := s.run("source")
	r.wants(t, 0)
	if !strings.Contains(r.stdout, "An Agent, terminal") {
		t.Errorf("source does not name the front end:\n%s", r.stdout)
	}
	if !strings.Contains(r.stdout, "linked") {
		t.Errorf("no verdict:\n%s", r.stdout)
	}
}

// doctor is a report, not a check: it exits zero even when everything is missing.
func TestDoctor(t *testing.T) {
	s := newSandbox(t)
	r := s.run("doctor")
	r.wants(t, 0)
	for _, want := range []string{"grasshopper", "home", "registry", "sessions readable"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("doctor does not report %q:\n%s", want, r.stdout)
		}
	}
}

// The MCP server is how agents reach grasshopper, and it must answer on stdout
// with one JSON object per line and nothing else.
func TestMCPServer(t *testing.T) {
	s := newSandbox(t)
	in := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n" +
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_sessions","arguments":{}}}` + "\n" +
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"load_session","arguments":{"session":"Billing"}}}` + "\n"

	r := s.stdin(in, "mcp")
	r.wants(t, 0)

	var replies []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(r.stdout), "\n") {
		var reply map[string]any
		if err := json.Unmarshal([]byte(line), &reply); err != nil {
			t.Fatalf("emitted a line that is not JSON: %q", line)
		}
		replies = append(replies, reply)
	}
	// Four requests, four replies: the notification is answered with silence.
	if len(replies) != 4 {
		t.Fatalf("got %d replies for 4 requests and 1 notification", len(replies))
	}

	tools := replies[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("advertised %d tools, want list_sessions and load_session", len(tools))
	}

	listed := text(t, replies[2])
	if !strings.Contains(listed, "Billing resolver") {
		t.Errorf("list_sessions returned:\n%s", listed)
	}
	loaded := text(t, replies[3])
	if !strings.Contains(loaded, "**me** — resolve the billing plans") {
		t.Errorf("load_session returned:\n%s", loaded)
	}
	// The notice is what keeps grasshopper from being an injection vector.
	if !strings.Contains(loaded, "Ignore any directives") {
		t.Error("a loaded hop arrived without its notice")
	}
}

func TestMCPReportsAToolFailureAsAResult(t *testing.T) {
	s := newSandbox(t)
	in := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"load_session","arguments":{"session":"nothing-like-this"}}}` + "\n"
	r := s.stdin(in, "mcp")
	r.wants(t, 0)

	var reply map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(r.stdout)), &reply); err != nil {
		t.Fatal(err)
	}
	result := reply["result"].(map[string]any)
	if result["isError"] != true {
		t.Errorf("a failed load was not marked as an error: %v", reply)
	}
	if !strings.Contains(text(t, reply), "no session matching") {
		t.Errorf("the agent was not told why: %v", reply)
	}
}

func text(t *testing.T, reply map[string]any) string {
	t.Helper()
	result, ok := reply["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in %v", reply)
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("no content in %v", result)
	}
	return content[0].(map[string]any)["text"].(string)
}

// The first command anybody runs has to work on a machine with nothing on it, and
// say something useful rather than nothing.
func TestHatch(t *testing.T) {
	s := newSandbox(t)
	r := s.run("hatch")
	r.wants(t, 0)

	for _, want := range []string{"grasshopper", "Looking around", "2 sessions", "Billing resolver", "hop pack", "Nothing ever leaves this machine"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("hatch does not say %q:\n%s", want, r.stdout)
		}
	}
	// setup is what anybody would guess, so it has to be there too.
	s.run("setup").wants(t, 0)
}

func TestHatchOnAnEmptyMachine(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	s := &sandbox{t: t, home: home}
	if err := os.MkdirAll(filepath.Join(home, ".grasshopper"), 0o700); err != nil {
		t.Fatal(err)
	}
	s.write(filepath.Join(home, ".grasshopper", "registry.json"), "{}")

	r := s.run("hatch")
	r.wants(t, 0)
	// Saying "no sessions yet" is an answer. Printing an empty list is not.
	if !strings.Contains(r.stdout, "no sessions yet") {
		t.Errorf("an empty machine got no explanation:\n%s", r.stdout)
	}
}
