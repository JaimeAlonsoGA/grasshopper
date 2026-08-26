package main

import (
	"fmt"
	"os"

	"grasshopper/internal/bundle"
	"grasshopper/internal/registry"
	"grasshopper/internal/sessions"
)

// runList and runShow are the same calls the agents make. Sharing them is the
// point: what you read here is exactly what an agent is given.
func runList(args []string) error {
	fs := flags("ls", "")
	active := fs.Bool("active", false, "only sessions written to in the last few minutes")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	text, err := listTool(argsJSON(map[string]any{"active": *active, "limit": 200}))
	if err != nil {
		return err
	}
	fmt.Print(text)
	return nil
}

func runShow(args []string) error {
	fs := flags("show", "[session]")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("%w: name a session; hop ls shows them", errUsage)
	}
	text, err := loadTool(argsJSON(map[string]any{"session": fs.Arg(0)}))
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
	rows := [][]string{{"AGENT", "STATE", "SESSIONS", "READABLE"}}
	for _, s := range statuses {
		rows = append(rows, []string{s.Key, dash(short(s.StateDir)), count(s.Transcripts), yesno(s.Readable)})
	}
	fmt.Println()
	writeTable(os.Stdout, rows)

	for _, s := range statuses {
		if !s.Stale() {
			continue
		}
		fmt.Printf("\n%s: the glob in your registry matches nothing, but this version's\n", s.Key)
		fmt.Printf("would find %d sessions. Its files probably moved. Replace\n", s.Shipped)
		fmt.Printf("  \"transcripts\": %q\n", s.Agent.Transcripts)
		fmt.Printf("with\n  \"transcripts\": %q\n", registry.Default()[s.Key].Transcripts)
		fmt.Printf("in %s, or delete that file to start over.\n", registry.Path())
	}

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
	return nil
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
