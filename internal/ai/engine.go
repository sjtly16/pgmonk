// Package ai produces AI insights for a snapshot using a local LLM, grounded
// in version-specific knowledge of the connected server.
package ai

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sjtly16/pgmonk/internal/analyzer"
	"github.com/sjtly16/pgmonk/internal/knowledge"
	"github.com/sjtly16/pgmonk/internal/model"
)

// Verifier checks that a suggested SELECT is valid on the server without
// executing it (see collector.ExplainReadOnly). It returns notReadOnly for
// statements it refuses to check.
type Verifier func(ctx context.Context, sql string) error

// Engine holds the per-connection state: provider, knowledge pack and the
// stable system prompt built from it.
type Engine struct {
	provider    Provider
	pack        *knowledge.Pack
	system      string
	verify      Verifier
	notReadOnly error
}

// NewEngine prepares an engine for one server. notReadOnly is the sentinel the
// verifier returns for statements it won't plan (may be nil).
func NewEngine(p Provider, info model.ServerInfo, verify Verifier, notReadOnly error) (*Engine, error) {
	pack, err := knowledge.BuildPack(info)
	if err != nil {
		return nil, err
	}
	return &Engine{
		provider:    p,
		pack:        pack,
		system:      SystemPrompt(pack),
		verify:      verify,
		notReadOnly: notReadOnly,
	}, nil
}

// Pack returns the knowledge pack in use.
func (e *Engine) Pack() *knowledge.Pack { return e.pack }

// ModelName returns the provider/model label.
func (e *Engine) ModelName() string { return e.provider.Name() }

// Result is one AI analysis.
type Result struct {
	Response   *Response // nil when the model's output couldn't be parsed
	Raw        string    // model output, shown when parsing fails
	ParseError error
	Usage      Usage
	Elapsed    time.Duration
	Model      string
	At         time.Time
}

// Analyze runs the rule engine, sends the digest to the model and validates
// what comes back against the live server.
func (e *Engine) Analyze(ctx context.Context, snap *model.Snapshot) (*Result, error) {
	if snap == nil {
		return nil, errors.New("no snapshot yet")
	}
	findings := analyzer.Analyze(snap)
	user := UserPrompt(Digest(snap, findings))

	start := time.Now()
	text, usage, err := e.provider.Complete(ctx, e.system, user)
	if err != nil {
		return nil, err
	}
	res := &Result{Raw: text, Usage: usage, Elapsed: time.Since(start), Model: e.provider.Name(), At: time.Now()}

	resp, err := parseResponse(text)
	if err != nil {
		res.ParseError = fmt.Errorf("model did not return valid JSON: %w", err)
		return res, nil
	}
	res.Response = resp
	e.validate(ctx, resp)
	addRestartNotes(resp, snap.Settings)
	return res, nil
}

var reColumnRef = regexp.MustCompile(`\b(pg_stat_[a-z_]+|pg_statio_[a-z_]+|pg_locks)\.([a-z_]+)\b`)

// UnknownColumns lists view.column references in text that don't exist in the
// server catalog.
func UnknownColumns(text string, cat model.Catalog) []string {
	if cat == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range reColumnRef.FindAllStringSubmatch(text, -1) {
		view, col := m[1], m[2]
		if cat.HasView(view) && cat.Has(view, col) {
			continue
		}
		ref := view + "." + col
		if !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	sort.Strings(out)
	return out
}

var reSettingName = regexp.MustCompile(`\b[a-z][a-z0-9_]*(?:\.[a-z0-9_]+)?\b`)

// addRestartNotes corrects a common small-model mistake: suggesting
// pg_reload_conf() for settings that only change after a server restart
// (pg_settings.context = 'postmaster').
func addRestartNotes(r *Response, settings []model.Setting) {
	restart := map[string]bool{}
	for _, s := range settings {
		if s.Context == "postmaster" {
			restart[s.Name] = true
		}
	}
	for i := range r.Findings {
		f := &r.Findings[i]
		text := strings.ToLower(f.Action + "\n" + f.SQL)
		if !strings.Contains(text, "pg_reload_conf") {
			continue
		}
		seen := map[string]bool{}
		for _, name := range reSettingName.FindAllString(text, -1) {
			if restart[name] && !seen[name] {
				seen[name] = true
				f.Notes = append(f.Notes, fmt.Sprintf("%s only takes effect after a server restart; pg_reload_conf() will not apply it.", name))
			}
		}
	}
}

func (e *Engine) validate(ctx context.Context, r *Response) {
	for i := range r.Findings {
		f := &r.Findings[i]
		all := strings.Join([]string{f.Title, f.Evidence, f.RootCause, f.Action, f.SQL}, "\n")
		f.Unknown = UnknownColumns(all, e.pack.Catalog)

		if f.SQL == "" || e.verify == nil {
			continue
		}
		switch err := e.verify(ctx, f.SQL); {
		case err == nil:
			f.SQLVerified = true
		case e.notReadOnly != nil && errors.Is(err, e.notReadOnly):
			f.SQLIsFix = true
		default:
			f.SQLError = err.Error()
		}
	}
}
