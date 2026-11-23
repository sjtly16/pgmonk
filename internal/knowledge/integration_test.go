//go:build integration

package knowledge

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sjtly16/pgmonk/internal/collector"
)

// TestReferenceMatchesLiveCatalog checks the generated documentation
// reference against a real server of the same major version: every documented
// column of a pack view must exist on the server. Columns that exist but are
// undocumented are logged (the pack still lists them, by name).
func TestReferenceMatchesLiveCatalog(t *testing.T) {
	dsn := os.Getenv("PGMONK_TEST_DSN")
	if dsn == "" {
		t.Skip("PGMONK_TEST_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	coll, err := collector.New(ctx, dsn, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer coll.Close()
	info := coll.ServerInfo()

	ref, exact, err := ForServer(info.VersionNum / 10000)
	if err != nil {
		t.Fatal(err)
	}
	if !exact {
		t.Skipf("no exact reference for server %d", info.VersionNum)
	}

	for _, view := range packViews {
		if !info.Catalog.HasView(view) {
			continue
		}
		docCols := ref.Views[docView(view)]
		if len(docCols) == 0 {
			t.Logf("%s: present on server but not in reference", view)
			continue
		}
		for _, c := range docCols {
			if !info.Catalog.Has(view, c.Name) {
				t.Errorf("PG %d: docs list %s.%s but the server has no such column", ref.Major, view, c.Name)
			}
		}
		documented := map[string]bool{}
		for _, c := range docCols {
			documented[c.Name] = true
		}
		for _, col := range info.Catalog.Columns(view) {
			if !documented[col] {
				t.Logf("PG %d: %s.%s exists but is undocumented", ref.Major, view, col)
			}
		}
	}

	if _, err := BuildPack(info); err != nil {
		t.Fatal(err)
	}
}
