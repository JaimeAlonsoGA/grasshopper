package main

import (
	"errors"
	"fmt"
	"time"

	"grasshopper/internal/pick"
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

	rows := make([]pick.Row, 0, len(all))
	for _, s := range all {
		rows = append(rows, pick.Row{
			Cells: []string{ago(s.When), surface(s), truncate(s.Label(), titleWidth), short(s.Dir())},
			// Everything but the recently-written is dimmed, so what is still
			// going stands out without needing a column of its own.
			Muted: !s.Active,
		})
	}

	i, err := pick.From(prompt, []string{"WHEN", "FROM", "TITLE", "DIRECTORY"}, rows)
	if err != nil {
		if errors.Is(err, pick.ErrCancelled) {
			return sessions.Session{}, errCancelled
		}
		return sessions.Session{}, err
	}
	return all[i], nil
}

// surface is which front end the session came from, as the agent recorded it.
// Grasshopper does not translate the value: the one they wrote is the one that
// will still be true after they rename a product.
func surface(s sessions.Session) string {
	if s.Surface == "" {
		return s.Agent
	}
	return s.Surface
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
