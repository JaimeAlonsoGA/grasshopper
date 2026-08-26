package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"grasshopper/internal/registry"
	"grasshopper/internal/sessions"
)

// runSources answers "is everything hooked up", which is a different question from
// "what is on this machine" and deserves its own command.
//
// It names a verdict per agent rather than printing numbers and leaving the
// reading to you, and every verdict that is not "fine" comes with the one thing to
// do about it.
func runSources(args []string) error {
	fs := flags("sources", "")
	repair := fs.Bool("repair", false, "rewrite globs this version knows have moved, keeping a backup")
	if _, err := parse(fs, args); err != nil {
		return err
	}

	statuses, err := registry.Discover()
	if err != nil {
		return err
	}
	all, err := sessions.List()
	if err != nil {
		return err
	}

	perAgent := map[string]int{}
	for _, s := range all {
		perAgent[s.Agent]++
	}

	rows := [][]string{{"SOURCE", "SESSIONS", "STATUS"}}
	var notes []string
	linked, broken := 0, 0

	for _, s := range statuses {
		verdict, note := verdictFor(s, perAgent[s.Key])
		rows = append(rows, []string{s.Key, count(perAgent[s.Key]), verdict})
		if note != "" {
			notes = append(notes, fmt.Sprintf("%s: %s", s.Key, note))
		}
		if s.Stale() {
			broken++
		} else if perAgent[s.Key] > 0 {
			linked++
		}
	}

	writeTable(os.Stdout, rows)
	fmt.Printf("\n%d of %d sources linked, %d readable sessions.\n", linked, len(statuses), len(all))
	for _, note := range notes {
		fmt.Printf("\n%s\n", note)
	}
	if broken > 0 && !*repair {
		fmt.Print("\nRun hop sources --repair to fix the globs this version knows have moved.\n")
	}
	if *repair {
		return doRepair(statuses)
	}
	return nil
}

// verdictFor turns a status into a sentence. The distinction that matters is
// between an agent that is not installed — nothing to fix — and one that is
// installed but pointed at the wrong place, which is the only case where
// grasshopper is silently missing sessions.
func verdictFor(s registry.Status, found int) (verdict, note string) {
	switch {
	case s.Stale():
		return fmt.Sprintf("missing %d", s.Shipped-found), fmt.Sprintf(
			"its files moved. Your glob finds %s; this version's finds %d, at\n  %s\nRun hop sources --repair, or edit %s.",
			plural(found), s.Shipped, registry.Default()[s.Key].Transcripts, registry.Path())

	case !s.Readable && s.Agent.Transcripts == "":
		return "not configured", fmt.Sprintf(
			"grasshopper does not know where %s keeps its sessions. Add a glob and a\nformat to %s — no code needed.",
			s.Key, registry.Path())

	case !s.Readable:
		return "no reader", fmt.Sprintf(
			"its sessions are found but nothing can read them. Set \"normalize\" in %s\nto one of: %s.",
			registry.Path(), strings.Join(readerNames(), ", "))

	case s.StateDir == "":
		return "not installed", ""

	case found == 0:
		return "installed, no sessions yet", ""

	default:
		return "linked", ""
	}
}

// doRepair rewrites only the globs this version knows have moved, and only those.
// Everything else in the file — including agents grasshopper has never heard of —
// is copied through as the bytes somebody typed.
func doRepair(statuses []registry.Status) error {
	var stale []registry.Status
	for _, s := range statuses {
		if s.Stale() {
			stale = append(stale, s)
		}
	}
	if len(stale) == 0 {
		fmt.Print("\nNothing to repair.\n")
		return nil
	}

	before, err := os.ReadFile(registry.Path())
	if err != nil {
		return err
	}
	var file map[string]map[string]any
	if err := json.Unmarshal(before, &file); err != nil {
		return fmt.Errorf("%s: %w", registry.Path(), err)
	}

	for _, s := range stale {
		entry, ok := file[s.Key]
		if !ok {
			continue
		}
		entry["transcripts"] = registry.Default()[s.Key].Transcripts
		file[s.Key] = entry
	}

	backup := registry.Path() + ".backup"
	if err := os.WriteFile(backup, before, 0o600); err != nil {
		return err
	}
	after, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(registry.Path(), append(after, '\n'), 0o600); err != nil {
		return err
	}

	fmt.Printf("\nrepaired %s (backup at %s)\n", registry.Path(), backup)
	for _, s := range stale {
		fmt.Printf("  %s now reads %s\n", s.Key, registry.Default()[s.Key].Transcripts)
	}
	return nil
}

func plural(n int) string {
	if n == 1 {
		return "1 session"
	}
	return fmt.Sprintf("%d sessions", n)
}
