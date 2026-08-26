// Package pick is an arrow-key chooser for a terminal.
//
// It costs about a hundred lines and no dependency. Raw mode comes from stty on
// /dev/tty rather than from a termios binding, which is a trade made on purpose:
// shelling out to a utility that has been on every Unix for forty years is less
// to trust than a library, and the failure mode is a fallback rather than a crash.
package pick

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Row is one choice. Cells are laid out in columns; Muted marks a row that is
// present but not the point — an idle session among running ones.
type Row struct {
	Cells []string
	Muted bool
}

// ErrCancelled is somebody deciding not to. Pressing escape is an answer, and it
// should not print an error.
var ErrCancelled = fmt.Errorf("cancelled")

// From chooses a row. When the terminal cannot be put into raw mode — a pipe, a
// dumb terminal, a CI job — it degrades to a numbered list read line by line,
// which works everywhere and needs nothing.
func From(prompt string, header []string, rows []Row) (int, error) {
	if len(rows) == 0 {
		return 0, fmt.Errorf("nothing to choose from")
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return numbered(prompt, header, rows, os.Stdin, os.Stderr)
	}
	defer tty.Close()

	restore, err := raw()
	if err != nil {
		return numbered(prompt, header, rows, os.Stdin, os.Stderr)
	}
	defer restore()

	return interactive(tty, prompt, header, rows)
}

func interactive(tty *os.File, prompt string, header []string, rows []Row) (int, error) {
	widths := columns(append([][]string{header}, cells(rows)...))
	cursor := 0
	drawn := 0

	draw := func() {
		var out strings.Builder
		if drawn > 0 {
			// Back up over what was drawn last time and overwrite it, rather than
			// clearing the screen: whatever the person had above stays put.
			fmt.Fprintf(&out, "\x1b[%dA", drawn)
		}
		drawn = 0

		writeLine(&out, "  "+dim(row(header, widths)))
		drawn++
		for i, r := range rows {
			marker, text := "  ", row(r.Cells, widths)
			switch {
			case i == cursor:
				marker, text = "› ", bold(text)
			case r.Muted:
				text = dim(text)
			}
			writeLine(&out, marker+text)
			drawn++
		}
		writeLine(&out, "  "+dim(prompt))
		drawn++
		fmt.Fprint(os.Stderr, out.String())
	}

	draw()
	reader := bufio.NewReader(tty)
	for {
		key, err := readKey(reader)
		if err != nil {
			return 0, ErrCancelled
		}
		switch key {
		case keyUp:
			if cursor > 0 {
				cursor--
			}
		case keyDown:
			if cursor < len(rows)-1 {
				cursor++
			}
		case keyTop:
			cursor = 0
		case keyBottom:
			cursor = len(rows) - 1
		case keyEnter:
			draw()
			return cursor, nil
		case keyCancel:
			draw()
			return 0, ErrCancelled
		default:
			// A digit jumps, so a long list does not need forty presses.
			if n, err := strconv.Atoi(string(rune(key))); err == nil && n >= 1 && n <= len(rows) {
				cursor = n - 1
			}
		}
		draw()
	}
}

type key int

const (
	keyNone key = iota
	keyUp
	keyDown
	keyEnter
	keyCancel
	keyTop
	keyBottom
)

// readKey turns bytes into one intention. Arrows arrive as an escape sequence, so
// a lone escape is only a cancel once nothing follows it.
func readKey(r *bufio.Reader) (key, error) {
	b, err := r.ReadByte()
	if err != nil {
		return keyNone, err
	}
	switch b {
	case '\r', '\n':
		return keyEnter, nil
	case 'q', 'Q', 3, 4: // q, ctrl-c, ctrl-d
		return keyCancel, nil
	case 'k':
		return keyUp, nil
	case 'j':
		return keyDown, nil
	case 'g':
		return keyTop, nil
	case 'G':
		return keyBottom, nil
	case 0x1b:
		if r.Buffered() == 0 {
			return keyCancel, nil
		}
		if next, _ := r.ReadByte(); next != '[' && next != 'O' {
			return keyNone, nil
		}
		final, err := r.ReadByte()
		if err != nil {
			return keyNone, err
		}
		switch final {
		case 'A':
			return keyUp, nil
		case 'B':
			return keyDown, nil
		case 'H':
			return keyTop, nil
		case 'F':
			return keyBottom, nil
		}
		return keyNone, nil
	}
	return key(b), nil
}

// numbered is the fallback: no raw mode, no arrows, works down a pipe.
func numbered(prompt string, header []string, rows []Row, in io.Reader, out io.Writer) (int, error) {
	widths := columns(append([][]string{header}, cells(rows)...))
	fmt.Fprintf(out, "   %s\n", row(header, widths))
	for i, r := range rows {
		fmt.Fprintf(out, "%2d %s\n", i+1, row(r.Cells, widths))
	}
	fmt.Fprintf(out, "%s [1-%d, or q] ", prompt, len(rows))

	line, err := bufio.NewReader(in).ReadString('\n')
	answer := strings.TrimSpace(line)
	if answer == "" || answer == "q" || answer == "Q" || (err != nil && answer == "") {
		return 0, ErrCancelled
	}
	n, convErr := strconv.Atoi(answer)
	if convErr != nil || n < 1 || n > len(rows) {
		return 0, fmt.Errorf("%q is not one of 1 to %d", answer, len(rows))
	}
	return n - 1, nil
}

// raw puts the terminal into a mode where a keypress arrives immediately and is
// not echoed, and returns the call that puts it back. Restoring is not optional:
// a terminal left in raw mode is a terminal the person has to reset by hand.
func raw() (func(), error) {
	saved, err := stty("-g")
	if err != nil {
		return nil, err
	}
	if _, err := stty("raw", "-echo"); err != nil {
		return nil, err
	}
	return func() {
		_, _ = stty(strings.TrimSpace(saved))
		fmt.Fprint(os.Stderr, "\x1b[?25h") // the cursor comes back too
	}, nil
}

func stty(args ...string) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	defer tty.Close()

	cmd := exec.Command("stty", args...)
	cmd.Stdin = tty
	out, err := cmd.Output()
	return string(out), err
}

func cells(rows []Row) [][]string {
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Cells)
	}
	return out
}

func columns(all [][]string) []int {
	var widths []int
	for _, row := range all {
		for i, cell := range row {
			for len(widths) <= i {
				widths = append(widths, 0)
			}
			if n := width(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}
	return widths
}

func row(cells []string, widths []int) string {
	var b strings.Builder
	for i, cell := range cells {
		b.WriteString(cell)
		if i < len(cells)-1 && i < len(widths) {
			b.WriteString(strings.Repeat(" ", widths[i]-width(cell)+2))
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// width counts what a terminal shows, which is runes rather than bytes: the
// characters these tables use for "absent" are three bytes wide and one column
// wide.
func width(s string) int { return len([]rune(s)) }

// writeLine clears to the end before the newline, so a shorter row does not leave
// the tail of a longer one behind it.
func writeLine(b *strings.Builder, text string) {
	b.WriteString("\r")
	b.WriteString(text)
	b.WriteString("\x1b[K\n")
}

func dim(s string) string  { return "\x1b[2m" + s + "\x1b[0m" }
func bold(s string) string { return "\x1b[1m" + s + "\x1b[0m" }
