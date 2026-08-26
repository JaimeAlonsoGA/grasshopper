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
	// an answer. Optional: an unnamed surface is shown as the agent recorded it.
	Surfaces map[string]string `json:"surfaces,omitempty"`

	// Launch is the command that starts this agent, for opening a new session
	// with a conversation already in it. Optional: an agent with no launch
	// command can still be listed, read, and copied to the clipboard.
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
			Transcripts: "~/.claude/projects/*/*.jsonl",
			Normalize:   "jsonl-tree",
			Launch:      "claude",
			Surfaces: map[string]string{
				"cli":            "Claude Code, terminal",
				"claude-desktop": "Claude desktop app",
				"claude-vscode":  "Claude in VS Code",
				"remote_mobile":  "Claude on a phone",
			},
		},
		"codex": {
			Transcripts: "~/.codex/sessions/*/*/*/*.jsonl,~/.codex/archived_sessions/*.jsonl",
			Normalize:   "jsonl-events",
			Index:       "~/.codex/session_index.jsonl",
			Launch:      "codex",
			Surfaces: map[string]string{
				"Codex Desktop": "ChatGPT desktop app",
				"Codex CLI":     "Codex, terminal",
				"codex_cli_rs":  "Codex, terminal",
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

// Load reads the registry, falling back to the shipped default when there is no
// file yet. Reading never writes: a command that only wants to know which agents
// exist has no business creating files as a side effect.
func Load() (Registry, error) {
	b, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return nil, err
	}
	var r Registry
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", Path(), err)
	}
	return merge(r), nil
}

// merge fills blanks in a known agent from the shipped default, and adds nothing
// else.
//
// The registry is a file people edit and grasshopper never overwrites, which
// otherwise means a field added in a later version never reaches anybody who
// already has one. Filling only what is empty keeps their edits, and refusing to
// add keys they do not have keeps an agent they deleted deleted.
func merge(r Registry) Registry {
	for key, shipped := range Default() {
		theirs, ok := r[key]
		if !ok {
			continue
		}
		if theirs.Transcripts == "" {
			theirs.Transcripts = shipped.Transcripts
		}
		if theirs.Normalize == "" {
			theirs.Normalize = shipped.Normalize
		}
		if theirs.Launch == "" {
			theirs.Launch = shipped.Launch
		}
		if theirs.Index == "" {
			theirs.Index = shipped.Index
		}
		if len(theirs.Surfaces) == 0 {
			theirs.Surfaces = shipped.Surfaces
		}
		r[key] = theirs
	}
	return r
}

// Write materialises the default registry so it can be edited. It refuses to
// touch a file that already exists: the user's edits are the point of the file.
func Write() (created bool, err error) {
	path := Path()
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	b, err := json.MarshalIndent(Default(), "", "  ")
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(path, append(b, '\n'), 0o644)
}

// Get resolves a key, and says what the alternatives were when it cannot.
func (r Registry) Get(key string) (Agent, error) {
	a, ok := r[key]
	if !ok {
		return Agent{}, fmt.Errorf("unknown agent %q (registry has: %s)", key, strings.Join(r.Keys(), ", "))
	}
	return a, nil
}

// Surface names one of an agent's front ends. An unrecognised value is returned
// as it was recorded: a name grasshopper has not been taught is still better than
// a blank, and it is what will still be true after a rename.
func (r Registry) Surface(agent, recorded string) string {
	if named, ok := r[agent].Surfaces[recorded]; ok {
		return named
	}
	if recorded != "" {
		return recorded
	}
	// Older transcripts predate the field. Naming the agent is honest; inventing
	// a front end for them would not be.
	return agent + ", surface not recorded"
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
