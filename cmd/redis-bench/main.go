/*
 * MIT License
 * Copyright (c) 2026 Crrow
 */

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/crrow/libxev-go/pkg/redismvp"
	"github.com/crrow/libxev-go/pkg/redisproto"
)

const (
	reportDir              = "benchmarks/reports"
	latestJSON             = "benchmarks/reports/latest.json"
	latestMD               = "benchmarks/reports/latest.md"
	defaultRedisServerPort = 6391
	defaultMVPort          = 6390
)

type operation struct {
	name   string
	weight int
}

type scenario struct {
	name        string
	description string
	mix         []operation
}

type scenarioResult struct {
	Scenario    string  `json:"scenario"`
	Description string  `json:"description"`
	Requests    int     `json:"requests"`
	Concurrency int     `json:"concurrency"`
	DurationMs  float64 `json:"duration_ms"`
	Throughput  float64 `json:"throughput_rps"`
	P50Ms       float64 `json:"p50_ms"`
	P95Ms       float64 `json:"p95_ms"`
	P99Ms       float64 `json:"p99_ms"`
	Errors      int     `json:"errors"`
}

type targetReport struct {
	Target    string           `json:"target"`
	Addr      string           `json:"addr"`
	Scenarios []scenarioResult `json:"scenarios"`
}

type gateConfig struct {
	MinThroughputRatio float64 `json:"min_throughput_ratio"`
	MaxP99Ratio        float64 `json:"max_p99_ratio"`
}

type comparison struct {
	Scenario            string  `json:"scenario"`
	ThroughputRatio     float64 `json:"throughput_ratio"`
	P99Ratio            float64 `json:"p99_ratio"`
	ThroughputPass      bool    `json:"throughput_pass"`
	P99Pass             bool    `json:"p99_pass"`
	OverallPass         bool    `json:"overall_pass"`
	MVPThroughputRPS    float64 `json:"mvp_throughput_rps"`
	RefThroughputRPS    float64 `json:"reference_throughput_rps"`
	MVPP99Ms            float64 `json:"mvp_p99_ms"`
	ReferenceP99Ms      float64 `json:"reference_p99_ms"`
	MVPErrorCount       int     `json:"mvp_error_count"`
	ReferenceErrorCount int     `json:"reference_error_count"`
}

type benchmarkReport struct {
	GeneratedAt time.Time      `json:"generated_at"`
	Requests    int            `json:"requests"`
	Concurrency int            `json:"concurrency"`
	Gates       gateConfig     `json:"gates"`
	Targets     []targetReport `json:"targets"`
	Comparisons []comparison   `json:"comparisons"`
	Command     string         `json:"command"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "compare":
		if err := runCompare(os.Args[2:]); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "bench-compare error: %v\n", err)
			os.Exit(1)
		}
	case "report":
		if err := runReport(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "bench-report error: %v\n", err)
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	_, _ = fmt.Fprintln(os.Stderr, "usage:")
	_, _ = fmt.Fprintln(os.Stderr, "  redis-bench compare --requests 2000 --concurrency 30")
	_, _ = fmt.Fprintln(os.Stderr, "  redis-bench report")
}

func runCompare(args []string) error {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	requests := fs.Int("requests", 2000, "total requests per scenario")
	concurrency := fs.Int("concurrency", 30, "number of concurrent workers")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *requests <= 0 || *concurrency <= 0 {
		return errors.New("requests and concurrency must be > 0")
	}

	scenarios := []scenario{
		{name: "ping_only", description: "100% PING", mix: []operation{{name: "PING", weight: 100}}},
		{name: "read_heavy", description: "70% GET + 30% SET", mix: []operation{{name: "GET", weight: 70}, {name: "SET", weight: 30}}},
		{name: "write_heavy", description: "80% SET + 20% GET", mix: []operation{{name: "SET", weight: 80}, {name: "GET", weight: 20}}},
	}

	mvpServer, err := redismvp.Start(fmt.Sprintf("127.0.0.1:%d", defaultMVPort))
	if err != nil {
		return fmt.Errorf("start mvp redis server failed: %w", err)
	}
	defer func() { _ = mvpServer.Close() }()

	redisServerCmd, err := startReferenceRedis(defaultRedisServerPort)
	if err != nil {
		return err
	}
	defer stopCommand(redisServerCmd)

	mvpAddr := mvpServer.Addr()
	refAddr := fmt.Sprintf("127.0.0.1:%d", defaultRedisServerPort)

	if err = waitUntilReady(mvpAddr, 3*time.Second); err != nil {
		return fmt.Errorf("mvp server not ready: %w", err)
	}
	if err = waitUntilReady(refAddr, 3*time.Second); err != nil {
		return fmt.Errorf("reference redis-server not ready: %w", err)
	}

	mvpResults, err := benchmarkTarget(mvpAddr, "libxev-go-mvp", scenarios, *requests, *concurrency)
	if err != nil {
		return fmt.Errorf("benchmark mvp target failed: %w", err)
	}
	refResults, err := benchmarkTarget(refAddr, "redis-server", scenarios, *requests, *concurrency)
	if err != nil {
		return fmt.Errorf("benchmark reference target failed: %w", err)
	}

	report := benchmarkReport{
		GeneratedAt: time.Now().UTC(),
		Requests:    *requests,
		Concurrency: *concurrency,
		Gates: gateConfig{
			MinThroughputRatio: 0.70,
			MaxP99Ratio:        1.50,
		},
		Targets: []targetReport{
			{Target: "libxev-go-mvp", Addr: mvpAddr, Scenarios: mvpResults},
			{Target: "redis-server", Addr: refAddr, Scenarios: refResults},
		},
		Command: strings.Join(os.Args, " "),
	}
	report.Comparisons = buildComparisons(report.Gates, mvpResults, refResults)

	if err := writeReport(report); err != nil {
		return err
	}
	printComparison(report)
	return nil
}

func runReport() error {
	data, err := os.ReadFile(latestJSON)
	if err != nil {
		return fmt.Errorf("read latest json report failed: %w", err)
	}

	var report benchmarkReport
	if err = json.Unmarshal(data, &report); err != nil {
		return fmt.Errorf("decode latest json report failed: %w", err)
	}

	md := renderMarkdown(report)
	if err = os.WriteFile(latestMD, []byte(md), 0o644); err != nil {
		return fmt.Errorf("write markdown report failed: %w", err)
	}

	ts := report.GeneratedAt.Format("20060102-150405")
	versioned := filepath.Join(reportDir, fmt.Sprintf("report-%s.md", ts))
	if err = os.WriteFile(versioned, []byte(md), 0o644); err != nil {
		return fmt.Errorf("write versioned markdown report failed: %w", err)
	}

	_, _ = fmt.Printf("wrote markdown report: %s\n", latestMD)
	return nil
}

func benchmarkTarget(addr, target string, scenarios []scenario, requests, concurrency int) ([]scenarioResult, error) {
	if err := prewarm(addr, 1000); err != nil {
		return nil, fmt.Errorf("prewarm %s failed: %w", target, err)
	}

	results := make([]scenarioResult, 0, len(scenarios))
	for _, sc := range scenarios {
		res, err := runScenario(addr, sc, requests, concurrency)
		if err != nil {
			return nil, err
		}
		results = append(results, res)
	}
	return results, nil
}

func runScenario(addr string, sc scenario, requests, concurrency int) (scenarioResult, error) {
	jobs := make(chan int, requests)
	for i := 0; i < requests; i++ {
		jobs <- i
	}
	close(jobs)

	var wg sync.WaitGroup
	type workerOut struct {
		latencies []float64
		errors    int
		err       error
	}
	outs := make(chan workerOut, concurrency)

	start := time.Now()
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			rng := rand.New(rand.NewSource(int64(workerID + 99)))
			lat := make([]float64, 0, requests/concurrency+8)
			errorsCount := 0

			for idx := range jobs {
				op := pickOperation(rng, sc.mix)
				key := fmt.Sprintf("bench:key:%d", idx%1000)
				val := fmt.Sprintf("value:%d", idx)

				cmd := []string{op, key}
				switch op {
				case "PING":
					cmd = []string{"PING"}
				case "SET":
					cmd = []string{"SET", key, val}
				}

				t0 := time.Now()
				_, execErr := execOnce(addr, cmd)
				elapsed := time.Since(t0).Seconds() * 1000.0
				lat = append(lat, elapsed)
				if execErr != nil {
					errorsCount++
				}
			}

			outs <- workerOut{latencies: lat, errors: errorsCount}
		}(w)
	}

	wg.Wait()
	close(outs)

	allLat := make([]float64, 0, requests)
	totalErrors := 0
	for out := range outs {
		if out.err != nil {
			return scenarioResult{}, out.err
		}
		allLat = append(allLat, out.latencies...)
		totalErrors += out.errors
	}

	dur := time.Since(start)
	sort.Float64s(allLat)
	res := scenarioResult{
		Scenario:    sc.name,
		Description: sc.description,
		Requests:    requests,
		Concurrency: concurrency,
		DurationMs:  dur.Seconds() * 1000.0,
		Throughput:  float64(requests) / dur.Seconds(),
		P50Ms:       percentile(allLat, 50),
		P95Ms:       percentile(allLat, 95),
		P99Ms:       percentile(allLat, 99),
		Errors:      totalErrors,
	}
	return res, nil
}

func pickOperation(rng *rand.Rand, ops []operation) string {
	total := 0
	for _, op := range ops {
		total += op.weight
	}
	if total <= 0 {
		return "PING"
	}
	pick := rng.Intn(total)
	acc := 0
	for _, op := range ops {
		acc += op.weight
		if pick < acc {
			return op.name
		}
	}
	return ops[len(ops)-1].name
}

func execOnce(addr string, args []string) (redisproto.Value, error) {
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return redisproto.Value{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	cmd := make([]redisproto.Value, 0, len(args))
	for _, arg := range args {
		cmd = append(cmd, redisproto.Value{Kind: redisproto.KindBulkString, Bulk: []byte(arg)})
	}
	wire, err := redisproto.Encode(redisproto.Value{Kind: redisproto.KindArray, Array: cmd})
	if err != nil {
		return redisproto.Value{}, err
	}
	if _, err = conn.Write(wire); err != nil {
		return redisproto.Value{}, err
	}
	return readOneRESP(conn)
}

func readOneRESP(r io.Reader) (redisproto.Value, error) {
	reader := bufio.NewReader(r)
	parser := redisproto.NewParser()

	buf := make([]byte, 4096)
	for {
		n, readErr := reader.Read(buf)
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return redisproto.Value{}, errors.New("connection closed")
			}
			return redisproto.Value{}, readErr
		}
		frames, parseErr := parser.Feed(buf[:n])
		if parseErr != nil {
			return redisproto.Value{}, parseErr
		}
		if len(frames) > 0 {
			return frames[0], nil
		}
	}
}

func prewarm(addr string, keys int) error {
	for i := 0; i < keys; i++ {
		key := fmt.Sprintf("bench:key:%d", i)
		val := fmt.Sprintf("warm:%d", i)
		if _, err := execOnce(addr, []string{"SET", key, val}); err != nil {
			return err
		}
	}
	return nil
}

func startReferenceRedis(port int) (*exec.Cmd, error) {
	bin := os.Getenv("REDIS_SERVER_BIN")
	if bin == "" {
		bin = "redis-server"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("redis-server binary not found; install redis-server or set REDIS_SERVER_BIN: %w", err)
	}

	cmd := exec.Command(bin,
		"--port", fmt.Sprintf("%d", port),
		"--save", "",
		"--appendonly", "no",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func stopCommand(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = cmd.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		_ = cmd.Process.Kill()
	}
}

func waitUntilReady(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := execOnce(addr, []string{"PING"}); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("target %s not ready in %s", addr, timeout)
}

func writeReport(report benchmarkReport) error {
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return fmt.Errorf("create reports dir failed: %w", err)
	}

	blob, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report failed: %w", err)
	}
	if err = os.WriteFile(latestJSON, blob, 0o644); err != nil {
		return fmt.Errorf("write latest report failed: %w", err)
	}
	ts := report.GeneratedAt.Format("20060102-150405")
	versioned := filepath.Join(reportDir, fmt.Sprintf("benchmark-%s.json", ts))
	if err = os.WriteFile(versioned, blob, 0o644); err != nil {
		return fmt.Errorf("write versioned report failed: %w", err)
	}
	_, _ = fmt.Printf("wrote benchmark report: %s\n", latestJSON)
	return nil
}

func buildComparisons(gates gateConfig, mvp, ref []scenarioResult) []comparison {
	refByScenario := make(map[string]scenarioResult, len(ref))
	for _, r := range ref {
		refByScenario[r.Scenario] = r
	}

	out := make([]comparison, 0, len(mvp))
	for _, m := range mvp {
		r, ok := refByScenario[m.Scenario]
		if !ok {
			continue
		}
		thrRatio := 0.0
		if r.Throughput > 0 {
			thrRatio = m.Throughput / r.Throughput
		}
		p99Ratio := 0.0
		if r.P99Ms > 0 {
			p99Ratio = m.P99Ms / r.P99Ms
		}
		thrPass := thrRatio >= gates.MinThroughputRatio
		p99Pass := p99Ratio <= gates.MaxP99Ratio
		out = append(out, comparison{
			Scenario:            m.Scenario,
			ThroughputRatio:     thrRatio,
			P99Ratio:            p99Ratio,
			ThroughputPass:      thrPass,
			P99Pass:             p99Pass,
			OverallPass:         thrPass && p99Pass,
			MVPThroughputRPS:    m.Throughput,
			RefThroughputRPS:    r.Throughput,
			MVPP99Ms:            m.P99Ms,
			ReferenceP99Ms:      r.P99Ms,
			MVPErrorCount:       m.Errors,
			ReferenceErrorCount: r.Errors,
		})
	}
	return out
}

func printComparison(report benchmarkReport) {
	_, _ = fmt.Println("scenario | mvp rps | redis rps | throughput ratio | mvp p99 ms | redis p99 ms | p99 ratio | pass")
	_, _ = fmt.Println("---|---:|---:|---:|---:|---:|---:|---")
	for _, c := range report.Comparisons {
		_, _ = fmt.Printf("%s | %.1f | %.1f | %.3f | %.3f | %.3f | %.3f | %t\n",
			c.Scenario,
			c.MVPThroughputRPS,
			c.RefThroughputRPS,
			c.ThroughputRatio,
			c.MVPP99Ms,
			c.ReferenceP99Ms,
			c.P99Ratio,
			c.OverallPass,
		)
	}
}

func renderMarkdown(report benchmarkReport) string {
	var b strings.Builder
	b.WriteString("# Redis MVP Benchmark Report\n\n")
	_, _ = fmt.Fprintf(&b, "Generated at: %s UTC\\n\\n", report.GeneratedAt.Format(time.RFC3339))
	_, _ = fmt.Fprintf(&b, "Requests per scenario: %d\\n\\n", report.Requests)
	_, _ = fmt.Fprintf(&b, "Concurrency: %d\\n\\n", report.Concurrency)

	b.WriteString("## Scenarios\n\n")
	b.WriteString("- ping_only: 100% PING\n")
	b.WriteString("- read_heavy: 70% GET + 30% SET\n")
	b.WriteString("- write_heavy: 80% SET + 20% GET\n\n")

	b.WriteString("## Gates\n\n")
	_, _ = fmt.Fprintf(&b, "- throughput ratio >= %.2f\\n", report.Gates.MinThroughputRatio)
	_, _ = fmt.Fprintf(&b, "- p99 ratio <= %.2f\\n\\n", report.Gates.MaxP99Ratio)

	b.WriteString("## Comparison\n\n")
	b.WriteString("scenario | mvp rps | redis rps | throughput ratio | mvp p99 ms | redis p99 ms | p99 ratio | pass\n")
	b.WriteString("---|---:|---:|---:|---:|---:|---:|---\n")
	for _, c := range report.Comparisons {
		_, _ = fmt.Fprintf(&b, "%s | %.1f | %.1f | %.3f | %.3f | %.3f | %.3f | %t\\n",
			c.Scenario,
			c.MVPThroughputRPS,
			c.RefThroughputRPS,
			c.ThroughputRatio,
			c.MVPP99Ms,
			c.ReferenceP99Ms,
			c.P99Ratio,
			c.OverallPass,
		)
	}

	b.WriteString("\n## Target Details\n\n")
	for _, target := range report.Targets {
		_, _ = fmt.Fprintf(&b, "### %s (%s)\\n\\n", target.Target, target.Addr)
		b.WriteString("scenario | throughput rps | p50 ms | p95 ms | p99 ms | errors\n")
		b.WriteString("---|---:|---:|---:|---:|---:\n")
		for _, s := range target.Scenarios {
			_, _ = fmt.Fprintf(&b, "%s | %.1f | %.3f | %.3f | %.3f | %d\\n",
				s.Scenario,
				s.Throughput,
				s.P50Ms,
				s.P95Ms,
				s.P99Ms,
				s.Errors,
			)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	idx := int((p / 100.0) * float64(len(sorted)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
