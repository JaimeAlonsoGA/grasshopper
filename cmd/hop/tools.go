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
				"limit":  field("integer", "How many to return, newest first. Default 25."),
				"active": field("boolean", "Only sessions written to in the last few minutes."),
				"match": field("string", "Narrow to sessions whose title, id, agent or app contains "+
					"this. Use it rather than a large limit when somebody names what they are after — "+
					"a machine with hundreds of sessions answers the question in one call instead of "+
					"returning a list to be read."),
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
				"last": field("integer", "Carry only the last N messages instead of the whole "+
					"conversation. Use it when somebody asks about the end of a thread — what was "+
					"just decided, the last thing tried, what it concluded — rather than the work "+
					"as a whole. The first thing they asked for is carried as well, so the excerpt "+
					"still has its question. Omit for the whole conversation."),
			}, "session"),
			Call: loadTool,
		},
	}
}

func listing(raw json.RawMessage) (string, error) {
	var args struct {
		Limit  int    `json:"limit"`
		Active bool   `json:"active"`
		Match  string `json:"match"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("unreadable arguments: %w", err)
		}
	}
	// Zero is "no preference", which is a listing an agent will read into its
	// context, so it is capped. Negative is "all of them", which only somebody
	// at a terminal asks for.
	if args.Limit == 0 {
		args.Limit = 25
	}

	all, err := sessions.List()
	if err != nil {
		return "", err
	}
	if args.Active {
		all = onlyActive(all)
	}
	// Narrowing before counting, so the tally at the bottom answers "how many
	// are there like this" rather than "how many are there".
	all = sessions.Matching(all, args.Match)

	// Title second and bounded, so the columns after it line up. The directory is
	// not here: it is in the hop's own header, where somebody reading a
	// conversation needs it — in a list of forty it is the same word forty times.
	surface := namer()
	rows := [][]string{{"ID", "SESSION", "FROM", "WHEN"}}
	shown := 0
	for _, s := range all {
		if args.Limit > 0 && shown >= args.Limit {
			break
		}
		shown++
		rows = append(rows, []string{s.ID, truncate(s.Label(), titleWidth), surface(s), ago(s.When)})
	}
	if shown == 0 {
		if args.Match != "" {
			return fmt.Sprintf("No session matches %q. Ask again without it to see them all.", args.Match), nil
		}
		return "No sessions found. hop doctor shows where grasshopper is looking.", nil
	}

	var out strings.Builder
	writeTable(&out, rows)
	fmt.Fprintf(&out, "\n%s\n", tally(shown, len(all), args.Match))
	return out.String(), nil
}

// listTool is the listing as an agent reads it, which ends by naming the tool
// that acts on a row. The terminal ends by naming a command instead — same
// listing, different next step, and neither audience is shown the other's.
func listTool(raw json.RawMessage) (string, error) {
	text, err := listing(raw)
	if err != nil || strings.HasPrefix(text, "No session") {
		return text, err
	}
	return text + "Load one with load_session.\n", nil
}

// tally says how much of the answer is on screen, and never implies there is no
// more when there is.
func tally(shown, total int, match string) string {
	switch {
	case shown >= total && match != "":
		return fmt.Sprintf("%d sessions matching %q.", total, match)
	case shown >= total:
		return fmt.Sprintf("%d sessions.", total)
	case match != "":
		return fmt.Sprintf("%d of %d matching %q, newest first.", shown, total, match)
	default:
		return fmt.Sprintf("%d of %d sessions, newest first.", shown, total)
	}
}

func onlyActive(all []sessions.Session) []sessions.Session {
	var out []sessions.Session
	for _, s := range all {
		if s.Active {
			out = append(out, s)
		}
	}
	return out
}

func loadTool(raw json.RawMessage) (string, error) {
	var args struct {
		Session string `json:"session"`
		Last    int    `json:"last"`
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
	b, err := session.Load(bundle.Cap, args.Last)
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
