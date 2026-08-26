package main

import (
	"fmt"
	"os"
	"os/exec"

	"grasshopper/internal/bundle"
	"grasshopper/internal/store"
)

// runFile writes the document and hands you the path.
//
// This is the artefact, and the other commands are ways of pointing at it. An
// agent that takes attachments takes this file; one that reads paths reads this
// path; a browser tab gets its contents pasted. Writing it is the thing that
// always happens, and everything else is a reference.
func runFile(args []string) error {
	fs := flags("file", "[session]")
	reveal := fs.Bool("reveal", false, "show it in the file manager, ready to drag into an app")
	rest, err := parse(fs, args)
	if err != nil {
		return err
	}

	session, err := choose(rest, "which session?")
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

	// The path on stdout and nothing else, so it can be piped or passed straight
	// to another command. Everything a person reads goes to stderr.
	fmt.Println(path)
	fmt.Fprintf(os.Stderr, "%s · %s · %s\n", b.Code, b.Source.Title, b.Content())

	if *reveal {
		if err := showInFileManager(path); err != nil {
			fmt.Fprintf(os.Stderr, "could not open a file manager: %v\n", err)
		}
	}
	return nil
}

// showInFileManager opens the folder with the file selected. Dragging a file into
// a chat window is how somebody attaches a conversation to an agent that has no
// idea grasshopper exists.
func showInFileManager(path string) error {
	for _, candidate := range [][]string{{"open", "-R"}, {"xdg-open"}} {
		binary, err := exec.LookPath(candidate[0])
		if err != nil {
			continue
		}
		args := append(candidate[1:], path)
		return exec.Command(binary, args...).Run()
	}
	return fmt.Errorf("no file manager on PATH")
}
