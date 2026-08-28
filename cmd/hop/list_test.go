package main

import (
	"strings"
	"testing"
)

// A listing that shows a page must say a page is what it showed. Saying nothing
// reads as "this is all of them", which is the failure a ceiling introduces if
// nobody mentions it.
func TestListingSaysWhenItIsShowingAPage(t *testing.T) {
	for _, c := range []struct {
		name              string
		shown, total      int
		match, wantPhrase string
	}{
		{"a page of many", 20, 60, "", "20 of 60"},
		{"all of them", 60, 60, "", "60 sessions."},
		{"a page of matches", 5, 40, "billing", `5 of 40 matching "billing"`},
		{"all the matches", 3, 3, "billing", `3 sessions matching "billing"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := tally(c.shown, c.total, c.match)
			if !strings.Contains(got, c.wantPhrase) {
				t.Errorf("tally(%d, %d, %q) = %q, want it to contain %q",
					c.shown, c.total, c.match, got, c.wantPhrase)
			}
			hidden := c.total - c.shown
			if hidden > 0 && !strings.Contains(got, "of") {
				t.Errorf("%d sessions were hidden and the line does not say so: %q", hidden, got)
			}
		})
	}
}
