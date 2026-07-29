# Requirements documents

| Document | Source | Output |
|---|---|---|
| High-Level Product Requirements & User Story Catalogue (v1.0) | `rentora-requirements.html` | [`Rentora-Requirements-v1.0.pdf`](Rentora-Requirements-v1.0.pdf) |

The PDF is generated from the HTML source — edit the HTML, never the PDF:

```bash
cd docs/requirements
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \\
  --headless=new --disable-gpu --no-pdf-header-footer \\
  --print-to-pdf="Rentora-Requirements-v1.0.pdf" \\
  "file://$PWD/rentora-requirements.html"
```

v1.0 widens scope beyond `docs/product-brief.md`: it adds the hostel/PG/co-living and
commercial verticals, the service-provider marketplace, the cost-sharing and mandate
model, and the sales/brokerage extension. Where the two differ on vertical scope, this
document is authoritative and the brief will be updated to match.
