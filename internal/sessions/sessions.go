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
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"grasshopper/internal/bundle"
	"grasshopper/internal/registry"
	"grasshopper/internal/transcript"
)

const (
	// idLength is how short a handle starts. Four characters is a thousand
	// sessions before a collision is likely, and short enough to read aloud.
	idLength = 4

	// idMaxLength is where lengthening stops and the raw identifier is used.
	idMaxLength = 12

	// titleWidth bounds a title that fell back to somebody's opening paragraph.
	titleWidth = 72
)

// truncate collapses whitespace and cuts on a rune boundary.
func truncate(s string, width int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	return string(runes[:width-1]) + "…"
}

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
	// Surface is which front end started it — a terminal, an editor extension, a
	// desktop app. Reported as the agent wrote it.
	Surface string

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
	if from, ok := continuedFrom(s.Opening); ok {
		return "↳ continued from " + from
	}
	if s.Opening != "" {
		return s.Opening
	}
	return "(nothing was said)"
}

// continuedFrom recognises a session grasshopper itself opened, before the agent
// has got round to naming it.
//
// Its first words are the prompt grasshopper wrote, so the raw opening reads as a
// file path — which says nothing. Saying which hop it came from says everything,
// and it makes a chain of handovers visible in the listing.
func continuedFrom(opening string) (string, bool) {
	const marker = "HOP-"
	if !strings.HasPrefix(opening, "Read ") || !strings.Contains(opening, "grasshopper") {
		return "", false
	}
	i := strings.Index(opening, marker)
	if i < 0 {
		return "", false
	}
	code := opening[i:]
	if end := strings.IndexAny(code, " .\n"); end > 0 {
		code = code[:end]
	}
	return strings.TrimSuffix(code, ".md"), true
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
		titles := index(agent)
		for _, path := range registry.Transcripts(agent) {
			s := describe(path, key, agent.Normalize)
			// A title from the agent's own index beats one read out of the
			// transcript only when the transcript had none.
			if s.Title == "" {
				s.Title = titles[identifier(path)]
			}
			s.Active = time.Since(s.When) < ActiveWindow
			all = append(all, s)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].When.After(all[j].When) })
	assignIDs(all)
	return all, nil
}

// assignIDs gives every session a short, stable handle.
//
// A prefix of the underlying identifier will not do. One format's identifiers are
// ordered by time, and sessions started in the same batch share their first
// twenty-four characters — on the machine this was written on, no prefix under
// twenty-five told them apart, which makes a prefix useless for the one thing an
// id is for.
//
// So the handle is derived instead: four characters of a hash, lengthened only if
// two collide. It is stable because it hashes the session's own identifier rather
// than its path — one agent archives a session by moving the file, and an id that
// changed when that happened would be an id you could not write down.
func assignIDs(all []Session) {
	for length := idLength; length <= idMaxLength; length++ {
		seen := map[string]bool{}
		clash := false
		for _, s := range all {
			id := handle(identifier(s.Path), length)
			if seen[id] {
				clash = true
				break
			}
			seen[id] = true
		}
		if clash {
			continue
		}
		for i := range all {
			all[i].ID = handle(identifier(all[i].Path), length)
		}
		return
	}
	for i := range all {
		all[i].ID = identifier(all[i].Path)
	}
}

// handle hashes an identifier down to something a person can type. Base32's
// alphabet excludes the character pairs people misread — no 0 against O, no 1
// against I.
func handle(identifier string, length int) string {
	sum := sha256.Sum256([]byte(identifier))
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:])
	return strings.ToLower(encoded[:length])
}

// identifier is the part of a filename that names the session. One format names
// its files after the session; another prefixes a word and a timestamp and puts
// the identifier last.
func identifier(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if found := uuid.FindString(name); found != "" {
		return found
	}
	return name
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
	if needle == "" {
		return Session{}, fmt.Errorf("name a session")
	}
	var matched []Session
	for _, s := range all {
		switch {
		// The handle from the listing, the underlying identifier for anyone who
		// has it, or a fragment of the title for anyone who does not.
		case s.ID == needle,
			strings.HasPrefix(strings.ToLower(identifier(s.Path)), needle),
			strings.Contains(strings.ToLower(s.Label()), needle):
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
		Agent: s.Agent,
		// Label, not Title: a format with no title record still has to name the
		// conversation, and the first thing said is what a person recognises.
		Title:    truncate(s.Label(), titleWidth),
		Dir:      s.Dir(),
		Captured: time.Now(),
		// The original is left where its own agent wrote it. Copying it would be
		// a second source of truth that can disagree with the first.
		RawPath: s.Path,
	}, turns, cap), nil
}

// index reads the agent's session list, for formats that keep their titles in one.
func index(a registry.Agent) map[string]string {
	titles := map[string]string{}
	for _, path := range registry.Index(a) {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		for id, name := range transcript.Titles(a.Normalize, f) {
			titles[id] = name
		}
		f.Close()
	}
	return titles
}

func describe(path, agent, format string) Session {
	// ID is filled in by assignIDs, which needs the whole list to know how short
	// it can be.
	s := Session{ID: identifier(path), Path: path, Agent: agent, Format: format}
	if info, err := os.Stat(path); err == nil {
		s.When, s.Bytes = info.ModTime(), info.Size()
	}
	// A preview is a convenience, so a transcript that will not preview still
	// appears in the list — with less to say about it, which is honest.
	if f, err := os.Open(path); err == nil {
		defer f.Close()
		if p, err := transcript.Peek(format, f); err == nil {
			s.Title, s.Surface, s.Dirs, s.Opening = p.Title, p.Surface, p.Dirs, p.Opening
		}
	}
	return s
}

// uuid finds a session identifier inside a filename.
var uuid = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
