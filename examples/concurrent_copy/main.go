/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

// concurrent_copy demonstrates and benchmarks concurrent file copy using
// libxev async I/O versus traditional goroutine-based blocking I/O.
//
// This example shows the key difference:
//   - Goroutine approach: N goroutines, each blocking on syscalls
//   - Xev approach: Single event loop, thread pool handles blocking I/O
//
// Usage:
//
//	go run .
//
// The program launches an interactive TUI to select and run benchmarks.
package main

import (
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/joho/godotenv"

	"github.com/crrow/libxev-go/pkg/cxev"
)

// FilePair represents a source and destination path.
type FilePair struct {
	Src string
	Dst string
}

// Scenario represents a benchmark test case.
type Scenario struct {
	Name  string
	Files int
	Size  int64
}

var scenarios = []Scenario{
	{"1000 × 4KB", 1000, 4 * 1024},
	{"200 × 64KB", 200, 64 * 1024},
	{"100 × 256KB", 100, 256 * 1024},
	{"50 × 1MB", 50, 1024 * 1024},
	{"20 × 5MB", 20, 5 * 1024 * 1024},
	{"10 × 10MB", 10, 10 * 1024 * 1024},
	{"5 × 50MB", 5, 50 * 1024 * 1024},
}

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			MarginBottom(1)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262"))

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#7D56F4")).
			Bold(true).
			PaddingLeft(2).
			PaddingRight(2)

	itemStyle = lipgloss.NewStyle().
			PaddingLeft(4)

	resultStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#04B575")).
			MarginTop(1)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF0000"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262")).
			MarginTop(1)
)

type model struct {
	scenarios []Scenario
	cursor    int
	running   bool
	result    string
	err       error
}

type benchmarkMsg struct {
	result string
	err    error
}

func initialModel() model {
	return model{
		scenarios: scenarios,
		cursor:    0,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.running {
			return m, nil
		}

		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+c", "q"))):
			return m, tea.Quit

		case key.Matches(msg, key.NewBinding(key.WithKeys("up", "k"))):
			if m.cursor > 0 {
				m.cursor--
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("down", "j"))):
			if m.cursor < len(m.scenarios) {
				m.cursor++
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("enter", " "))):
			m.running = true
			m.result = ""
			m.err = nil

			if m.cursor == len(m.scenarios) {
				// Run all scenarios
				return m, runAllScenarios
			}
			// Run selected scenario
			return m, runBenchmark(m.scenarios[m.cursor])
		}

	case benchmarkMsg:
		m.running = false
		m.result = msg.result
		m.err = msg.err
		return m, nil
	}

	return m, nil
}

func (m model) View() string {
	if m.running {
		return "\n  Running benchmark...\n\n"
	}

	var b strings.Builder

	b.WriteString(titleStyle.Render("Concurrent File Copy Benchmark"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render(fmt.Sprintf("GOMAXPROCS: %d", runtime.GOMAXPROCS(0))))
	b.WriteString("\n\n")

	// Render menu items
	for i, scenario := range m.scenarios {
		cursor := "  "
		style := itemStyle
		if m.cursor == i {
			cursor = "▶ "
			style = selectedStyle
		}
		b.WriteString(cursor + style.Render(scenario.Name))
		b.WriteString("\n")
	}

	// "Run All" option
	cursor := "  "
	style := itemStyle
	if m.cursor == len(m.scenarios) {
		cursor = "▶ "
		style = selectedStyle
	}
	b.WriteString(cursor + style.Render("Run All Scenarios"))
	b.WriteString("\n")

	// Display result
	if m.result != "" {
		b.WriteString(resultStyle.Render("\n" + m.result))
	}

	if m.err != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("\nError: %v", m.err)))
	}

	// Help
	b.WriteString(helpStyle.Render("\n\nUp/Down: Navigate • Enter: Run • q: Quit"))

	return b.String()
}

func runBenchmark(scenario Scenario) tea.Cmd {
	return func() tea.Msg {
		// Check xev availability
		if !cxev.ExtLibLoaded() {
			return benchmarkMsg{
				err: fmt.Errorf("libxev extended library not loaded. Run 'just build-extended'"),
			}
		}

		// Setup test files
		srcDir, dstDir, pairs, err := setupTestFiles(scenario.Files, scenario.Size)
		if err != nil {
			return benchmarkMsg{err: fmt.Errorf("setup failed: %w", err)}
		}
		defer os.RemoveAll(srcDir)
		defer os.RemoveAll(dstDir)

		// Run xev benchmark
		xevDuration, err := benchmarkXev(pairs)
		if err != nil {
			return benchmarkMsg{err: fmt.Errorf("xev copy failed: %w", err)}
		}

		// Clean dst for goroutine run
		cleanDstDir(dstDir, pairs)

		// Run goroutine benchmark
		goroutineDuration, err := benchmarkGoroutine(pairs, 0)
		if err != nil {
			return benchmarkMsg{err: fmt.Errorf("goroutine copy failed: %w", err)}
		}

		// Verify copied files
		if err := verifyFiles(pairs); err != nil {
			return benchmarkMsg{err: fmt.Errorf("verification failed: %w", err)}
		}

		// Calculate results
		totalSize := scenario.Size * int64(scenario.Files)
		xevThroughput := float64(totalSize) / xevDuration.Seconds() / 1024 / 1024
		goroutineThroughput := float64(totalSize) / goroutineDuration.Seconds() / 1024 / 1024

		var winner string
		if xevDuration < goroutineDuration {
			speedup := float64(goroutineDuration) / float64(xevDuration)
			winner = fmt.Sprintf("xev %.2fx faster", speedup)
		} else {
			speedup := float64(xevDuration) / float64(goroutineDuration)
			winner = fmt.Sprintf("goroutine %.2fx faster", speedup)
		}

		result := fmt.Sprintf(
			"%s\n  xev:       %v (%.2f MB/s)\n  goroutine: %v (%.2f MB/s)\n  Winner: %s",
			scenario.Name,
			xevDuration.Round(time.Millisecond),
			xevThroughput,
			goroutineDuration.Round(time.Millisecond),
			goroutineThroughput,
			winner,
		)

		return benchmarkMsg{result: result}
	}
}

func runAllScenarios() tea.Msg {
	// Find project root (look for justfile)
	dir, err := os.Getwd()
	if err != nil {
		return benchmarkMsg{err: fmt.Errorf("get working directory: %w", err)}
	}

	// Walk up to find justfile
	for {
		if _, err := os.Stat(filepath.Join(dir, "justfile")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return benchmarkMsg{err: fmt.Errorf("justfile not found")}
		}
		dir = parent
	}

	// Run the just command
	cmd := exec.Command("just", "example-concurrent-copy-bench")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return benchmarkMsg{
			result: string(output),
			err:    fmt.Errorf("benchmark failed: %w", err),
		}
	}

	return benchmarkMsg{result: string(output)}
}

func main() {
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	loadDotEnv()
}

func loadDotEnv() {
	var paths []string

	if exampleDir, err := exampleDir(); err == nil {
		paths = append(paths, filepath.Join(exampleDir, ".env"))
		if root, err := findRepoRoot(exampleDir); err == nil && root != exampleDir {
			paths = append(paths, filepath.Join(root, ".env"))
		}
	} else if cwd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(cwd, ".env"))
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			_ = godotenv.Load(path)
		}
	}
}

func exampleDir() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime caller unavailable")
	}
	return filepath.Dir(filename), nil
}

func findRepoRoot(start string) (string, error) {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "justfile")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("repo root not found")
		}
		dir = parent
	}
}

func setupTestFiles(numFiles int, fileSize int64) (srcDir, dstDir string, pairs []FilePair, err error) {
	srcDir, err = os.MkdirTemp("", "copy_bench_src_*")
	if err != nil {
		return "", "", nil, err
	}

	dstDir, err = os.MkdirTemp("", "copy_bench_dst_*")
	if err != nil {
		os.RemoveAll(srcDir)
		return "", "", nil, err
	}

	pairs = make([]FilePair, numFiles)
	data := make([]byte, fileSize)

	for i := 0; i < numFiles; i++ {
		// Generate random data for each file
		if _, err := rand.Read(data); err != nil {
			os.RemoveAll(srcDir)
			os.RemoveAll(dstDir)
			return "", "", nil, fmt.Errorf("generate random data: %w", err)
		}

		srcPath := filepath.Join(srcDir, fmt.Sprintf("file_%04d.bin", i))
		dstPath := filepath.Join(dstDir, fmt.Sprintf("file_%04d.bin", i))

		if err := os.WriteFile(srcPath, data, 0644); err != nil {
			os.RemoveAll(srcDir)
			os.RemoveAll(dstDir)
			return "", "", nil, fmt.Errorf("write source file: %w", err)
		}

		pairs[i] = FilePair{Src: srcPath, Dst: dstPath}
	}

	return srcDir, dstDir, pairs, nil
}

func cleanDstDir(dstDir string, pairs []FilePair) {
	for _, pair := range pairs {
		os.Remove(pair.Dst)
	}
}

func benchmarkXev(pairs []FilePair) (time.Duration, error) {
	copier, err := NewXevCopier()
	if err != nil {
		return 0, err
	}
	defer copier.Close()

	start := time.Now()
	err = copier.CopyFiles(pairs)
	return time.Since(start), err
}

func benchmarkGoroutine(pairs []FilePair, maxWorkers int) (time.Duration, error) {
	// Don't use Sync() for fair comparison - xev doesn't sync either
	copier := NewGoroutineCopier(maxWorkers, false)

	start := time.Now()
	err := copier.CopyFiles(pairs)
	return time.Since(start), err
}

func verifyFiles(pairs []FilePair) error {
	for _, pair := range pairs {
		srcInfo, err := os.Stat(pair.Src)
		if err != nil {
			return fmt.Errorf("stat src %s: %w", pair.Src, err)
		}

		dstInfo, err := os.Stat(pair.Dst)
		if err != nil {
			return fmt.Errorf("stat dst %s: %w", pair.Dst, err)
		}

		if srcInfo.Size() != dstInfo.Size() {
			return fmt.Errorf("size mismatch: %s (%d) vs %s (%d)",
				pair.Src, srcInfo.Size(), pair.Dst, dstInfo.Size())
		}
	}
	return nil
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
