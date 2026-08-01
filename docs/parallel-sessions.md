# Working on this repository in parallel

More than one agent session works on Dwellm8 at the same time, deliberately. Two
issues that touch different modules genuinely can be built at once, and waiting for
one to finish before starting the other buys nothing.

What it costs is a class of collision that does not exist when one person works
alone, and every one of them is quiet: the build stays green, the tests pass, and
the damage is found later by somebody reading a file that has two of something.
This page is the list of those, how to avoid each, and what to check before you
commit.

Read it before you start, and again before you commit — the second read is the one
that matters, because by then the tree has moved.

---

## 1. Assume the tree moved while you were working

It does. In a single afternoon this repository took three commits from another
session while a fourth piece of work was in progress.

So:

- **`git fetch` and read the log before you commit**, not only when you start.
  `git log --oneline HEAD..origin/main` and `git log --oneline -8` both matter: the
  second catches a sibling session that committed locally.
- **Do not stash-and-pull blindly.** If your work is uncommitted and the tree has
  moved, look at what moved first. A pull that succeeds is not the same as a pull
  that is safe.
- **Never `git checkout .`, `git reset --hard`, or `git clean` a shared tree.**
  Another session's uncommitted work looks exactly like your own junk, and it is
  not recoverable.
- If a file you did not touch appears modified, **it is not yours** — leave it. The
  same goes for an unfamiliar package under `internal/`: it is somebody's story in
  progress, not dead code.

---

## 2. The numbered things are where collisions actually happen

Three sequences in this product are numbered by hand, and a number is exactly the
kind of thing two sessions pick simultaneously.

### Schema chapters

The schema lives in `tesserix-k8s` (see [`schema-changes.md`](schema-changes.md))
as ordered chapters that the bootstrap concatenates:

```bash
ls ../tesserix-k8s/charts/apps/db-schema-bootstrap/schemas/dwellm8/dwellm8/*.sql | sort
```

**Claim the number by listing the directory immediately before you create the
file**, and leave a gap of 10 as the existing files do. This has already collided
once: two sessions each wrote a `250_`, and both applied — `sort` put them in a
stable order and nothing broke — but a duplicated number is a sequence nobody can
reason about, and the second author renumbered.

A chapter is applied in filename order, so **a chapter that depends on another must
sort after it**. Anything referencing `organisations` needs `010`; anything
referencing the platform organisation needs `190`; anything wanting the resident
deny loop or the assertions to cover it needs to run *before* `220`/`230` — and
since new chapters cannot, they must write their own resident-deny and rely on the
next replay. Both `260_checklists.sql` and `270_automations.sql` do this and say so
in a comment.

### ADR numbers

```bash
ls docs/adr/*.md | tail -3
```

Same rule: claim it by listing, and add the row to
[`docs/adr/README.md`](adr/README.md) in the same change. An ADR that exists and is
not indexed is one nobody finds.

### Issue numbers in commit messages

Two sessions closing two issues in one afternoon will both write `(#NNN)`. Nothing
enforces that the number is right, so check it.

---

## 3. The files two sessions both want to edit

| File | Why both want it | What to do |
|---|---|---|
| `services/api/cmd/api/main.go` | Every feature wires itself here | Add your block; do not reorder or reformat anything else. Keep the diff to what you added. |
| `services/api/internal/platform/authz/model.fga` | Every feature adds a type | Edit `model.fga`, then **regenerate** `model.json` (below). Never hand-edit the JSON. |
| `docs/adr/README.md` | Every ADR adds a row | Append; do not re-sort. |
| `charts/apps/dwellm8-api/values.yaml` | Every job adds a CronJob | Append a block; do not touch the others. |
| `apps/ops/app/(tabs)/*.tsx` | Every feature adds an entry point | Add one `ListRow`; leave the rest. |

`model.json` is generated and CI diffs it against the transform, so a stale one
fails the build rather than shipping:

```bash
cd services/api/internal/platform/authz
fga model transform --file model.fga > model.json
fga model test --tests model.fga.yaml
```

---

## 4. Your test database is yours

Every session that runs the Go suite needs PostgreSQL 16 with the schema applied.
**Use your own database name**, because the isolation harness commits and two
sessions sharing one database read each other's fixtures — which produces failures
that look like isolation bugs and are not.

```bash
# Combine the chapters as the bootstrap does, into a scratch file.
cd ../tesserix-k8s/charts/apps/db-schema-bootstrap/schemas/dwellm8/dwellm8
cat $(ls *.sql | sort) > /tmp/dwellm8-combined.sql

# A database named for what you are doing, not "dwellm8".
psql -h 127.0.0.1 -p 55433 -U postgres \
  -c "DROP DATABASE IF EXISTS dwellm8_issue200" \
  -c "CREATE DATABASE dwellm8_issue200 OWNER dwellm8"

# Twice: the assertions in 230 only see tables created in earlier chapters, so a
# chapter numbered above 230 is first checked on the second pass. A single pass
# proves less than it looks like it does.
PGPASSWORD=local psql -h 127.0.0.1 -p 55433 -U dwellm8 -d dwellm8_issue200 \
  -v ON_ERROR_STOP=1 -f /tmp/dwellm8-combined.sql
```

Re-combine and re-apply **whenever the other session lands a chapter**. A suite that
fails on a table you have never heard of is almost always this.

---

## 5. Before you commit: check and verify

In this order, because each catches something the next assumes.

```bash
cd services/api
export TEST_DATABASE_URL="postgres://dwellm8_api:local@127.0.0.1:55433/<yours>?sslmode=disable"
export TEST_PLATFORM_DATABASE_URL="postgres://dwellm8_platform:local@127.0.0.1:55433/<yours>?sslmode=disable"
export TEST_PURGE_DATABASE_URL="postgres://dwellm8_purge:local@127.0.0.1:55433/<yours>?sslmode=disable"

gofmt -l .            # empty
go build ./...
go vet ./...
go test -race ./...   # the whole suite, not your package
```

```bash
cd apps/ops && npx tsc --noEmit          # and any other app you touched
cd services/api/internal/platform/authz && fga model test --tests model.fga.yaml
cd ../tesserix-k8s && helm template charts/apps/dwellm8-api > /dev/null
```

**The whole suite, not your package.** The failure that matters in a parallel repo
is the one your change causes somewhere you did not look, and `go test ./...` is
thirty seconds.

Then look at the diff you are about to commit:

```bash
git status --short
git diff --stat
```

Anything in that list you do not recognise belongs to the other session. Do not
commit it, and do not revert it.

---

## 6. Committing

- **Commit your own paths explicitly.** `git add -A` in a shared tree stages the
  other session's work.
- Identity comes from the remote — see `~/.claude/CLAUDE.md`. It is not carried over
  from the last repository.
- Push promptly once green. Uncommitted work is what the other session cannot see,
  and everything on this page is about what the other session cannot see.
- `tesserix/dwellm8` stays **public**; the public→CI→private cycle in the workspace
  rules does not apply to it.

---

## 7. In flight

Kept current by whoever is holding work. If you are about to touch something listed
here, the other session has an opinion about it.

| Issue | Holding | State |
|---|---|---|
| — | — | — |

*(Empty means everything is committed. Add a row when you start; delete it when you
push.)*

---

## What this page is not

It is not a lock. Nothing here stops two sessions editing one file, and nothing
should — the cost of coordination is higher than the cost of the occasional
conflict, which git is good at.

What it stops is the *silent* class: a duplicate chapter number, a stale
`model.json`, a suite passing against a database three chapters behind, a
`git add -A` that commits somebody else's half-finished story. Every one of those
looks fine at the moment it happens.
