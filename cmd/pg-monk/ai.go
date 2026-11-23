package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sjtly16/pgmonk/internal/ai"
	"github.com/sjtly16/pgmonk/internal/analyzer"
	"github.com/sjtly16/pgmonk/internal/collector"
	"github.com/sjtly16/pgmonk/internal/config"
	"github.com/sjtly16/pgmonk/internal/model"
)

// AI flags (persistent, so they work on every subcommand).
var (
	flagAI         bool
	flagAIModel    string
	flagAIURL      string
	flagAIProvider string
)

func addAIFlags(root *cobra.Command) {
	pf := root.PersistentFlags()
	pf.BoolVar(&flagAI, "ai", false, "Enable AI insights from a local LLM (Ollama or OpenAI-compatible)")
	pf.StringVar(&flagAIModel, "ai-model", "", "AI model (default "+config.DefaultAIModel+")")
	pf.StringVar(&flagAIURL, "ai-url", "", "AI server URL (default "+config.DefaultOllamaURL+")")
	pf.StringVar(&flagAIProvider, "ai-provider", "", `AI provider: "ollama" or "openai" (any OpenAI-compatible server)`)
}

// aiSettings resolves the effective AI configuration: file < env < flags.
func aiSettings(cfg *config.Config) config.AIConfig {
	if flagAIProvider != "" {
		cfg.AI.Provider = flagAIProvider
	}
	if flagAIURL != "" {
		cfg.AI.BaseURL = flagAIURL
	}
	if flagAIModel != "" {
		cfg.AI.Model = flagAIModel
	}
	cfg.ApplyAIDefaults()
	return cfg.AI
}

func aiEnabled(cfg *config.Config) bool { return flagAI || cfg.AI.Enabled }

// newAIEngine builds an engine for the connected server, warning once if the
// model server isn't on this machine.
func newAIEngine(a config.AIConfig, coll *collector.Collector) (*ai.Engine, error) {
	provider, err := ai.NewProvider(a.Provider, a.BaseURL, a.Model, a.APIKey, a.NumCtx, a.KeepAlive)
	if err != nil {
		return nil, err
	}
	if !ai.IsLocal(a.BaseURL) {
		fmt.Fprintf(os.Stderr, "  ⚠ AI server %s is not on this machine — query text and statistics will be sent to it.\n", a.BaseURL)
	}
	return ai.NewEngine(provider, coll.ServerInfo(), coll.ExplainReadOnly, collector.ErrNotReadOnly)
}

// printAIAnalysis runs and prints an AI analysis for `diagnose --ai`.
func printAIAnalysis(cfg *config.Config, coll *collector.Collector, snap *model.Snapshot) error {
	a := aiSettings(cfg)
	engine, err := newAIEngine(a, coll)
	if err != nil {
		return err
	}
	p := engine.Pack()
	fmt.Printf("  🤖 AI analysis — %s, PostgreSQL %d knowledge", engine.ModelName(), p.RefMajor)
	if !p.Exact {
		fmt.Printf(" (nearest to %d)", p.Major)
	}
	fmt.Println()
	fmt.Println("     (the first run loads the model; this can take a minute on CPU)")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), a.Timeout)
	defer cancel()
	res, err := engine.Analyze(ctx, snap)
	if err != nil {
		return fmt.Errorf("AI analysis failed: %w (run `pg-monk ai check`)", err)
	}

	if res.Response == nil {
		fmt.Printf("  ⚠ %v — raw answer:\n\n%s\n\n", res.ParseError, indent(res.Raw, "     "))
		return nil
	}
	r := res.Response
	fmt.Printf("  %s\n\n", r.Summary)
	for i, f := range r.Findings {
		fmt.Printf("  %d. %s [%s] %s\n", i+1, severityEmoji(f.Severity), strings.ToUpper(f.Severity), f.Title)
		printField("Evidence", f.Evidence)
		printField("Cause", f.RootCause)
		printField("Action", f.Action)
		switch {
		case f.SQL == "":
		case f.SQLIsFix:
			printField("Fix", f.SQL)
			fmt.Println("               (review before running — pg-monk never runs changes)")
		default:
			printField("Check", f.SQL)
			if f.SQLVerified {
				fmt.Println("               ✓ verified on this server (EXPLAIN)")
			} else {
				fmt.Printf("               ⚠ %s\n", f.SQLError)
			}
		}
		for _, n := range f.Notes {
			fmt.Printf("     ℹ pg-monk: %s\n", n)
		}
		if len(f.Unknown) > 0 {
			fmt.Printf("     ⚠ mentions columns that don't exist on this server: %s\n", strings.Join(f.Unknown, ", "))
		}
		fmt.Println()
	}
	if len(r.Findings) == 0 {
		fmt.Println("  ✓ No additional issues found.")
		fmt.Println()
	}
	fmt.Printf("  %s in %s · %d prompt tokens", engine.ModelName(), res.Elapsed.Round(100*time.Millisecond), res.Usage.PromptTokens)
	if res.Usage.PromptEval > 0 {
		fmt.Printf(" (processed in %s)", res.Usage.PromptEval.Round(10*time.Millisecond))
	}
	fmt.Println()
	return nil
}

func printField(label, v string) {
	if v != "" {
		fmt.Printf("     %-9s %s\n", label+":", v)
	}
}

func severityEmoji(s string) string {
	switch s {
	case "critical":
		return "🔴"
	case "warning":
		return "🟡"
	default:
		return "🔵"
	}
}

func indent(s, prefix string) string {
	return prefix + strings.ReplaceAll(strings.TrimSpace(s), "\n", "\n"+prefix)
}

// ---- `pg-monk ai` subcommands ----

func newAICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ai",
		Short: "Local LLM insights: check setup, inspect prompts",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "check [connection-string]",
		Short: "Verify the AI server and model, and show the knowledge pack for a database",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runAICheck,
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "prompt [connection-string]",
		Short: "Print the exact prompt pg-monk would send to the model",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runAIPrompt,
	})
	return cmd
}

func runAICheck(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	a := aiSettings(cfg)
	fmt.Printf("🧘 pg-monk ai check\n\n")
	fmt.Printf("  Provider: %s\n  Server:   %s\n  Model:    %s\n", a.Provider, a.BaseURL, a.Model)
	if !ai.IsLocal(a.BaseURL) {
		fmt.Println("  ⚠ The server is not on this machine — query text and statistics would be sent to it.")
	}
	fmt.Println()

	ok := true
	if a.Provider == "ollama" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		models, err := ai.ListOllamaModels(ctx, a.BaseURL)
		cancel()
		switch {
		case err != nil:
			fmt.Printf("  ✗ %v\n    Install Ollama (https://ollama.com) or set --ai-url / PGMONK_AI_URL.\n", err)
			return nil
		case !containsModel(models, a.Model):
			fmt.Printf("  ✗ Model %q is not pulled. Run:\n\n      ollama pull %s\n\n", a.Model, a.Model)
			if len(models) > 0 {
				fmt.Printf("    Available: %s\n", strings.Join(models, ", "))
			}
			return nil
		default:
			fmt.Printf("  ✓ Ollama reachable, %s is pulled\n", a.Model)
		}
	}

	provider, err := ai.NewProvider(a.Provider, a.BaseURL, a.Model, a.APIKey, a.NumCtx, a.KeepAlive)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), a.Timeout)
	start := time.Now()
	_, _, err = provider.Complete(ctx, "Reply with JSON only.", `Reply with {"summary":"ok","findings":[]}`)
	cancel()
	if err != nil {
		ok = false
		fmt.Printf("  ✗ Test completion failed: %v\n", err)
	} else {
		fmt.Printf("  ✓ Test completion in %s\n", time.Since(start).Round(100*time.Millisecond))
	}

	// Knowledge pack for a database, if we can reach one.
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	coll, err := collector.New(ctx, buildDSN(args), time.Second)
	if err != nil {
		fmt.Println("  – Database: not connected (pass a connection to see its knowledge pack)")
	} else {
		defer coll.Close()
		engine, err := newAIEngine(a, coll)
		if err != nil {
			return err
		}
		p := engine.Pack()
		info := coll.ServerInfo()
		exact := "exact match"
		if !p.Exact {
			exact = fmt.Sprintf("nearest available to %d", p.Major)
		}
		fmt.Printf("  ✓ Database: PostgreSQL %d.%d — knowledge pack PG %d (%s), ~%d tokens, %d views in live catalog\n",
			info.VersionNum/10000, info.VersionNum%10000, p.RefMajor, exact, len(p.Text)/4, len(info.Catalog))
	}

	fmt.Println()
	if ok {
		fmt.Println("  Ready: run `pg-monk --ai` and open the Insights tab (7), or `pg-monk diagnose --ai`.")
	}
	return nil
}

func runAIPrompt(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	coll, err := collector.New(ctx, buildDSN(args), time.Second)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer coll.Close()
	if err := coll.CollectOnce(ctx); err != nil {
		return err
	}
	time.Sleep(time.Second)
	if err := coll.CollectOnce(ctx); err != nil {
		return err
	}
	snap := coll.Snapshot()

	a := aiSettings(cfg)
	provider, err := ai.NewProvider(a.Provider, a.BaseURL, a.Model, a.APIKey, a.NumCtx, a.KeepAlive)
	if err != nil {
		return err
	}
	engine, err := ai.NewEngine(provider, coll.ServerInfo(), nil, nil)
	if err != nil {
		return err
	}
	fmt.Println("===== SYSTEM (stable per server; cached by the model server) =====")
	fmt.Println(ai.SystemPrompt(engine.Pack()))
	fmt.Println("===== USER (changes every analysis) =====")
	fmt.Println(ai.UserPrompt(ai.Digest(snap, analyzer.Analyze(snap))))
	return nil
}

func containsModel(models []string, want string) bool {
	for _, m := range models {
		if m == want || (!strings.Contains(want, ":") && m == want+":latest") {
			return true
		}
	}
	return false
}
