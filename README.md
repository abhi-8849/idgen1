# idgen

A small Go proof of concept of a **Unique ID Generator**: 64-bit, unique, roughly time-sortable IDs in the style of Twitter's Snowflake.

> **Status:** work in progress. The package layout is in place; the generator, service, client and deployment files are being built step by step. Sections below describe the target design.

## Why

- IDs are generated locally, with no database round trip.
- IDs fit in 64 bits, so they work as a primary key and sort roughly by creation time.
- No two nodes issue the same ID, as long as each node owns a unique `(datacenter, worker)` pair.

## ID layout

```
 0 | 41 bits: ms since epoch | 5 bits: datacenter | 5 bits: worker | 12 bits: sequence
```

| Bits | Field         | Range                                |
|------|---------------|--------------------------------------|
| 1    | sign          | always 0                             |
| 41   | timestamp     | milliseconds since epoch, about 69 years |
| 5    | datacenter id | 0–31                                 |
| 5    | worker id     | 0–31                                 |
| 12   | sequence      | 0–4,095 per millisecond              |

```
id = (nowMs - epoch) << 22 | dc << 17 | worker << 12 | seq
```

The default epoch is `2026-01-01T00:00:00Z`. One worker can issue up to 4,096 IDs per millisecond, and the layout supports 1,024 workers in total.

## Generation rules

1. Read the current time in milliseconds.
2. If the worker id lease is lost, return `ErrLeaseLost`.
3. If the clock moved backwards, wait for it to catch up when the gap is small (default 5 ms). Otherwise return `ErrClockBackwards`. An ID with an older timestamp is never issued.
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

## Worker ids

- **Static:** `dc` and `worker` come from flags. Use it for a single node or local runs.
- **etcd:** each node claims `/idgen/workers/{dc}/{worker}` under a 10 s lease with a create-if-absent transaction, so nodes get different ids automatically. If no keep-alive succeeds for 7 s, the node stops issuing IDs before the lease can expire and another node can take its id.

## HTTP service

| Endpoint                 | Description                                                         |
|--------------------------|---------------------------------------------------------------------|
| `GET /v1/ids?count=n`    | Returns `{"ids":["..."]}`. Default 1, max 1,000. IDs are strings, because JavaScript loses precision above 2^53. A bad `count` returns 400. A backward clock or lost lease returns 503. |
| `GET /healthz`           | `ok`, or 503 if the lease is lost.                                  |
| `GET /debug/vars`        | expvar counters.                                                    |

Flags: `-addr :8080`, `-dc 0`, `-worker 0`, `-allocator static|etcd`, `-etcd localhost:2379`.

Counters: `ids_issued_total`, `clock_backward_total`, `refusals_total`, `seq_overflow_waits_total`.

## Client

`client.New(nodes).NextID(ctx)` serves IDs from a local buffer. When the buffer is empty it fetches 100 IDs from a node. On error it tries the next node, and returns the error if all fail.

## Build

Requires Go 1.22 or newer.

```sh
go build ./... && go vet ./...
```

Run commands for the service, Docker and Kubernetes will be added here as those parts are built.

## Out of scope

UUIDs, short codes, strict global ordering, gapless IDs, secret IDs, persisting the last timestamp, gRPC, Prometheus, highly available etcd, Helm, CI.
