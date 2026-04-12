package main

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/shashank/pgmonk/internal/analyzer"
	"github.com/shashank/pgmonk/internal/collector"
	"github.com/shashank/pgmonk/internal/config"
	"github.com/shashank/pgmonk/internal/export"
	"github.com/shashank/pgmonk/internal/model"
	"github.com/shashank/pgmonk/internal/tui"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// Connection flags (psql-compatible).
var (
	flagHost     string
	flagPort     int
	flagUser     string
	flagPassword string
	flagDBName   string
	flagSSLMode  string
	flagInterval time.Duration
	flagConfig   string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "pg-monk [connection-string]",
		Short: "🧘 pg-monk — real-time PostgreSQL performance dashboard",
		Long: `pg-monk connects to your PostgreSQL database and provides a real-time
terminal dashboard showing performance metrics, active sessions, query
statistics, table health, and lock information.

Connect using a connection string:
  pg-monk "postgresql://user:pass@localhost:5432/mydb"

Or using psql-compatible flags:
  pg-monk -H localhost -p 5432 -U monitor -d mydb

Or using environment variable:
  PGMONK_DSN="postgresql://..." pg-monk`,
		Args:    cobra.MaximumNArgs(1),
		RunE:    runDashboard,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
	}

	// psql-compatible connection flags.
	rootCmd.Flags().StringVarP(&flagHost, "host", "H", "localhost", "Database host")
	rootCmd.Flags().IntVarP(&flagPort, "port", "p", 5432, "Database port")
	rootCmd.Flags().StringVarP(&flagUser, "user", "U", "", "Database user")
	rootCmd.Flags().StringVarP(&flagPassword, "password", "W", "", "Database password")
	rootCmd.Flags().StringVarP(&flagDBName, "dbname", "d", "", "Database name")
	rootCmd.Flags().StringVar(&flagSSLMode, "sslmode", "prefer", "SSL mode (disable, require, prefer)")
	rootCmd.Flags().DurationVarP(&flagInterval, "interval", "i", 2*time.Second, "Refresh interval")
	rootCmd.PersistentFlags().StringVar(&flagConfig, "config", "", "Config file path (default: ~/.pgmonk.json)")

	// Snapshot subcommand.
	snapshotCmd := &cobra.Command{
		Use:   "snapshot [connection-string]",
		Short: "Take a one-time snapshot and output as JSON or CSV",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runSnapshot,
	}
	snapshotCmd.Flags().StringP("format", "f", "json", "Output format: json, csv")
	snapshotCmd.Flags().StringP("output", "o", "", "Output file (defaults to stdout)")
	rootCmd.AddCommand(snapshotCmd)

	// Diagnose subcommand.
	diagnoseCmd := &cobra.Command{
		Use:   "diagnose [connection-string]",
		Short: "Run a health check and print insights",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runDiagnose,
	}
	rootCmd.AddCommand(diagnoseCmd)

	// Init-config subcommand.
	initConfigCmd := &cobra.Command{
		Use:   "init-config",
		Short: "Write a default config file to ~/.pgmonk.json",
		RunE:  runInitConfig,
	}
	initConfigCmd.Flags().StringP("output", "o", "", "Output path (default: ~/.pgmonk.json)")
	rootCmd.AddCommand(initConfigCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// buildDSN constructs a connection string from flags or args.
func buildDSN(args []string) string {
	// Priority: positional arg > env var > flags.
	if len(args) > 0 {
		return args[0]
	}
	if dsn := os.Getenv("PGMONK_DSN"); dsn != "" {
		return dsn
	}
	// Also check standard PG env vars.
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return dsn
	}

	// Build from flags.
	user := flagUser
	if user == "" {
		user = os.Getenv("PGUSER")
		if user == "" {
			user = os.Getenv("USER")
		}
	}
	password := flagPassword
	if password == "" {
		password = os.Getenv("PGPASSWORD")
	}
	dbname := flagDBName
	if dbname == "" {
		dbname = os.Getenv("PGDATABASE")
		if dbname == "" {
			dbname = user
		}
	}
	host := flagHost
	if h := os.Getenv("PGHOST"); h != "" && flagHost == "localhost" {
		host = h
	}

	dsn := fmt.Sprintf("postgresql://%s", host)
	if flagPort != 0 {
		dsn = fmt.Sprintf("postgresql://%s:%d", host, flagPort)
	}
	if dbname != "" {
		dsn += "/" + dbname
	}
	dsn += fmt.Sprintf("?sslmode=%s", flagSSLMode)
	if user != "" {
		if password != "" {
			dsn = fmt.Sprintf("postgresql://%s:%s@%s:%d/%s?sslmode=%s",
				user, password, host, flagPort, dbname, flagSSLMode)
		} else {
			dsn = fmt.Sprintf("postgresql://%s@%s:%d/%s?sslmode=%s",
				user, host, flagPort, dbname, flagSSLMode)
		}
	}

	return dsn
}

// runDashboard is the main interactive TUI command.
func runDashboard(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	dsn := buildDSN(args)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("🧘 pg-monk — connecting to database...")

	coll, err := collector.New(ctx, dsn, flagInterval)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer coll.Close()

	info := coll.ServerInfo()
	fmt.Printf("  Connected to PostgreSQL %d\n", info.VersionNum/10000)
	if info.HasStatStatements {
		fmt.Println("  ✓ pg_stat_statements detected")
	} else {
		fmt.Println("  ⚠ pg_stat_statements not found (query stats disabled)")
	}
	if cfg.APIKey != "" {
		fmt.Println("  ✓ pgmonk.dev API key detected — AI insights enabled")
	}
	fmt.Println("  Starting dashboard...")
	fmt.Println()

	// Build API config for AI insights.
	apiCfg := tui.APIConfig{
		APIKey:        cfg.APIKey,
		APIEndpoint:   cfg.APIEndpoint,
		DBFingerprint: tui.ComputeFingerprint(dsn),
		PGVersion:     info.VersionNum,
	}

	// Create Bubble Tea program.
	model := tui.NewModel(info, apiCfg)
	p := tea.NewProgram(model, tea.WithAltScreen())

	// Start the collector in a background goroutine.
	go func() {
		// Do initial collection immediately.
		if err := coll.CollectOnce(ctx); err == nil {
			if snap := coll.Snapshot(); snap != nil {
				p.Send(tui.SnapshotMsg{Snapshot: snap})
			}
		}

		ticker := time.NewTicker(flagInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := coll.CollectOnce(ctx); err != nil {
					continue
				}
				snap := coll.Snapshot()
				if snap != nil {
					p.Send(tui.SnapshotMsg{Snapshot: snap})
				}
			}
		}
	}()

	// Run the TUI (blocking).
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	return nil
}

// runSnapshot takes a one-time snapshot and outputs it.
func runSnapshot(cmd *cobra.Command, args []string) error {
	dsn := buildDSN(args)
	format, _ := cmd.Flags().GetString("format")
	output, _ := cmd.Flags().GetString("output")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	coll, err := collector.New(ctx, dsn, time.Second)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer coll.Close()

	if err := coll.CollectOnce(ctx); err != nil {
		return fmt.Errorf("collection failed: %w", err)
	}

	snap := coll.Snapshot()
	if snap == nil {
		return fmt.Errorf("no data collected")
	}

	// Determine output writer.
	w := os.Stdout
	if output != "" {
		f, err := os.Create(output)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}

	switch format {
	case "json":
		return export.WriteJSON(w, snap)
	case "csv":
		return export.WriteCSVOverview(w, snap)
	default:
		return fmt.Errorf("unknown format: %s (supported: json, csv)", format)
	}
}

// runDiagnose performs a health check and prints insights.
func runDiagnose(cmd *cobra.Command, args []string) error {
	if _, err := loadConfig(); err != nil {
		return err
	}

	dsn := buildDSN(args)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("🧘 pg-monk diagnose — analyzing database health...")
	fmt.Println()

	coll, err := collector.New(ctx, dsn, time.Second)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer coll.Close()

	// Need two collections for rate data.
	if err := coll.CollectOnce(ctx); err != nil {
		return fmt.Errorf("collection failed: %w", err)
	}
	time.Sleep(2 * time.Second)
	if err := coll.CollectOnce(ctx); err != nil {
		return fmt.Errorf("second collection failed: %w", err)
	}

	snap := coll.Snapshot()
	if snap == nil {
		return fmt.Errorf("no data collected")
	}

	// Run the analyzer.
	insights := analyzer.Analyze(snap)

	o := snap.Overview
	fmt.Printf("  Database:       %s\n", o.DatabaseName)
	fmt.Printf("  Connections:    %d / %d\n", o.TotalConnections, o.MaxConnections)
	fmt.Printf("  Cache Hit:      %.2f%%\n", o.CacheHitRatio)
	fmt.Printf("  Deadlocks:      %d\n", o.Deadlocks)
	fmt.Printf("  DB Size:        %s\n", formatBytesSimple(o.DatabaseSize))
	fmt.Printf("  Tables:         %d\n", len(snap.Tables))
	fmt.Printf("  Active Queries: %d\n", countActive(snap))
	if o.DataFrozenXIDAge > 0 {
		fmt.Printf("  XID Age:        %s\n", formatNumberSimple(o.DataFrozenXIDAge))
	}
	if o.RollbackRatio > 0 {
		fmt.Printf("  Rollback Ratio: %.1f%%\n", o.RollbackRatio)
	}
	fmt.Println()

	if len(insights) == 0 {
		fmt.Println("  ✓ No issues found. Database looks healthy!")
		return nil
	}

	// Group by severity.
	critCount, warnCount, infoCount := 0, 0, 0
	for _, ins := range insights {
		switch ins.Severity {
		case analyzer.Critical:
			critCount++
		case analyzer.Warning:
			warnCount++
		case analyzer.Info:
			infoCount++
		}
	}

	fmt.Printf("  Found %d insights: %d critical, %d warnings, %d info\n\n",
		len(insights), critCount, warnCount, infoCount)

	for _, ins := range insights {
		var icon string
		switch ins.Severity {
		case analyzer.Critical:
			icon = "🔴"
		case analyzer.Warning:
			icon = "🟡"
		case analyzer.Info:
			icon = "🔵"
		}
		fmt.Printf("  %s [%s] %s\n", icon, ins.Severity, ins.Title)
		fmt.Printf("     %s\n", ins.Description)
		fmt.Printf("     💡 %s\n\n", ins.Suggestion)
	}

	return nil
}

func formatBytesSimple(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	default:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	}
}

func countActive(snap *model.Snapshot) int {
	count := 0
	for _, a := range snap.Activities {
		if a.State == "active" {
			count++
		}
	}
	return count
}

func formatNumberSimple(n int64) string {
	if n >= 1_000_000_000 {
		return fmt.Sprintf("%.2fB", float64(n)/1_000_000_000)
	}
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	return fmt.Sprintf("%d", n)
}

// loadConfig loads config, applies thresholds to the analyzer, and returns the config.
func loadConfig() (*config.Config, error) {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	// Apply interval from config if not overridden by flag.
	if flagInterval == 2*time.Second && cfg.Interval != 2*time.Second {
		flagInterval = cfg.Interval
	}

	// Apply thresholds to the analyzer.
	t := cfg.Thresholds
	at := analyzer.DefaultThresholds()
	if t.CacheHitWarn > 0 {
		at.CacheHitWarn = t.CacheHitWarn
	}
	if t.CacheHitCritical > 0 {
		at.CacheHitCritical = t.CacheHitCritical
	}
	if t.ConnectionWarn > 0 {
		at.ConnectionWarn = t.ConnectionWarn
	}
	if t.ConnectionCritical > 0 {
		at.ConnectionCritical = t.ConnectionCritical
	}
	if t.DeadTuplesWarn > 0 {
		at.DeadTuplesWarn = t.DeadTuplesWarn
	}
	if t.DeadTuplesCritical > 0 {
		at.DeadTuplesCritical = t.DeadTuplesCritical
	}
	if t.IdleInTxWarn > 0 {
		at.IdleInTxWarn = t.IdleInTxWarn
	}
	if t.IdleInTxCritical > 0 {
		at.IdleInTxCritical = t.IdleInTxCritical
	}
	if t.XIDWarn > 0 {
		at.XIDWarn = t.XIDWarn
	}
	if t.XIDCritical > 0 {
		at.XIDCritical = t.XIDCritical
	}
	if t.ReplicationLagWarn > 0 {
		at.ReplicationLagWarn = t.ReplicationLagWarn
	}
	if t.ReplicationLagCritical > 0 {
		at.ReplicationLagCritical = t.ReplicationLagCritical
	}
	if t.RollbackRatioWarn > 0 {
		at.RollbackRatioWarn = t.RollbackRatioWarn
	}
	if t.TempBytesWarn > 0 {
		at.TempBytesWarn = t.TempBytesWarn
	}
	if t.LongTxWarn > 0 {
		at.LongTxWarn = t.LongTxWarn
	}
	if t.LongTxCritical > 0 {
		at.LongTxCritical = t.LongTxCritical
	}
	analyzer.SetThresholds(at)
	return cfg, nil
}

// runInitConfig writes a default config file.
func runInitConfig(cmd *cobra.Command, args []string) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("could not determine home directory: %w", err)
		}
		output = home + "/.pgmonk.json"
	}
	if err := config.WriteDefault(output); err != nil {
		return err
	}
	fmt.Printf("  ✓ Default config written to %s\n", output)
	fmt.Println("  Edit this file to customize analyzer thresholds.")
	return nil
}
