// Package pgapi provides an HTTP client for the pgmonk.dev backend API.
package pgapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/shashank/pgmonk/internal/model"
)

// Insight is an AI-generated database insight returned by the API.
type Insight struct {
	Severity    string `json:"severity"`
	Category    string `json:"category"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Suggestion  string `json:"suggestion"`
}

// Usage holds the user's current API usage for today.
type Usage struct {
	AnalysesToday int    `json:"analyses_today"`
	Limit         int    `json:"limit"`
	Tier          string `json:"tier"`
}

// AnalyzeResponse is the response from POST /v1/analyze.
type AnalyzeResponse struct {
	DatabaseID string    `json:"database_id"`
	AIEnhanced bool      `json:"ai_enhanced"`
	Model      string    `json:"model"`
	Insights   []Insight `json:"insights"`
	Usage      Usage     `json:"usage"`
}

type analyzeRequest struct {
	DatabaseFingerprint string          `json:"database_fingerprint"`
	DatabaseName        string          `json:"database_name"`
	PGVersion           int             `json:"pg_version"`
	Snapshot            *model.Snapshot `json:"snapshot"`
}

// Analyze sends a snapshot to the pgmonk.dev API and returns AI insights.
// Returns ErrRateLimited if the daily limit is reached.
// Returns ErrInvalidKey if the API key is invalid.
func Analyze(ctx context.Context, apiKey, endpoint, fingerprint, dbName string, pgVersion int, snap *model.Snapshot) (*AnalyzeResponse, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("no API key configured")
	}
	if endpoint == "" {
		endpoint = "https://api.pgmonk.dev"
	}

	req := analyzeRequest{
		DatabaseFingerprint: fingerprint,
		DatabaseName:        dbName,
		PGVersion:           pgVersion,
		Snapshot:            snap,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("serializing snapshot: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint+"/v1/analyze", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "pgmonk-cli/1.0")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// OK
	case http.StatusTooManyRequests:
		return nil, fmt.Errorf("daily analysis limit reached — upgrade at pgmonk.dev")
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("invalid API key — check your ~/.pgmonk.json")
	default:
		return nil, fmt.Errorf("API error: HTTP %d", resp.StatusCode)
	}

	var result AnalyzeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return &result, nil
}
