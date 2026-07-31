# Tenant web view

Where a rent reminder lands. Issue #51, ADR-0029.

One file, `index.html`, with no build step and no dependencies at rest. It shows a
tenant what they owe and why, lets them pay it, and holds every receipt they have
ever been given.

## Why it is not an Expo app

`apps/live` is the tenant app and is the right answer for somebody who installs
something. This is the right answer for a link: the person opening it has just
received an SMS, is on a phone that may be four years old, and has not agreed to
install anything. First paint does not wait on a bundle, and the whole page is
about 20KB.

The one thing fetched from elsewhere is Identity Platform's auth SDK, and it is
imported only when somebody is actually signing in. A returning tenant with a live
token never downloads it.

## Configuration

Read from meta tags, so the same file ships to every environment:

```html
<meta name="dwellm8-api"     content="https://api.dwellm8.com">
<meta name="gip-api-key"     content="…">
<meta name="gip-auth-domain" content="…firebaseapp.com">
<meta name="gip-tenant"      content="dwellm8-live">
```

The GIP tenant must be the **Live** pool. A token from any other surface is
refused by the API as well (ADR-0027), so this is the first of two locks.

## Running it locally

```bash
python3 -m http.server 4173 --directory apps/web/tenant
```

With `AUTH_ENFORCE=false` and `DEV_IMPERSONATE_RESIDENT=+91…` on the API, the
page serves the impersonated renter and the sign-in step is never reached.

## What it deliberately does not do

- **No service worker.** A cached tenant view is one renter's dues served to the
  next person on a shared phone. Every response carries `no-store`.
- **No PDF generation.** A receipt opens as its own printable page; the browser's
  print-to-PDF produces a document good enough for an HRA claim without this repo
  acquiring a PDF library and a font to embed.
- **No amount rounding.** Money arrives as `amount_minor` and an integer, and
  becomes text once, in `inr()`.
