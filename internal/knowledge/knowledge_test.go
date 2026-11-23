package knowledge

import (
	"strings"
	"testing"

	"github.com/sjtly16/pgmonk/internal/model"
)

func TestEveryMajorLoads(t *testing.T) {
	majors := Majors()
	if len(majors) < 8 || majors[0] != 12 {
		t.Fatalf("expected PG 12.. references, got %v", majors)
	}
	for _, m := range majors {
		r, err := Load(m)
		if err != nil {
			t.Fatalf("PG %d: %v", m, err)
		}
		for _, v := range []string{"pg_stat_activity", "pg_stat_database", "pg_stat_all_tables", "pg_stat_statements"} {
			if len(r.Views[v]) == 0 {
				t.Errorf("PG %d: %s missing from reference", m, v)
			}
		}
	}
}

func TestForServer(t *testing.T) {
	majors := Majors()
	newest := majors[len(majors)-1]
	cases := []struct {
		major     int
		wantMajor int
		wantExact bool
	}{
		{17, 17, true},
		{newest + 1, newest, false}, // future server: nearest older pack
		{11, 12, false},             // unsupported old server: oldest pack
	}
	for _, c := range cases {
		r, exact, err := ForServer(c.major)
		if err != nil {
			t.Fatal(err)
		}
		if r.Major != c.wantMajor || exact != c.wantExact {
			t.Errorf("ForServer(%d) = PG %d exact=%v, want PG %d exact=%v", c.major, r.Major, exact, c.wantMajor, c.wantExact)
		}
	}
}

// TestNotesMatchReference guards the hand-written notes: every `view.column`
// a note mentions must exist (or, with ~, must not exist) in the generated
// documentation reference for every version the note claims to cover.
func TestNotesMatchReference(t *testing.T) {
	notes, err := Notes()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) == 0 {
		t.Fatal("no notes parsed")
	}
	refs := map[int]*Reference{}
	for _, m := range Majors() {
		refs[m], _ = Load(m)
	}
	for _, n := range notes {
		matches := reColRef.FindAllStringSubmatch(n.Text, -1)
		for m, ref := range refs {
			if !n.Applies(m) {
				continue
			}
			for _, cr := range matches {
				negated, view, col := cr[1] == "~", cr[2], cr[3]
				if got := ref.Has(view, col); got == negated {
					want := "exist"
					if negated {
						want = "NOT exist"
					}
					t.Errorf("PG %d: note [%d-%d] says %s.%s should %s\n  note: %s", m, n.From, n.To, view, col, want, n.Text)
				}
			}
		}
	}
}

func TestRenderStripsNegation(t *testing.T) {
	n := Note{Text: "gone: `~pg_stat_wal.wal_write`, kept `pg_stat_wal.wal_bytes`"}
	if got := n.render(); got != "gone: `pg_stat_wal.wal_write`, kept `pg_stat_wal.wal_bytes`" {
		t.Errorf("render = %q", got)
	}
}

func TestDescribeKeysExist(t *testing.T) {
	refs := map[int]*Reference{}
	for _, m := range Majors() {
		refs[m], _ = Load(m)
	}
	for key := range describe {
		view, col, _ := strings.Cut(key, ".")
		found := false
		for _, r := range refs {
			for _, c := range r.Views[view] {
				if c.Name == col {
					found = true
					if c.Desc == "" || strings.Contains(c.Desc, ": This state") {
						t.Errorf("%s: unusable description %q (PG %d)", key, c.Desc, r.Major)
					}
				}
			}
		}
		if !found {
			t.Errorf("describe key %s not documented in any version (typo?)", key)
		}
	}
}

func TestBuildPackIsDeterministic(t *testing.T) {
	info := model.ServerInfo{VersionNum: 170004}
	a, err := BuildPack(info)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		b, _ := BuildPack(info)
		if a.Text != b.Text {
			t.Fatal("pack text differs between builds; this breaks LLM prompt caching")
		}
	}
	if !a.Exact || a.RefMajor != 17 {
		t.Errorf("want exact PG 17 reference, got %d exact=%v", a.RefMajor, a.Exact)
	}
}

func TestBuildPackUsesLiveCatalog(t *testing.T) {
	cat := model.Catalog{}
	for _, c := range []string{"pid", "state", "query", "fork_only_column"} {
		cat.Add("pg_stat_activity", c)
	}
	cat.Add("pg_stat_wal", "wal_bytes")
	p, err := BuildPack(model.ServerInfo{VersionNum: 180000, Catalog: cat})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"pg_stat_activity: pid, state, query, fork_only_column", "pg_stat_wal: wal_bytes"} {
		if !strings.Contains(p.Text, want) {
			t.Errorf("pack missing %q", want)
		}
	}
	for _, absent := range []string{"pg_stat_database:", "leader_pid"} {
		if strings.Contains(p.Text[strings.Index(p.Text, "### Statistics"):], absent) {
			t.Errorf("pack lists %q which is not in the live catalog", absent)
		}
	}
}

func TestBuildPackFutureVersion(t *testing.T) {
	majors := Majors()
	p, err := BuildPack(model.ServerInfo{VersionNum: (majors[len(majors)-1] + 1) * 10000})
	if err != nil {
		t.Fatal(err)
	}
	if p.Exact || !strings.Contains(p.Text, "No reference ships for PostgreSQL") {
		t.Error("future server should be flagged as using an older reference")
	}
}
