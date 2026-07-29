# Requirements documents

| Document | Source | Output |
|---|---|---|
| High-Level Product Requirements & User Story Catalogue (v1.3) | `rentora-requirements.html` | [`Rentora-Requirements-v1.3.pdf`](Rentora-Requirements-v1.3.pdf) · [`Rentora-Requirements-v1.3.docx`](Rentora-Requirements-v1.3.docx) |

The PDF is generated from the HTML source — edit the HTML, never the PDF:

```bash
cd docs/requirements
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \\
  --headless=new --disable-gpu --no-pdf-header-footer \\
  --print-to-pdf="Rentora-Requirements-v1.3.pdf" \\
  "file://$PWD/rentora-requirements.html"
```

DOCX is produced from the same source:

```bash
pandoc rentora-requirements.html -f html -t docx --toc --toc-depth=3 \
  -o Rentora-Requirements-v1.3.docx
```

A copy of both is delivered to `~/Desktop/samyak-work/projects/Property-Manager/` as
`Rentora_High_Level_Product_Requirements.{pdf,docx}`.

v1.3 adds public discovery, listings and inspection scheduling (a new **Prospect** persona
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
