# idgen

A small Go implementation of a **Unique ID Generator**: 64-bit, unique, roughly time-sortable IDs in the style of Twitter's Snowflake.

## Contents

[Why](#why) · [ID layout](#id-layout) · [Generation rules](#generation-rules) · [Project layout](#project-layout) · [Quick start](#quick-start) ·
[Library usage](#library-usage) · [Worker ids](#worker-ids) · [HTTP service](#http-service) · [Client](#client) · [Docker](#docker) · [Kubernetes](#kubernetes) ·
[Observability](#observability) · [Testing](#testing) · [Out of scope](#out-of-scope)

## Why

- IDs are generated locally, with no database round trip.
- IDs fit in 64 bits, so they work as a primary key and sort roughly by creation time.
- No two nodes issue the same ID, as long as each node owns a unique `(datacenter, worker)` pair.

## ID layout

```
 0 | 41 bits: ms since epoch | 5 bits: datacenter | 5 bits: worker | 12 bits: sequence
```

| Bits | Field         | Range                                    |
|------|---------------|------------------------------------------|
| 1    | sign          | always 0                                 |
| 41   | timestamp     | milliseconds since epoch, about 69 years |
| 5    | datacenter id | 0–31                                     |
| 5    | worker id     | 0–31                                     |
| 12   | sequence      | 0–4,095 per millisecond                  |

```
id = (nowMs - epoch) << 22 | dc << 17 | worker << 12 | seq
```

The default epoch is `2026-01-01T00:00:00Z`. One worker can issue up to 4,096 IDs per millisecond, and the layout supports 1,024 workers in total.

## Generation rules

1. Read the current time in milliseconds.
2. If the worker id lease is lost, return `ErrLeaseLost`.
3. If the clock moved backwards, wait for it to catch up when the gap is small (default 5 ms). Otherwise return `ErrClockBackwards`.
   An ID with an older timestamp is never issued.
4. Same millisecond as the last ID: increment the sequence. If it wraps, wait for the next millisecond.
5. New millisecond: reset the sequence to 0.
6. If the timestamp no longer fits in 41 bits, return `ErrEpochExhausted`.

A single mutex guards the last timestamp and the sequence.

## Project layout

```
snowflake/       reusable library, no dependencies
workerid/        worker id leases: static, or etcd with self-fencing
  etcd/
server/          HTTP handlers and expvar metrics
client/          batch prefetch and failover
cmd/idserver/    server binary: flags, wiring, graceful shutdown
deploy/          Dockerfile, docker-compose.yml, Kubernetes manifests
```

## Quick start

Requires Go 1.26 or newer (see `go.mod`).

```sh
go build ./... && go vet ./...
go run ./cmd/idserver                       # static ids: -dc 0 -worker 0, listens on :8080
curl "localhost:8080/v1/ids?count=3"        # {"ids":["...","...","..."]}
curl localhost:8080/healthz                 # ok
curl localhost:8080/debug/vars              # the four counters
```

## Library usage

```go
import "github.com/abhi-8849/idgen1/snowflake"

gen, err := snowflake.New(snowflake.Config{DC: 1, Worker: 2}, nil) // nil clock = system clock
id, err := gen.NextID(ctx)
parts := gen.Decode(id) // parts.Time, parts.DC, parts.Worker, parts.Seq
```

`Config.Lost` (closed when the worker id lease is lost) and `Config.Hooks` (counters and logs) are optional.

## Worker ids

- **Static:** `dc` and `worker` come from flags. Use it for a single node or local runs.
- **etcd:** each node claims `/idgen/workers/{dc}/{worker}` under a 10 s lease with a create-if-absent transaction, so nodes get different ids
  automatically. If no keep-alive succeeds for 7 s, the node stops issuing IDs before the lease can expire and another node can take its id.

## HTTP service

| Endpoint              | Description                                                                                          |
|-----------------------|------------------------------------------------------------------------------------------------------|
| `GET /v1/ids?count=n` | Returns `{"ids":["..."]}`. Default 1, max 1,000. IDs are strings (JavaScript loses precision above 2^53). |
| `GET /healthz`        | `ok`, or 503 if the lease is lost.                                                                   |
| `GET /debug/vars`     | expvar counters.                                                                                     |

Status codes for `/v1/ids`: 400 for a bad `count`, 503 for a backward clock or a lost lease.

Flags: `-addr :8080`, `-dc 0`, `-worker 0`, `-allocator static|etcd`, `-etcd localhost:2379`.
With `-allocator etcd` the worker id comes from etcd and `-worker` is ignored.

## Client

`client.New(nodes).NextID(ctx)` serves IDs from a local buffer. When the buffer is empty it fetches 100 IDs from a node.
On error it tries the next node, and returns the error if all fail.

```go
c := client.New([]string{"http://localhost:8081", "http://localhost:8082"})
id, err := c.NextID(ctx)
```

## Docker

```sh
docker compose -f deploy/docker-compose.yml up --build   # one etcd and two idservers
curl localhost:8081/v1/ids; curl localhost:8082/v1/ids   # different worker bits
```

## Kubernetes

```sh
docker build -f deploy/Dockerfile -t idserver:latest .   # then make the image visible to your cluster (kind/minikube: load it)
kubectl apply -f deploy/k8s                              # etcd (single replica, not highly available) + 3 idserver pods
kubectl port-forward svc/idserver 8080:8080
curl "localhost:8080/v1/ids?count=3"
```

## Observability

- **Counters** at `/debug/vars`: `ids_issued_total`, `clock_backward_total`, `refusals_total`, `seq_overflow_waits_total`.
- **Logs** (`log/slog`): server start and stop, clock went backwards, request refused, lease lost.
- **Health:** `/healthz` returns 503 once the lease is lost, and the server then exits so Docker or Kubernetes restarts it with a fresh worker id.
- **Metrics hook:** counters plug in through the `snowflake.Hooks` interface, so Prometheus could replace expvar.

## Testing

```sh
go test -race ./...
```

Unit tests cover the generator, the HTTP server and the client.

## Out of scope

UUIDs, short codes, strict global ordering, gapless IDs, secret IDs, persisting the last timestamp, gRPC, Prometheus, highly available etcd, Helm, CI.
