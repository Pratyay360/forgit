package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"charm.land/lipgloss/v2"
)

// forgeColors maps forge identifiers to their brand-ish colors.
var forgeColors = map[string]string{
	"github":    "#6cc644",
	"gitlab":    "#fc6d26",
	"forgejo":   "#4a9bdb",
	"sourcehut": "#8b7cf6",
	"bitbucket": "#2684ff",
}

// writeTable renders header and rows as an aligned table. When w is a terminal
// the table is styled with lipgloss; otherwise (pipes, tests) plain text is
// emitted so the output stays script-friendly.
func writeTable(w io.Writer, header []string, rows [][]string) error {
	if !isTerminal(w) {
		return writePlainTable(w, header, rows)
	}
	return writeStyledTable(w, header, rows)
}

// isTerminal reports whether w is a character device, i.e. an interactive
// terminal rather than a pipe or file.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func writePlainTable(w io.Writer, header []string, rows [][]string) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	writeTabRow(tw, header)
	for _, row := range rows {
		writeTabRow(tw, row)
	}
	return tw.Flush()
}

func writeTabRow(w io.Writer, cells []string) {
	for i, c := range cells {
		if i > 0 {
			fmt.Fprint(w, "\t")
		}
		fmt.Fprint(w, c)
	}
	fmt.Fprintln(w)
}

func writeStyledTable(w io.Writer, header []string, rows [][]string) error {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = lipgloss.Width(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if n := lipgloss.Width(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}

	var b strings.Builder

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
	for i, h := range header {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(headerStyle.Render(padCell(h, widths[i])))
	}
	b.WriteString("\n")

	total := 0
	for _, wd := range widths {
		total += wd + 2
	}
	b.WriteString(lipgloss.NewStyle().Faint(true).Render(strings.Repeat("─", total)))
	b.WriteString("\n")

	for _, row := range rows {
		for i, cell := range row {
			if i > 0 {
				b.WriteString(" ")
			}
			if i == 0 {
				if c, ok := forgeColors[strings.ToLower(cell)]; ok {
					cell = lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render(cell)
				}
			}
			b.WriteString(padCell(cell, widths[i]))
		}
		b.WriteString("\n")
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// padCell right-pads a cell to the given width plus a 2-space gutter.
func padCell(s string, width int) string {
	if n := lipgloss.Width(s); n < width {
		return s + strings.Repeat(" ", width-n+2)
	}
	return s + "  "
}
