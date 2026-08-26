package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"grasshopper/internal/bundle"
	"grasshopper/internal/store"
)

// runPack packs a session into a hop and puts a reference to it on the clipboard.
//
// One command rather than two, because packing without copying is a step nobody
// wanted: the next thing anybody does after making a hop is paste it somewhere.
// Having pack and copy as separate verbs meant running pack, pressing cmd-v, and
// getting whatever was on the clipboard before — which is exactly what happened.
func runPack(args []string) error {
	fs := flags("pack", "[session]")
	full := fs.Bool("full", false, "put the whole hop on the clipboard, for somewhere that cannot read a file")
	reveal := fs.Bool("reveal", false, "show it in the file manager, ready to drag into an app")
	rest, err := parse(fs, args)
	if err != nil {
		return err
	}

	session, err := choose(rest, "pack which session?")
	if err != nil {
		return err
	}
	b, err := session.Load(bundle.Cap)
	if err != nil {
		return err
	}
	path, err := store.Write(b)
	if err != nil {
		return err
	}

	// A reference by default: pasting a whole conversation spends the context the
	// handover was meant to save. --full is for a browser tab, which cannot read a
	// file on this machine.
	clipboard := bundle.Pointer(b, path)
	if *full {
		clipboard = bundle.Render(b)
	}
	copied := toClipboard(clipboard) == nil

	// The path alone on stdout, so it can be piped or passed to another command.
	// Everything a person reads goes to stderr.
	fmt.Println(path)
	fmt.Fprintf(os.Stderr, "%s · %s · %s\n", b.Code, b.Source.Title, b.Content())
	switch {
	case !copied:
		fmt.Fprint(os.Stderr, "no clipboard command found — the path above is the hop\n")
	case *full:
		fmt.Fprintf(os.Stderr, "the whole hop is on your clipboard (%d bytes)\n", len(clipboard))
	default:
		fmt.Fprintf(os.Stderr, "a reference is on your clipboard (%d bytes) — paste it where an agent can read files,\n", len(clipboard))
		fmt.Fprint(os.Stderr, "or attach the file itself. For a browser tab, hop pack --full.\n")
	}

	if *reveal {
		if err := showInFileManager(path); err != nil {
			fmt.Fprintf(os.Stderr, "could not open a file manager: %v\n", err)
		}
	}
	return nil
}

// showInFileManager opens the folder with the file selected. Dragging a file into
// a chat window is how somebody hands a conversation to an agent that has never
// heard of grasshopper.
func showInFileManager(path string) error {
	for _, candidate := range [][]string{{"open", "-R"}, {"xdg-open"}} {
		binary, err := exec.LookPath(candidate[0])
		if err != nil {
			continue
		}
		return exec.Command(binary, append(candidate[1:], path)...).Run()
	}
	return fmt.Errorf("no file manager on PATH")
}

func toClipboard(text string) error {
	for _, candidate := range [][]string{{"pbcopy"}, {"wl-copy"}, {"xclip", "-selection", "clipboard"}} {
		binary, err := exec.LookPath(candidate[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(binary, candidate[1:]...)
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	}
	return fmt.Errorf("no clipboard command found")
}
