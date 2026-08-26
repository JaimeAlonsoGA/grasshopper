// Package sessions answers the question grasshopper exists to answer: which
// conversations are on this machine, and which one do I mean?
//
// Before this, naming a session meant naming a file called
// 3d2205e4-6792-44ff-9d97-c0c4d1b9f800.jsonl, which is a thing nobody can know by
// looking. This is the seam between the registry, which knows where sessions
// live, and the transcript reader, which knows how to read a line of one. It
// holds no product knowledge of its own.
package sessions

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"grasshopper/internal/bundle"
	"grasshopper/internal/registry"
	"grasshopper/internal/transcript"
)

// idLength is how much of a session's name is enough to name it. Eight characters
// is what git taught everybody to expect, and it is short enough to type.
const idLength = 8

type Session struct {
	ID     string // short, stable, derived from the filename
	Path   string
	Agent  string // registry key
	Format string

	// Title is what the agent named this session — the same string the person
	// already recognises, not a guess made from their first line.
	Title string

	// Dirs is every working directory the session was seen in, most recent first.
	// A session moves between projects, so there is no single answer.
	Dirs    []string
	Opening string
	When    time.Time
	Bytes   int64

	// Active means the session was written to within ActiveWindow. It is
	// deliberately not "has a process running": agents append to their transcript
	// and close it, so no open file descriptor identifies a session, and a cwd
	// only identifies a directory that a dozen sessions may share. What the
	// question is really asking — is this conversation still going — is answered
	// by when it was last written to, and that answer is exact.
	Active bool
}

// ActiveWindow is how recently a session must have been written to to count as
// still going. Long enough to survive an agent thinking, short enough that
// yesterday's work is not offered as current.
const ActiveWindow = 15 * time.Minute

// Label names the session for a person: its own title, or failing that the first
// thing they said.
func (s Session) Label() string {
	if s.Title != "" {
		return s.Title
	}
	if s.Opening != "" {
		return s.Opening
	}
	return "(nothing was said)"
}

func (s Session) Dir() string {
	if len(s.Dirs) == 0 {
		return ""
	}
	return s.Dirs[0]
}

// List is every session grasshopper can read, newest first.
func List() ([]Session, error) {
	reg, err := registry.Load()
	if err != nil {
		return nil, err
	}
	var all []Session
	for _, key := range reg.Keys() {
		agent := reg[key]
		if agent.Normalize == "" {
			continue
		}
		for _, path := range registry.Transcripts(agent) {
			s := describe(path, key, agent.Normalize)
			s.Active = time.Since(s.When) < ActiveWindow
			all = append(all, s)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].When.After(all[j].When) })
	return all, nil
}

// Find resolves what somebody typed: a full path, an id, the first characters of
// one, or a fragment of the title. An ambiguous answer is an error rather than a
// guess, because loading the wrong conversation is the one mistake this package
// exists to prevent.
func Find(want string) (Session, error) {
	all, err := List()
	if err != nil {
		return Session{}, err
	}

	if info, statErr := os.Stat(want); statErr == nil && !info.IsDir() {
		abs, _ := filepath.Abs(want)
		for _, s := range all {
			if s.Path == abs {
				return s, nil
			}
		}
		return describe(abs, "", "jsonl-tree"), nil
	}

	needle := strings.ToLower(strings.TrimSuffix(filepath.Base(want), ".jsonl"))
	var matched []Session
	for _, s := range all {
		name := strings.TrimSuffix(filepath.Base(s.Path), ".jsonl")
		if strings.HasPrefix(name, needle) || strings.Contains(strings.ToLower(s.Title), needle) {
			matched = append(matched, s)
		}
	}
	switch len(matched) {
	case 0:
		return Session{}, fmt.Errorf("no session matching %q", want)
	case 1:
		return matched[0], nil
	default:
		var names []string
		for _, s := range matched {
			names = append(names, fmt.Sprintf("%s (%s)", s.ID, s.Label()))
		}
		return Session{}, fmt.Errorf("%q matches %d sessions: %s", want, len(matched), strings.Join(names, ", "))
	}
}

// Load reads a session in full and renders it as a bundle.
func (s Session) Load(cap int) (bundle.Bundle, error) {
	reader, err := transcript.Get(s.Format)
	if err != nil {
		return bundle.Bundle{}, err
	}
	f, err := os.Open(s.Path)
	if err != nil {
		return bundle.Bundle{}, err
	}
	defer f.Close()

	turns, err := reader(f)
	if err != nil {
		return bundle.Bundle{}, fmt.Errorf("%s: %w", s.Label(), err)
	}
	return bundle.New(bundle.Source{
		Agent:    s.Agent,
		Title:    s.Title,
		Dir:      s.Dir(),
		Captured: time.Now(),
		// The original is left where its own agent wrote it. Copying it would be
		// a second source of truth that can disagree with the first.
		RawPath: s.Path,
	}, turns, cap), nil
}

func describe(path, agent, format string) Session {
	name := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	s := Session{ID: shorten(name), Path: path, Agent: agent, Format: format}
	if info, err := os.Stat(path); err == nil {
		s.When, s.Bytes = info.ModTime(), info.Size()
	}
	// A preview is a convenience, so a transcript that will not preview still
	// appears in the list — with less to say about it, which is honest.
	if f, err := os.Open(path); err == nil {
		defer f.Close()
		if p, err := transcript.Peek(format, f); err == nil {
			s.Title, s.Dirs, s.Opening = p.Title, p.Dirs, p.Opening
		}
	}
	return s
}

func shorten(full string) string {
	if len(full) <= idLength {
		return full
	}
	return full[:idLength]
}
