// Package registry is the only place in hop that knows a product exists.
//
// Everywhere else an agent is an opaque string key. That boundary is what makes
// the rest of the tool indifferent to the market: an agent that reads AGENTS.md
// is supported by adding four fields of JSON, and one that does not is not
// supported by any amount of code elsewhere. A test enforces the boundary
// mechanically, because an invariant nothing checks is a preference.
//
// The file is JSON rather than TOML for one reason: JSON is in the standard
// library and TOML is a dependency.
package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Agent is everything hop needs to know about a coding agent.
//
// Transcripts and Normalize are how a session is captured by reading. When
// Normalize is empty the agent is captured cooperatively instead — the AGENTS.md
// block asks it to write its own handoff — which is why the tool ships one
// reader and still works with everything.
type Agent struct {
	// Name is what to call this agent to a person. The key is for typing; this is
	// for reading.
	Name string `json:"name,omitempty"`

	// Transcripts is one glob, or several separated by commas. Several, because an
	// agent may keep its sessions in more than one place — one directory for the
	// current ones and another for the archived — and no single glob spans them.
	Transcripts string `json:"transcripts"`
	Normalize   string `json:"normalize"`

	// Index is a file that names an agent's sessions, when the titles are not in
	// the transcripts themselves. One format records its title inside the
	// conversation; another keeps a separate list. Optional.
	Index string `json:"index,omitempty"`

	// Surfaces names an agent's front ends, keyed by the value its transcripts
	// record. One agent ships a terminal, a desktop app, an editor extension and
	// a phone client that all write to the same place, and telling somebody they
	// have "claude-code" when what they installed was three separate apps is not
	// an answer.
	//
	// The value is the front end alone — "terminal", "desktop app" — and the
	// agent's own name is put in front of it. Written that way round, the
	// convention cannot drift: it is composed rather than typed, so nobody ends up
	// with "Claude in VS Code" beside "Codex, terminal" in the same column.
	Surfaces map[string]string `json:"surfaces,omitempty"`

	// MCPAdd and MCPRemove are how to tell this agent about an MCP server, as
	// arguments to its own command line. {name} and {command} are filled in.
	//
	// Arguments rather than a shell string, and data rather than code, because
	// they differ per agent for no reason anybody can predict — one takes a
	// --scope, the next has never heard of it — and a new agent should cost a
	// line of JSON. Registering through an agent's own command line also means
	// grasshopper never edits somebody else's configuration file by hand.
	MCPAdd    []string `json:"mcp_add,omitempty"`
	MCPRemove []string `json:"mcp_remove,omitempty"`

	// Launch is where to find the command that starts this agent: a name to look
	// up on PATH, a path to a file, or several of either separated by commas and
	// tried in order.
	//
	// Several, because an app can ship its own command line inside itself without
	// putting it on anybody's PATH — the ChatGPT app carries a working codex
	// binary in its Resources folder. "Not installed" was the wrong answer for
	// something already on the disk.
	Launch string `json:"launch,omitempty"`
}

type Registry map[string]Agent

// Default is the shipped registry. Only three entries name a product, and every
// other tool that reads AGENTS.md works by adding one like them — or by needing
// nothing at all, since the context file is a shared standard.
func Default() Registry {
	return Registry{
		// One entry covers a terminal, a desktop app and an editor extension:
		// they are the same program underneath and they all write here.
		"claude-code": {
			Name:        "Claude Code",
			Transcripts: "~/.claude/projects/*/*.jsonl",
			Normalize:   "jsonl-tree",
			Launch:      "claude,~/.local/bin/claude,/opt/homebrew/bin/claude",
			MCPAdd:      []string{"mcp", "add", "{name}", "--scope", "user", "--", "{command}", "mcp"},
			MCPRemove:   []string{"mcp", "remove", "{name}", "--scope", "user"},
			Surfaces: map[string]string{
				"cli":            "terminal",
				"claude-desktop": "desktop app",
				"claude-vscode":  "VS Code",
				"remote_mobile":  "phone",
			},
		},
		"codex": {
			Name:        "Codex",
			Transcripts: "~/.codex/sessions/*/*/*/*.jsonl,~/.codex/archived_sessions/*.jsonl",
			Normalize:   "jsonl-events",
			Index:       "~/.codex/session_index.jsonl",
			Launch:      "codex,/Applications/ChatGPT.app/Contents/Resources/codex",
			MCPAdd:      []string{"mcp", "add", "{name}", "--", "{command}", "mcp"},
			MCPRemove:   []string{"mcp", "remove", "{name}"},
			Surfaces: map[string]string{
				"Codex Desktop": "ChatGPT app",
				"Codex CLI":     "terminal",
				"codex_cli_rs":  "terminal",
				"codex-tui":     "terminal",
				"codex_vscode":  "VS Code",
			},
		},
		// Every editor in the VS Code family writes its chat to the same place
		// under its own name. Listing the forks costs nothing when they are not
		// installed — a glob that matches no file contributes no session — and
		// the day one of them is, it is read without a release. Cursor keeps its
		// own Composer in SQLite rather than here, so its folder appears in this
		// list and stays empty until that changes.
		"vscode-chat": {
			Name: "Copilot",
			Transcripts: "~/Library/Application Support/Code/User/workspaceStorage/*/chatSessions/*.jsonl," +
				"~/Library/Application Support/Code - Insiders/User/workspaceStorage/*/chatSessions/*.jsonl," +
				"~/Library/Application Support/Cursor/User/workspaceStorage/*/chatSessions/*.jsonl," +
				"~/Library/Application Support/Windsurf/User/workspaceStorage/*/chatSessions/*.jsonl," +
				"~/Library/Application Support/Trae/User/workspaceStorage/*/chatSessions/*.jsonl," +
				"~/Library/Application Support/Antigravity/User/workspaceStorage/*/chatSessions/*.jsonl," +
				"~/.config/Code/User/workspaceStorage/*/chatSessions/*.jsonl," +
				"~/.config/Cursor/User/workspaceStorage/*/chatSessions/*.jsonl",
			Normalize: "jsonl-patch",
			// This format writes no entrypoint of its own, so the folder the file
			// was found in is what names the editor.
			Surfaces: map[string]string{
				"Code":            "VS Code",
				"Code - Insiders": "VS Code Insiders",
				"Cursor":          "Cursor",
				"Windsurf":        "Windsurf",
				"Trae":            "Trae",
				"Antigravity":     "Antigravity",
			},
		},
	}
}

// Home is where grasshopper keeps its own state. GRASSHOPPER_HOME exists so a
// test never has to touch the real one.
func Home() string {
	if h := os.Getenv("GRASSHOPPER_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".grasshopper"
	}
	return filepath.Join(home, ".grasshopper")
}

func Path() string { return filepath.Join(Home(), "registry.json") }

// Load reads the registry: the shipped agents, with the user's file laid over
// them field by field.
//
// The file holds changes, not a copy of the defaults. That is the whole point:
// grasshopper will never overwrite a file somebody may have edited, so a file
// containing copies of the defaults freezes them at the version that wrote it —
// and a glob or a launch path improved later never reaches anybody. Three
// improvements were silently lost that way before this worked like this.
func Load() (Registry, error) {
	b, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return nil, err
	}
	var theirs Registry
	if err := json.Unmarshal(b, &theirs); err != nil {
		return nil, fmt.Errorf("%s: %w", Path(), err)
	}
	return merge(theirs), nil
}

// merge lays the user's file over the shipped agents. A field they set wins; a
// field they left out is whatever this version ships. An agent only they know
// about is added as it stands.
func merge(theirs Registry) Registry {
	out := Default()
	for key, override := range theirs {
		agent, known := out[key]
		if !known {
			out[key] = override
			continue
		}
		if override.Name != "" {
			agent.Name = override.Name
		}
		if override.Transcripts != "" {
			agent.Transcripts = override.Transcripts
		}
		if override.Normalize != "" {
			agent.Normalize = override.Normalize
		}
		if override.Launch != "" {
			agent.Launch = override.Launch
		}
		if override.Index != "" {
			agent.Index = override.Index
		}
		if len(override.Surfaces) > 0 {
			agent.Surfaces = override.Surfaces
		}
		if len(override.MCPAdd) > 0 {
			agent.MCPAdd = override.MCPAdd
		}
		if len(override.MCPRemove) > 0 {
			agent.MCPRemove = override.MCPRemove
		}
		out[key] = agent
	}
	return out
}

// Write creates an empty registry, ready to be edited. Empty, not a copy of the
// defaults: a copy would freeze this version's globs and launch paths into
// somebody's file forever, and grasshopper will not overwrite it to fix them.
func Write() (created bool, err error) {
	path := Path()
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte("{}\n"), 0o600)
}

// Get resolves a key, and says what the alternatives were when it cannot.
func (r Registry) Get(key string) (Agent, error) {
	a, ok := r[key]
	if !ok {
		return Agent{}, fmt.Errorf("unknown agent %q (registry has: %s)", key, strings.Join(r.Keys(), ", "))
	}
	return a, nil
}

// Called is what to show a person for an agent. The key is a thing to type, not
// a thing to read.
func (r Registry) Called(key string) string {
	if name := r[key].Name; name != "" {
		return name
	}
	return key
}

// MCPArgs fills in an agent's registration arguments.
func MCPArgs(template []string, name, command string) []string {
	args := make([]string, 0, len(template))
	for _, arg := range template {
		arg = strings.ReplaceAll(arg, "{name}", name)
		arg = strings.ReplaceAll(arg, "{command}", command)
		args = append(args, arg)
	}
	return args
}

// Surface names one of an agent's front ends, always as "<agent>, <front end>".
//
// One shape for all of them, composed rather than typed, so the column reads as
// one convention. A value grasshopper has not been taught is shown as the agent
// recorded it: a name nobody chose is still better than a blank, and it is what
// will still be true after somebody renames a product.
func (r Registry) Surface(agent, recorded string) string {
	name := r.Called(agent)
	if front, ok := r[agent].Surfaces[recorded]; ok {
		return name + ", " + front
	}
	if recorded != "" {
		return name + ", " + recorded
	}
	// Older transcripts predate the field entirely.
	return name + ", unknown"
}

func (r Registry) Keys() []string {
	keys := make([]string, 0, len(r))
	for k := range r {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// expand resolves a leading ~ the way a shell would. The registry is a file
// people edit by hand, so it has to accept what they will actually type.
func expand(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~"))
}
