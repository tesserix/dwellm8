# Architecture decision records

One file per decision, numbered, never edited after acceptance — a decision that
changes gets a new ADR that supersedes the old one, so the reasoning at the time
stays readable.

| ADR | Decision | Status |
|---|---|---|
| [0001](0001-modular-monolith-api.md) | Modular monolith API: eight modules, one deployable, one repository | Accepted |
| [0002](0002-event-backbone-and-outbox.md) | Event backbone: transactional outbox, JetStream, idempotent consumers | Accepted |
| [0003](0003-tenancy-and-row-level-security.md) | Organisation tenancy and the row-level security standard | Accepted |
| [0005](0005-owner-delegation-grants.md) | Owner delegation: effective-dated, scoped, revocable grants | Accepted |
| [0006](0006-chart-of-accounts-and-posting-rules.md) | Chart of accounts, immutable postings, derived balances | Accepted |
| [0007](0007-money-representation-and-rounding.md) | Money as int64 minor units: one rounding rule, largest-remainder allocation, no floats | Accepted |
| [0008](0008-effective-dating-and-temporal-queries.md) | Effective dating: half-open date intervals, no-overlap by exclusion constraint, correction ≠ change | Accepted |
| [0009](0009-property-block-unit-model.md) | Property, block and unit: one tree, and grant scope enforced at unit granularity | Accepted |
| [0010](0010-lease-lifecycle-state-machine.md) | Lease lifecycle: agreed term ≠ actual end, billing driven by an interval, one flat one tenancy | Accepted |
| [0011](0011-payment-provider-adapter.md) | Payment provider adapter: idempotency by index, advisory webhooks, forward-only states | Accepted |
| [0012](0012-settlement-reconciliation-and-drift.md) | Settlement reconciliation: two directions, drift by which pair disagreed, `reconciled` earned not claimed | Accepted |
| [0013](0013-kyc-data-handling.md) | KYC data handling: three tiers, a full identifier made unstorable, a column allowlist | Accepted |
| [0015](0015-durable-workflow-standard.md) | Durable workflows: a rule that generates the list, the irreversible step last, keys from the workflow | Accepted |
| [0021](0021-demo-sandbox-architecture.md) | Demo sandbox: purgeable because nothing real originates in it, and vice versa | Accepted |

0004 is missing on purpose: the identity and authorisation decision it was
reserved for was closed as not planned, and the numbers are not reused.

The other gaps mean something different — accepted out of order, not skipped. Each
number is reserved by an open planning issue and will be written when the work
reaches it: 0014 database topology and RPO/RTO ([#25](https://github.com/tesserix/dwellm8/issues/25)),
0016 mobile stack ([#118](https://github.com/tesserix/dwellm8/issues/118)),
0018 web topology ([#120](https://github.com/tesserix/dwellm8/issues/120)),
0019 public listing surface ([#132](https://github.com/tesserix/dwellm8/issues/132)),
0020 OpenFGA authorisation ([#148](https://github.com/tesserix/dwellm8/issues/148)).
The money spine was built first because everything else posts to it.

## Writing one

Keep the context honest about what was actually known at the time, and record
the rejected alternatives with the reason they were rejected — an ADR that only
argues for the chosen option is a press release.
