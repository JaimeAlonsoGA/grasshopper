package transcript

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"grasshopper/internal/bundle"
)

// A Container is a file that holds more than one conversation.
//
// Every other format in here is one session per file, which is what the rest of
// grasshopper assumes: a path is a session and a reader takes bytes. Some agents
// keep a single database with every conversation they have ever had, and the
// difference is not cosmetic — you cannot name one of those conversations with a
// path, so the path alone stops being an address.
//
// So a container answers two questions instead of one: what is in this file, and
// what was said in one particular thing that is in it.
type Container interface {
	// List is what the file holds, newest last. It reads metadata only: a listing
	// must stay instant on a database holding a year of conversations.
	List(path string) ([]Contained, error)

	// Turns reads one of them. The key is whatever List handed back.
	Turns(path, key string) ([]bundle.Turn, error)
}

// Contained is one conversation inside a container, described well enough to be
// listed without being read.
type Contained struct {
	Key     string
	Title   string
	Opening string
	Dir     string
	When    int64 // unix seconds; zero when the format does not say
}

var containers = map[string]Container{
	"sqlite-cursor":   cursorDB{},
	"sqlite-openclaw": openclawDB{},
}

// IsContainer reports whether a format keeps many conversations in one file.
func IsContainer(format string) bool {
	_, ok := containers[format]
	return ok
}

// Inside lists the conversations a container file holds.
func Inside(format, path string) ([]Contained, error) {
	c, ok := containers[format]
	if !ok {
		return nil, fmt.Errorf("format %q is not a container", format)
	}
	found, err := c.List(path)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].When < found[j].When })
	return found, nil
}

// One reads a single conversation out of a container file.
func One(format, path, key string) ([]bundle.Turn, error) {
	c, ok := containers[format]
	if !ok {
		return nil, fmt.Errorf("format %q is not a container", format)
	}
	turns, err := c.Turns(path, key)
	if err != nil {
		return nil, err
	}
	if len(turns) == 0 {
		return nil, fmt.Errorf("%w: nothing was said in %s", ErrNothingSaid, key)
	}
	return turns, nil
}

// errNoTable is what a container reports when the database it opened is not the
// one it knows. Agents rename and reshape their storage, and a stale glob finding
// somebody else's database should be quiet rather than loud.
var errNoTable = errors.New("this database does not hold conversations in the shape expected")

// speak turns a role and a body into a turn, dropping what nobody said.
func speak(who bundle.Speaker, text string) (bundle.Turn, bool) {
	text = strings.TrimSpace(text)
	if text == "" || IsInjected(text) {
		return bundle.Turn{}, false
	}
	return bundle.Turn{Who: who, Text: text}, true
}
