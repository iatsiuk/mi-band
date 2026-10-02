package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// output formats
const (
	formatMarkdown = "markdown"
	formatJSON     = "json"
)

// markdowner is a command result that renders itself as markdown; JSON uses encoding/json.
type markdowner interface {
	markdown(b *strings.Builder)
}

func validFormat(f string) bool {
	return f == formatMarkdown || f == formatJSON
}

func writeOutput(w io.Writer, format string, v markdowner) error {
	if format == formatJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	var b strings.Builder
	v.markdown(&b)
	_, err := io.WriteString(w, b.String())
	return err
}

// table writes a markdown table; numeric columns (right) are right-aligned.
type table struct {
	header []string
	right  map[int]bool
	rows   [][]string
}

func (t *table) add(cells ...string) {
	t.rows = append(t.rows, cells)
}

func (t *table) write(b *strings.Builder) {
	if len(t.rows) == 0 {
		b.WriteString("No data.\n")
		return
	}
	writeRow(b, t.header)
	sep := make([]string, len(t.header))
	for i := range sep {
		sep[i] = "---"
		if t.right[i] {
			sep[i] = "---:"
		}
	}
	b.WriteString("|" + strings.Join(sep, "|") + "|\n")
	for _, r := range t.rows {
		writeRow(b, r)
	}
}

func writeRow(b *strings.Builder, cells []string) {
	b.WriteString("|")
	for _, c := range cells {
		b.WriteString(" " + cellEscaper.Replace(c) + " |")
	}
	b.WriteString("\n")
}

var cellEscaper = strings.NewReplacer("|", `\|`, "\n", " ")

func heading(b *strings.Builder, title string) {
	b.WriteString("\n## " + title + "\n\n")
}

// localTime shows t in the time zone the record was written in.
func localTime(t time.Time, zoneOffset int) string {
	return t.In(time.FixedZone("", zoneOffset)).Format("2006-01-02 15:04")
}

func optInt(v *int) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(*v)
}

func duration(s int) string {
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func code(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	return "`" + strings.ReplaceAll(string(raw), "`", "'") + "`"
}
