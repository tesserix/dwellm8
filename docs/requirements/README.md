# Requirements documents

| Document | Source | Output |
|---|---|---|
| High-Level Product Requirements & User Story Catalogue (v1.1) | `rentora-requirements.html` | [`Rentora-Requirements-v1.1.pdf`](Rentora-Requirements-v1.1.pdf) · [`Rentora-Requirements-v1.1.docx`](Rentora-Requirements-v1.1.docx) |

The PDF is generated from the HTML source — edit the HTML, never the PDF:

```bash
cd docs/requirements
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \\
  --headless=new --disable-gpu --no-pdf-header-footer \\
  --print-to-pdf="Rentora-Requirements-v1.1.pdf" \\
  "file://$PWD/rentora-requirements.html"
```

DOCX is produced from the same source:

```bash
pandoc rentora-requirements.html -f html -t docx --toc --toc-depth=3 \
  -o Rentora-Requirements-v1.1.docx
```

A copy of both is delivered to `~/Desktop/samyak-work/projects/Property-Manager/` as
`Rentora_High_Level_Product_Requirements.{pdf,docx}`.

v1.1 replaces the subscription model with a **flat 3.9% platform fee on every in-app
payment** (no subscriptions, all functionality free) plus a future **premium AI tier at
₹499 per organisation per month**, and adds the Admin web console, the end-to-end
workflows and the integration framework.

v1.0 widens scope beyond `docs/product-brief.md`: it adds the hostel/PG/co-living and
commercial verticals, the service-provider marketplace, the cost-sharing and mandate
model, and the sales/brokerage extension. Where the two differ on vertical scope, this
document is authoritative and the brief will be updated to match.
