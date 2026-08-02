package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteTable(t *testing.T) {
	tests := []struct {
		name   string
		header []string
		rows   [][]string
		want   []string // lines that must appear in order
	}{
		{
			name:   "empty",
			header: []string{"FORGE", "REPOSITORY"},
			rows:   nil,
			want:   []string{"FORGE  REPOSITORY"},
		},
		{
			name:   "aligned rows",
			header: []string{"FORGE", "REPOSITORY", "URL"},
			rows: [][]string{
				{"github", "alice/repo", "https://github.com/alice/repo"},
				{"gitlab", "alice/other", "https://gitlab.com/alice/other"},
			},
			want: []string{
				"FORGE   REPOSITORY   URL",
				"github  alice/repo   https://github.com/alice/repo",
				"gitlab  alice/other  https://gitlab.com/alice/other",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := writeTable(&buf, tt.header, tt.rows); err != nil {
				t.Fatalf("writeTable: %v", err)
			}
			lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
			if len(lines) != len(tt.want) {
				t.Fatalf("got %d lines, want %d: %q", len(lines), len(tt.want), lines)
			}
			for i, want := range tt.want {
				if lines[i] != want {
					t.Errorf("line %d = %q, want %q", i, lines[i], want)
				}
			}
		})
	}
}
