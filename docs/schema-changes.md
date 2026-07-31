# Changing the database schema

Written for the engineer who will only ever see this repository, and who is about to
look for the migrations directory. There isn't one, and that is deliberate.

---

## Where the schema lives

```
tesserix-k8s/charts/apps/db-schema-bootstrap/schemas/dwellm8/dwellm8/dwellm8.sql
```

One file. Every table, index, constraint, trigger, function, row-level-security policy,
grant and seeded reference row for Dwellm8 is in it. This repository holds none of them —
CI refuses a `.sql` file outside `docs/`, and refuses a `migrations/`, `alembic/`,
`flyway/` or `liquibase/` directory, with a pointer back to this page.

**Why here and not there.** One source of truth, and one author. A schema with two authors
— a migration tool in the app repository and the bootstrap job in the cluster — diverges on
the first day nobody is watching, and the divergence is discovered by a query that returns
the wrong number rather than by an error.

---

## How a change reaches the cluster

1. Edit `dwellm8.sql` in `tesserix-k8s`.
2. Commit and push to `main`.
3. ArgoCD syncs the `dwellm8-db-schema-bootstrap` Application, which renders the file into
   a ConfigMap.
4. A CronJob applies it, every 30 minutes, idempotently.

No step of that is manual and none of it involves `kubectl apply`.

### The file must be idempotent, by construction

The CronJob replays the whole file on every run — `applySchemaToExistingDatabases: true`
for Dwellm8, which is *not* the chart's default. It is safe here because nothing else
authors these tables at runtime: no GORM `AutoMigrate`, no Alembic. (HomeChef is the
cautionary tale the default was set for: a frozen `pg_dump` replayed over a database that
GORM also managed, fighting it on a 30-minute loop.)

So everything in the file is written to survive being run again:

| Object | How |
|---|---|
| Tables | `CREATE TABLE IF NOT EXISTS` |
| Indexes | `CREATE INDEX IF NOT EXISTS` |
| Functions, views | `CREATE OR REPLACE` |
| Policies | `DROP POLICY IF EXISTS` then `CREATE POLICY` |
| Triggers | `DROP TRIGGER IF EXISTS` then `CREATE` |
| Constraints added later | `DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = …) THEN ALTER TABLE … END IF; END $$;` |
| Seed rows | `INSERT … ON CONFLICT … DO UPDATE`, and see the note in the statutory section about which columns a replay may touch |

**The one thing a replay cannot repair** is a column that somebody altered by hand:
`CREATE TABLE IF NOT EXISTS` skips a table that exists, so a dropped `NOT NULL` stays
dropped. CI's planted-defect steps restore that kind of change explicitly for the same
reason.

---

## Dropping things

A change that drops a column or a table is not an ordinary edit. `IF NOT EXISTS` will not
undo it, the CronJob will not warn about it, and the data is gone on the next sync.

Route it as a reviewed change with the drop written explicitly and a note in the pull
request saying what is being lost and what reads it today. In most cases the right answer
is not to drop at all: ADR-0006 revokes `DELETE` on the ledger, ADR-0008 replaces rows
rather than editing them, and the tables that record what somebody declared are the ones a
dispute turns on years later.

---

## Testing a change

CI fetches the schema **from `tesserix-k8s` on `main`**, not from a copy in this repository
— see the "Load the dwellm8 schema" step in `.github/workflows/api.yml`. So:

- A schema change must be **pushed to `tesserix-k8s` first**, or the API tests that depend
  on it will fail here.
- Nothing in this repository can drift from what the cluster applies, because the tests run
  against the same bytes.

Locally, [`SETUP_LOCAL.md`](../SETUP_LOCAL.md) has a PostgreSQL 16 and the one command that
replays the file into it.

**PostgreSQL 16 or later.** The schema uses `security_invoker` views, which PostgreSQL 14
does not have; a 14 will fail part-way through and leave you with half a schema.

---

## What lives in this repository instead

ORM models and query code under `services/api/internal/*/store`, written against the schema
rather than defining it. Where the Go and the SQL both encode the same rule — a state
machine, a vocabulary, a set of transitions — there is a **contract test** that fails the
build when they disagree. `internal/lease/store/lifecycle_test.go` and
`internal/platform/statutory/store/rules_test.go` are the pattern to copy.

Two copies of a rule is a deliberate trade: Go so a handler can decide without a round
trip, SQL so a write that never went through Go cannot break it. The contract test is the
price, and the dangerous direction of drift is always the quiet one — the database refusing
something the application believes it did.
