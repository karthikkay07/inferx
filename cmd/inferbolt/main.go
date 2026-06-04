package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"

	"github.com/inferbolthq/inferbolt/internal/cli"
	"github.com/inferbolthq/inferbolt/internal/jobs"
	"github.com/inferbolthq/inferbolt/internal/router"
)

// Set by GoReleaser via ldflags.
var version = "dev"

// ── shared state ──────────────────────────────────────────────────────────────

var (
	cfg       *cli.Config
	apiClient *cli.Client

	serverFlag string
	apiKeyFlag string
	outputFlag string
)

// ── root command ──────────────────────────────────────────────────────────────

var rootCmd = &cobra.Command{
	Use:   "inferbolt",
	Short: "InferBolt — open-source LLM inference optimization toolkit",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// configure and version work without a loaded config
		name := cmd.Name()
		if name == "configure" || name == "version" {
			return nil
		}
		var err error
		cfg, err = cli.Load()
		if err != nil {
			return err
		}
		if serverFlag != "" {
			cfg.ServerURL = serverFlag
		}
		if apiKeyFlag != "" {
			cfg.APIKey = apiKeyFlag
		}
		if outputFlag != "" {
			cfg.OutputFmt = outputFlag
		}
		apiClient = cfg.NewClient()
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&serverFlag, "server", "", "InferBolt server URL")
	rootCmd.PersistentFlags().StringVar(&apiKeyFlag, "api-key", "", "InferBolt API key")
	rootCmd.PersistentFlags().StringVar(&outputFlag, "output", "table", "Output format: table or json")

	rootCmd.AddCommand(newBenchmarkCmd())
	rootCmd.AddCommand(newJobsCmd())
	rootCmd.AddCommand(newMetricsCmd())
	rootCmd.AddCommand(newRouteCmd())
	rootCmd.AddCommand(newConfigureCmd())
	rootCmd.AddCommand(newVersionCmd())
	rootCmd.AddCommand(newAdminCmd())
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// ── benchmark ─────────────────────────────────────────────────────────────────

func newBenchmarkCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "benchmark", Short: "Run and compare inference benchmarks"}
	cmd.AddCommand(newBenchmarkRunCmd())
	cmd.AddCommand(newBenchmarkCompareCmd())
	return cmd
}

func newBenchmarkRunCmd() *cobra.Command {
	var (
		model        string
		enginesFlag  string
		gpu          string
		concurrency  int
		promptTokens int
		outputTokens int
		requests     int
		autoRoute    bool
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a benchmark job",
		RunE: func(cmd *cobra.Command, args []string) error {
			if model == "" {
				return fmt.Errorf("--model is required")
			}
			if !autoRoute && enginesFlag == "" {
				return fmt.Errorf("--engines is required unless --auto-route is set")
			}
			if gpu == "" {
				return fmt.Errorf("--gpu is required")
			}

			engines := splitCSV(enginesFlag)

			req := cli.CreateJobRequest{
				Model:   model,
				Engines: engines,
				Workload: jobs.WorkloadConfig{
					Concurrency:  concurrency,
					PromptTokens: promptTokens,
					OutputTokens: outputTokens,
					NumRequests:  requests,
				},
				GPUProfile: gpu,
				AutoRoute:  autoRoute,
			}

			jobResp, err := apiClient.CreateJob(cmd.Context(), req)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			engineLabel := strings.Join(engines, ",")
			if jobResp.RecommendedEngine != "" {
				engineLabel = jobResp.RecommendedEngine
			}

			// Progress bar — updates on each state change
			bar := progressbar.NewOptions(-1,
				progressbar.OptionSetDescription(
					fmt.Sprintf("Benchmarking %s on %s...", model, engineLabel)),
				progressbar.OptionSpinnerType(14),
				progressbar.OptionSetWriter(os.Stderr),
				progressbar.OptionEnableColorCodes(true),
				progressbar.OptionOnCompletion(func() { fmt.Fprintln(os.Stderr) }),
			)

			finalJob, err := apiClient.PollJob(cmd.Context(), jobResp.JobID, func(j jobs.Job) {
				bar.Describe(fmt.Sprintf("Benchmarking %s on %s [%s]...",
					model, engineLabel, string(j.State)))
				bar.Add(1) //nolint:errcheck
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			bar.Finish() //nolint:errcheck

			if finalJob.State == jobs.StateFailed {
				fmt.Fprintf(os.Stderr, "Benchmark failed: %s\n", finalJob.ErrorMsg)
				os.Exit(1)
			}

			results, err := apiClient.GetJobResults(cmd.Context(), jobResp.JobID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error fetching results: %v\n", err)
				os.Exit(1)
			}

			if isJSON() {
				return json.NewEncoder(os.Stdout).Encode(map[string]any{
					"job_id":  jobResp.JobID,
					"model":   model,
					"state":   string(finalJob.State),
					"engines": engines,
					"results": results,
				})
			}

			printResultsTable(results)
			return nil
		},
	}

	cmd.Flags().StringVar(&model, "model", "", "Model to benchmark (required)")
	cmd.Flags().StringVar(&enginesFlag, "engines", "", "Comma-separated engines (required unless --auto-route)")
	cmd.Flags().StringVar(&gpu, "gpu", "", "GPU profile, e.g. a100-80gb (required)")
	cmd.Flags().IntVar(&concurrency, "concurrency", 32, "Number of concurrent requests")
	cmd.Flags().IntVar(&promptTokens, "prompt-tokens", 512, "Prompt length in tokens")
	cmd.Flags().IntVar(&outputTokens, "output-tokens", 256, "Max output tokens")
	cmd.Flags().IntVar(&requests, "requests", 200, "Total number of requests")
	cmd.Flags().BoolVar(&autoRoute, "auto-route", false, "Auto-select best engine via classifier")
	return cmd
}

func newBenchmarkCompareCmd() *cobra.Command {
	var (
		model        string
		enginesFlag  string
		gpu          string
		concurrency  int
		promptTokens int
		outputTokens int
		requests     int
	)

	cmd := &cobra.Command{
		Use:   "compare",
		Short: "Compare multiple engines in a single benchmark run",
		RunE: func(cmd *cobra.Command, args []string) error {
			if model == "" {
				return fmt.Errorf("--model is required")
			}
			engines := splitCSV(enginesFlag)
			if len(engines) < 2 {
				return fmt.Errorf("--engines requires at least 2 comma-separated engines for comparison")
			}
			if gpu == "" {
				return fmt.Errorf("--gpu is required")
			}

			req := cli.CreateJobRequest{
				Model:   model,
				Engines: engines,
				Workload: jobs.WorkloadConfig{
					Concurrency:  concurrency,
					PromptTokens: promptTokens,
					OutputTokens: outputTokens,
					NumRequests:  requests,
				},
				GPUProfile: gpu,
			}

			jobResp, err := apiClient.CreateJob(cmd.Context(), req)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			bar := progressbar.NewOptions(-1,
				progressbar.OptionSetDescription(
					fmt.Sprintf("Comparing engines for %s...", model)),
				progressbar.OptionSpinnerType(14),
				progressbar.OptionSetWriter(os.Stderr),
				progressbar.OptionOnCompletion(func() { fmt.Fprintln(os.Stderr) }),
			)

			finalJob, err := apiClient.PollJob(cmd.Context(), jobResp.JobID, func(j jobs.Job) {
				bar.Describe(fmt.Sprintf("Comparing engines for %s [%s]...", model, j.State))
				bar.Add(1) //nolint:errcheck
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			bar.Finish() //nolint:errcheck

			if finalJob.State == jobs.StateFailed {
				fmt.Fprintf(os.Stderr, "Benchmark failed: %s\n", finalJob.ErrorMsg)
				os.Exit(1)
			}

			results, err := apiClient.GetJobResults(cmd.Context(), jobResp.JobID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			if isJSON() {
				return json.NewEncoder(os.Stdout).Encode(map[string]any{
					"job_id":  jobResp.JobID,
					"model":   model,
					"results": results,
				})
			}

			printComparisonTable(results)
			return nil
		},
	}

	cmd.Flags().StringVar(&model, "model", "", "Model to benchmark (required)")
	cmd.Flags().StringVar(&enginesFlag, "engines", "", "Comma-separated engines, min 2 (required)")
	cmd.Flags().StringVar(&gpu, "gpu", "", "GPU profile (required)")
	cmd.Flags().IntVar(&concurrency, "concurrency", 32, "Concurrent requests")
	cmd.Flags().IntVar(&promptTokens, "prompt-tokens", 512, "Prompt tokens")
	cmd.Flags().IntVar(&outputTokens, "output-tokens", 256, "Output tokens")
	cmd.Flags().IntVar(&requests, "requests", 200, "Total requests")
	return cmd
}

// ── jobs ──────────────────────────────────────────────────────────────────────

func newJobsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "jobs", Short: "Manage benchmark jobs"}
	cmd.AddCommand(newJobsListCmd())
	cmd.AddCommand(newJobsGetCmd())
	return cmd
}

func newJobsListCmd() *cobra.Command {
	var (
		state string
		limit int
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List benchmark jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			jobList, err := apiClient.ListJobs(cmd.Context(), state, limit)
			if err != nil {
				return err
			}
			if isJSON() {
				return json.NewEncoder(os.Stdout).Encode(jobList)
			}
			tbl := tablewriter.NewWriter(os.Stdout)
			tbl.SetHeader([]string{"JOB ID", "MODEL", "STATE", "ENGINES", "CREATED"})
			tbl.SetBorder(false)
			for _, j := range jobList {
				id := j.ID
				if len(id) > 8 {
					id = id[:8]
				}
				tbl.Append([]string{
					id,
					j.Model,
					string(j.State),
					strings.Join(j.Engines, ","),
					j.CreatedAt.Format("2006-01-02 15:04"),
				})
			}
			tbl.Render()
			return nil
		},
	}
	cmd.Flags().StringVar(&state, "state", "", "Filter by state (pending, running, completed, failed)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum jobs to return")
	return cmd
}

func newJobsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <jobID>",
		Short: "Get job details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			job, err := apiClient.GetJob(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if isJSON() {
				return json.NewEncoder(os.Stdout).Encode(job)
			}
			tbl := tablewriter.NewWriter(os.Stdout)
			tbl.SetHeader([]string{"FIELD", "VALUE"})
			tbl.SetBorder(false)
			tbl.Append([]string{"ID", job.ID})
			tbl.Append([]string{"MODEL", job.Model})
			tbl.Append([]string{"STATE", string(job.State)})
			tbl.Append([]string{"ENGINES", strings.Join(job.Engines, ", ")})
			tbl.Append([]string{"GPU PROFILE", job.GPUProfile})
			tbl.Append([]string{"CREATED", job.CreatedAt.Format(time.RFC3339)})
			tbl.Append([]string{"UPDATED", job.UpdatedAt.Format(time.RFC3339)})
			if job.ErrorMsg != "" {
				tbl.Append([]string{"ERROR", job.ErrorMsg})
			}
			tbl.Render()
			return nil
		},
	}
}

// ── metrics ───────────────────────────────────────────────────────────────────

func newMetricsCmd() *cobra.Command {
	var (
		engine string
		model  string
		since  string
	)
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Query benchmark metrics",
		RunE: func(cmd *cobra.Command, args []string) error {
			sinceTime, err := parseSince(since)
			if err != nil {
				return fmt.Errorf("invalid --since value %q: %w", since, err)
			}

			results, err := apiClient.GetMetrics(cmd.Context(), engine, model, sinceTime)
			if err != nil {
				return err
			}
			if isJSON() {
				return json.NewEncoder(os.Stdout).Encode(results)
			}
			printResultsTable(results)
			return nil
		},
	}
	cmd.Flags().StringVar(&engine, "engine", "", "Engine name")
	cmd.Flags().StringVar(&model, "model", "", "Model name")
	cmd.Flags().StringVar(&since, "since", "24h", "Time window (e.g. 24h, 7d, 1h)")
	return cmd
}

// ── route ─────────────────────────────────────────────────────────────────────

func newRouteCmd() *cobra.Command {
	var (
		promptTokens       int
		outputTokens       int
		concurrency        int
		structuredOutput   bool
		toolCalls          bool
		sharedPrefixRatio  float64
	)
	cmd := &cobra.Command{
		Use:   "route",
		Short: "Classify a workload and get engine recommendation",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := apiClient.ClassifyWorkload(cmd.Context(), router.ClassificationInput{
				PromptTokens:      promptTokens,
				OutputTokens:      outputTokens,
				Concurrency:       concurrency,
				StructuredOutput:  structuredOutput,
				ToolCalls:         toolCalls,
				SharedPrefixRatio: sharedPrefixRatio,
			})
			if err != nil {
				return err
			}
			if isJSON() {
				return json.NewEncoder(os.Stdout).Encode(result)
			}
			fmt.Printf("Workload type:       %s\n", result.WorkloadType)
			fmt.Printf("Recommended engine:  %s\n", result.RecommendedEngine)
			fmt.Printf("Confidence:          %.0f%%\n", result.Confidence*100)
			fmt.Printf("Reasoning:           %s\n", result.Reasoning)
			return nil
		},
	}
	cmd.Flags().IntVar(&promptTokens, "prompt-tokens", 512, "Prompt length in tokens")
	cmd.Flags().IntVar(&outputTokens, "output-tokens", 256, "Max output tokens")
	cmd.Flags().IntVar(&concurrency, "concurrency", 32, "Expected concurrent requests")
	cmd.Flags().BoolVar(&structuredOutput, "structured-output", false, "Requires structured/JSON output")
	cmd.Flags().BoolVar(&toolCalls, "tool-calls", false, "Uses tool/function calls")
	cmd.Flags().Float64Var(&sharedPrefixRatio, "shared-prefix-ratio", 0.0, "Fraction of shared prompt prefix across requests")
	return cmd
}

// ── configure ─────────────────────────────────────────────────────────────────

func newConfigureCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "configure",
		Short: "Configure InferBolt CLI",
		RunE: func(cmd *cobra.Command, args []string) error {
			sc := bufio.NewScanner(os.Stdin)

			fmt.Print("Server URL [http://localhost:8080]: ")
			sc.Scan()
			serverURL := strings.TrimSpace(sc.Text())
			if serverURL == "" {
				serverURL = "http://localhost:8080"
			}

			fmt.Print("API key: ")
			sc.Scan()
			apiKey := strings.TrimSpace(sc.Text())

			fmt.Print("Tenant ID (optional): ")
			sc.Scan()
			tenantID := strings.TrimSpace(sc.Text())

			c := &cli.Config{
				ServerURL: serverURL,
				APIKey:    apiKey,
				TenantID:  tenantID,
				OutputFmt: "table",
			}
			if err := cli.Save(c); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			fmt.Println("Configuration saved to ~/.inferbolt/config.yaml")
			return nil
		},
	}
}

// ── version ───────────────────────────────────────────────────────────────────

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			serverURL := "http://localhost:8080"
			if cfg != nil {
				serverURL = cfg.ServerURL
			} else if serverFlag != "" {
				serverURL = serverFlag
			}

			fmt.Printf("inferbolt version %s\n", version)
			fmt.Printf("server: %s\n", serverURL)

			if apiClient != nil {
				health, err := apiClient.Health(cmd.Context())
				if err != nil {
					fmt.Printf("server status: unreachable (%v)\n", err)
					return nil
				}
				fmt.Printf("server version: %s\n", health.Version)
				fmt.Printf("postgres: %s\n", health.Postgres)
			}
			return nil
		},
	}
}

// ── admin ─────────────────────────────────────────────────────────────────────

func newAdminCmd() *cobra.Command {
	adminCmd := &cobra.Command{Use: "admin", Short: "Administrative commands"}
	apiKeysCmd := &cobra.Command{Use: "apikeys", Short: "Manage API keys"}
	apiKeysCmd.AddCommand(newAdminAPIKeysCreateCmd())
	adminCmd.AddCommand(apiKeysCmd)
	return adminCmd
}

func newAdminAPIKeysCreateCmd() *cobra.Command {
	var (
		tenantID   string
		scopesFlag string
		expiryDays int
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new API key",
		RunE: func(cmd *cobra.Command, args []string) error {
			if tenantID == "" {
				return fmt.Errorf("--tenant-id is required")
			}
			if scopesFlag == "" {
				return fmt.Errorf("--scopes is required")
			}
			scopes := splitCSV(scopesFlag)

			resp, err := apiClient.CreateAPIKey(cmd.Context(), tenantID, scopes, expiryDays)
			if err != nil {
				return err
			}

			if isJSON() {
				return json.NewEncoder(os.Stdout).Encode(resp)
			}

			fmt.Println()
			fmt.Println("⚠  Store this token securely — it will not be shown again")
			fmt.Println()
			fmt.Printf("Token:      %s\n", resp.Token)
			fmt.Printf("Expires at: %s\n", resp.ExpiresAt.Format(time.RFC3339))
			return nil
		},
	}
	cmd.Flags().StringVar(&tenantID, "tenant-id", "", "Tenant ID (required)")
	cmd.Flags().StringVar(&scopesFlag, "scopes", "", "Comma-separated scopes (required)")
	cmd.Flags().IntVar(&expiryDays, "expiry-days", 30, "Days until expiry (1–365)")
	return cmd
}

// ── display helpers ───────────────────────────────────────────────────────────

func printResultsTable(results []jobs.Result) {
	tbl := tablewriter.NewWriter(os.Stdout)
	tbl.SetHeader([]string{"ENGINE", "TTFT P50", "TTFT P99", "TOK/S", "GPU MEM", "COST/MTOK", "KV HIT"})
	tbl.SetBorder(false)
	for _, r := range results {
		tbl.Append([]string{
			r.Engine,
			fmt.Sprintf("%.1f ms", r.TTFTP50Ms),
			fmt.Sprintf("%.1f ms", r.TTFTP99Ms),
			fmt.Sprintf("%.0f", r.TokPerSec),
			fmt.Sprintf("%d MB", r.GPUMemMB),
			fmt.Sprintf("$%.4f", r.CostPerMTok),
			fmt.Sprintf("%.1f%%", r.KVCacheHit*100),
		})
	}
	tbl.Render()
}

func printComparisonTable(results []jobs.Result) {
	if len(results) == 0 {
		fmt.Println("No results available.")
		return
	}

	// Find best/worst indices for each numeric metric
	type metric struct {
		higherBetter bool
		vals         []float64
	}
	metrics := []metric{
		{false, make([]float64, len(results))}, // TTFT P50 - lower better
		{false, make([]float64, len(results))}, // TTFT P99 - lower better
		{true, make([]float64, len(results))},  // TOK/S - higher better
		{false, make([]float64, len(results))}, // GPU MEM - lower better
		{false, make([]float64, len(results))}, // COST/MTOK - lower better
		{true, make([]float64, len(results))},  // KV HIT - higher better
	}
	for i, r := range results {
		metrics[0].vals[i] = r.TTFTP50Ms
		metrics[1].vals[i] = r.TTFTP99Ms
		metrics[2].vals[i] = r.TokPerSec
		metrics[3].vals[i] = float64(r.GPUMemMB)
		metrics[4].vals[i] = r.CostPerMTok
		metrics[5].vals[i] = r.KVCacheHit
	}

	bestIdx := make([]int, len(metrics))
	worstIdx := make([]int, len(metrics))
	for m, met := range metrics {
		best := 0
		worst := 0
		for i, v := range met.vals {
			if met.higherBetter {
				if v > met.vals[best] {
					best = i
				}
				if v < met.vals[worst] {
					worst = i
				}
			} else {
				if v < met.vals[best] {
					best = i
				}
				if v > met.vals[worst] {
					worst = i
				}
			}
		}
		bestIdx[m] = best
		worstIdx[m] = worst
	}

	tbl := tablewriter.NewWriter(os.Stdout)
	tbl.SetHeader([]string{"ENGINE", "TTFT P50", "TTFT P99", "TOK/S", "GPU MEM", "COST/MTOK", "KV HIT"})
	tbl.SetBorder(false)

	for i, r := range results {
		row := []string{
			r.Engine,
			fmt.Sprintf("%.1f ms", r.TTFTP50Ms),
			fmt.Sprintf("%.1f ms", r.TTFTP99Ms),
			fmt.Sprintf("%.0f", r.TokPerSec),
			fmt.Sprintf("%d MB", r.GPUMemMB),
			fmt.Sprintf("$%.4f", r.CostPerMTok),
			fmt.Sprintf("%.1f%%", r.KVCacheHit*100),
		}
		colors := make([]tablewriter.Colors, 8) // ENGINE col + 6 metric cols + padding
		for col := 0; col < len(metrics); col++ {
			switch i {
			case bestIdx[col]:
				colors[col+1] = tablewriter.Colors{tablewriter.FgGreenColor, tablewriter.Bold}
			case worstIdx[col]:
				colors[col+1] = tablewriter.Colors{tablewriter.FgRedColor}
			}
		}
		tbl.Rich(row, colors)
	}
	tbl.Render()

	// Recommendation: engine with highest tok/s
	best := results[0]
	for _, r := range results[1:] {
		if r.TokPerSec > best.TokPerSec {
			best = r
		}
	}
	fmt.Printf("\nRecommendation: use %s — highest throughput at %.0f tok/s ($%.4f/MTok)\n",
		best.Engine, best.TokPerSec, best.CostPerMTok)
}

// ── utility helpers ───────────────────────────────────────────────────────────

func isJSON() bool {
	if outputFlag == "json" {
		return true
	}
	if cfg != nil && cfg.OutputFmt == "json" {
		return true
	}
	return false
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func parseSince(s string) (time.Time, error) {
	// Handle Xd format (e.g. "7d")
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err == nil && days > 0 {
			return time.Now().Add(-time.Duration(days) * 24 * time.Hour), nil
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return time.Time{}, err
	}
	return time.Now().Add(-d), nil
}

