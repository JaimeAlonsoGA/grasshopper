package transcript

import "strings"

// Host CLIs inject their own bookkeeping into the conversation wearing the
// human's role: an editor telling the model which file is open, a task finishing
// in the background, the stdout of a local command. It arrives as XML-ish
// elements inside an otherwise ordinary user turn, so it cannot be filtered by
// record type — it has to be recognised by its wrapper.
//
// This matters more than it looks. Human turns are the part of a transcript that
// carries intent, and they are tiny. Letting a dozen "the user opened a file"
// notices through would bury the handful of sentences that actually say what the
// work is.
//
// The two lists are deliberately closed. A wrapper hop has not seen passes
// through as visible noise, which a reader can dismiss; guessing at unknown tags
// would eventually eat something a person wrote.
var (
	// discarded elements are plumbing all the way through, wrapper and content.
	discarded = []string{
		"system-reminder",
		"local-command-caveat",
		"local-command-stdout",
		"local-command-stderr",
		"command-message",
		"task-notification",
		"user-prompt-submit-hook",
		"ide_opened_file",
		"ide_selection",
		"ide_diagnostics",
	}
	// unwrapped elements have a wrapper worth losing and content worth keeping.
	// A slash command is something a person chose to run; it is intent.
	unwrapped = []string{
		"command-name",
		"command-args",
	}
)

func stripEnvelopes(s string) string {
	for _, tag := range discarded {
		s = removeElement(s, tag)
	}
	for _, tag := range unwrapped {
		s = strings.ReplaceAll(s, "<"+tag+">", "")
		s = strings.ReplaceAll(s, "</"+tag+">", "")
	}
	return dropStatusLines(s)
}

// removeElement deletes every occurrence of an element and its content. An
// unterminated open tag takes everything after it: the host truncated its own
// notice, and the remainder is not something a person said.
func removeElement(s, tag string) string {
	open, close := "<"+tag+">", "</"+tag+">"
	for {
		i := strings.Index(s, open)
		if i < 0 {
			return s
		}
		rest := s[i+len(open):]
		j := strings.Index(rest, close)
		if j < 0 {
			return s[:i]
		}
		s = s[:i] + rest[j+len(close):]
	}
}

// dropStatusLines removes the bracketed notices the host writes in place of a
// turn when a person cancels something. They record that an interruption
// happened, not what anybody wanted.
func dropStatusLines(s string) string {
	if !strings.Contains(s, "[Request interrupted") {
		return s
	}
	lines := strings.Split(s, "\n")
	kept := lines[:0]
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[Request interrupted") && strings.HasSuffix(trimmed, "]") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
