package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Somebody else's configuration file, with somebody else's settings in it.
// Adding a server must change one key and leave every other one exactly as it
// was — including the ones grasshopper has never heard of.
func TestWriteMCPConfigLeavesEverythingElseAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	before := `{
	  "editor.fontSize": 13,
	  "mcp": { "servers": { "theirs": { "command": "/bin/theirs" } }, "inputs": [] },
	  "workbench.colorTheme": "Dark"
	}`
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeMCPConfig(path, "mcp.servers", "grasshopper", "/bin/hop"); err != nil {
		t.Fatalf("writeMCPConfig: %v", err)
	}
	after := load(t, path)
	if after["editor.fontSize"] != float64(13) || after["workbench.colorTheme"] != "Dark" {
		t.Errorf("unrelated settings changed: %v", after)
	}
	servers := after["mcp"].(map[string]any)["servers"].(map[string]any)
	if _, theirs := servers["theirs"]; !theirs {
		t.Error("their server was dropped")
	}
	if _, ours := servers["grasshopper"]; !ours {
		t.Error("ours was not added")
	}
	if inputs, ok := after["mcp"].(map[string]any)["inputs"]; !ok || inputs == nil {
		t.Error("a sibling key inside the section was lost")
	}
}

func TestRemoveMCPConfigTakesOnlyOurs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"mcp":{"servers":{"theirs":{"command":"/bin/theirs"}}}}`), 0o644)
	writeMCPConfig(path, "mcp.servers", "grasshopper", "/bin/hop")
	if !hasMCPConfig(path, "mcp.servers", "grasshopper") {
		t.Fatal("it was not there to remove")
	}
	if err := removeMCPConfig(path, "mcp.servers", "grasshopper"); err != nil {
		t.Fatal(err)
	}
	if hasMCPConfig(path, "mcp.servers", "grasshopper") {
		t.Error("still registered")
	}
	servers := load(t, path)["mcp"].(map[string]any)["servers"].(map[string]any)
	if _, theirs := servers["theirs"]; !theirs {
		t.Error("their server went with ours")
	}
}

// An agent writes its config the first time it needs one, and grasshopper may
// arrive before it does. Neither absent nor empty is a failure.
func TestWriteMCPConfigCreatesWhatIsNotThere(t *testing.T) {
	for _, c := range []struct{ name, body string }{
		{"absent", ""},
		{"empty", "   \n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "deep", "mcp.json")
			if c.body != "" {
				os.MkdirAll(filepath.Dir(path), 0o755)
				os.WriteFile(path, []byte(c.body), 0o644)
			}
			if err := writeMCPConfig(path, "mcpServers", "grasshopper", "/bin/hop"); err != nil {
				t.Fatalf("writeMCPConfig: %v", err)
			}
			if !hasMCPConfig(path, "mcpServers", "grasshopper") {
				t.Error("not registered")
			}
		})
	}
}

// A file that will not parse is somebody's afternoon. It is reported, not
// replaced.
func TestWriteMCPConfigRefusesToClobberBrokenJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	broken := `{"mcp": {"servers": {  // a comment JSON does not allow`
	os.WriteFile(path, []byte(broken), 0o644)
	if err := writeMCPConfig(path, "mcp.servers", "grasshopper", "/bin/hop"); err == nil {
		t.Error("it should refuse rather than guess")
	}
	got, _ := os.ReadFile(path)
	if string(got) != broken {
		t.Errorf("the file was changed:\n%s", got)
	}
}

// Two agents keep their servers in TOML, where the section header answers the
// only question being asked.
func TestHasMCPConfigReadsTOMLSections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[cli]\ninstaller = \"internal\"\n\n[mcp_servers.grasshopper]\ncommand = \"/bin/hop\"\n"), 0o644)
	if !hasMCPConfig(path, "mcp_servers", "grasshopper") {
		t.Error("the section is right there")
	}
	if hasMCPConfig(path, "mcp_servers", "something-else") {
		t.Error("found a server that is not in the file")
	}
}

func load(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("the file we wrote is not JSON: %v", err)
	}
	return out
}
