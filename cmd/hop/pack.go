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
	last := fs.Int("last", 0, "carry only the last N messages, plus what was first asked for")
	attach := fs.Bool("attach", false, "put the file on the clipboard too, for a chat window that takes attachments")
	reveal := fs.Bool("reveal", false, "show it in the file manager, ready to drag into an app")
	rest, err := parse(fs, args)
	if err != nil {
		return err
	}

	session, err := choose(rest, "pack which session?")
	if err != nil {
		return err
	}
	b, err := session.Load(bundle.Cap, *last)
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
	// The text alone, unless asked otherwise.
	//
	// A clipboard carrying both a file and a string was meant to let each
	// destination take what it understood. What it actually does is let the
	// destination choose, and some of them choose the file and then paste its
	// name: "HOP-K3QZ.md", no path, no words, nothing anybody can act on. That
	// failure is silent and it lands on the one command this tool exists for.
	//
	// So the string is the default everywhere, because it works everywhere — it
	// carries the absolute path, so an agent that can read a file still can. The
	// attachment is worth having for a chat window, and it is now asked for.
	attached, copied := toClipboard(path, text, *attach && !*full)

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
		fmt.Fprint(os.Stderr, "\nPress cmd-v.\n")
	default:
		fmt.Fprintf(os.Stderr, "  file       %s\n", path)
		fmt.Fprint(os.Stderr, "\nNo clipboard command on this machine — the file above is the hop.\n")
	}
	if !*full {
		fmt.Fprint(os.Stderr, "For a browser tab, which cannot read your disk: hop pack --full.\n")
		if !*attach {
			fmt.Fprint(os.Stderr, "For a chat window that takes attachments: hop pack --attach.\n")
		}
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

// toClipboard puts the hop on the clipboard and reports what made it there.
//
// withFile adds the file beside the text. It is off by default: a clipboard
// holding two flavours does not let each destination take what it understands, it
// lets the destination decide — and an editor that decides on the file pastes its
// name and nothing else.
func toClipboard(path, text string, withFile bool) (attached, copied bool) {
	if withFile {
		if err := bothFlavours(path, text); err == nil {
			return true, true
		}
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
