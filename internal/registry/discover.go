package registry

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Transcripts is every file an agent's configured glob currently matches, newest
// last so the final element is the session most likely still open.
func Transcripts(a Agent) []string {
	return matches(a.Transcripts, func(paths []string) {
		sort.Slice(paths, func(i, j int) bool { return modTime(paths[i]).Before(modTime(paths[j])) })
	})
}

// OnPath reports whether an agent's launch command can actually be run.
func OnPath(launch string) bool {
	if launch == "" {
		return false
	}
	_, err := exec.LookPath(launch)
	return err == nil
}

// Index is the file naming an agent's sessions, when it keeps one.
func Index(a Agent) []string { return matches(a.Index, nil) }

// matches expands one glob or several, comma separated, and deduplicates. An
// agent that moves a session from one directory to another mid-run would
// otherwise appear twice for as long as both globs see it.
func matches(patterns string, order func([]string)) []string {
	if patterns == "" {
		return nil
	}
	var found []string
	seen := map[string]bool{}
	for _, pattern := range strings.Split(patterns, ",") {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		paths, err := filepath.Glob(expand(pattern))
		if err != nil {
			continue
		}
		for _, path := range paths {
			if !seen[path] {
				seen[path] = true
				found = append(found, path)
			}
		}
	}
	if order != nil {
		order(found)
	}
	return found
}

// Status is what discovery found about one agent. It looks rather than assumes:
// the registry says where things should be, this reports whether they are there,
// which is the difference between a report and a guess.
type Status struct {
	Key         string
	Agent       Agent
	StateDir    string // "" when the agent has left no state behind
	Transcripts int    // how many files the glob matches right now
	Readable    bool   // a transcript format is configured

	// Shipped is how many this version's own glob would match. grasshopper will
	// not overwrite a registry somebody may have edited, so when the two disagree
	// the only honest thing left is to say so.
	Shipped int
}

// Stale reports a configured glob that is missing sessions the shipped one finds.
//
// Finding fewer, not finding none: an agent that has just written one session
// into the old location while twenty sit in the new one still matches its glob,
// and reporting that as linked is the silent failure this exists to catch.
func (s Status) Stale() bool { return s.Shipped > s.Transcripts }

func Discover() ([]Status, error) {
	r, err := Load()
	if err != nil {
		return nil, err
	}
	out := make([]Status, 0, len(r))
	for _, key := range r.Keys() {
		agent := r[key]
		status := Status{
			Key:         key,
			Agent:       agent,
			StateDir:    stateDir(agent),
			Transcripts: len(Transcripts(agent)),
			Readable:    agent.Normalize != "",
		}
		if shipped, ok := Default()[key]; ok && shipped.Transcripts != agent.Transcripts {
			status.Shipped = len(Transcripts(shipped))
		}
		out = append(out, status)
	}
	return out, nil
}

// stateDir is derived rather than declared: the transcript glob names the
// directory outright, so a fifth field to keep in sync is not needed.
func stateDir(a Agent) string {
	if a.Transcripts == "" {
		return ""
	}
	path := expand(strings.Split(a.Transcripts, ",")[0])
	if i := strings.IndexAny(path, "*?["); i >= 0 {
		path = filepath.Dir(path[:i])
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return path
	}
	return ""
}

func modTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}
