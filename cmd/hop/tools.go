package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"grasshopper/internal/bundle"
	"grasshopper/internal/mcp"
	"grasshopper/internal/sessions"
	"grasshopper/internal/transcript"
)

// The two tools are the whole product. Everything else in this binary exists so a
// person can check what the agents are seeing.
func tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name: "list_sessions",
			Description: "List the AI conversations on this machine — every Claude Code, " +
				"Cowork and editor-extension session — newest first, with the title each one " +
				"gave itself, its working directory, and whether it was written to recently. " +
				"Use it to find the session a person is referring to before loading it.",
			Schema: object(map[string]any{
				"limit":  field("integer", "How many to return. Default 25."),
				"active": field("boolean", "Only sessions written to in the last few minutes."),
			}),
			Call: listTool,
		},
		{
			Name: "load_session",
			Description: "Load one conversation from this machine into the current context. " +
				"Returns the whole exchange as reference material, with the path to the " +
				"untouched original for when more is needed. Identify the session by its id, " +
				"by a fragment of its title, or by its path — whatever list_sessions showed.",
			Schema: object(map[string]any{
				"session": field("string", "Id, title fragment, or path of the session to load."),
			}, "session"),
			Call: loadTool,
		},
	}
}

func listTool(raw json.RawMessage) (string, error) {
	var args struct {
		Limit  int  `json:"limit"`
		Active bool `json:"active"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("unreadable arguments: %w", err)
		}
	}
	if args.Limit <= 0 {
		args.Limit = 25
	}

	all, err := sessions.List()
	if err != nil {
		return "", err
	}

	// Title second and bounded, so the columns after it line up. The directory is
	// not here: it is in the hop's own header, where somebody reading a
	// conversation needs it — in a list of forty it is the same word forty times.
	surface := namer()
	rows := [][]string{{"ID", "SESSION", "FROM", "WHEN"}}
	shown := 0
	for _, s := range all {
		if args.Active && !s.Active {
			continue
		}
		if shown >= args.Limit {
			break
		}
		shown++
		rows = append(rows, []string{s.ID, truncate(s.Label(), titleWidth), surface(s), ago(s.When)})
	}
	if shown == 0 {
		return "No sessions found. hop doctor shows where grasshopper is looking.", nil
	}

	var out strings.Builder
	writeTable(&out, rows)
	fmt.Fprintf(&out, "\n%d of %d sessions. Load one with load_session.\n", shown, len(all))
	return out.String(), nil
}

func loadTool(raw json.RawMessage) (string, error) {
	var args struct {
		Session string `json:"session"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("unreadable arguments: %w", err)
	}
	if strings.TrimSpace(args.Session) == "" {
		return "", fmt.Errorf("name a session; list_sessions shows them")
	}

	session, err := sessions.Find(args.Session)
	if err != nil {
		return "", err
	}
	b, err := session.Load(bundle.Cap)
	if err != nil {
		return "", err
	}
	return bundle.Render(b), nil
}

func runMCP(args []string) error {
	if _, err := parse(flags("mcp", ""), args); err != nil {
		return err
	}
	// stdout belongs to the protocol from here on. Anything this process prints
	// there is a corrupt frame to the host.
	server := &mcp.Server{Name: "grasshopper", Version: version, Tools: tools()}
	return server.Serve(os.Stdin, os.Stdout)
}

func object(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func field(kind, description string) map[string]any {
	return map[string]any{"type": kind, "description": description}
}

// titleWidth keeps a listing to one line per session, and keeps the columns after
// the title aligned. Sessions whose agent gave them no title fall back to the
// first thing said, which can run for paragraphs.
const titleWidth = 52

func truncate(s string, width int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	return string(runes[:width-1]) + "…"
}

// short trims a home-relative path so a listing stays one line per session.
func short(dir string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(dir, home) {
		return "~" + strings.TrimPrefix(dir, home)
	}
	if dir == "" {
		return "—"
	}
	return dir
}

// readerNames is the formats grasshopper can actually read, for an error message
// that tells somebody what to type instead of only what was wrong.
func readerNames() []string { return transcript.Names() }
