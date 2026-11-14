# NORCA Mock APM Server - Specification & Architecture Design

## 1. Overview & Goals

**NORCA Mock APM** is a lightweight, single-instance mock server designed for local testing, integration test suites, and CI/CD pipelines.
Its primary objective is to act as a fully functional, standard OpenTelemetry (OTel) gRPC collector endpoint for applications instrumented with OpenTelemetry SDKs, eliminating the need to deploy and manage heavy observability infrastructure during local development.

### Key Highlights
- **License:** MIT License (Open Source, fully permissive).
- **Protocol:** Standard gRPC OTLP (OpenTelemetry Protocol v1).
- **Data Persistence:** Categorized filesystem storage (`logs/`, `metrics/`, `traces/`), structured in separate directories and files.
- **Instance Management:** Strictly single-instance via PID lock and local IPC/admin endpoint.
- **CLI Commands:** Dedicated CLI commands (`start`, `shutdown`, `restart`, `reset`, `status`).
- **Connection Handling:** Standard OTLP gRPC collector compatibility, permissive metadata/headers handling (accepts all tokens/headers and captures them for test assertions).

---

## 2. Architecture & System Design

```mermaid
flowchart TD
    subgraph Client ["Client Applications / Test Harness"]
        App["Application (OTel SDK)"]
        CLI["norca-mock-apm CLI"]
    end

    subgraph Server ["NORCA Mock APM Server (Single Instance)"]
        GRPC["OTLP gRPC Collector (:4317)"]
        Admin["Admin / IPC Controller"]
        Storage["Storage Engine (File Writer)"]
        Lock["PID / Lock Manager"]
    end

    subgraph DataDir ["Data Directory (.norca-data/)"]
        LogsDir["/logs/"]
        MetricsDir["/metrics/"]
        TracesDir["/traces/"]
    end

    App -->|OTLP Logs / Metrics / Traces (gRPC)| GRPC
    CLI -->|restart / shutdown / reset| Admin
    Admin --> Storage
    Admin --> Lock
    GRPC --> Storage
    Storage --> LogsDir
    Storage --> MetricsDir
    Storage --> TracesDir
```

### 2.1. OTLP gRPC Endpoints

The mock server implements standard OpenTelemetry v1 collector proto definitions:
- **Logs:** `opentelemetry.proto.collector.logs.v1.LogsService/Export`
- **Metrics:** `opentelemetry.proto.collector.metrics.v1.MetricsService/Export`
- **Traces:** `opentelemetry.proto.collector.trace.v1.TraceService/Export`

The server enforces an increased message size limit of 64MB (`MaxRecvMsgSize` / `MaxSendMsgSize`) to handle large batches and voluminous stack traces without `ResourceExhausted` errors.

Each incoming request is acknowledged with a successful status code and an empty response (`Export*ServiceResponse`), while the payload is serialized and written to disk.

### 2.2. Connection & Metadata Handling
Telemetry clients (e.g., Go, Python, Java, Node.js, .NET OTel SDKs) connect via standard HTTP/2 gRPC channels:
- **Permissive Ingest:** Any authentication headers (Bearer, Basic token, API key) or custom headers are accepted without rejection, ensuring local test setups run without authentication friction.
- **Metadata Capturing:** The server captures incoming gRPC metadata and attaches it to the written record (optional flag/mode), enabling assertions on client headers during integration tests.

---

## 3. Storage & Directory Layout

Incoming telemetry payloads are separated into category-specific directories in human-readable and machine-parseable formats.

### Directory Structure:
```text
.norca-data/                     # Configurable via --data-dir flag
├── norca.pid                    # PID and IPC port for the running instance
├── logs/
│   ├── current.jsonl            # Active NDJSON file
│   └── logs_2026-11-14T20-00.jsonl
├── metrics/
│   ├── current.jsonl
│   └── metrics_2026-11-14T20-00.jsonl
└── traces/
    ├── current.jsonl
    └── traces_2026-11-14T20-00.jsonl
```

### File Format:
- **NDJSON (Newline-Delimited JSON):** Each incoming batch/request is stored as a single, valid JSON line.
- **Canonical Protobuf-to-JSON Mapping:** Standard OTel field naming conventions (e.g., `traceId`, `spanId`, `name`, `attributes`), making it simple to inspect or assert using tools like `jq` or native test frameworks.
- **Optional Metadata Wrapping:**
  ```json
  {
    "_mock_meta": {
      "received_at": "2026-11-14T20:25:00.123Z",
      "peer_addr": "127.0.0.1:54321",
      "headers": { "authorization": "***" }
    },
    "resourceSpans": [ ... ]
  }
  ```

---

## 4. Command Line Interface (CLI) & Process Control

The server runs strictly as a **single instance**. A lock file and a local admin loopback listener guarantee that only one instance is active per data directory/machine.

### Supported Commands:

1. `norca-mock-apm start`
   - **Description:** Starts the mock APM server on the configured port.
   - **Flags:**
     - `--port, -p`: gRPC listen port (default: `4317`).
     - `--admin-port`: Local admin/IPC control port (default: `4319` or loopback socket).
     - `--data-dir, -d`: Directory where telemetry files are saved (default: `./.norca-data`).
     - `--daemon, -b`: Run in background mode.
     - `--format`: Output format (`jsonl` [default], `raw-proto`).

2. `norca-mock-apm shutdown`
   - **Description:** Reads the running instance PID/admin endpoint, executes a graceful shutdown, flushes file buffers, and removes lock files.

3. `norca-mock-apm restart`
   - **Description:** Gracefully stops the running instance (if running) and launches a fresh instance with the specified configuration.

4. `norca-mock-apm reset`
   - **Description:** Purges all collected records in `logs/`, `metrics/`, and `traces/` without restarting the server or breaking open gRPC connections. Essential for test teardown / cleanup between individual test cases.

5. `norca-mock-apm search [query]`
   - **Description:** Searches collected logs, metrics, and traces using exact substring, regular expressions (`regex:...`), or semantic vector similarity (TF-IDF & Cosine Similarity). Operates via the running instance's Admin API or directly on disk if offline.
   - **Flags:**
     - `--mode, -m`: Search mode: `text` (default), `vector`, or `hybrid`.
     - `--category, -c`: `logs`, `metrics`, `traces`, or `all` (default).
     - `--service, -s`: Filter by `service.name`.
     - `--severity`: Filter by log severity text.
     - `--trace-id`: Filter by traceId.
     - `--min-score`: Minimum relevance threshold for vector similarity.
     - `--limit, -l`: Maximum number of results.

6. `norca-mock-apm status`
   - **Description:** Checks if an instance is running, uptime, active listening port, and counts of collected log, metric, and trace batches.

---

## 5. Technology Stack Recommendation

| Aspect | Go (Golang) - **Recommended** | Rust | Node.js / Python |
| :--- | :--- | :--- | :--- |
| **Distribution** | Single self-contained static binary (Win, Linux, macOS) | Single static binary | Requires runtime environment (Node/Python) |
| **OTel & gRPC Support** | First-party `go.opentelemetry.io/proto/otlp` & `google.golang.org/grpc` | Strong ecosystem, more complex build setups | Good, but higher footprint |
| **Resource Footprint** | Extremely low RAM (~15-25MB), instant startup (<10ms) | Minimal RAM, instant startup | 50-120MB+ RAM, slower startup |
| **Maintainability** | Simple, clear, standard library concurrency | High complexity | Moderate complexity |

> **Recommendation:** **Go (Golang)** with `spf13/cobra` for the CLI and official OTel proto packages provides the fastest developer experience and minimal runtime overhead for local testing.

---

## 6. Verification & Test Strategy

1. **Unit Testing:**
   - Protobuf to canonical JSON serializer tests.
   - File system writer, directory creation, and rotation tests.
2. **Integration Testing (E2E):**
   - Execute dummy OTel client requests using standard OTel SDKs for traces, metrics, and logs.
   - Verify files are created in the respective `logs/`, `metrics/`, and `traces/` folders with valid NDJSON content.
   - Execute `norca-mock-apm reset` and assert directories are cleared while client connections remain healthy.
   - Test `restart` and `shutdown` CLI cycles.
