package transcript

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"grasshopper/internal/bundle"
)

// peekBytes bounds each end of the read. The opening exchange is at the top of a
// transcript and the latest working directory is at the bottom; a listing has no
// business reading the sixty megabytes in between.
const peekBytes = 256 << 10

// Preview is the little a listing needs for somebody to recognise which session
// they are looking at, and to know where its work was happening.
//
// Dirs is every working directory the session was seen in, most recent first,
// because a session moves: the one that built this tool started in another
// project entirely and ended two directories away from where it began. A single
// answer to "where was this session working" would be a guess, and the caller
// needs the truth to put in front of somebody.
type Preview struct {
	// Surface is which front end started the session — a terminal, an editor
	// extension, a desktop app, a phone. Both formats record it, under different
	// names, and it is reported raw rather than translated: the value the agent
	// wrote is the one that will still be true after they rename something.
	Surface string

	// Title is what the agent named this session, taken from the transcript
	// itself rather than from any sidecar the app keeps: the app's own index only
	// holds sessions currently open in its sidebar, so it forgets a conversation
	// the moment you close it. The transcript remembers forever. Measured across
	// every session on this machine, the title record sits in the first 8% of the
	// file — well inside the head this already reads.
	Title   string
	Opening string
	Dirs    []string
}

// Peek reads both ends of a transcript. It is separate from Get because the two
// have opposite priorities: a capture must be exact and may take its time, a
// listing must be instant and may be incomplete.
func Peek(format string, r io.ReadSeeker) (Preview, error) {
	switch format {
	case "jsonl-tree":
		return peekJSONLTree(r)
	case "jsonl-events":
		return peekEvents(r)
	case "jsonl-patch":
		return peekPatch(r)
	case "jsonl-grok":
		return peekGrok(r)
	case "jsonl-steps":
		return peekSteps(r)
	case "":
		return Preview{}, errors.New("no transcript format configured for this agent")
	default:
		return Preview{}, nil
	}
}

func peekJSONLTree(r io.ReadSeeker) (Preview, error) {
	size, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return Preview{}, err
	}

	var preview Preview
	var seen []string

	// The head carries the opening question, which is what a person recognises a
	// session by.
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return Preview{}, err
	}
	scan(io.LimitReader(r, peekBytes), false, func(rec headRecord) {
		seen = append(seen, rec.Dir)
		// A title the person typed beats one the agent generated, and a later
		// title beats an earlier one: both get revised as a session finds its
		// subject.
		if rec.Entrypoint != "" && preview.Surface == "" {
			preview.Surface = rec.Entrypoint
		}
		if rec.CustomTitle != "" {
			preview.Title = rec.CustomTitle
		} else if rec.AITitle != "" && preview.Title == "" {
			preview.Title = rec.AITitle
		}
		if preview.Opening == "" {
			if turn, ok := turnOf(rec.record); ok && turn.Who == bundle.Me {
				preview.Opening = firstLine(turn.Text)
			}
		}
	})

	// The tail carries where the session ended up.
	if size > peekBytes {
		if _, err := r.Seek(size-peekBytes, io.SeekStart); err != nil {
			return Preview{}, err
		}
		scan(r, true, func(rec headRecord) { seen = append(seen, rec.Dir) })
	}

	preview.Dirs = mostRecentFirst(seen)
	return preview, nil
}

// headRecord is a transcript line plus the one field a preview needs beyond what
// a capture reads.
type headRecord struct {
	record
	Dir         string `json:"cwd"`
	CustomTitle string `json:"customTitle"`
	AITitle     string `json:"aiTitle"`
	Entrypoint  string `json:"entrypoint"`
}

// scan walks JSON lines, dropping the first when the read began mid-file and so
// began mid-line.
func scan(r io.Reader, partialFirstLine bool, each func(headRecord)) {
	br := bufio.NewReaderSize(r, 1<<16)
	if partialFirstLine {
		if _, err := br.ReadString('\n'); err != nil {
			return
		}
	}
	for {
		line, err := br.ReadString('\n')
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			var rec headRecord
			if json.Unmarshal([]byte(trimmed), &rec) == nil {
				each(rec)
			}
		}
		if err != nil {
			return
		}
	}
}

// mostRecentFirst reverses and deduplicates, so the caller offers the latest
// place the session was working before the ones it left behind.
func mostRecentFirst(seen []string) []string {
	var dirs []string
	present := map[string]bool{}
	for i := len(seen) - 1; i >= 0; i-- {
		dir := seen[i]
		if dir == "" || present[dir] {
			continue
		}
		present[dir] = true
		dirs = append(dirs, dir)
	}
	return dirs
}

// firstLine is what a person recognises a session by. A prompt that opens with a
// pasted file reference or an injected notice tells you nothing, so the first
// line with words of their own in it wins over the literally-first one.
func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "@") || strings.HasPrefix(line, "<") {
			continue
		}
		return line
	}
	return strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
}
