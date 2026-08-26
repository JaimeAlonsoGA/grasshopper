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

// The file holds changes, not a copy of the defaults. A field somebody set wins;
// everything they left out is whatever this version ships — which is how a glob
// or a launch path improved later actually reaches them.
func TestLoadOverlaysTheirFileOnTheDefaults(t *testing.T) {
	home(t, `{"claude-code":{"transcripts":"~/mine/*.jsonl"}}`)
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	got := r["claude-code"]
	if got.Transcripts != "~/mine/*.jsonl" {
		t.Errorf("overwrote their edit: %q", got.Transcripts)
	}
	if got.Launch != Default()["claude-code"].Launch {
		t.Errorf("Launch = %q, want this version's value", got.Launch)
	}
	if got.Normalize != Default()["claude-code"].Normalize {
		t.Errorf("Normalize = %q, want this version's value", got.Normalize)
	}
}

// An agent only they know about is added as it stands.
func TestLoadKeepsAnAgentOfTheirOwn(t *testing.T) {
	home(t, `{"mine":{"transcripts":"~/mine/*.jsonl","normalize":"jsonl-tree","launch":"mine"}}`)
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if r["mine"].Launch != "mine" {
		t.Errorf("their agent was lost: %+v", r["mine"])
	}
	// And the shipped ones are still there, because the file is an overlay.
	if _, ok := r["claude-code"]; !ok {
		t.Error("the shipped agents went missing")
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

func TestWriteCreatesAnEmptyFile(t *testing.T) {
	home(t, "")
	created, err := Write()
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	body, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	// Empty, not a copy of the defaults: a copy freezes this version's globs and
	// launch paths into somebody's file forever.
	if strings.TrimSpace(string(body)) != "{}" {
		t.Errorf("wrote %q, want an empty object", body)
	}

	edited := `{"mine":{"launch":"mine"}}`
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

// An app can ship its own command line inside itself without putting it on
// anybody's PATH. "Not installed" was the wrong answer for something on the disk.
func TestLauncherTriesEveryCandidate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	bundled := filepath.Join(dir, "Some.app", "Resources", "tool")
	if err := os.MkdirAll(filepath.Dir(bundled), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundled, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, ok := Launcher(Agent{Launch: "definitely-not-a-real-command," + bundled})
	if !ok || got != bundled {
		t.Errorf("Launcher = %q, %v; want the bundled one", got, ok)
	}
	// A path that is there but not executable is not a launcher.
	plain := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(plain, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := Launcher(Agent{Launch: plain}); ok {
		t.Error("a non-executable file was accepted")
	}
	if _, ok := Launcher(Agent{}); ok {
		t.Error("an agent with no launch command reported one")
	}
	// A leading ~ is resolved, because the registry is a file people edit.
	if _, ok := Launcher(Agent{Launch: "~/Some.app/Resources/tool"}); !ok {
		t.Error("a home-relative path was not expanded")
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
		if agent.Transcripts != "" && agent.Normalize == "" {
			t.Errorf("%s says where its transcripts are but not how to read them", key)
		}
		// An entry with nothing in it is almost always a half-applied edit, not a
		// deliberate placeholder.
		if agent.Transcripts == "" && agent.Normalize == "" && agent.Launch == "" {
			t.Errorf("%s is entirely empty", key)
		}
		// A named surface nothing records is a name that will never be shown.
		for recorded, named := range agent.Surfaces {
			if recorded == "" || named == "" {
				t.Errorf("%s has a blank surface mapping", key)
			}
		}
	}
}

// An agent that has just written one session into the old location while twenty
// sit in the new one still matches its glob. Reporting that as linked is the
// silent failure this is here to catch.
func TestStaleIsAboutMissingSessionsNotZero(t *testing.T) {
	cases := []struct {
		name           string
		found, shipped int
		want           bool
	}{
		{"agreeing", 19, 0, false},
		{"nothing anywhere", 0, 0, false},
		{"configured finds none, shipped finds some", 0, 19, true},
		{"configured finds one, shipped finds twenty", 1, 21, true},
		{"configured finds more than shipped", 21, 19, false},
	}
	for _, c := range cases {
		s := Status{Transcripts: c.found, Shipped: c.shipped}
		if s.Stale() != c.want {
			t.Errorf("%s: Stale() = %v, want %v", c.name, s.Stale(), c.want)
		}
	}
}
