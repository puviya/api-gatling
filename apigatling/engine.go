package apigatling

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

var uuidPlaceholder = []byte("{{UUID}}")

type DynamicPayloadFunc func() []byte

type ProgressCallback func(current, total int)

type Option func(*Engine) error

type Engine struct {
	url           string
	method        string
	headers       map[string]string
	concurrency   int
	totalRequests int
	payload       []byte
	dynamic       DynamicPayloadFunc
	progress      ProgressCallback
	client        *http.Client
	hasUUID       bool
}

type counters struct {
	success      atomic.Uint64
	clientErrors atomic.Uint64
	serverErrors atomic.Uint64
	networkErrs  atomic.Uint64
	otherCodes   atomic.Uint64
	totalSent    atomic.Uint64
	totalDone    atomic.Uint64
}

func New(url string, opts ...Option) (*Engine, error) {
	if url == "" {
		return nil, errors.New("url is required")
	}

	e := &Engine{
		url:           url,
		method:        http.MethodGet,
		headers:       make(map[string]string),
		concurrency:   10,
		totalRequests: 1000,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}

	for _, opt := range opts {
		if err := opt(e); err != nil {
			return nil, err
		}
	}

	if e.concurrency <= 0 {
		return nil, errors.New("concurrency must be > 0")
	}
	if e.totalRequests <= 0 {
		return nil, errors.New("total requests must be > 0")
	}
	if e.client == nil {
		return nil, errors.New("http client must not be nil")
	}

	e.hasUUID = bytes.Contains(e.payload, uuidPlaceholder)
	return e, nil
}

func WithMethod(method string) Option {
	return func(e *Engine) error {
		if method == "" {
			return errors.New("method cannot be empty")
		}
		e.method = method
		return nil
	}
}

func WithHeaders(headers map[string]string) Option {
	return func(e *Engine) error {
		for k, v := range headers {
			e.headers[k] = v
		}
		return nil
	}
}

func WithConcurrency(n int) Option {
	return func(e *Engine) error {
		e.concurrency = n
		return nil
	}
}

func WithTotalRequests(n int) Option {
	return func(e *Engine) error {
		e.totalRequests = n
		return nil
	}
}

func WithPayload(payload []byte) Option {
	return func(e *Engine) error {
		e.payload = payload
		return nil
	}
}

func WithDynamicPayload(fn DynamicPayloadFunc) Option {
	return func(e *Engine) error {
		e.dynamic = fn
		return nil
	}
}

func WithProgressCallback(cb ProgressCallback) Option {
	return func(e *Engine) error {
		e.progress = cb
		return nil
	}
}

func WithHTTPClient(client *http.Client) Option {
	return func(e *Engine) error {
		if client == nil {
			return errors.New("http client cannot be nil")
		}
		e.client = client
		return nil
	}
}

func (e *Engine) Run(ctx context.Context) (Report, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	jobs := make(chan struct{}, e.concurrency)
	var wg sync.WaitGroup
	var c counters
	latencies := newLatencyCollector(e.totalRequests)
	start := time.Now()

	stopProgress := make(chan struct{})
	var progressWG sync.WaitGroup
	if e.progress != nil {
		progressWG.Add(1)
		go func() {
			defer progressWG.Done()
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					e.progress(int(c.totalDone.Load()), e.totalRequests)
				case <-stopProgress:
					e.progress(int(c.totalDone.Load()), e.totalRequests)
					return
				}
			}
		}()
	}

	for i := 0; i < e.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.worker(ctx, jobs, &c, latencies)
		}()
	}

sendLoop:
	for i := 0; i < e.totalRequests; i++ {
		select {
		case <-ctx.Done():
			break sendLoop
		case jobs <- struct{}{}:
			c.totalSent.Add(1)
		}
	}
	close(jobs)

	wg.Wait()
	close(stopProgress)
	progressWG.Wait()
	totalExecution := time.Since(start)

	averageLatency, minLatency, maxLatency, p99Latency := computeLatencyStats(latencies.snapshot())
	rps := 0.0
	if totalExecution > 0 {
		rps = float64(c.totalDone.Load()) / totalExecution.Seconds()
	}

	report := Report{
		Success:            c.success.Load(),
		ClientErrors:       c.clientErrors.Load(),
		ServerErrors:       c.serverErrors.Load(),
		NetworkErrors:      c.networkErrs.Load(),
		OtherCodes:         c.otherCodes.Load(),
		TotalSent:          c.totalSent.Load(),
		TotalDone:          c.totalDone.Load(),
		TotalExecutionTime: totalExecution,
		RequestsPerSecond:  rps,
		AverageLatency:     averageLatency,
		MinLatency:         minLatency,
		MaxLatency:         maxLatency,
		P99Latency:         p99Latency,
	}

	if err := ctx.Err(); err != nil {
		return report, err
	}

	return report, nil
}

func (e *Engine) worker(ctx context.Context, jobs <-chan struct{}, c *counters, latencies *latencyCollector) {
	for range jobs {
		e.doRequest(ctx, c, latencies)
	}
}

func (e *Engine) doRequest(ctx context.Context, c *counters, latencies *latencyCollector) {
	startedAt := time.Now()
	defer func() {
		c.totalDone.Add(1)
		latencies.add(time.Since(startedAt))
	}()

	body := e.requestPayload()
	req, err := http.NewRequestWithContext(ctx, e.method, e.url, bytes.NewReader(body))
	if err != nil {
		c.networkErrs.Add(1)
		return
	}

	for k, v := range e.headers {
		req.Header.Set(k, v)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		c.networkErrs.Add(1)
		return
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode <= 299:
		c.success.Add(1)
	case resp.StatusCode >= 400 && resp.StatusCode <= 499:
		c.clientErrors.Add(1)
	case resp.StatusCode >= 500 && resp.StatusCode <= 599:
		c.serverErrors.Add(1)
	default:
		c.otherCodes.Add(1)
	}
}

func (e *Engine) requestPayload() []byte {
	if e.dynamic != nil {
		return e.dynamic()
	}

	if !e.hasUUID {
		return e.payload
	}

	id := uuid.NewString()
	return bytes.Replace(e.payload, uuidPlaceholder, []byte(id), -1)
}
