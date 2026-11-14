# NORCA Mock APM Server

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/bacanoo-io/norca-mock-apm)](https://goreportcard.com/report/github.com/bacanoo-io/norca-mock-apm)

A lightweight, single-instance mock APM server for local development, integration testing, and CI/CD pipelines. It receives OpenTelemetry (OTel) **logs**, **metrics**, and **traces** over standard **gRPC OTLP (v1)** on a preconfigured port (default `:4317`) and persists them into separate directories and files for straightforward inspection and assertions.

---

## Features

- **Standard OTLP/gRPC Ingest:** Full support for `LogsService`, `MetricsService`, and `TraceService` endpoints.
- **Permissive Metadata Handling:** Accepts arbitrary authorization tokens, headers, and metadata, capturing them alongside telemetry for test validation.
- **Categorized Local Storage:** Saves incoming requests into distinct directories (`logs/`, `metrics/`, `traces/`) in Newline-Delimited JSON (NDJSON) format with canonical OTel field names.
- **Single-Instance Lifecycle Control:** Robust CLI commands to manage the server lifecycle: `start`, `shutdown`, `restart`, `reset`, and `status`.
- **Zero Runtime Dependencies:** Compiles into a single standalone binary for Windows, Linux, and macOS.
- **Open Source:** Released under the permissive **MIT License**.

---

## Installation & Build

Requires **Go 1.25+**.

```bash
# Clone the repository
git clone https://github.com/bacanoo-io/norca-mock-apm.git
cd norca-mock-apm

# Build the binary
go build -o norca-mock-apm ./cmd/norca-mock-apm
```

---

## CLI Usage

### 1. Start the Server
Start in foreground:
```bash
norca-mock-apm start -p 4317 --admin-port 4319 -d ./.norca-data
```

Start as a background daemon:
```bash
norca-mock-apm start -p 4317 --admin-port 4319 -d ./.norca-data --daemon
```

### 2. Check Server Status
```bash
norca-mock-apm status -d ./.norca-data
```
Output:
```text
==================================================
  NORCA Mock APM Server - Status
==================================================
  Status:      RUNNING
  PID:         35924
  gRPC Port:   :4317
  Admin Port:  127.0.0.1:4318
  Data Dir:    /path/to/.norca-data
  Uptime:      45.2s
--------------------------------------------------
  Collected Telemetry Batches:
    Logs:      12
    Metrics:   45
    Traces:    30
    Last Seen: 2026-11-14T20:40:00Z
==================================================
```

### 3. Reset Collected Telemetry
Clears all saved log, metric, and trace files and resets counters without interrupting active connections:
```bash
norca-mock-apm reset -d ./.norca-data
```

### 4. Restart the Server
Gracefully stops any active instance and starts a fresh one:
```bash
norca-mock-apm restart -d ./.norca-data
```

### 5. Shut Down the Server
Gracefully terminates the running mock APM server:
```bash
norca-mock-apm shutdown -d ./.norca-data
```

### 6. Complex Text & Vector Search
Search through collected telemetry using text matching, regular expressions, or semantic vector similarity (TF-IDF & Cosine Similarity):

#### CLI Search:
```bash
# Vector / Semantic similarity search
norca-mock-apm search "database connection timeout" --mode vector -d ./.norca-data

# Regex search on traces
norca-mock-apm search "regex:status_code\":\s*500" --category traces -d ./.norca-data

# Service and severity filtered search
norca-mock-apm search "payment error" --service billing-service --severity ERROR -d ./.norca-data
```

#### HTTP API Search:
```bash
# Vector search via POST /api/v1/search
curl -X POST http://localhost:4319/api/v1/search \
  -H "Content-Type: application/json" \
  -d '{
    "query": "failed connection to postgres database",
    "mode": "vector",
    "category": "logs",
    "min_score": 0.1,
    "limit": 10
  }'
```

---

### 7. Test Assertion & Simple Query API
Query collected records directly over HTTP for fast assertions in automated test frameworks:
```bash
# Query latest logs
curl http://localhost:4319/api/v1/logs?limit=50

# Query latest traces
curl http://localhost:4319/api/v1/traces?limit=10

# Query latest metrics
curl http://localhost:4319/api/v1/metrics?limit=10
```

Response format:
```json
{
  "category": "traces",
  "count": 1,
  "records": [
    {
      "_mock_meta": { "received_at": "...", "headers": { ... } },
      "resourceSpans": [ ... ]
    }
  ]
}
```

---

## Directory & Data Layout

By default, data is saved into `./.norca-data` (configurable via `-d, --data-dir`):

```text
.norca-data/
├── norca.pid               # JSON file containing PID, listening ports, and startup timestamp
├── daemon.log              # Logs generated in background mode
├── logs/
│   └── current.jsonl       # NDJSON records for received logs
├── metrics/
│   └── current.jsonl       # NDJSON records for received metrics
└── traces/
    └── current.jsonl       # NDJSON records for received traces
```

Each record in `current.jsonl` contains the standard OTel protobuf canonical JSON representation, wrapped with captured metadata:

```json
{
  "_mock_meta": {
    "received_at": "2026-11-14T20:25:00.123Z",
    "peer_addr": "127.0.0.1:54321",
    "headers": {
      "authorization": "Bearer dummy-token",
      "user-agent": "opentelemetry-go"
    }
  },
  "resourceSpans": [ ... ]
}
```

---

## Client Configuration Example

Configure any OpenTelemetry SDK to export to NORCA Mock APM:

### Environment Variables
```bash
export OTEL_EXPORTER_OTLP_ENDPOINT="http://localhost:4317"
export OTEL_EXPORTER_OTLP_PROTOCOL="grpc"
export OTEL_EXPORTER_OTLP_INSECURE="true"
```

---

## Running Tests

Unit and end-to-end integration tests are included:

```bash
go test -v ./...
```

---

## License

This project is licensed under the [MIT License](LICENSE).

