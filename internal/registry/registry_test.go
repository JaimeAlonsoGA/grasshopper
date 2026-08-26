package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func home(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GRASSHOPPER_HOME", dir)
	if contents != "" {
		if err := os.WriteFile(filepath.Join(dir, "registry.json"), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoadFallsBackWithoutWriting(t *testing.T) {
	dir := home(t, "")
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != len(Default()) {
		t.Errorf("got %d agents, want the shipped default", len(r))
	}
	// Reading must not create files: a command that only wants to know which
	// agents exist has no business leaving something behind.
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("Load wrote %v", entries)
	}
}

// A field added in a later version has to reach somebody who already has a
// registry, because grasshopper never overwrites theirs.
func TestLoadFillsBlanksInAKnownAgent(t *testing.T) {
	home(t, `{"claude-code":{"transcripts":"~/mine/*.jsonl","normalize":"jsonl-tree"}}`)
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	got := r["claude-code"]
	if got.Transcripts != "~/mine/*.jsonl" {
		t.Errorf("overwrote their edit: %q", got.Transcripts)
	}
	if got.Launch != Default()["claude-code"].Launch {
		t.Errorf("Launch = %q, want the shipped default filled in", got.Launch)
	}
}

// An agent they deleted stays deleted. Filling blanks must not become adding keys.
func TestLoadDoesNotResurrectADeletedAgent(t *testing.T) {
	home(t, `{"mine":{"transcripts":"~/mine/*.jsonl","normalize":"jsonl-tree","launch":"mine"}}`)
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 1 {
		t.Fatalf("got %d agents, want only theirs: %v", len(r), r.Keys())
	}
	if _, back := r["claude-code"]; back {
		t.Error("a deleted agent came back")
	}
}

func TestLoadReportsABrokenFile(t *testing.T) {
	home(t, "{ not json")
	_, err := Load()
	if err == nil {
		t.Fatal("a broken registry must not silently become the default")
	}
	if !strings.Contains(err.Error(), "registry.json") {
		t.Errorf("error %q does not name the file", err)
	}
}

func TestWriteOnlyCreates(t *testing.T) {
	home(t, "")
	created, err := Write()
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	body, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(body), "claude-code", "renamed", 1)
	if err := os.WriteFile(Path(), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	created, err = Write()
	if err != nil || created {
		t.Fatalf("second call: created=%v err=%v", created, err)
	}
	if after, _ := os.ReadFile(Path()); string(after) != edited {
		t.Error("an edited registry was overwritten")
	}
}

func TestGetNamesTheAlternatives(t *testing.T) {
	r := Default()
	if _, err := r.Get("claude-code"); err != nil {
		t.Error(err)
	}
	_, err := r.Get("nope")
	if err == nil {
		t.Fatal("unknown agent must be an error")
	}
	// The error is what somebody reads instead of documentation.
	for _, key := range r.Keys() {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error %q does not mention %q", err, key)
		}
	}
}

func TestDefaultIsSelfConsistent(t *testing.T) {
	for key, agent := range Default() {
		if agent.Normalize != "" && agent.Transcripts == "" {
			t.Errorf("%s claims a format but says where no transcripts are", key)
		}
	}
}
