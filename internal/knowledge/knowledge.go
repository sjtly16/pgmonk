// Package knowledge provides version-specific PostgreSQL statistics-view
// knowledge used to ground AI insights.
//
// Two sources are embedded in the binary:
//   - data/pg<N>.json: column references generated from each release's
//     official documentation (see gen/), and
//   - notes.md: short, hand-curated notes on what changed between versions,
//     each column reference in it verified by tests.
//
// At runtime Pack combines them with the live server catalog so the model
// only sees columns that actually exist on the connected server.
package knowledge

//go:generate go run ./gen data

import (
	"embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

//go:embed data/*.json notes.md
var files embed.FS

// Column is one documented column.
type Column struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
	Desc string `json:"desc"`
}

// Reference is the documented column set for one major version.
type Reference struct {
	Major  int                 `json:"major"`
	Source string              `json:"source"`
	Views  map[string][]Column `json:"views"`
}

// Has reports whether view.column is documented in this reference.
func (r *Reference) Has(view, column string) bool {
	for _, c := range r.Views[docView(view)] {
		if c.Name == column {
			return true
		}
	}
	return false
}

// Majors lists the major versions with an embedded reference, ascending.
func Majors() []int {
	entries, _ := files.ReadDir("data")
	var out []int
	for _, e := range entries {
		var v int
		if _, err := fmt.Sscanf(e.Name(), "pg%d.json", &v); err == nil {
			out = append(out, v)
		}
	}
	sort.Ints(out)
	return out
}

// Load returns the embedded reference for exactly this major version.
func Load(major int) (*Reference, error) {
	b, err := files.ReadFile(fmt.Sprintf("data/pg%d.json", major))
	if err != nil {
		return nil, fmt.Errorf("no knowledge reference for PostgreSQL %d", major)
	}
	var r Reference
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ForServer picks the reference to use for a server major version: the exact
// one if embedded, otherwise the nearest older one (or the oldest available
// for servers older than anything we ship). exact reports whether it matched.
func ForServer(major int) (ref *Reference, exact bool, err error) {
	majors := Majors()
	if len(majors) == 0 {
		return nil, false, fmt.Errorf("no knowledge references embedded")
	}
	pick := majors[0]
	for _, m := range majors {
		if m <= major {
			pick = m
		}
	}
	ref, err = Load(pick)
	return ref, pick == major, err
}

// Note is one curated version note.
type Note struct {
	From, To int // inclusive; To == 0 means "and later"
	Text     string
}

// Applies reports whether the note covers the given major version.
func (n Note) Applies(major int) bool {
	return major >= n.From && (n.To == 0 || major <= n.To)
}

var reNote = regexp.MustCompile(`^\[(\d+)-(\d*)\]\s+(.+)$`)

// Notes returns all curated notes.
func Notes() ([]Note, error) {
	b, err := files.ReadFile("notes.md")
	if err != nil {
		return nil, err
	}
	var out []Note
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := reNote.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("notes.md:%d: malformed note %q", i+1, line)
		}
		from, _ := strconv.Atoi(m[1])
		to := 0
		if m[2] != "" {
			to, _ = strconv.Atoi(m[2])
		}
		out = append(out, Note{From: from, To: to, Text: m[3]})
	}
	return out, nil
}

// reColRef matches `view.column` or `~view.column` references in notes.
var reColRef = regexp.MustCompile("`(~?)(pg_[a-z_]+)\\.([a-z_]+)`")

// render strips note markup for the model: backticks stay (they help the
// model treat names as identifiers), the ~ negation marker goes.
func (n Note) render() string {
	return reColRef.ReplaceAllString(n.Text, "`$2.$3`")
}

// docView maps a view to the one whose columns the docs describe:
// pg_stat_user_tables shares pg_stat_all_tables' columns, etc.
func docView(view string) string {
	for _, p := range []string{"_user_", "_sys_"} {
		if strings.Contains(view, p) {
			return strings.Replace(view, p, "_all_", 1)
		}
	}
	return view
}
