# Dwellm8 Pro

The technician and service-provider app. A day of jobs, proof that each one
happened, and a clear answer to "when am I paid".

| Journey | Screens |
|---|---|
| Work the day | `(tabs)/index` — next job, route, start action |
| Take or refuse work | `(tabs)/offers` — pay, distance and window before accepting |
| Start a job | `otp` — the tenant's four-digit code is the only way in |
| Finish a job | `complete` — photos, parts, outcome, sign-off |
| Price extra work | `(tabs)/quotes` → `quote` |
| Get paid | `(tabs)/earnings` — settlement date and the TDS line |

Everything renders from `src/data/mock.ts` — demonstration data per
requirements §9.6. Demonstration OTPs are printed on the OTP screen.

```bash
npm install          # from the repository root
npm run ios -w @dwellm8/pro
```
