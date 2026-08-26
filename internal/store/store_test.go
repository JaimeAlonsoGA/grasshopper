package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"grasshopper/internal/bundle"
)

func hop(text string) bundle.Bundle {
	return bundle.New(bundle.Source{
		Agent: "an-agent", Title: "Billing resolver",
		Captured: time.Date(2026, 8, 26, 16, 12, 0, 0, time.UTC),
	}, []bundle.Turn{{Who: bundle.Me, Text: text}}, bundle.Cap)
}

func TestWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRASSHOPPER_HOME", home)

	b := hop("something worth keeping")
	path, err := Write(b)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != b.Code+".md" {
		t.Errorf("path = %q, want it named after the code", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != bundle.Render(b) {
		t.Error("what was written is not what Render produces")
	}
}

// A hop holds whatever was said in a working session, including whatever was
// pasted into it. It gets the permissions the agents give the transcripts it came
// from, not the friendlier default.
func TestWriteIsReadableOnlyByItsOwner(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRASSHOPPER_HOME", home)

	path, err := Write(hop("a secret"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != fileMode {
		t.Errorf("hop mode %o, want %o", perm, fileMode)
	}
	dir, err := os.Stat(Dir())
	if err != nil {
		t.Fatal(err)
	}
	if perm := dir.Mode().Perm(); perm != dirMode {
		t.Errorf("directory mode %o, want %o", perm, dirMode)
	}
}

// The code fingerprints the content, so writing the same hop twice is the same
// file — there is nothing to collide with and nothing to clean up.
func TestWriteTwiceIsOneFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRASSHOPPER_HOME", home)

	first, err := Write(hop("same"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Write(hop("same"))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("%q and %q are different files", first, second)
	}
	entries, err := os.ReadDir(Dir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("got %d files, want one", len(entries))
	}

	// Different content, different file.
	third, err := Write(hop("different"))
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Error("two different hops share a file")
	}
}

func TestDirIsUnderHome(t *testing.T) {
	t.Setenv("GRASSHOPPER_HOME", "/tmp/elsewhere")
	if !strings.HasPrefix(Dir(), "/tmp/elsewhere") {
		t.Errorf("Dir = %q", Dir())
	}
}
