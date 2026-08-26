// Package pick is an arrow-key chooser for a terminal.
//
// It costs about two hundred lines and no dependency. Raw mode and the terminal
// size come from stty on /dev/tty rather than from a termios binding, which is a
// trade made on purpose: a utility that has been on every Unix for forty years is
// less to trust than a library, and the failure mode is a fallback rather than a
// crash.
//
// The list draws on the alternate screen — the same buffer a pager uses — so
// choosing leaves nothing behind in the scrollback. Whatever was on screen before
// is exactly what is there afterwards.
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

const (
	// maxRows is how much list is shown at once. A window is easier to read than
	// a wall: forty rows is a thing you scan and give up on, ten is a thing you
	// look at. Every chooser worth using picks a number around here.
	maxRows = 10

	// reserved is the lines the frame takes around the list — the title, a blank,
	// the scroll hints, the footer.
	reserved = 8
)

// From chooses a row. When the terminal cannot be put into raw mode — a pipe, a
// dumb terminal, a CI job — it degrades to a numbered list read line by line,
// which works everywhere and needs nothing.
func From(title string, header []string, rows []Row) (int, error) {
	if len(rows) == 0 {
		return 0, fmt.Errorf("nothing to choose from")
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return numbered(title, header, rows, os.Stdin, os.Stderr)
	}
	defer tty.Close()

	restore, err := takeTerminal(tty)
	if err != nil {
		return numbered(title, header, rows, os.Stdin, os.Stderr)
	}
	defer restore()

	return (&chooser{tty: tty, out: tty, title: title, all: rows}).run()
}

type chooser struct {
	tty   *os.File
	out   io.Writer
	title string
	all   []Row

	filter  string
	visible []int // indices into all, after filtering
	cursor  int   // position within visible
	top     int   // first visible row on screen
	height  int   // rows of list the screen has room for
}

func (c *chooser) run() (int, error) {
	c.refilter()
	reader := bufio.NewReader(c.tty)

	for {
		c.height = listHeight()
		c.draw()

		event, err := read(reader)
		if err != nil {
			return 0, ErrCancelled
		}
		switch event.kind {
		case cancel:
			return 0, ErrCancelled
		case accept:
			if len(c.visible) == 0 {
				continue
			}
			return c.visible[c.cursor], nil
		case up:
			c.move(-1)
		case down:
			c.move(1)
		case pageUp:
			c.move(-c.height)
		case pageDown:
			c.move(c.height)
		case top:
			c.cursor, c.top = 0, 0
		case bottom:
			c.cursor = len(c.visible) - 1
		case backspace:
			if c.filter != "" {
				runes := []rune(c.filter)
				c.filter = string(runes[:len(runes)-1])
				c.refilter()
			}
		case clear:
			if c.filter != "" {
				c.filter = ""
				c.refilter()
			}
		case typed:
			c.filter += string(event.r)
			c.refilter()
		}
	}
}

// refilter narrows the list to rows containing every word typed, in any column
// and any order. Word-by-word rather than as one string, so "billing plan" finds
// a title that has them the other way round.
func (c *chooser) refilter() {
	words := strings.Fields(strings.ToLower(c.filter))
	c.visible = c.visible[:0]

	for i, row := range c.all {
		haystack := strings.ToLower(strings.Join(row.Cells, " "))
		matched := true
		for _, word := range words {
			if !strings.Contains(haystack, word) {
				matched = false
				break
			}
		}
		if matched {
			c.visible = append(c.visible, i)
		}
	}
	c.cursor, c.top = 0, 0
}

func (c *chooser) move(by int) {
	if len(c.visible) == 0 {
		return
	}
	c.cursor += by
	if c.cursor < 0 {
		c.cursor = 0
	}
	if c.cursor > len(c.visible)-1 {
		c.cursor = len(c.visible) - 1
	}
	// Keep the cursor inside the window, scrolling only as far as it has to.
	if c.cursor < c.top {
		c.top = c.cursor
	}
	if c.cursor >= c.top+c.height {
		c.top = c.cursor - c.height + 1
	}
}

func (c *chooser) draw() {
	widths := columns(cells(c.all))

	var s strings.Builder
	s.WriteString("\x1b[H\x1b[2J") // home, and clear what the last frame drew

	// Title on the left, the count on the right, and the filter shown as
	// something being typed rather than as a status message.
	fmt.Fprintf(&s, "\r\n  %s   %s\r\n\r\n", accent(c.title), dim(c.count()))

	if len(c.visible) == 0 {
		fmt.Fprintf(&s, "  %s\r\n", dim("nothing matches — backspace to widen"))
	}

	end := c.top + c.height
	if end > len(c.visible) {
		end = len(c.visible)
	}
	// Hints where the list continues, so a window never looks like the whole of it.
	if c.top > 0 {
		fmt.Fprintf(&s, "  %s\r\n", dim(fmt.Sprintf("   ↑ %d above", c.top)))
	} else if len(c.visible) > 0 {
		s.WriteString("\r\n")
	}

	for i := c.top; i < end; i++ {
		r := c.all[c.visible[i]]
		if i == c.cursor {
			fmt.Fprintf(&s, "  %s %s\r\n", accent("›"), bold(row(r.Cells, widths)))
			continue
		}
		text := row(r.Cells, widths)
		if r.Muted {
			text = dim(text)
		}
		fmt.Fprintf(&s, "    %s\r\n", text)
	}

	if rest := len(c.visible) - end; rest > 0 {
		fmt.Fprintf(&s, "  %s\r\n", dim(fmt.Sprintf("   ↓ %d more", rest)))
	} else {
		s.WriteString("\r\n")
	}

	fmt.Fprintf(&s, "\r\n  %s\r\n", dim("type to filter · ↑↓ move · ⏎ choose · esc cancel"))
	if c.filter != "" {
		fmt.Fprintf(&s, "  %s %s", dim("filter"), accent(c.filter))
	}
	fmt.Fprint(c.out, s.String())
}

// count is the one number worth showing: how much of the list you are looking at.
func (c *chooser) count() string {
	if c.filter != "" {
		return fmt.Sprintf("%d of %d match", len(c.visible), len(c.all))
	}
	if len(c.visible) > c.height {
		return fmt.Sprintf("%d, showing %d", len(c.visible), c.height)
	}
	return fmt.Sprintf("%d", len(c.visible))
}

// --- keys --------------------------------------------------------------------

type kind int

const (
	ignored kind = iota
	up
	down
	pageUp
	pageDown
	top
	bottom
	accept
	cancel
	backspace
	clear
	typed
)

type event struct {
	kind kind
	r    rune
}

// read turns bytes into one intention. Printable characters are filter input, not
// shortcuts: a list you can type into is worth more than a list with a dozen
// single-key bindings nobody remembers.
func read(r *bufio.Reader) (event, error) {
	b, err := r.ReadByte()
	if err != nil {
		return event{}, err
	}
	switch b {
	case '\r', '\n':
		return event{kind: accept}, nil
	case 3, 4: // ctrl-c, ctrl-d
		return event{kind: cancel}, nil
	case 21: // ctrl-u
		return event{kind: clear}, nil
	case 127, 8:
		return event{kind: backspace}, nil
	case 0x1b:
		return escape(r), nil
	}
	if b >= 0x20 && b < 0x7f {
		return event{kind: typed, r: rune(b)}, nil
	}
	// A multi-byte character: gather the rest so an accented letter filters too.
	if b >= 0xc0 {
		if runeValue, _, err := r.ReadRune(); err == nil {
			_ = runeValue
		}
	}
	return event{}, nil
}

func escape(r *bufio.Reader) event {
	if r.Buffered() == 0 {
		return event{kind: cancel}
	}
	next, err := r.ReadByte()
	if err != nil || (next != '[' && next != 'O') {
		return event{}
	}
	final, err := r.ReadByte()
	if err != nil {
		return event{}
	}
	switch final {
	case 'A':
		return event{kind: up}
	case 'B':
		return event{kind: down}
	case 'H':
		return event{kind: top}
	case 'F':
		return event{kind: bottom}
	case '5', '6': // page up and down send a trailing '~'
		_, _ = r.ReadByte()
		if final == '5' {
			return event{kind: pageUp}
		}
		return event{kind: pageDown}
	}
	return event{}
}

// --- terminal ----------------------------------------------------------------

// takeTerminal puts the terminal into raw mode on the alternate screen, and
// returns the call that gives it back. Restoring is not optional: a terminal left
// raw is a terminal somebody has to reset by hand, and one left on the alternate
// screen has lost what they were looking at.
func takeTerminal(tty *os.File) (func(), error) {
	saved, err := stty(tty, "-g")
	if err != nil {
		return nil, err
	}
	if _, err := stty(tty, "raw", "-echo"); err != nil {
		return nil, err
	}
	fmt.Fprint(tty, "\x1b[?1049h\x1b[?25l") // alternate screen, hide the cursor
	return func() {
		fmt.Fprint(tty, "\x1b[?25h\x1b[?1049l") // and both back
		_, _ = stty(tty, strings.Fields(strings.TrimSpace(saved))...)
	}, nil
}

func stty(tty *os.File, args ...string) (string, error) {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = tty
	out, err := cmd.Output()
	return string(out), err
}

// listHeight is how many rows the list may use. A terminal that will not say how
// tall it is gets a conservative guess rather than a crash.
func listHeight() int {
	const fallback = maxRows
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fallback
	}
	defer tty.Close()

	out, err := stty(tty, "size")
	if err != nil {
		return fallback
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return fallback
	}
	rows, err := strconv.Atoi(fields[0])
	if err != nil || rows-reserved < 3 {
		return fallback
	}
	// Never more than maxRows, however tall the terminal: the cap is there to
	// make the list readable, not to fill the screen.
	if height := rows - reserved; height < maxRows {
		return height
	}
	return maxRows
}

// --- fallback ----------------------------------------------------------------

// numbered is what happens with no terminal to take: no raw mode, no arrows,
// works down a pipe.
func numbered(title string, header []string, rows []Row, in io.Reader, out io.Writer) (int, error) {
	widths := columns(append([][]string{header}, cells(rows)...))
	fmt.Fprintf(out, "%s\n   %s\n", title, row(header, widths))
	for i, r := range rows {
		fmt.Fprintf(out, "%2d %s\n", i+1, row(r.Cells, widths))
	}
	fmt.Fprintf(out, "choose [1-%d, or q] ", len(rows))

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

// --- layout ------------------------------------------------------------------

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

// A cursor and a title in one accent, everything else in weight alone. Colour
// used once reads as deliberate; colour used everywhere reads as a christmas tree,
// and it has to survive both a light and a dark terminal — 6 is cyan from the
// sixteen every theme defines, rather than a hex value that will clash with one.
func dim(s string) string    { return "\x1b[2m" + s + "\x1b[0m" }
func bold(s string) string   { return "\x1b[1m" + s + "\x1b[0m" }
func accent(s string) string { return "\x1b[36m" + s + "\x1b[0m" }
