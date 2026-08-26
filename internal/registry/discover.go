package registry

import (
	"os"
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

func matches(pattern string, order func([]string)) []string {
	if pattern == "" {
		return nil
	}
	found, err := filepath.Glob(expand(pattern))
	if err != nil {
		return nil
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
}

func Discover() ([]Status, error) {
	r, err := Load()
	if err != nil {
		return nil, err
	}
	out := make([]Status, 0, len(r))
	for _, key := range r.Keys() {
		agent := r[key]
		out = append(out, Status{
			Key:         key,
			Agent:       agent,
			StateDir:    stateDir(agent),
			Transcripts: len(Transcripts(agent)),
			Readable:    agent.Normalize != "",
		})
	}
	return out, nil
}

// stateDir is derived rather than declared: the transcript glob names the
// directory outright, so a fifth field to keep in sync is not needed.
func stateDir(a Agent) string {
	if a.Transcripts == "" {
		return ""
	}
	path := expand(a.Transcripts)
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
