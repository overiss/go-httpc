# go-httpc

High-performance HTTP client for Go with:

- **Circuit breaker** ([sony/gobreaker](https://github.com/sony/gobreaker))
- **Round-robin** load balancing across multiple `BaseURLs`
- **Context cancellation** (same model as pgx/mongo drivers)
- **GET / POST / PUT / PATCH / DELETE** helpers
- Default and per-request headers
- Two APIs: a long-lived **`Client`** and lightweight **`Simple*`** one-off calls

Requires **Go 1.26+**.

## Layout

```
go-httpc/
  httpc.go              # public API (import github.com/overiss/go-httpc)
  client/               # long-lived Client, Config, RequestParams, hooks
  simple/               # one-off Simple* calls
  internal/engine/      # HTTP executor, circuit breaker, load balancing
```

## Install

```bash
go get github.com/overiss/go-httpc
```

## Configured Client

Use when the client is created once (DI, service layer) and reused:

```go
import (
    "context"
    "net/http"
    "time"

    httpc "github.com/overiss/go-httpc"
    "github.com/sony/gobreaker"
)

func main() {
    client, err := httpc.New(httpc.Config{
        BaseURLs: []string{
            "https://api-1.example.com",
            "https://api-2.example.com",
        },
        DefaultHeaders: httpc.Headers{
            "Authorization": "Bearer <token>",
        },
        Timeout:        5 * time.Second,
        CircuitBreaker: &gobreaker.Settings{
            Name:        "payments-api",
            MaxRequests: 3,
            Interval:    10 * time.Second,
            Timeout:     30 * time.Second,
            ReadyToTrip: func(c gobreaker.Counts) bool {
                return c.ConsecutiveFailures >= 5
            },
        },
    })
    if err != nil {
        panic(err)
    }

    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()

    resp, err := client.Get(ctx, "/v1/users", httpc.RequestParams{
        Headers: httpc.Headers{"X-Request-ID": "req-42"},
        Query:   httpc.Query{"limit": {"10"}},
    })
    if err != nil {
        // context.Canceled, ErrCircuitOpen, network errors
        return
    }
    if resp.OK() {
        _ = resp.Body
    }
}
```

POST with a body:

```go
body := []byte(`{"name":"alice"}`)
resp, err := client.Post(ctx, "/v1/users", body, httpc.RequestParams{
    Headers: httpc.Headers{"Content-Type": "application/json"},
})
```

Also available: `Put`, `Patch`, `Delete`, and `Do` for arbitrary methods.

### Propagating incoming request data

Copy selected headers and query from an `*http.Request` (e.g. gateway → upstream):

```go
var params httpc.RequestParams
params.WithReqParams(incoming, httpc.ReqCopyOptions{
    CopyHeaders:  true,
    HeaderKeys:   []string{"X-Request-ID", "X-Trace-ID"},
    CopyQuery:    true,
    QueryKeys:    []string{"limit", "cursor"},
    SkipHopByHop: true,
})
resp, err := client.Get(ctx, "/v1/users", params)
```

`ReqCopyOptions`:

| Field | Meaning |
|-------|---------|
| `CopyHeaders` | Copy header fields from `r` |
| `CopyQuery` | Copy URL query from `r.URL` |
| `HeaderKeys` | Allow-list of headers; empty = all (when `CopyHeaders`) |
| `QueryKeys` | Allow-list of query keys; empty = full query (when `CopyQuery`) |
| `SkipHopByHop` | Skip `Connection`, `Transfer-Encoding`, etc. |

Headers already set on `RequestParams` win over copied values.

## Simple API (one-off requests)

No `Client` construction — pass settings at call time:

```go
resp, err := httpc.SimpleGet(ctx, httpc.SimpleParams{
    URL:     "https://httpbin.org/get",
    Headers: httpc.Headers{"User-Agent": "my-app/1.0"},
    Timeout: 3 * time.Second,
})

payload := []byte(`{"ok":true}`)
resp, err = httpc.SimplePost(ctx, httpc.SimpleParams{
    URL: "https://httpbin.org/post",
}, payload)
```

Helpers: `SimpleGet`, `SimplePost`, `SimplePut`, `SimplePatch`, `SimpleDelete`.

## Request cancellation

Every method accepts `context.Context`. Cancelling the context aborts the in-flight request via `net/http`:

```go
ctx, cancel := context.WithCancel(context.Background())
go func() {
    time.Sleep(100 * time.Millisecond)
    cancel()
}()
_, err := client.Get(ctx, "/slow") // err == context.Canceled
```

## Load balancing

With multiple `BaseURLs`, each request uses the next **healthy** host in round-robin order (lock-free, `atomic`). Paths are relative to the chosen base, e.g. `"/v1/items"` → `https://api-1.example.com/v1/items`.

## Circuit breaker

There is a **separate circuit breaker per upstream host** (breaker name = base URL). When a host’s breaker is **open**, the load balancer **skips** it on round-robin. If a call to one host fails, the client tries the next healthy host in the same request (failover).

When every host is open, calls return `httpc.ErrCircuitOpen`. Transport failures count toward the per-host breaker. HTTP 5xx responses are still returned to the caller; tune `gobreaker.Settings` (e.g. `ReadyToTrip`) if you need a different policy. Health-check failures do not trip the breaker.

## Health check

Validate responses after a successful HTTP round-trip. Built-in checks:

```go
httpc.HealthCheckOK200  // status == 200
httpc.HealthCheckOK2xx  // 2xx
```

Set a default on the client or override per request:

```go
client, _ := httpc.New(httpc.Config{
    BaseURLs:    []string{"https://api.example.com"},
    HealthCheck: httpc.HealthCheckOK2xx,
})

resp, err := client.Get(ctx, "/v1/items", httpc.RequestParams{
    HealthCheck: httpc.HealthCheckOK200, // overrides client default
})
if errors.Is(err, httpc.ErrHealthCheck) {
    // resp is still populated (status, body, headers)
}
```

`SimpleParams.HealthCheck` works the same for one-off calls.

## Hooks (configured Client only)

```go
client, _ := httpc.New(httpc.Config{
    BaseURLs: []string{"https://api.example.com"},
    Timeout:  5 * time.Second,
    Hooks: httpc.Hooks{
        OnTimeout: func(ctx context.Context, e httpc.TimeoutEvent) {
            // e.Source: "context" | "client"
        },
        OnRequestError: func(ctx context.Context, e httpc.RequestErrorEvent) {
            // connection refused, DNS, etc.
        },
        OnCircuitBreaker: func(e httpc.CircuitBreakerEvent) {
            // e.Rejected == true when call blocked (ErrCircuitOpen)
            // or state transition via e.From / e.To
        },
        OnHealthCheckFailed: func(ctx context.Context, e httpc.HealthCheckEvent) {
            // e.Response contains the upstream reply
        },
    },
})
```

Hooks must be fast and non-blocking. Context cancellation is not reported. The Simple API does not support hooks.

## Lint & CI

```bash
make test    # go test -race
make lint    # golangci-lint
```

GitHub Actions runs tests and `golangci-lint` on push/PR.

## Performance notes

- Reuses `http.Transport` with an idle connection pool when `Config.Transport` is nil
- Lock-free round-robin
- Pooled response body reads; default cap **16 MiB** via `MaxResponseBytes`
- Default headers compiled once at `New()`
- Use **`Client`** for hot paths; **`Simple*`** creates a new `http.Client` per call
