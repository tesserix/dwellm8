# Running Dwellm8 locally

Everything here runs on a laptop with no cluster access and no credentials. That is the
point: an engineer who cannot reach GKE must still be able to build, test and run.

**Prerequisites**: Go 1.26, Node 22, PostgreSQL **16 or later**, and a clone of
[`tesserix-k8s`](https://github.com/tesserix/tesserix-k8s) beside this one — the schema
lives there ([`docs/schema-changes.md`](docs/schema-changes.md) explains why).

```
~/work/
  dwellm8/        ← this repository
  tesserix-k8s/   ← the schema and the charts
```

---

## 1. A database

PostgreSQL 16 or later, because the schema uses `security_invoker` views. A 14 fails
part-way through and leaves you with half a schema, which is worse than failing outright.

```bash
brew install postgresql@16
/opt/homebrew/opt/postgresql@16/bin/pg_ctl \
  -D /opt/homebrew/var/postgresql@16 -o "-p 55433" -l /tmp/pg16.log start
```

Any port and any installation will do — 55433 is used below only so it cannot collide with
a system PostgreSQL on 5432.

### The roles and the database

The schema creates its own roles, so this is the whole of it:

```bash
export PG="psql -h 127.0.0.1 -p 55433 -U postgres"

$PG -c "CREATE ROLE dwellm8 LOGIN CREATEROLE PASSWORD 'local'"
$PG -c "CREATE DATABASE dwellm8 OWNER dwellm8"

PGPASSWORD=local psql -h 127.0.0.1 -p 55433 -U dwellm8 -d dwellm8 -v ON_ERROR_STOP=1 \
  -f ../tesserix-k8s/charts/apps/db-schema-bootstrap/schemas/dwellm8/dwellm8/dwellm8.sql

# The application roles the schema created have no passwords yet.
$PG -d dwellm8 \
  -c "ALTER ROLE dwellm8_api      PASSWORD 'local'" \
  -c "ALTER ROLE dwellm8_platform PASSWORD 'local'" \
  -c "ALTER ROLE dwellm8_purge    PASSWORD 'local'"
```

Re-run the `-f` line whenever the schema changes. It is idempotent by construction, so
replaying it is the normal way to pick up a change rather than something to be careful
about.

**Three roles, not one.** `dwellm8_api` is the request path and is subject to row-level
security; `dwellm8_platform` is the audited support path; `dwellm8_purge` may delete, and
only inside a sandbox organisation. Running everything as the owner would bypass every
policy in the schema and hide exactly the bugs the policies exist to catch.

---

## 2. The API

```bash
cd services/api

export DATABASE_URL="postgres://dwellm8_api:local@127.0.0.1:55433/dwellm8?sslmode=disable"
export PLATFORM_DATABASE_URL="postgres://dwellm8_platform:local@127.0.0.1:55433/dwellm8?sslmode=disable"
export APP_ENV=dev PORT=8080

# Identity Platform is not provisioned yet (issue #229), so locally every
# request impersonates one organisation. There is no default: without this the
# process refuses to start, rather than quietly serving nobody.
export AUTH_ENFORCE=false
export DEV_IMPERSONATE_ORG="$(psql -h 127.0.0.1 -p 55433 -U postgres -d dwellm8 -tAc \
  "SELECT id FROM organisations LIMIT 1")"

go run ./cmd/api
```

**The impersonation is dev-only and the process enforces that**: `AUTH_ENFORCE=false`
outside `APP_ENV=dev` is a startup failure, because an API that forgot to authenticate
does not fail — it works, for everybody. Every impersonated request logs a warning, once
per request rather than once at boot, since a boot line scrolls away.

Once [#229](https://github.com/tesserix/dwellm8/issues/229) lands, the apps send a real
GIP token and both variables go away.

`curl localhost:8080/healthz` answers when it is up.

No payment provider is configured, so collection falls back to the `offline` adapter —
which is correct locally: nothing should be able to reach Cashfree from a laptop.

### Tests

```bash
export TEST_DATABASE_URL="postgres://dwellm8_api:local@127.0.0.1:55433/dwellm8?sslmode=disable"
export TEST_PLATFORM_DATABASE_URL="postgres://dwellm8_platform:local@127.0.0.1:55433/dwellm8?sslmode=disable"
export TEST_PURGE_DATABASE_URL="postgres://dwellm8_purge:local@127.0.0.1:55433/dwellm8?sslmode=disable"

go test ./...
```

Without those three variables the database-backed tests **skip rather than fail**, so a
green run with none of them set proves much less than it appears to. If you are changing
anything that touches the schema, set them.

A useful habit: replay the schema into a scratch database first, and point the tests at
that. It catches the case where your local database has drifted from the file through
something you did by hand two weeks ago.

---

## 3. The apps

Six Expo apps and one shared package, in a single npm workspace.

```bash
npm ci
npm run typecheck        # every workspace

cd apps/ops && npm run web    # or: ios, android
```

| App | Who it is for |
|---|---|
| `own` | Owners — their portfolio, statements, approvals |
| `ops` | Managers and agencies — tenancies, tickets, payouts |
| `pro` | Vendors and technicians |
| `live` | Tenants and residents |
| `find` | The public marketplace |
| `admin` | The internal control plane |

`packages/mobile-shared` is the design system, the icon set and the money formatters. An
app may not reimplement anything it exports, and nothing in it may import from an app.

---

## 4. What you cannot do locally, on purpose

- **Build or push images.** Those run in CI. `docker build` here is never the way to check
  a change.
- **Apply anything to a cluster.** No `kubectl apply`, ever — the change goes to
  `tesserix-k8s` and ArgoCD syncs it.
- **Reach a real payment provider.** The offline adapter is the local one, and the sandbox
  credentials live in GCP Secret Manager rather than in a `.env`.

---

## 5. Continuous integration, and the visibility cycle

Three workflows: `api` (Go, with a real PostgreSQL and the schema fetched from
`tesserix-k8s`), `apps` (typecheck plus a web bundle per app), and `repo` (rules that hold
everywhere — no SQL here, no migration tooling, the ADR index matching the ADRs).

The repository is currently **public**, so CI runs without ceremony. If it is ever made
private, Actions minutes for the org are limited and the cycle is:

```bash
gh repo edit tesserix/dwellm8 --visibility public --accept-visibility-change-consequences
git push origin main
gh run list --repo tesserix/dwellm8 --limit 3     # wait for green
gh repo edit tesserix/dwellm8 --visibility private --accept-visibility-change-consequences
```

Never leave it public overnight, and never flip it back to private on a red build — the
repository stays public until CI is green, because a red build you cannot re-run is a
red build you will forget.
