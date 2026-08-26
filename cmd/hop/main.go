// Command hop is grasshopper's command line. grasshopper is the software; hop is
// what you type.
//
// Two faces, one binary: a handful of commands for a person at a terminal, and an
// MCP server for the agents. They share every line underneath, so what an agent
// loads and what you read are the same document.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

// version is stamped at build time by the Makefile.
var version = "dev"

const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

var (
	// errUsage asks for exit 2 without the command knowing the number.
	errUsage = errors.New("usage")
	// errHelp is a question, not a mistake. The flag package already printed the
	// answer, so main says nothing more and exits zero.
	errHelp = errors.New("help")
	// errCancelled is somebody backing out of a prompt. An answer, not a failure.
	errCancelled = errors.New("cancelled")
)

type command struct {
	name     string
	synopsis string
	summary  string
	run      func(args []string) error
}

func commands() []command {
	return []command{
		{"ls", "", "the sessions on this machine, newest first", runList},
		{"pack", "[session]", "pack a session into a hop, and copy a reference to it", runPack},
		{"to", "[agent] [session]", "send a hop to another agent and open it there", runTo},
		{"show", "[session]", "print a session as a bundle", runShow},
		{"mcp", "", "serve grasshopper to agents over stdio (not for humans)", runMCP},
		{"source", "", "which apps grasshopper can read, and which are not linked", runSource},
		{"hatch", "", "wake grasshopper up on this machine, and show you around", runHatch},
		{"uninstall", "", "unregister it from every agent", runUninstall},
		{"doctor", "", "where grasshopper is looking, and what it found", runDoctor},
		{"version", "", "print the version and where this binary is", runVersion},
	}
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		usage(os.Stdout)
		os.Exit(exitOK)
	}
	if args[0] == "-v" || args[0] == "--version" {
		args[0] = "version"
	}

	// setup is what anybody would guess for this, and a first command that is not
	// there is a poor first impression.
	if args[0] == "setup" {
		args[0] = "hatch"
	}

	for _, c := range commands() {
		if c.name != args[0] {
			continue
		}
		switch err := c.run(args[1:]); {
		case err == nil, errors.Is(err, errHelp), errors.Is(err, errCancelled):
			os.Exit(exitOK)
		case errors.Is(err, errUsage):
			fmt.Fprintf(os.Stderr, "hop %s: %v\n", c.name, err)
			os.Exit(exitUsage)
		default:
			fmt.Fprintf(os.Stderr, "hop: %v\n", err)
			os.Exit(exitFailure)
		}
	}

	fmt.Fprintf(os.Stderr, "hop: unknown command %q\n\n", args[0])
	usage(os.Stderr)
	os.Exit(exitUsage)
}

func usage(w *os.File) {
	fmt.Fprint(w, `grasshopper carries a conversation from one agent to another.

Two ways to use it. Ask an agent — "bring me the thread about billing" — and it
reaches grasshopper over MCP without you typing anything. Or do it yourself:

    hop ls                       see the sessions on this machine
    hop pack                     pack one into a hop, reference on your clipboard
    hop pack --full              the whole hop on your clipboard, for a browser tab
    hop to                       send a hop to another agent — it asks which
    hop source                   which apps are linked

usage: hop <command> [flags]

`)
	rows := make([][]string, 0, len(commands()))
	for _, c := range commands() {
		rows = append(rows, []string{"  " + c.name, c.summary})
	}
	writeTable(w, rows)
	fmt.Fprintf(w, "\ngrasshopper %s\n", version)
}

func runVersion(args []string) error {
	if _, err := parse(flags("version", ""), args); err != nil {
		return err
	}
	fmt.Printf("grasshopper %s\n", version)
	if binary, err := os.Executable(); err == nil {
		fmt.Println(binary)
	}
	return nil
}

// flags builds a flag set that reports usage errors rather than exiting on its
// own, and whose usage line names the positional arguments the flag package
// cannot know about.
func flags(name, synopsis string) *flag.FlagSet {
	fs := flag.NewFlagSet("hop "+name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), strings.TrimRight("usage: hop "+name+" "+synopsis, " "))
		any := false
		fs.VisitAll(func(*flag.Flag) { any = true })
		if any {
			fmt.Fprintln(fs.Output())
			fs.PrintDefaults()
		}
	}
	return fs
}

// parse reads flags whether they come before or after the positional arguments.
// Go's flag package stops at the first non-flag word, which silently turns
// "hop show --raw session" into a command that parsed no flags at all.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var flagArgs, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name := strings.TrimLeft(arg, "-")
		if !strings.HasPrefix(arg, "-") || name == "" {
			positional = append(positional, arg)
			continue
		}
		flagArgs = append(flagArgs, arg)
		if strings.Contains(name, "=") {
			continue
		}
		if f := fs.Lookup(name); f != nil && !isBool(f) && i+1 < len(args) {
			i++
			flagArgs = append(flagArgs, args[i])
		}
	}
	if err := fs.Parse(append(flagArgs, positional...)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, errHelp
		}
		return nil, fmt.Errorf("%w: %v", errUsage, err)
	}
	return fs.Args(), nil
}

func isBool(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}
