package main

import (
	"encoding/json"
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
	text := bundle.Pointer(b, path)
	if *full {
		text = bundle.Render(b)
	}
	// The file goes on the clipboard beside the text, so one paste does the right
	// thing wherever it lands.
	attached, copied := toClipboard(path, text)

	// The path alone on stdout, so it can be piped or passed to another command.
	// Everything a person reads goes to stderr.
	fmt.Println(path)

	fmt.Fprintf(os.Stderr, "\n%s · %s · %s\n", b.Code, b.Source.Title, b.Content())
	kind := "a reference to it"
	if *full {
		kind = fmt.Sprintf("the whole hop, %d bytes", len(text))
	}
	switch {
	case attached:
		fmt.Fprintf(os.Stderr, "  clipboard  the file, and %s\n", kind)
		fmt.Fprintf(os.Stderr, "  file       %s\n", path)
		fmt.Fprint(os.Stderr, "\nPress cmd-v. A chat that takes attachments takes the file; anywhere else\n")
		fmt.Fprint(os.Stderr, "gets the text.\n")
	case copied:
		fmt.Fprintf(os.Stderr, "  clipboard  %s\n", kind)
		fmt.Fprintf(os.Stderr, "  file       %s\n", path)
		fmt.Fprint(os.Stderr, "\nPaste it, or attach the file.\n")
	default:
		fmt.Fprintf(os.Stderr, "  file       %s\n", path)
		fmt.Fprint(os.Stderr, "\nNo clipboard command on this machine — the file above is the hop.\n")
	}
	if !*full {
		fmt.Fprint(os.Stderr, "For a browser tab, which cannot read your disk: hop pack --full.\n")
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

// toClipboard puts the file and the text on the clipboard together, and reports
// which of the two made it.
//
// Both, because one paste has to work in three different kinds of destination: a
// chat window that takes attachments wants the file, a terminal agent wants a path
// it can read, and a browser tab wants the words. A clipboard holding several
// formats lets each of them take what it understands, which is how copying a file
// in a file manager has always worked.
func toClipboard(path, text string) (attached, copied bool) {
	if err := bothFlavours(path, text); err == nil {
		return true, true
	}
	// Everywhere else, or if that failed: the text alone.
	for _, candidate := range [][]string{{"pbcopy"}, {"wl-copy"}, {"xclip", "-selection", "clipboard"}} {
		binary, err := exec.LookPath(candidate[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(binary, candidate[1:]...)
		cmd.Stdin = strings.NewReader(text)
		return false, cmd.Run() == nil
	}
	return false, false
}

// bothFlavours puts a file reference and a string on the clipboard together.
//
// Written through the pasteboard directly rather than with "set the clipboard to",
// because AppleScript writes a string in MacRoman whatever flavour it is asked
// for, and every em dash in a hop came out as a question mark. Only macOS offers
// two flavours from a command line without a library; elsewhere the caller falls
// back to text alone.
func bothFlavours(path, text string) error {
	binary, err := exec.LookPath("osascript")
	if err != nil {
		return err
	}
	script := fmt.Sprintf(`ObjC.import('AppKit');
const pb = $.NSPasteboard.generalPasteboard;
pb.clearContents;
pb.declareTypesOwner($([$.NSPasteboardTypeString, $.NSPasteboardTypeFileURL]), $());
pb.setStringForType($(%s), $.NSPasteboardTypeString);
pb.setStringForType($($.NSURL.fileURLWithPath(%s).absoluteString), $.NSPasteboardTypeFileURL);
`, jsString(text), jsString(path))

	cmd := exec.Command(binary, "-l", "JavaScript")
	cmd.Stdin = strings.NewReader(script)
	return cmd.Run()
}

// jsString quotes a string as a JavaScript literal. JSON's escaping is a subset of
// JavaScript's, so the standard library already does this exactly.
func jsString(s string) string {
	quoted, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(quoted)
}
