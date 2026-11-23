package ai

import (
	"encoding/json"
	"strings"

	"github.com/sjtly16/pgmonk/internal/knowledge"
)

// instructions is the fixed part of the system prompt. Together with the
// knowledge pack it forms a prefix that is byte-identical across requests for
// the same server, so local LLM servers can reuse their KV cache for it.
const instructions = `You are an expert PostgreSQL DBA. You review a live performance snapshot taken by pg-monk and explain what matters.

Rules:
1. Use only facts from the snapshot. Quote the specific numbers that support each finding.
2. The knowledge section below is authoritative for this server's PostgreSQL version. Only reference statistics views and columns it lists. Never invent columns.
3. pg-monk's rule-based findings are already shown to the user. Do not just repeat them: connect them into root causes (for example, one idle-in-transaction session that blocks other sessions and also stops vacuum from cleaning dead tuples), rank what to fix first, and say why.
4. Each "sql" value must be a single read-only SELECT that helps confirm the finding, or an empty string. Put fixes (VACUUM, CREATE INDEX, ALTER SYSTEM, pg_terminate_backend) in "action" as text, never in "sql".
5. If nothing is wrong, say so in the summary and return an empty findings list. Do not invent problems.
6. At most 5 findings, most important first. Be concise.

Respond with JSON only, no prose and no code fences, exactly in this shape:
{"summary": "one or two sentences", "findings": [{"severity": "critical|warning|info", "title": "max 70 chars", "evidence": "numbers from the snapshot", "root_cause": "why it happens", "action": "what to do", "sql": "optional single SELECT"}]}
`

// SystemPrompt builds the stable system message for a server.
func SystemPrompt(pack *knowledge.Pack) string {
	var b strings.Builder
	b.WriteString(instructions)
	b.WriteString("\n")
	b.WriteString(pack.Text)
	return b.String()
}

// UserPrompt wraps the per-request digest.
func UserPrompt(digest string) string {
	return digest + "\nAnalyze this snapshot and respond with the JSON object only.\n"
}

// Finding is one AI insight.
type Finding struct {
	Severity  string `json:"severity"`
	Title     string `json:"title"`
	Evidence  string `json:"evidence"`
	RootCause string `json:"root_cause"`
	Action    string `json:"action"`
	SQL       string `json:"sql"`

	// Set by validation, not by the model.
	SQLVerified bool     `json:"-"`
	SQLError    string   `json:"-"`
	Unknown     []string `json:"-"` // view.column references that don't exist on the server
}

// Response is the model's structured answer.
type Response struct {
	Summary  string    `json:"summary"`
	Findings []Finding `json:"findings"`
}

// responseSchema constrains Ollama's output (structured outputs).
var responseSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "summary": {"type": "string"},
    "findings": {
      "type": "array",
      "maxItems": 5,
      "items": {
        "type": "object",
        "properties": {
          "severity":   {"type": "string", "enum": ["critical", "warning", "info"]},
          "title":      {"type": "string"},
          "evidence":   {"type": "string"},
          "root_cause": {"type": "string"},
          "action":     {"type": "string"},
          "sql":        {"type": "string"}
        },
        "required": ["severity", "title", "evidence", "root_cause", "action", "sql"]
      }
    }
  },
  "required": ["summary", "findings"]
}`)

// parseResponse decodes the model output, tolerating code fences and leading
// or trailing prose that small models sometimes add.
func parseResponse(text string) (*Response, error) {
	s := strings.TrimSpace(text)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	if i, j := strings.Index(s, "{"), strings.LastIndex(s, "}"); i >= 0 && j > i {
		s = s[i : j+1]
	}
	var r Response
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return nil, err
	}
	for i := range r.Findings {
		f := &r.Findings[i]
		f.Severity = strings.ToLower(strings.TrimSpace(f.Severity))
		switch f.Severity {
		case "critical", "warning", "info":
		default:
			f.Severity = "info"
		}
		f.SQL = strings.TrimSpace(f.SQL)
	}
	return &r, nil
}
