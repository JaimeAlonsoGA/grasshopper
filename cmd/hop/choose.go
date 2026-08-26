package main

import (
	"errors"
	"fmt"
	"time"

	"grasshopper/internal/pick"
	"grasshopper/internal/registry"
	"grasshopper/internal/sessions"
)

// choose resolves which session a command means. Named on the command line, it
// resolves that; named nothing, it asks — because the alternative is telling
// somebody to go and look up an id first, which is the errand this tool exists to
// remove.
func choose(args []string, prompt string) (sessions.Session, error) {
	if len(args) > 0 {
		return sessions.Find(args[0])
	}

	all, err := sessions.List()
	if err != nil {
		return sessions.Session{}, err
	}
	if len(all) == 0 {
		return sessions.Session{}, errors.New("no sessions found; hop doctor shows where grasshopper is looking")
	}

	surface := namer()
	rows := make([]pick.Row, 0, len(all))
	for _, s := range all {
		// Title second: it is the only thing anybody recognises a session by. The
		// source after it, because two sessions can share a name across two apps.
		// Not dimmed by age — the list is already sorted newest first.
		rows = append(rows, pick.Row{
			Cells: []string{s.ID, truncate(s.Label(), titleWidth), surface(s), ago(s.When)},
		})
	}

	i, err := pick.From(prompt, nil, rows)
	if err != nil {
		if errors.Is(err, pick.ErrCancelled) {
			return sessions.Session{}, errCancelled
		}
		return sessions.Session{}, err
	}
	return all[i], nil
}

// namer resolves front ends to the names their apps go by. The registry is loaded
// once for a whole listing rather than once per row: it is a file read, and a list
// of forty would read it forty times.
//
// This exists because the pretty names were in the registry and the listings were
// not asking for them — forty rows of "claude-desktop" when the answer was "Claude
// desktop app".
func namer() func(sessions.Session) string {
	reg, err := registry.Load()
	if err != nil {
		return func(s sessions.Session) string {
			if s.Surface != "" {
				return s.Surface
			}
			return s.Agent
		}
	}
	return func(s sessions.Session) string { return reg.Surface(s.Agent, s.Surface) }
}

// ago is how people think about when something happened. Absolute time goes in
// the bundle, where it will be read another day; a list is read now.
func ago(when time.Time) string {
	if when.IsZero() {
		return "—"
	}
	switch d := time.Since(when); {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return when.Format("2 Jan")
	}
}
