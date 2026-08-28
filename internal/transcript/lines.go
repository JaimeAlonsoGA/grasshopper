package transcript

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

// Every format in here is JSON lines. They disagree about what a line says and
// agree completely about how a file is cut into them, so the cutting lives here
// once and the disagreement lives in the readers.
//
// The policy is the same for all of them and it is deliberate: a line that will
// not parse is skipped rather than fatal. Transcripts are appended to while they
// are being read, and a file caught mid-write ends in half a line — refusing the
// whole conversation over its last forty bytes would lose the other forty
// thousand.
//
// Lines are read without a size cap. A single pasted file inside a transcript
// runs to megabytes, and a reader that stops at a buffer boundary would drop the
// turn that mattered most.

// eachLine hands over every non-empty line, trimmed.
//
// skipFirst drops the opening line, for a read that began at an offset and so
// began in the middle of one.
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

// eachJSON hands over every line that decodes into T, and counts them.
//
// The count is what tells "this file holds no conversation" apart from "this file
// is not a transcript at all", which are different answers to the person asking
// and only one of them is their mistake.
func eachJSON[T any](r io.Reader, skipFirst bool, each func(T)) int {
	parsed := 0
	eachLine(r, skipFirst, func(raw []byte) {
		var value T
		if json.Unmarshal(raw, &value) != nil {
			return
		}
		parsed++
		each(value)
	})
	return parsed
}
