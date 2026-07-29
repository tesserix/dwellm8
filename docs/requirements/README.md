# Requirements documents

| Document | Source | Output |
|---|---|---|
| High-Level Product Requirements & User Story Catalogue (v1.9) | `rentora-requirements.html` | [`Rentora-Requirements-v1.9.pdf`](Rentora-Requirements-v1.9.pdf) · [`Rentora-Requirements-v1.9.docx`](Rentora-Requirements-v1.9.docx) |

The PDF is generated from the HTML source — edit the HTML, never the PDF:

```bash
cd docs/requirements
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \\
  --headless=new --disable-gpu --no-pdf-header-footer \\
  --print-to-pdf="Rentora-Requirements-v1.9.pdf" \\
  "file://$PWD/rentora-requirements.html"
```

DOCX is produced from the same source:

```bash
pandoc rentora-requirements.html -f html -t docx --toc --toc-depth=3 \
  -o Rentora-Requirements-v1.9.docx
```

A copy of both is delivered to `~/Desktop/samyak-work/projects/Property-Manager/` as
`Rentora_High_Level_Product_Requirements.{pdf,docx}`.

v1.9 charges the 3.9% platform fee **once, deducted by the property manager when they pay
the owner** — the tenant is never surcharged for paying rent. Payment instrument costs
(card, netbanking, NACH) are separate disclosed pass-throughs. Rate, base and cap are
rule-table values, and a per-payout cap ships from day one.

v1.7 settled three things: the **property manager bears the 3.9% fee** (generalised as
"the party earning the operating margin" — §5.5, the tenant is never surcharged); the
**authorisation model shape** with its type inventory, DSL sketch and nine isolation
invariants (§9.7); and **Rentora Ops as a full web console** that is a peer of the app,
not a back-office annexe.

v1.6 added module **M19 Demo & Sandbox** — one fully isolated sandbox serving two jobs:
try the real product with **no sign-up at all** and continue into a real account, and give
existing customers and their staff a safe place to **test and train on new features and
releases**. Each demo or sandbox account is its own ephemeral organisation, isolated by
the same tenancy and authorisation mechanisms that separate two paying customers.

v1.5 added §9.5 access tiers (anonymous → verified prospect → customer → operator →
administrator, each an explicit relation) with a hard **no-anonymous rule for every
administrative surface**, and §9.6 the **demonstration workspace** — a populated sample
portfolio for new owners and managers, structurally isolated and provably inert.

v1.4 settled identity and authorisation in a new §9.4: **Google Identity Platform for
every user** (consumers, staff and admin alike — no Keycloak) with **OpenFGA plus RBAC**
for permissions. Tokens carry identity, never authority; OpenFGA decides, and PostgreSQL
row-level security remains an independent second line of defence.

v1.3 added public discovery, listings and inspection scheduling (a new **Prospect** persona
and 13 stories in M15), and settles the technology stack in §9.3 — **Next.js** web,
**Go + Gin** services, and **React Native on Expo** for the five apps, matching the
existing `mark8ly` and `Home-Chef-App` mobile stack (expo-router, NativeWind,
`@tesserix/native`, TanStack Query, zustand, react-native-razorpay, EAS Build).

v1.2 made the Admin surface **web and mobile together** (five apps, Admin among them),
with high-consequence configuration deliberately web-only.

v1.1 replaced the subscription model with a **flat 3.9% platform fee on every in-app
payment** (no subscriptions, all functionality free) plus a future **premium AI tier at
₹499 per organisation per month**, and adds the Admin web console, the end-to-end
workflows and the integration framework.

v1.0 widens scope beyond `docs/product-brief.md`: it adds the hostel/PG/co-living and
commercial verticals, the service-provider marketplace, the cost-sharing and mandate
model, and the sales/brokerage extension. Where the two differ on vertical scope, this
document is authoritative and the brief will be updated to match.
