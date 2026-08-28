package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// An agent with no command line can only be told about a server by writing the
// file it reads at startup. That is somebody else's file, so the rules here are
// narrow on purpose: read what is there, change one key, write it back. A file
// that will not parse is left exactly as it is and the failure is reported,
// because a config somebody spent an afternoon on is not worth an MCP server.

// writeMCPConfig adds grasshopper to an agent's config file, in place.
func writeMCPConfig(path, key, name, command string) error {
	config, err := readConfig(path)
	if err != nil {
		return err
	}
	servers := descend(config, key, true)
	if servers == nil {
		return fmt.Errorf("%s: %s is not a place servers can go", path, key)
	}
	servers[name] = map[string]any{
		"command": command,
		"args":    []any{"mcp"},
	}
	return saveConfig(path, config)
}

// descend walks a dotted key to the map that holds the servers.
//
// One agent keeps them at the top of its config and another keeps them three
// words in, under a section it also uses for other settings. A dotted key
// addresses both, and nothing between here and there is disturbed: create says
// whether a missing step should be made or the walk should simply fail.
func descend(config map[string]any, key string, create bool) map[string]any {
	node := config
	for _, step := range strings.Split(key, ".") {
		next, ok := node[step].(map[string]any)
		if !ok {
			if !create {
				return nil
			}
			if _, taken := node[step]; taken {
				return nil // something else already lives here; leave it alone
			}
			next = map[string]any{}
			node[step] = next
		}
		node = next
	}
	return node
}

// removeMCPConfig takes grasshopper back out and leaves everything else.
func removeMCPConfig(path, key, name string) error {
	config, err := readConfig(path)
	if err != nil {
		return err
	}
	servers := descend(config, key, false)
	if servers == nil {
		return nil
	}
	if _, there := servers[name]; !there {
		return nil
	}
	// Only this key. An empty map is left behind rather than the section
	// removed: an absent section is a different statement from an empty one, and
	// which of the two an agent wanted is not grasshopper's to decide.
	delete(servers, name)
	return saveConfig(path, config)
}

// hasMCPConfig reports whether the agent has been told, without changing
// anything. This is what lets doctor answer the question that matters — is
// grasshopper actually reachable from here — rather than only whether its
// sessions can be read.
func hasMCPConfig(path, key, name string) bool {
	// A config written in TOML is not read, only looked at. Its section header
	// says everything the question asks — is this server in here — and parsing a
	// second configuration language to learn it would be a lot of code for a
	// column in a diagnostic.
	if strings.HasSuffix(path, ".toml") {
		raw, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		return strings.Contains(string(raw), "["+key+"."+name+"]")
	}
	config, err := readConfig(path)
	if err != nil {
		return false
	}
	servers := descend(config, key, false)
	_, there := servers[name]
	return there
}

// readConfig loads the file, treating "not there" and "empty" as an empty
// object. Both are ordinary: the agent writes the file the first time it needs
// one, and grasshopper may arrive before it does.
func readConfig(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(trimSpace(raw)) == 0 {
		return map[string]any{}, nil
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("%s is not JSON this can edit safely: %w", path, err)
	}
	return config, nil
}

// saveConfig writes through a temporary file in the same directory, so an
// interrupted write cannot leave somebody's configuration half-replaced.
func saveConfig(path string, config map[string]any) error {
	body, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".hop-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func trimSpace(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && isSpace(b[start]) {
		start++
	}
	for end > start && isSpace(b[end-1]) {
		end--
	}
	return b[start:end]
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}
