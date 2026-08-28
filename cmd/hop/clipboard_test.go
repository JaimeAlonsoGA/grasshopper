package main

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// The default must be the text and only the text.
//
// A clipboard carrying a file as well lets the destination choose, and an editor
// that chooses the file pastes its name — no path, no words, nothing to act on.
// That is a silent failure on the one command this tool exists for, so the
// default is pinned here.
func TestClipboardDefaultCarriesTextOnly(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the two-flavour clipboard only exists on macOS")
	}
	if _, err := exec.LookPath("pbcopy"); err != nil {
		t.Skip("no clipboard on this machine")
	}
	restore := keepClipboard(t)
	defer restore()

	const text = "Read /tmp/HOP-TEST.md - a record of an earlier session."
	attached, copied := toClipboard("/tmp/HOP-TEST.md", text, false)
	if attached {
		t.Error("the file went on the clipboard without being asked for")
	}
	if !copied {
		t.Fatal("the text did not make it to the clipboard")
	}
	if got := paste(t); got != text {
		t.Errorf("clipboard = %q, want the reference text", got)
	}
}

// And when it is asked for, both are there: the string is still what a
// destination that wants words will find.
func TestClipboardAttachKeepsTheText(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the two-flavour clipboard only exists on macOS")
	}
	if _, err := exec.LookPath("osascript"); err != nil {
		t.Skip("no osascript on this machine")
	}
	restore := keepClipboard(t)
	defer restore()

	const text = "Read /tmp/HOP-TEST.md - a record of an earlier session."
	if attached, _ := toClipboard("/tmp/HOP-TEST.md", text, true); !attached {
		t.Skip("this machine would not take a two-flavour clipboard")
	}
	if got := paste(t); got != text {
		t.Errorf("clipboard string = %q, want the reference text alongside the file", got)
	}
}

// Tests run on somebody's own machine, and their clipboard is theirs.
func keepClipboard(t *testing.T) func() {
	t.Helper()
	before, err := exec.Command("pbpaste").Output()
	if err != nil {
		return func() {}
	}
	return func() {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(string(before))
		_ = cmd.Run()
	}
}

func paste(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("pbpaste").Output()
	if err != nil {
		t.Fatalf("pbpaste: %v", err)
	}
	return strings.TrimRight(string(out), "\n")
}
