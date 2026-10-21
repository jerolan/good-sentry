# Two-factor authentication acceptance (2026-10-21)

Verified optional TOTP two-factor authentication end to end: enrollment from
the Account view, one-time recovery codes, the cookie-free challenge sign-in,
a recovery-code sign-in and a password-confirmed disable.

## Environment

- GoodSentry from commit `1379f82` (backend) and `0f5b230` (UI), built as `/tmp/goodsentry-2fa` and later verified from the published image.
- `GOODSENTRY_AUTH=required``, PostgreSQL 18 container `goodsentry-verification-20261020` with schema `goodsentry_stats_grafana_20261021_2130`.
- Fixture owner `stats@example.test`; the account was returned to the disabled state after the run.
- Chromium `1187` through the workspace Playwright module at 1440x1000 and 390x844. The harness computes codes itself from the base32 secret with HMAC-SHA1, so no external authenticator is involved.

## Receipts

| File | Contents |
| --- | --- |
| `local-browser.json` | Steps, recovery-code counts, overflow and the classified console failures. |
| `account-two-factor-1440.png` | Account panel immediately after confirmation, showing the ten one-time codes. |
| `account-two-factor-390.png` | The same panel on a phone viewport after a recovery-code sign-in. |
| `signin-challenge-1440.png` | Challenge step after the password. |
| `signin-challenge-390.png` | The same step at 390 pixels. |
| `public-browser.json` | The same workflow repeated against the published image. |

## Observed behavior

- Enrolling returns a 32-character base32 secret and its `otpauth://totp/` URI; confirming with the current code returns ten recovery codes exactly once and leaves the account enabled across a reload.
- The next sign-in returns a challenge instead of a session: `/api/v1/me` stays 401 until the code is accepted. A code for the next step signs in, and replaying the accepted challenge is rejected.
- One recovery code signs in and is then rejected; the panel reports nine remaining.
- Disabling requires the current password and returns the panel to the disabled state.
- The only console failures are the expected session probe (`401 GET /api/v1/me`) and the browser's automatic `404 GET /favicon.ico`, which `index.html` does not declare. No horizontal overflow appears at either width.

## Reproduce

Start GoodSentry with `GOODSENTRY_AUTH=required`, sign in, open Account, enable two-factor authentication, and confirm with a code computed from the displayed secret. Then sign out and sign in again to see the challenge step.
