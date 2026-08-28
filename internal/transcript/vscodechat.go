package transcript

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"grasshopper/internal/bundle"
)

// JSONLPatch reads a chat session that is written as a running log of edits
// rather than as a list of messages.
//
// The first line carries the whole session state. Every line after it is a patch:
// a path into that state, a value, and whether the value replaces what is there
// or is appended to it. The conversation is therefore not in any single line —
// it is what the state holds once every patch has been applied, which is why this
// reader walks the file to the end before it has a single turn.
//
// Only two paths matter, and everything else is ignored on purpose: the requests
// list, and the response of one request inside it. The format carries selections,
// input drafts, token counts, followups and model identities in the same stream,
// none of which is a thing anybody said.
func JSONLPatch(r io.ReadSeeker) ([]bundle.Turn, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	state, lines := replay(r)
	if lines == 0 {
		return nil, fmt.Errorf("no parseable JSON lines")
	}

	var turns []bundle.Turn
	for _, request := range state.Requests {
		if said := strings.TrimSpace(request.Message.Text); said != "" && !IsInjected(said) {
			turns = append(turns, bundle.Turn{Who: bundle.Me, Text: said})
		}
		if replied := request.reply(); replied != "" {
			turns = append(turns, bundle.Turn{Who: bundle.Agent, Text: replied})
		}
	}
	if len(turns) == 0 {
		return nil, fmt.Errorf("%w: %d lines, but nothing was said", ErrNothingSaid, lines)
	}
	return turns, nil
}

// chatState is the part of the session this reader keeps. The format has some
// thirty other top-level fields.
type chatState struct {
	Title    string
	Requests []chatRequest
}

type chatRequest struct {
	Message struct {
		Text string `json:"text"`
	} `json:"message"`
	Response []chatPart `json:"response"`
}

// chatPart is one piece of an agent's answer. A part with a kind is machinery —
// reasoning, a tool call, a file reference, an edit group — and a part without
// one is prose. That is the whole rule, and it holds across every part shape in
// the format: the prose parts are markdown strings, which have no kind because
// they are not events.
type chatPart struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`

	// BaseURI is the folder the answer was written against, carried on the prose
	// parts themselves. It is the only place this format names a directory.
	BaseURI struct {
		Path string `json:"path"`
	} `json:"baseUri"`
}

// reply joins the prose an agent produced for one request, dropping everything
// that was machinery.
func (c chatRequest) reply() string {
	var said []string
	for _, part := range c.Response {
		if part.Kind != "" {
			continue
		}
		if text := strings.TrimSpace(part.Value); text != "" {
			said = append(said, part.Value)
		}
	}
	return strings.TrimSpace(strings.Join(said, ""))
}

// patch is a line of the log. Kind 0 is the base state, 1 replaces what is at the
// path, 2 splices into it.
type patch struct {
	Kind  int               `json:"kind"`
	Path  []json.RawMessage `json:"k"`
	Value json.RawMessage   `json:"v"`

	// At is where a splice lands. Absent means the end, which is the ordinary
	// append. Present means everything from there on is replaced — the writer
	// re-sends an overlapping window rather than only what is new, and appending
	// those windows instead of splicing them repeats whole paragraphs of an
	// answer. This one field is the difference between a transcript and a stutter.
	At *int `json:"i"`
}

// splice grows a list the way this format's kind 2 means it to.
func splice[T any](have []T, add []T, at *int) []T {
	if at != nil && *at >= 0 && *at <= len(have) {
		have = have[:*at]
	}
	return append(have, add...)
}

// replay applies every line to the state and reports how many it could read.
func replay(r io.Reader) (chatState, int) {
	var state chatState
	lines := eachJSON(r, false, func(p patch) { apply(&state, p) })
	return state, lines
}

// apply routes one patch. An unrecognised path is not an error: this reader is
// deliberately blind to most of the format, and a session that grows a field it
// has never heard of still reads.
func apply(state *chatState, p patch) {
	// Kind 0 is the snapshot the log opens with, and it has no path: its value is
	// the state itself. Everything after it edits what this line established.
	if p.Kind == 0 && len(p.Path) == 0 {
		var base struct {
			Title    string        `json:"customTitle"`
			Requests []chatRequest `json:"requests"`
		}
		if json.Unmarshal(p.Value, &base) == nil {
			if base.Title != "" {
				state.Title = base.Title
			}
			state.Requests = base.Requests
		}
		return
	}

	path := segments(p.Path)
	switch {
	case len(path) == 1 && path[0] == "customTitle":
		var title string
		if json.Unmarshal(p.Value, &title) == nil {
			state.Title = title
		}

	case len(path) == 1 && path[0] == "requests":
		added := many[chatRequest](p.Value)
		if p.Kind == 2 {
			state.Requests = splice(state.Requests, added, p.At)
			return
		}
		state.Requests = added

	case len(path) == 3 && path[0] == "requests" && path[2] == "response":
		at, err := strconv.Atoi(path[1])
		if err != nil || at < 0 {
			return
		}
		// A patch can name a request the base state did not have, because the
		// base is a snapshot and the log continues past it.
		for len(state.Requests) <= at {
			state.Requests = append(state.Requests, chatRequest{})
		}
		added := many[chatPart](p.Value)
		if p.Kind == 2 {
			state.Requests[at].Response = splice(state.Requests[at].Response, added, p.At)
			return
		}
		state.Requests[at].Response = added
	}
}

// many reads a patch value that may be one element or a list of them.
//
// An append carries a list here and a bare element there, in the same file, for
// the same path. Guessing from the patch kind is wrong; the shape of the value is
// the only thing that says which it is.
func many[T any](raw json.RawMessage) []T {
	var list []T
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	var one T
	if json.Unmarshal(raw, &one) == nil {
		return []T{one}
	}
	return nil
}

// segments flattens a path whose elements are either object keys or array
// indices, so one comparison serves both.
func segments(raw []json.RawMessage) []string {
	out := make([]string, 0, len(raw))
	for _, seg := range raw {
		var key string
		if json.Unmarshal(seg, &key) == nil {
			out = append(out, key)
			continue
		}
		var index int
		if json.Unmarshal(seg, &index) == nil {
			out = append(out, strconv.Itoa(index))
			continue
		}
		return nil
	}
	return out
}

// peekPatch previews a session of this shape.
//
// Both ends, like the other formats, and here the reason is sharper: the opening
// snapshot of a long session is the whole session, so on a file of tens of
// megabytes the first line alone can overrun the head and arrive truncated. The
// tail is what rescues those — a title revised late, the folder the session ended
// in, and the appends that carry the requests themselves.
func peekPatch(r io.ReadSeeker) (Preview, error) {
	size, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return Preview{}, err
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return Preview{}, err
	}

	head, _ := replay(io.LimitReader(r, peekBytes))
	tail := chatState{}
	if size > peekBytes {
		if _, err := r.Seek(size-peekBytes, io.SeekStart); err != nil {
			return Preview{}, err
		}
		// The read begins mid-line, so the first one is dropped rather than
		// half-parsed.
		tail.Requests = nil
		var state chatState
		eachJSON(r, true, func(p patch) { apply(&state, p) })
		tail = state
	}

	preview := Preview{Title: head.Title}
	if tail.Title != "" {
		preview.Title = tail.Title
	}
	var seen []string
	for _, state := range []chatState{head, tail} {
		for _, request := range state.Requests {
			if preview.Opening == "" {
				if said := strings.TrimSpace(request.Message.Text); said != "" {
					preview.Opening = firstLine(said)
				}
			}
			for _, part := range request.Response {
				seen = append(seen, part.BaseURI.Path)
			}
		}
	}
	preview.Dirs = mostRecentFirst(seen)
	return preview, nil
}
