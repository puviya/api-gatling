# 🔫 API-Gatling

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

<img width="1200" height="1200" alt="API-Gatling High-Concurrency Load Testing in Go  (1)" src="https://github.com/user-attachments/assets/acf83480-132d-41a6-81cb-361ca6ec6d32" />


A high-performance, memory-safe HTTP load testing engine written in Go. 

API-Gatling is designed to strictly benchmark backend infrastructure, database write-locks, and system breaking points by bypassing edge caches via dynamic payload templating. It can be used as a standalone **CLI tool** for quick tests, or imported as a **Go Package** for automated CI/CD performance assertions.

---

## 🧠 The Problem vs. The Solution

**The Problem:** Most load testers send the exact same static JSON payload 100,000 times. Backend systems (like Redis, Cloudflare, or DB connection pools) quickly cache the request or reject it as a duplicate. You are testing your RAM/Cache, *not* your database's true Disk I/O or transaction locks.

**The Solution:** API-Gatling uses a bounded Goroutine worker pool combined with a byte-templating engine. By injecting unique UUIDs into your payloads in real-time (`{{UUID}}`), it forces your backend to execute true database inserts on every single request. Furthermore, the payload is loaded into a single `[]byte` slice shared across all workers, keeping local RAM usage under 20MB even at 20,000 requests/sec.

---

## 🚀 Features

- ⚡ **Bounded Concurrency:** Fire hundreds of thousands of requests using a managed Goroutine worker pool.
- 🧬 **Dynamic Payload Templating:** Generate unique payloads in microseconds to bypass HTTP caches.
- 📊 **High-Precision Telemetry:** Tracks Avg, Min, Max, and P99 Latency, alongside RPS (Requests Per Second).
- 💥 **Deep Error Tracking:** Distinguishes between HTTP Status Errors and raw TCP/Network connection drops.
- 📦 **Composable Architecture:** Built using the Functional Options pattern. Use the CLI, or import the engine directly into your Go code.

---

## 🛠 Installation

**To use the CLI:**

1. go install github.com/puviya/api-gatling/cmd/api-gatling@latest
   
2. Run this command to add Go's bin folder to your path
   
   	export PATH=$PATH:$(go env GOPATH)/bin (Mac or Linux)
   
	If you are on Windows:

	You need to add %USERPROFILE%\go\bin to your System Environment Variables (under "Path").

**To use as a Go Package in your project:**
go get github.com/puviya/api-gatling/apigatling

# 💻 Usage 1: The CLI

go install github.com/puviya/api-gatling/cmd/api-gatling@latest

### Basic GET Request (Network/Cache Test)

api-gatling --url "http://localhost:3000/api/users" \
    --requests 100000 \
    --concurrency 500

### Advanced POST Request (Database Stress Test)
Create a payload.json file. Use the {{UUID}} tag anywhere inside it. API-Gatling will dynamically replace this tag with a real UUID for every single request.

{
  "email": "tester_{{UUID}}@mycompany.com",
  "amount": 50
}
Run the load test:
api-gatling --url "http://localhost:3000/api/checkout" \
    --method POST \
    --headers "Content-Type: application/json, Authorization: Bearer token123" \
    --payload ./payload.json \
    --requests 100000 \
    --concurrency 500

#### Sample Output

[=======================>] 100,000 / 100,000 requests

🚀 API-Gatling Load Test Report
--------------------------------------------------
🌐 Target: POST http://localhost:3000/api/checkout
⏱️  Time taken: 34.26 seconds
⚡ Requests/sec (RPS): 2,918.88

📊 Latency Metrics:
   - Avg: 171ms
   - Min: 12ms
   - Max: 2.025s
   - P99: 1.233s

📈 Status Codes:
   - 2xx Success: 99,883
   - 4xx Client Errors: 0
   - 5xx Server Errors: 0
   - 🔀 1xx/3xx Redirects: 0
   - 💥 Network/Timeout Errors: 117
--------------------------------------------------

# ⚙️ Usage 2: The Go Package (CI/CD Automation)
You can import api-gatling into your _test.go files to automate performance regression testing in your CI/CD pipelines (e.g., GitHub Actions).
package myapi_test

	import (
		"context"
		"fmt"
		"math/rand"
		"testing"
		"time"
		"go get github.com/puviya/api-gatling/apigatling"
	)

	func TestCheckoutPerformance(t *testing.T) {
		// 1. Initialize the engine using Functional Options
		engine := apigatling.New(
			"https://staging.mycompany.com/checkout",
			apigatling.WithMethod("POST"),
			apigatling.WithConcurrency(200),
			apigatling.WithTotalRequests(10000),
		
		// 2. Programmatically generate highly complex, dynamic payloads per request
		apigatling.WithDynamicPayload(func() []byte {
			return []byte(fmt.Sprintf(`{"user_id": %d, "amount": 100}`, rand.Intn(999999)))
		}),
	)

	// 3. Fire the engine
	report := engine.Run(context.Background())

	// 4. Assert Performance constraints
	if report.ErrorCount > 0 || report.NetworkErrors > 0 {
		t.Fatalf("Test Failed: Server dropped requests under load")
	}

	if report.P99Latency > 300 * time.Millisecond {
		t.Fatalf("Test Failed: API is too slow! P99 Latency was %v (Limit: 300ms)", report.P99Latency)
	}

	t.Logf("Performance passed! RPS: %.2f | P99: %v", report.RPS, report.P99Latency)
	}

