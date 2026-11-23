// Command gen builds the per-version column references in ../data from the
// official PostgreSQL documentation sources (monitoring.sgml and
// pgstatstatements.sgml on each REL_<n>_STABLE branch).
//
// Run via `go generate ./internal/knowledge` (needs network access). The output
// is committed so normal builds stay offline and reproducible.
package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Versions to generate. Add the next major here when it is released.
var versions = []int{12, 13, 14, 15, 16, 17, 18, 19}

var docFiles = []string{"monitoring.sgml", "pgstatstatements.sgml"}

const rawURL = "https://raw.githubusercontent.com/postgres/postgres/REL_%d_STABLE/doc/src/sgml/%s"

// Column is one documented column.
type Column struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
	Desc string `json:"desc"`
}

// Reference is the generated file for one major version.
type Reference struct {
	Major  int                 `json:"major"`
	Source string              `json:"source"`
	Views  map[string][]Column `json:"views"`
}

var (
	reTable   = regexp.MustCompile(`(?s)<table id="([a-z0-9-]+)"[^>]*>(.*?)</table>`)
	reNewCell = regexp.MustCompile(`(?s)<entry role="catalog_table_entry">(.*?)</entry>`)
	reOldRow  = regexp.MustCompile(`(?s)<row>(.*?)</row>`)
	reEntry   = regexp.MustCompile(`(?s)<entry[^>]*>(.*?)</entry>`)
	reField   = regexp.MustCompile(`<structfield>([^<]+)</structfield>`)
	reType    = regexp.MustCompile(`<type>([^<]+)</type>`)
	rePara    = regexp.MustCompile(`(?s)<para>(.*?)</para>`)
	reTag     = regexp.MustCompile(`<[^>]+>`)
	reSpace   = regexp.MustCompile(`\s+`)
	reSentEnd = regexp.MustCompile(`\.\s`)
)

func main() {
	outDir := "data"
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fail(err)
	}
	client := &http.Client{Timeout: 60 * time.Second}

	for _, v := range versions {
		ref := Reference{
			Major:  v,
			Source: fmt.Sprintf("postgres/postgres REL_%d_STABLE: doc/src/sgml/{%s}", v, strings.Join(docFiles, ",")),
			Views:  map[string][]Column{},
		}
		for _, f := range docFiles {
			src, err := fetch(client, fmt.Sprintf(rawURL, v, f))
			if err != nil {
				fail(fmt.Errorf("PG %d %s: %w", v, f, err))
			}
			for view, cols := range parse(src) {
				ref.Views[view] = cols
			}
		}
		if len(ref.Views) == 0 {
			fail(fmt.Errorf("PG %d: no views parsed — doc markup changed?", v))
		}
		b, err := json.MarshalIndent(ref, "", " ")
		if err != nil {
			fail(err)
		}
		path := filepath.Join(outDir, fmt.Sprintf("pg%d.json", v))
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			fail(err)
		}
		n := 0
		for _, c := range ref.Views {
			n += len(c)
		}
		fmt.Printf("PG %d: %d views, %d columns -> %s\n", v, len(ref.Views), n, path)
	}
}

// parse extracts column tables for statistics views. It understands both the
// PG 13+ markup (<entry role="catalog_table_entry">) and the older
// three-column <row><entry> layout used by PG 12.
func parse(src string) map[string][]Column {
	out := map[string][]Column{}
	for _, m := range reTable.FindAllStringSubmatch(src, -1) {
		view := viewName(m[1])
		if view == "" {
			continue
		}
		body := m[2]
		var cols []Column
		if cells := reNewCell.FindAllStringSubmatch(body, -1); len(cells) > 0 {
			for _, c := range cells {
				name := reField.FindStringSubmatch(c[1])
				if name == nil {
					continue
				}
				col := Column{Name: name[1]}
				if t := reType.FindStringSubmatch(c[1]); t != nil {
					col.Type = t[1]
				}
				if paras := rePara.FindAllStringSubmatch(c[1], -1); len(paras) > 0 {
					col.Desc = firstSentence(paras[len(paras)-1][1])
				}
				cols = append(cols, col)
			}
		} else {
			for _, r := range reOldRow.FindAllStringSubmatch(body, -1) {
				entries := reEntry.FindAllStringSubmatch(r[1], -1)
				if len(entries) < 3 {
					continue
				}
				name := reField.FindStringSubmatch(entries[0][1])
				if name == nil {
					continue
				}
				col := Column{Name: name[1], Desc: firstSentence(entries[len(entries)-1][1])}
				if t := reType.FindStringSubmatch(entries[1][1]); t != nil {
					col.Type = t[1]
				}
				cols = append(cols, col)
			}
		}
		if len(cols) > 0 {
			out[view] = cols
		}
	}
	return out
}

// viewName maps a doc table id to the view it documents.
func viewName(id string) string {
	switch {
	case id == "pgstatstatements-columns":
		return "pg_stat_statements"
	case strings.HasPrefix(id, "pg-stat") && strings.HasSuffix(id, "-view"):
		return strings.ReplaceAll(strings.TrimSuffix(id, "-view"), "-", "_")
	}
	return ""
}

func firstSentence(s string) string {
	s = reTag.ReplaceAllString(s, "")
	s = html.UnescapeString(reSpace.ReplaceAllString(s, " "))
	s = strings.TrimSpace(s)
	if loc := reSentEnd.FindStringIndex(s); loc != nil {
		s = s[:loc[0]+1]
	}
	if r := []rune(s); len(r) > 220 {
		s = string(r[:217]) + "..."
	}
	return s
}

func fetch(c *http.Client, url string) (string, error) {
	resp, err := c.Get(url)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen:", err)
	os.Exit(1)
}
