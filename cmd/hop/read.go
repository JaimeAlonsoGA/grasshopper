package main

import (
	"fmt"
	"os"
	"strings"

	"grasshopper/internal/bundle"
	"grasshopper/internal/registry"
	"grasshopper/internal/sessions"
)

// runList and runShow are the same calls the agents make. Sharing them is the
// point: what you read here is exactly what an agent is given.
func runList(args []string) error {
	fs := flags("ls", "[what]")
	active := fs.Bool("active", false, "only sessions written to in the last few minutes")
	n := fs.Int("n", listPage, "how many to show, newest first")
	all := fs.Bool("all", false, "every session, however many there are")
	rest, err := parse(fs, args)
	if err != nil {
		return err
	}
	if *all {
		*n = -1 // every one of them
	}

	// The same code the agents call. A listing somebody reads and a listing an
	// agent reads are the same question, and two implementations of it would
	// disagree the week after they were written.
	text, err := listing(argsJSON(map[string]any{
		"active": *active,
		"limit":  *n,
		"match":  strings.Join(rest, " "),
	}))
	if err != nil {
		return err
	}
	fmt.Print(text)
	if strings.Contains(text, " of ") {
		fmt.Print("Narrow it with hop ls <word>, or see them all with hop ls --all.\n")
	}
	return nil
}

// listPage is how many sessions a listing shows before it says there are more.
//
// A page rather than everything, because the number grows without limit and a
// terminal does not: past a screenful the answer scrolls away from the person
// who asked for it. Twenty fits a small window with the footer still visible,
// and the footer is what makes the ceiling honest — it says how many were left
// out and how to ask for them.
const listPage = 20

func runShow(args []string) error {
	fs := flags("show", "[session]")
	last := fs.Int("last", 0, "print only the last N messages, plus what was first asked for")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("%w: name a session; hop ls shows them", errUsage)
	}
	text, err := loadTool(argsJSON(map[string]any{"session": fs.Arg(0), "last": *last}))
	if err != nil {
		return err
	}
	fmt.Print(text)
	return nil
}

// runDoctor reports. It exits zero even when everything is missing, because "what
// is on this machine" is a question, not an assertion.
func runDoctor(args []string) error {
	if _, err := parse(flags("doctor", ""), args); err != nil {
		return err
	}

	binary, err := os.Executable()
	if err != nil {
		binary = "(unknown)"
	}
	fmt.Printf("grasshopper  %s at %s\n", version, binary)
	fmt.Printf("home         %s\n", registry.Home())
	fmt.Printf("registry     %s\n", registry.Path())
	if created, err := registry.Write(); err == nil && created {
		fmt.Print("             (just created; edit it to add an agent)\n")
	}

	statuses, err := registry.Discover()
	if err != nil {
		return err
	}
	reg, _ := registry.Load()
	rows := [][]string{{"AGENT", "STATE", "SESSIONS", "READABLE", "KNOWS HOP"}}
	for _, s := range statuses {
		rows = append(rows, []string{
			s.Key, dash(short(s.StateDir)), count(s.Transcripts), yesno(s.Readable), knowsHop(reg[s.Key]),
		})
	}
	fmt.Println()
	writeTable(os.Stdout, rows)

	all, err := sessions.List()
	if err != nil {
		return err
	}
	active := 0
	for _, s := range all {
		if s.Active {
			active++
		}
	}
	fmt.Printf("\n%d sessions readable, %d active in the last %s.\n", len(all), active, sessions.ActiveWindow)
	fmt.Printf("Bundles are capped at %d bytes of conversation; the original is always linked.\n", bundle.Cap)
	for _, s := range statuses {
		if s.Stale() {
			fmt.Print("Something has moved — run hop source.\n")
			break
		}
	}
	for _, s := range statuses {
		if knowsHop(reg[s.Key]) == "no" {
			fmt.Print("An agent that does not know hop cannot be asked for a session — run hop hatch.\n")
			break
		}
	}
	return nil
}

// knowsHop answers the question that a listing of readable sessions does not:
// can this agent be asked for one.
//
// Reading an agent's sessions and being reachable from inside it are separate
// things, and grasshopper reported only the first. Somebody installed it, saw
// every session listed, asked their editor for one of them, and the editor had
// never heard of grasshopper — with nothing anywhere saying so.
//
// The answer comes from the file the agent keeps its servers in, not from asking
// the agent: one of them health-checks every server it has before answering,
// which is eight seconds for a question a diagnostic asks about seven agents.
func knowsHop(a registry.Agent) string {
	config := registry.ConfigPath(a)
	switch {
	case config != "" && a.MCPConfigKey != "":
		if hasMCPConfig(config, a.MCPConfigKey, serverName) {
			return "yes"
		}
		return "no"
	case len(a.MCPAdd) > 0:
		// Registered through a command line whose list this cannot read cheaply.
		return "?"
	default:
		return "—"
	}
}

func argsJSON(m map[string]any) []byte {
	b, err := marshal(m)
	if err != nil {
		return []byte("{}")
	}
	return b
}

func dash(s string) string {
	if s == "" || s == "—" {
		return "—"
	}
	return s
}

func count(n int) string {
	if n == 0 {
		return "—"
	}
	return fmt.Sprintf("%d", n)
}

func yesno(ok bool) string {
	if ok {
		return "yes"
	}
	return "cooperative"
}
