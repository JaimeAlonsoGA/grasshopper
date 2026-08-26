package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

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

	reg, err := registry.Load()
	if err != nil {
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

	// Grouped by front end, not by registry key: what somebody installed was a
	// terminal, a desktop app and an editor extension, and telling them they have
	// "claude-code" answers a question they did not ask.
	found := map[string]int{}
	newest := map[string]time.Time{}
	perAgent := map[string]int{}
	for _, s := range all {
		name := reg.Surface(s.Agent, s.Surface)
		found[name]++
		perAgent[s.Agent]++
		if s.When.After(newest[name]) {
			newest[name] = s.When
		}
	}

	rows := [][]string{{"APP", "SESSIONS", "LAST USED", "STATUS"}}
	for _, name := range sortedByCount(found) {
		rows = append(rows, []string{name, fmt.Sprintf("%d", found[name]), ago(newest[name]), "linked"})
	}

	// An agent whose glob has been overtaken is a problem and gets a row. One
	// that is simply not configured is not a source at all — listing it beside
	// apps somebody actually has is how a report becomes noise.
	var notes, unconfigured []string
	for _, s := range statuses {
		if perAgent[s.Key] > 0 && !s.Stale() {
			continue
		}
		if s.Agent.Transcripts == "" {
			unconfigured = append(unconfigured, s.Key)
			continue
		}
		verdict, note := verdictFor(s, perAgent[s.Key])
		rows = append(rows, []string{s.Key, count(perAgent[s.Key]), "—", verdict})
		if note != "" {
			notes = append(notes, fmt.Sprintf("%s: %s", s.Key, note))
		}
	}

	writeTable(os.Stdout, rows)
	fmt.Printf("\n%s across %s, %d readable.\n",
		plural(len(found)), plural2(len(perAgent), "agent", "agents"), len(all))
	for _, note := range notes {
		fmt.Printf("\n%s\n", note)
	}

	stale := false
	for _, s := range statuses {
		if s.Stale() {
			stale = true
		}
	}
	if stale && !*repair {
		fmt.Print("\nRun hop sources --repair to fix the globs this version knows have moved.\n")
	}
	if len(unconfigured) > 0 {
		fmt.Printf("\nIn your registry but not set up: %s.\n", strings.Join(unconfigured, ", "))
	}
	if !stale && len(notes) == 0 {
		fmt.Printf("Anything else is added by editing %s — a glob and a format, no code.\n", registry.Path())
	}
	if *repair {
		return doRepair(statuses)
	}
	return nil
}

// sortedByCount orders the front ends by how much they are used, so the one
// somebody lives in is the first line they read.
func sortedByCount(counts map[string]int) []string {
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})
	return names
}

// verdictFor turns a status into a sentence. The distinction that matters is
// between an agent that is not installed — nothing to fix — and one that is
// installed but pointed at the wrong place, which is the only case where
// grasshopper is silently missing sessions.
func verdictFor(s registry.Status, found int) (verdict, note string) {
	switch {
	case s.Stale():
		return fmt.Sprintf("missing %d", s.Shipped-found), fmt.Sprintf(
			"its files moved. Your glob finds %d; this version's finds %d, at\n  %s\nRun hop sources --repair, or edit %s.",
			found, s.Shipped, registry.Default()[s.Key].Transcripts, registry.Path())

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

	default:
		return "no sessions yet", ""
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

func plural(n int) string { return plural2(n, "app", "apps") }

func plural2(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
