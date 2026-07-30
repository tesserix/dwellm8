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
| [0009](0009-property-block-unit-model.md) | Property, block and unit: one tree, and grant scope enforced at unit granularity | Accepted |
| [0011](0011-payment-provider-adapter.md) | Payment provider adapter: idempotency by index, advisory webhooks, forward-only states | Accepted |

0004 is missing on purpose: the identity and authorisation decision it was
reserved for was closed as not planned, and the numbers are not reused.

## Writing one

Keep the context honest about what was actually known at the time, and record
the rejected alternatives with the reason they were rejected — an ADR that only
argues for the chosen option is a press release.
