# Dwellm8 API

One Go API for every client — six mobile apps, three web consoles and the
public listing site. A modular monolith per
[ADR-0001](../../docs/adr/0001-modular-monolith-api.md): eight modules, one
database, one transaction boundary.

```
cmd/api/                 main, wiring, graceful shutdown
internal/
  identity/ property/ lease/ money/
  maintenance/ community/ discovery/ notify/
    http/     handlers, request and response types
    service/  this module's public Go interface — the extraction seam
    domain/   aggregates and rules, no framework imports
    store/    SQL for this module's tables only
    events/   what it publishes and subscribes to
  platform/   config, health, telemetry, and the boundary tests
```

## The rule that matters

A module owns its tables and is the only writer of them. Other modules reach it
through `service/`, never through `store/`. Enforced three ways:

1. `internal/platform/arch` — a test that fails the build on a cross-module
   import. Prove it still bites with `internal/platform/arch/violation_check.sh`.
2. Per-module PostgreSQL roles in the schema bootstrap.
3. Row-level security on `tenant_id`, as the second line rather than the first.

## Running it

```bash
go test ./...                       # includes the boundary tests
APP_ENV=dev PORT=8080 go run ./cmd/api
curl localhost:8080/healthz         # alive — says nothing about dependencies
curl localhost:8080/readyz          # ready — checks them
```

`/healthz` and `/readyz` are deliberately different. A pod that has lost the
database is alive and restarting it will not help; it is simply not ready. On
SIGTERM the process stops being ready, waits one probe interval, then stops
accepting — so a rolling deploy is invisible to a tenant halfway through paying
rent.

## Configuration

| Variable | Default | Notes |
|---|---|---|
| `APP_ENV` | `dev` | `dev`, `uat`, `prod` |
| `PORT` | `8080` | |
| `DATABASE_URL` | — | Required outside dev; comes from the CNPG app secret |
| `LOG_LEVEL` | `info` | JSON logs in prod, text elsewhere |
| `SHUTDOWN_GRACE_SECONDS` | `20` | |
| `SANDBOX_ORG_SLUGS` | — | Organisations holding demonstration data (M19) |

## Schema

SQL does not live here. Every table is defined in
`tesserix-k8s/charts/apps/db-schema-bootstrap/schemas/dwellm8/`, one file per
module, applied idempotently by the bootstrap CronJob. This repository holds
ORM models only.
