package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// writeTable measures columns in runes, not bytes. The em dash these tables use
// for "absent" is three bytes wide and one column wide, and counting its bytes is
// what makes a hand-aligned table crooked.
func writeTable(w io.Writer, rows [][]string) {
	var widths []int
	for _, row := range rows {
		for i, cell := range row {
			for len(widths) <= i {
				widths = append(widths, 0)
			}
			if n := utf8.RuneCountInString(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}
	for _, row := range rows {
		var line strings.Builder
		for i, cell := range row {
			line.WriteString(cell)
			if i < len(row)-1 {
				line.WriteString(strings.Repeat(" ", widths[i]-utf8.RuneCountInString(cell)+2))
			}
		}
		fmt.Fprintln(w, strings.TrimRight(line.String(), " "))
	}
}

func marshal(v any) ([]byte, error) { return json.Marshal(v) }
