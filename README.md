# httpstat

httpstat visualizes `curl(1)` statistics in a way of beauty and clarity.

This is a Go port of the original Python [httpstat](https://github.com/reorx/httpstat) by Xiao Meng. It is a **single-package** Go program that has **no third-party dependencies** and produces the same output and JSON schema as the Python original.

## Features

- **Beautiful terminal output** — timing breakdown of DNS, TCP, TLS, server processing, and content transfer
- **Structured JSON output** — `--format json` / `jsonl` for machine consumption with a stable v1 schema
- **SLO threshold checking** — `--slo total=500,connect=100` exits with code 4 on violation
- **Save results to file** — `--save path.json` for multi-step workflows
- **NO_COLOR support** — respects the [NO_COLOR](https://no-color.org) convention

## Installation

Build from source:

```bash
go build -o httpstat .
```

Or install into your `$GOBIN`:

```bash
go install .
```

## Usage

```bash
./httpstat httpbin.org/get
```

### cURL Options

Because `httpstat` is a wrapper of cURL, you can pass any cURL supported option after the url (except for `-w`, `-D`, `-o`, `-s`, `-S` which are already used by `httpstat`):

```bash
./httpstat httpbin.org/post -X POST --data-urlencode "a=b" -v
```

### Structured Output

Use `--format` (`-f`) to get machine-readable output:

```bash
./httpstat httpbin.org/get --format json
```

```json
{
  "schema_version": 1,
  "url": "httpbin.org/get",
  "ok": true,
  "exit_code": 0,
  "response": {
    "status_line": "HTTP/2 200",
    "status_code": 200,
    "remote_ip": "...",
    "remote_port": "443",
    "headers": {"Content-Type": "application/json", "Server": "nginx", "...": "..."}
  },
  "timings_ms": {
    "dns": 5, "connect": 10, "tls": 15,
    "server": 50, "transfer": 20, "total": 100,
    "namelookup": 5, "initial_connect": 15,
    "pretransfer": 30, "starttransfer": 80
  },
  "speed": { "download_kbs": 1234.5, "upload_kbs": 0.0 },
  "slo": null
}
```

Use `--format jsonl` for compact single-line JSON (useful for log pipelines).

### SLO Thresholds

Check response times against thresholds. Exits with code `4` on violation:

```bash
./httpstat httpbin.org/get --slo total=500,connect=100,ttfb=200
```

Supported keys: `total`, `connect`, `ttfb` (time to first byte), `dns`, `tls`.

In pretty mode, violations are printed in red at the end of the output.
In JSON mode, violations appear in the `slo` field:

```json
{
  "slo": {
    "pass": false,
    "violations": [
      { "key": "total", "threshold_ms": 500, "actual_ms": 823 }
    ]
  }
}
```

### Save Results

Write structured JSON output to a file (works with any `--format`):

```bash
./httpstat httpbin.org/get --save result.json
./httpstat httpbin.org/get --format json --save result.json
```

### Environment Variables

- **`HTTPSTAT_SHOW_BODY`** — show response body (truncated to 1024 bytes). Default `false`.
- **`HTTPSTAT_SHOW_IP`** — show remote and local IP/port. Default `true`.
- **`HTTPSTAT_SHOW_SPEED`** — show download/upload speed. Default `false`.
- **`HTTPSTAT_SAVE_BODY`** — store body in a tmp file. Default `true`.
- **`HTTPSTAT_CURL_BIN`** — path to `curl` binary. Default `curl`.
- **`HTTPSTAT_METRICS_ONLY`** — legacy alias for `--format json`. Default `false`.
- **`HTTPSTAT_DEBUG`** — enable debug logs to stderr. Default `false`.
- **`NO_COLOR`** — when set (to any value) disables colored output.

## Testing

Unit tests (no network):

```bash
make unit
```

End-to-end tests (requires network access):

```bash
make e2e
```

## License

MIT — see [LICENSE](LICENSE).
