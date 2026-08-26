// Package bundle is the domain: what a transported conversation is, what
// survives the trip, and the document another agent reads.
//
// The thing it produces is called a hop — the same word as the command and the
// software, the way Docker calls its artefact an image. "Send me that hop" is a
// sentence somebody says out loud, which is the test a name has to pass.
//
// Nothing here touches the world. No os, no filesystem, no exec, no clock. The
// rendered bundle is the only surface of this product anyone actually reads, so
// it is worth being a pure function of its inputs. Everything that needs the
// world lives in a caller.
package bundle

import (
	"crypto/sha256"
	"encoding/base32"
	"strings"
	"time"
)

// Cap is a safety valve, not a budget.
//
// Measured on real transcripts: a 64 MB session reduces to 109 KB once thinking
// and tool payloads are dropped — a 99.8% cut without altering a word of what was
// said. That fits any context window with room to spare, so nearly nothing is
// ever capped. An aggressive budget was the earlier mistake: it threw away three
// quarters of a conversation that would have loaded fine.
const Cap = 150_000

// Speaker is who said a turn. Two values, never a product name: what matters is
// whether a turn carries intent or answers.
type Speaker string

const (
	Me    Speaker = "me"
	Agent Speaker = "agent"
)

type Turn struct {
	Who  Speaker
	Text string
}

// Source is where a bundle came from. Every field is here so the receiving agent
// can place the conversation without asking: which tool, which session, when,
// and where the work was happening.
type Source struct {
	Agent    string // registry key
	Title    string // the agent's own name for the session, if it has one
	Dir      string // the working directory
	Branch   string // "" when the directory is not a repository
	Captured time.Time
	RawPath  string // the untouched transcript, for when the bundle is not enough
}

type Bundle struct {
	Code   string
	Source Source
	Turns  []Turn

	// Omitted counts turns dropped for size, and OmittedAfter says where the hole
	// is: -1 when it sits before everything, 0 when the objective was rescued and
	// the gap opens after it. The renderer marks the hole rather than hiding it.
	Omitted      int
	OmittedAfter int
}

// New assembles a bundle: filter, cap, then fingerprint what survived. One
// constructor, so Code and Turns can never disagree.
func New(src Source, turns []Turn, cap int) Bundle {
	kept, omitted, after := Fit(Compact(turns), cap)
	return Bundle{Code: Code(kept), Source: src, Turns: kept, Omitted: omitted, OmittedAfter: after}
}

// Compact merges consecutive turns from the same speaker and normalises
// whitespace, dropping turns that hold nothing once normalised.
func Compact(turns []Turn) []Turn {
	out := make([]Turn, 0, len(turns))
	for _, t := range turns {
		text := normalize(t.Text)
		if text == "" {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Who == t.Who {
			out[n-1].Text += "\n\n" + text
			continue
		}
		out = append(out, Turn{Who: t.Who, Text: text})
	}
	return out
}

// Fit brings a conversation under the cap, and it protects one turn specially:
// the first thing the human said.
//
// That turn is the objective, stated in their own words, and it sits at the far
// end of the file from everything else worth keeping. Dropping purely from the
// oldest end loses it first — which leaves the receiving agent the conclusion of
// a discussion whose purpose it can only guess at. A cap of zero or less means no
// limit. A single turn larger than the cap is kept anyway: a turn cut in half is
// a turn that lies about what was said.
func Fit(turns []Turn, cap int) (kept []Turn, omitted, after int) {
	if cap <= 0 || len(turns) == 0 {
		return turns, 0, -1
	}

	sizes := make([]int, len(turns))
	total := len(separator) * (len(turns) - 1)
	for i, t := range turns {
		sizes[i] = len(renderTurn(t))
		total += sizes[i]
	}
	if total <= cap {
		return turns, 0, -1
	}

	objective := -1
	for i, t := range turns {
		if t.Who == Me {
			objective = i
			break
		}
	}

	first := 0
	for first < len(turns)-1 && total > cap {
		if first != objective {
			total -= sizes[first] + len(separator)
		}
		first++
	}

	// The objective rejoins the front of what survived, and the gap between them
	// is declared by the renderer rather than hidden.
	if objective >= 0 && objective < first {
		return append([]Turn{turns[objective]}, turns[first:]...), first - 1, 0
	}
	return turns[first:], first, -1
}

// Code is a short fingerprint of the conversation. It exists so a person can tell
// at a glance whether the receiving agent really read the bundle or merely said
// it did.
//
// Base32's alphabet is uppercase letters and 2-7, which excludes every character
// pair people misread aloud — no 0/O, no 1/I.
func Code(turns []Turn) string {
	h := sha256.New()
	for _, t := range turns {
		h.Write([]byte(t.Who))
		h.Write([]byte{0})
		h.Write([]byte(t.Text))
		h.Write([]byte{0})
	}
	sum := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(h.Sum(nil))
	return "HOP-" + sum[:4]
}

// normalize collapses runs of blank lines and strips trailing spaces, while
// leaving fenced blocks exactly as they were. Code is the part of a turn that
// must survive byte for byte; reflowing it turns a working snippet into a
// plausible-looking broken one.
func normalize(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	fence := ""
	blanks := 0

	for _, line := range lines {
		if fence != "" {
			out = append(out, line)
			if closesFence(line, fence) {
				fence = ""
			}
			continue
		}
		if open := opensFence(line); open != "" {
			fence, blanks = open, 0
			out = append(out, line)
			continue
		}
		if strings.TrimSpace(line) == "" {
			blanks++
			if blanks > 1 || len(out) == 0 {
				continue
			}
			out = append(out, "")
			continue
		}
		blanks = 0
		out = append(out, strings.TrimRight(line, " \t"))
	}
	return strings.Trim(strings.Join(out, "\n"), "\n")
}

// opensFence returns the fence marker a line opens, or "" if it opens none.
func opensFence(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	for _, ch := range []string{"`", "~"} {
		run := 0
		for run < len(trimmed) && trimmed[run:run+1] == ch {
			run++
		}
		if run >= 3 {
			return strings.Repeat(ch, run)
		}
	}
	return ""
}

func closesFence(line, fence string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, fence) {
		return false
	}
	// A closing fence is the marker and nothing else; an info string only ever
	// appears on the opening one.
	return strings.TrimLeft(trimmed, fence[:1]) == ""
}
