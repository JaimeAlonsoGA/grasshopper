package pick

import (
	"bufio"
	"strings"
	"testing"
)

func sample() *chooser {
	return &chooser{
		title: "choose",
		all: []Row{
			{Cells: []string{"aaaa", "Billing resolver"}},
			{Cells: []string{"bbbb", "Monorepo cleanup"}, Muted: true},
			{Cells: []string{"cccc", "Plan for the pilot"}, Muted: true},
			{Cells: []string{"dddd", "Billing rewrite"}},
		},
		height: 2,
	}
}

// Typing narrows the list. Word by word and in any order, so somebody who
// remembers two words does not have to remember which came first.
func TestFilter(t *testing.T) {
	cases := []struct {
		filter string
		want   int
	}{
		{"", 4},
		{"billing", 2},
		{"BILLING", 2},
		{"resolver billing", 1},
		{"bbbb", 1},
		{"nothing like this", 0},
	}
	for _, c := range cases {
		ch := sample()
		ch.filter = c.filter
		ch.refilter()
		if len(ch.visible) != c.want {
			t.Errorf("filter %q matched %d, want %d", c.filter, len(ch.visible), c.want)
		}
	}
}

func TestFilterResetsTheCursor(t *testing.T) {
	ch := sample()
	ch.refilter()
	ch.move(3)
	if ch.cursor == 0 {
		t.Fatal("cursor did not move")
	}
	ch.filter = "billing"
	ch.refilter()
	// Otherwise the cursor points past the end of a list that just got shorter.
	if ch.cursor != 0 || ch.top != 0 {
		t.Errorf("cursor = %d, top = %d after filtering", ch.cursor, ch.top)
	}
}

// The window scrolls only as far as it must, so a long list does not jump.
func TestViewportFollowsTheCursor(t *testing.T) {
	ch := sample()
	ch.refilter()

	ch.move(1)
	if ch.top != 0 {
		t.Errorf("top = %d, want the window to stay put while the cursor is inside it", ch.top)
	}
	ch.move(1)
	if ch.top != 1 {
		t.Errorf("top = %d, want the window to follow by one", ch.top)
	}
	ch.move(-10)
	if ch.cursor != 0 || ch.top != 0 {
		t.Errorf("cursor = %d, top = %d after going up past the start", ch.cursor, ch.top)
	}
	ch.move(100)
	if ch.cursor != len(ch.all)-1 {
		t.Errorf("cursor = %d, want the last row", ch.cursor)
	}
}

func TestMoveOnAnEmptyListDoesNothing(t *testing.T) {
	ch := sample()
	ch.filter = "nothing"
	ch.refilter()
	ch.move(1)
	if ch.cursor != 0 {
		t.Errorf("cursor = %d on an empty list", ch.cursor)
	}
}

func TestKeys(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want kind
		r    rune
	}{
		{"return", "\r", accept, 0},
		{"newline", "\n", accept, 0},
		{"ctrl-c", "\x03", cancel, 0},
		{"lone escape", "\x1b", cancel, 0},
		{"up", "\x1b[A", up, 0},
		{"down", "\x1b[B", down, 0},
		{"home", "\x1b[H", top, 0},
		{"end", "\x1b[F", bottom, 0},
		{"page up", "\x1b[5~", pageUp, 0},
		{"page down", "\x1b[6~", pageDown, 0},
		{"backspace", "\x7f", backspace, 0},
		{"ctrl-u", "\x15", clear, 0},
		// A printable character is filter input, not a shortcut. A list you can
		// type into beats a list with a dozen bindings nobody remembers.
		{"letter", "b", typed, 'b'},
		{"space", " ", typed, ' '},
		{"digit", "7", typed, '7'},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := read(bufio.NewReader(strings.NewReader(c.in)))
			if err != nil {
				t.Fatal(err)
			}
			if got.kind != c.want || got.r != c.r {
				t.Errorf("read(%q) = %v/%q, want %v/%q", c.in, got.kind, got.r, c.want, c.r)
			}
		})
	}
}

// Escape alone cancels, but escape followed by a bracket is an arrow. Reading it
// as a cancel would make every arrow press quit.
func TestEscapeSequenceIsNotACancel(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("\x1b[A"))
	got, err := read(r)
	if err != nil {
		t.Fatal(err)
	}
	if got.kind != up {
		t.Errorf("got %v, want up", got.kind)
	}
}

func TestNumberedFallback(t *testing.T) {
	rows := sample().all
	var out strings.Builder

	i, err := numbered("choose", []string{"ID", "TITLE"}, rows, strings.NewReader("3\n"), &out)
	if err != nil || i != 2 {
		t.Errorf("got %d, %v", i, err)
	}
	if !strings.Contains(out.String(), "Plan for the pilot") {
		t.Errorf("the list was not printed:\n%s", out.String())
	}

	for _, answer := range []string{"\n", "q\n"} {
		if _, err := numbered("choose", nil, rows, strings.NewReader(answer), &strings.Builder{}); err != ErrCancelled {
			t.Errorf("answer %q gave %v, want ErrCancelled", answer, err)
		}
	}
	if _, err := numbered("choose", nil, rows, strings.NewReader("99\n"), &strings.Builder{}); err == nil {
		t.Error("an out-of-range answer should complain")
	}
}

// The frame is drawn on the alternate screen and never wider than the terminal
// row, so it leaves nothing in the scrollback and wraps nothing.
func TestDrawFitsTheViewport(t *testing.T) {
	ch := sample()
	ch.out = &strings.Builder{}
	ch.refilter()
	ch.draw()

	drawn := ch.out.(*strings.Builder).String()
	if !strings.HasPrefix(drawn, "\x1b[H\x1b[2J") {
		t.Error("did not start from a cleared frame")
	}
	// Two rows of list, because that is the height it was given.
	if n := strings.Count(drawn, "aaaa") + strings.Count(drawn, "bbbb") + strings.Count(drawn, "cccc"); n != 2 {
		t.Errorf("drew %d of the three candidate rows with height 2:\n%s", n, drawn)
	}
	if !strings.Contains(drawn, "type to filter") {
		t.Error("no hint about how to use it")
	}
}

func TestDrawSaysWhenNothingMatches(t *testing.T) {
	ch := sample()
	ch.out = &strings.Builder{}
	ch.filter = "zzz"
	ch.refilter()
	ch.draw()
	if !strings.Contains(ch.out.(*strings.Builder).String(), "nothing matches") {
		t.Error("an empty result looks like a bug unless it says so")
	}
}
