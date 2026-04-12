package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Config holds all configurable pg-monk settings.
// Loaded from ~/.pgmonk.json or --config flag.
type Config struct {
	// Refresh interval for the TUI dashboard.
	Interval time.Duration `json:"-"`
	// String version for JSON marshaling.
	IntervalStr string `json:"interval,omitempty"`

	// Analyzer thresholds.
	Thresholds Thresholds `json:"thresholds"`
}

// Thresholds holds configurable analyzer rule thresholds.
type Thresholds struct {
	// Cache hit ratio: warn below this percentage (default: 99.0).
	CacheHitWarn float64 `json:"cache_hit_warn,omitempty"`
	// Cache hit ratio: critical below this percentage (default: 95.0).
	CacheHitCritical float64 `json:"cache_hit_critical,omitempty"`

	// Connection usage: warn above this percentage (default: 70.0).
	ConnectionWarn float64 `json:"connection_warn,omitempty"`
	// Connection usage: critical above this percentage (default: 85.0).
	ConnectionCritical float64 `json:"connection_critical,omitempty"`

	// Dead tuples: warn above this count per table (default: 10000).
	DeadTuplesWarn int64 `json:"dead_tuples_warn,omitempty"`
	// Dead tuples: critical above this count per table (default: 100000).
	DeadTuplesCritical int64 `json:"dead_tuples_critical,omitempty"`

	// Idle-in-transaction: warn above this duration (default: "30s").
	IdleInTxWarn    time.Duration `json:"-"`
	IdleInTxWarnStr string        `json:"idle_in_tx_warn,omitempty"`

	// Idle-in-transaction: critical above this duration (default: "5m").
	IdleInTxCritical    time.Duration `json:"-"`
	IdleInTxCriticalStr string        `json:"idle_in_tx_critical,omitempty"`

	// XID wraparound: warn above this age (default: 500000000).
	XIDWarn int64 `json:"xid_warn,omitempty"`
	// XID wraparound: critical above this age (default: 1500000000).
	XIDCritical int64 `json:"xid_critical,omitempty"`

	// Replication lag: warn above this duration (default: "30s").
	ReplicationLagWarn    time.Duration `json:"-"`
	ReplicationLagWarnStr string        `json:"replication_lag_warn,omitempty"`

	// Replication lag: critical above this duration (default: "5m").
	ReplicationLagCritical    time.Duration `json:"-"`
	ReplicationLagCriticalStr string        `json:"replication_lag_critical,omitempty"`

	// Rollback ratio: warn above this percentage (default: 5.0).
	RollbackRatioWarn float64 `json:"rollback_ratio_warn,omitempty"`

	// Temp file usage: warn above this byte count (default: 1073741824 = 1GB).
	TempBytesWarn int64 `json:"temp_bytes_warn,omitempty"`

	// Long-running transaction: warn above this duration (default: "1h").
	LongTxWarn    time.Duration `json:"-"`
	LongTxWarnStr string        `json:"long_tx_warn,omitempty"`

	// Long-running transaction: critical above this duration (default: "6h").
	LongTxCritical    time.Duration `json:"-"`
	LongTxCriticalStr string        `json:"long_tx_critical,omitempty"`
}

// DefaultConfig returns a Config with all default values.
func DefaultConfig() *Config {
	return &Config{
		Interval:    2 * time.Second,
		IntervalStr: "2s",
		Thresholds: Thresholds{
			CacheHitWarn:              99.0,
			CacheHitCritical:          95.0,
			ConnectionWarn:            70.0,
			ConnectionCritical:        85.0,
			DeadTuplesWarn:            10000,
			DeadTuplesCritical:        100000,
			IdleInTxWarn:              30 * time.Second,
			IdleInTxWarnStr:           "30s",
			IdleInTxCritical:          5 * time.Minute,
			IdleInTxCriticalStr:       "5m",
			XIDWarn:                   500_000_000,
			XIDCritical:              1_500_000_000,
			ReplicationLagWarn:        30 * time.Second,
			ReplicationLagWarnStr:     "30s",
			ReplicationLagCritical:    5 * time.Minute,
			ReplicationLagCriticalStr: "5m",
			RollbackRatioWarn:         5.0,
			TempBytesWarn:             1 << 30, // 1 GB
			LongTxWarn:                1 * time.Hour,
			LongTxWarnStr:             "1h",
			LongTxCritical:            6 * time.Hour,
			LongTxCriticalStr:         "6h",
		},
	}
}

// Load reads a config from the given file path, applying values on top of defaults.
// If path is empty, tries ~/.pgmonk.json, then returns defaults.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, ".pgmonk.json")
		}
	}

	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	// Resolve duration strings.
	cfg.resolveDurations()

	return cfg, nil
}

// resolveDurations parses the string duration fields into time.Duration.
func (c *Config) resolveDurations() {
	if c.IntervalStr != "" {
		if d, err := time.ParseDuration(c.IntervalStr); err == nil {
			c.Interval = d
		}
	}
	t := &c.Thresholds
	if t.IdleInTxWarnStr != "" {
		if d, err := time.ParseDuration(t.IdleInTxWarnStr); err == nil {
			t.IdleInTxWarn = d
		}
	}
	if t.IdleInTxCriticalStr != "" {
		if d, err := time.ParseDuration(t.IdleInTxCriticalStr); err == nil {
			t.IdleInTxCritical = d
		}
	}
	if t.ReplicationLagWarnStr != "" {
		if d, err := time.ParseDuration(t.ReplicationLagWarnStr); err == nil {
			t.ReplicationLagWarn = d
		}
	}
	if t.ReplicationLagCriticalStr != "" {
		if d, err := time.ParseDuration(t.ReplicationLagCriticalStr); err == nil {
			t.ReplicationLagCritical = d
		}
	}
	if t.LongTxWarnStr != "" {
		if d, err := time.ParseDuration(t.LongTxWarnStr); err == nil {
			t.LongTxWarn = d
		}
	}
	if t.LongTxCriticalStr != "" {
		if d, err := time.ParseDuration(t.LongTxCriticalStr); err == nil {
			t.LongTxCritical = d
		}
	}
}

// WriteDefault writes a default config file to path as a reference.
func WriteDefault(path string) error {
	cfg := DefaultConfig()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
