package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/puviya/api-gatling/apigatling"
)

func main() {
	url := flag.String("url", "", "Target URL (required)")
	method := flag.String("method", http.MethodGet, "HTTP method")
	payloadPath := flag.String("payload", "", "Path to payload file")
	totalRequests := flag.Int("requests", 1000, "Total number of requests")
	concurrency := flag.Int("concurrency", 100, "Worker count")
	headersRaw := flag.String("headers", "", "Comma-separated headers: Key: Value, Another: Value")
	timeout := flag.Duration("timeout", 15*time.Second, "HTTP client timeout")
	flag.Parse()

	if strings.TrimSpace(*url) == "" {
		fmt.Fprintln(os.Stderr, "error: --url is required")
		os.Exit(1)
	}

	headers, err := parseHeaders(*headersRaw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid --headers value: %v\n", err)
		os.Exit(1)
	}

	var payload []byte
	if strings.TrimSpace(*payloadPath) != "" {
		payload, err = os.ReadFile(*payloadPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: failed to read payload file: %v\n", err)
			os.Exit(1)
		}
	}

	var lastPrinted atomic.Int64
	progress := func(current, total int) {
		if total <= 0 {
			return
		}

		// Avoid spamming the terminal if callback ticks faster than useful updates.
		prev := int(lastPrinted.Load())
		if current < total && current-prev < maxInt(1, total/200) {
			return
		}
		lastPrinted.Store(int64(current))

		fmt.Printf("\r%s", progressLine(current, total, 24))
	}

	engine, err := apigatling.New(
		*url,
		apigatling.WithMethod(*method),
		apigatling.WithHeaders(headers),
		apigatling.WithConcurrency(*concurrency),
		apigatling.WithTotalRequests(*totalRequests),
		apigatling.WithPayload(payload),
		apigatling.WithProgressCallback(progress),
		apigatling.WithHTTPClient(&http.Client{Timeout: *timeout}),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to create engine: %v\n", err)
		os.Exit(1)
	}

	report, err := engine.Run(context.Background())
	fmt.Print("\n")
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: run finished with context error: %v\n", err)
	}

	fmt.Println("🚀 API-Gatling Load Test Report")
	fmt.Printf("🌐 Target: %s %s\n", strings.ToUpper(*method), *url)
	fmt.Printf("⏱️ Time taken: %.2f seconds\n", report.TotalExecutionTime.Seconds())
	fmt.Printf("⚡ Requests/sec (RPS): %s\n", formatFloatWithCommas(report.RequestsPerSecond, 2))
	fmt.Println("📊 Latency Metrics:")
	fmt.Printf("Avg: %s\n", report.AverageLatency.Round(time.Millisecond))
	fmt.Printf("Min: %s\n", report.MinLatency.Round(time.Millisecond))
	fmt.Printf("Max: %s\n", report.MaxLatency.Round(time.Millisecond))
	fmt.Printf("P99: %s\n", report.P99Latency.Round(time.Millisecond))
	fmt.Println("📈 Status Codes:")
	fmt.Printf("2xx Success: %s\n", formatUintWithCommas(report.Success))
	fmt.Printf("4xx Client Errors: %s\n", formatUintWithCommas(report.ClientErrors))
	fmt.Printf("5xx Server Errors: %s\n", formatUintWithCommas(report.ServerErrors))
	fmt.Printf("💥 Network/Timeout Errors: %s\n", formatUintWithCommas(report.NetworkErrors))
	fmt.Printf("🔀 1xx/3xx Redirects: %s\n", formatUintWithCommas(report.OtherCodes))

	classifiedTotal := report.Success + report.ClientErrors + report.ServerErrors + report.NetworkErrors + report.OtherCodes
	fmt.Printf("✅ Classified Total: %s / %s\n", formatUintWithCommas(classifiedTotal), formatUintWithCommas(report.TotalSent))
}

func parseHeaders(raw string) (map[string]string, error) {
	result := map[string]string{}
	if strings.TrimSpace(raw) == "" {
		return result, nil
	}

	entries := strings.Split(raw, ",")
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid header %q", entry)
		}
		k := strings.TrimSpace(parts[0])
		v := strings.TrimSpace(parts[1])
		if k == "" {
			return nil, fmt.Errorf("empty header key in %q", entry)
		}
		result[k] = v
	}

	return result, nil
}

func progressLine(current, total, width int) string {
	if width < 3 {
		width = 3
	}
	if current < 0 {
		current = 0
	}
	if current > total {
		current = total
	}

	ratio := 0.0
	if total > 0 {
		ratio = float64(current) / float64(total)
	}
	filled := int(math.Round(ratio * float64(width)))
	if filled > width {
		filled = width
	}

	bar := strings.Repeat("=", maxInt(0, filled)) + strings.Repeat(" ", maxInt(0, width-filled))
	if filled > 0 && filled <= width {
		runes := []rune(bar)
		runes[filled-1] = '>'
		bar = string(runes)
	}

	return fmt.Sprintf("[%s] %s / %s requests", bar, formatIntWithCommas(current), formatIntWithCommas(total))
}

func formatIntWithCommas(n int) string {
	return formatUintWithCommas(uint64(n))
}

func formatUintWithCommas(n uint64) string {
	s := strconv.FormatUint(n, 10)
	if len(s) <= 3 {
		return s
	}

	rem := len(s) % 3
	if rem == 0 {
		rem = 3
	}

	var b strings.Builder
	b.Grow(len(s) + len(s)/3)
	b.WriteString(s[:rem])
	for i := rem; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}

	return b.String()
}

func formatFloatWithCommas(v float64, precision int) string {
	base := strconv.FormatFloat(v, 'f', precision, 64)
	parts := strings.SplitN(base, ".", 2)
	whole := parts[0]
	frac := ""
	if len(parts) == 2 {
		frac = parts[1]
	}

	negative := strings.HasPrefix(whole, "-")
	if negative {
		whole = strings.TrimPrefix(whole, "-")
	}

	wholeWithCommas := formatUintWithCommas(parseUintSafe(whole))
	if negative {
		wholeWithCommas = "-" + wholeWithCommas
	}

	if precision <= 0 {
		return wholeWithCommas
	}
	return wholeWithCommas + "." + frac
}

func parseUintSafe(s string) uint64 {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
