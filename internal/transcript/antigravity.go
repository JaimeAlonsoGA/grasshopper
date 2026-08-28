package transcript

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"grasshopper/internal/bundle"
)

// JSONLSteps reads a transcript written as an ordered list of steps, where a
// step is whatever the agent did next: a question arriving, a plan being spoken,
// a checkpoint being written, a file being touched.
//
// Two of those step types are somebody talking and the rest are the agent
// narrating its own machinery, so the rest are dropped — including the
// checkpoint, which is the agent summarising the conversation back to itself and
// is exactly the kind of inferred text a hop must never carry as if it had been
// said.
func JSONLSteps(r io.ReadSeeker) ([]bundle.Turn, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	var turns []bundle.Turn
	lines := 0
	eachLine(r, false, func(raw []byte) {
		var s step
		if json.Unmarshal(raw, &s) != nil {
			return
		}
		lines++
		if turn, ok := s.turn(); ok {
			turns = append(turns, turn)
		}
	})

	if lines == 0 {
		return nil, fmt.Errorf("no parseable JSON lines")
	}
	if len(turns) == 0 {
		return nil, fmt.Errorf("%w: %d lines, but nothing was said", ErrNothingSaid, lines)
	}
	return turns, nil
}

// step is a line of the transcript. Thinking and tool calls live in fields of
// their own, which is convenient: not reading them is the whole policy.
type step struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

func (s step) turn() (bundle.Turn, bool) {
	switch s.Type {
	case "USER_INPUT":
		text := strings.TrimSpace(request(s.Content))
		if text == "" || IsInjected(text) {
			return bundle.Turn{}, false
		}
		return bundle.Turn{Who: bundle.Me, Text: text}, true

	case "PLANNER_RESPONSE":
		if text := strings.TrimSpace(s.Content); text != "" {
			return bundle.Turn{Who: bundle.Agent, Text: text}, true
		}
	}
	return bundle.Turn{}, false
}

// request takes what somebody sent out of the envelope the host wrapped it in.
// The envelope also carries the local time and the open editors, which are the
// host describing the moment rather than anything anybody said.
func request(content string) string {
	const open, close = "<USER_REQUEST>", "</USER_REQUEST>"
	start := strings.Index(content, open)
	if start < 0 {
		return content
	}
	end := strings.Index(content[start:], close)
	if end < 0 {
		return content
	}
	return content[start+len(open) : start+end]
}

func peekSteps(r io.ReadSeeker) (Preview, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return Preview{}, err
	}
	var preview Preview
	eachLine(io.LimitReader(r, peekBytes), false, func(raw []byte) {
		if preview.Opening != "" {
			return
		}
		var s step
		if json.Unmarshal(raw, &s) != nil {
			return
		}
		if turn, ok := s.turn(); ok && turn.Who == bundle.Me {
			preview.Opening = firstLine(turn.Text)
		}
	})
	return preview, nil
}

// eachLine walks JSON lines and hands each one over whole.
//
// It is shared by the readers whose format is one self-contained object per line,
// which is most of them: the differences between those formats are in what the
// object says, never in how the file is cut into objects.
func eachLine(r io.Reader, skipFirst bool, each func([]byte)) {
	br := bufio.NewReaderSize(r, 1<<16)
	if skipFirst {
		if _, err := br.ReadString('\n'); err != nil {
			return
		}
	}
	for {
		line, err := br.ReadString('\n')
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			each([]byte(trimmed))
		}
		if err != nil {
			return
		}
	}
}
