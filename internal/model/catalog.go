package model

import "sort"

// Catalog records which statistics views and columns actually exist on the
// connected server. It is read from pg_attribute at connect time, so it is
// exact for the server's minor version, for forks (Aurora, Neon, ...) and for
// the installed version of extensions such as pg_stat_statements — none of
// which a server_version_num comparison can tell you.
type Catalog map[string]map[string]bool

// Add records that view has column.
func (c Catalog) Add(view, column string) {
	cols, ok := c[view]
	if !ok {
		cols = map[string]bool{}
		c[view] = cols
	}
	cols[column] = true
}

// HasView reports whether the view exists.
func (c Catalog) HasView(view string) bool {
	_, ok := c[view]
	return ok
}

// Has reports whether view.column exists.
func (c Catalog) Has(view, column string) bool {
	return c[view][column]
}

// Columns returns the view's columns in sorted order.
func (c Catalog) Columns(view string) []string {
	cols := make([]string, 0, len(c[view]))
	for col := range c[view] {
		cols = append(cols, col)
	}
	sort.Strings(cols)
	return cols
}

// Views returns all view names in sorted order.
func (c Catalog) Views() []string {
	views := make([]string, 0, len(c))
	for v := range c {
		views = append(views, v)
	}
	sort.Strings(views)
	return views
}
