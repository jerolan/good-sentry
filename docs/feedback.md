# User feedback

[Documentation](README.md) · [Project home](../README.md)

Modern feedback submissions and legacy user reports use separate views and retention rules.

## Modern user feedback

The official Node SDK `11.7.0` sends feedback through its normal transport.
After configuring its DSN, capture a standalone submission or associate it
with an error from the same project:

```js
const errorID = Sentry.captureException(new Error("Checkout failed"));
Sentry.captureFeedback({
  message: "The checkout button stopped responding.",
  name: "Alex",
  email: "alex@example.com",
  associatedEventId: errorID,
});
await Sentry.flush(5000);
```

Open **Feedback** to search, assign, comment on, resolve, reopen or ignore
submissions. Each new SDK feedback ID starts its own issue; related feedback
issues can be merged. Feedback stays separate from error counts, error alert
thresholds and release regression policies.

The submission list has 25 rows per page. Inspect the selected submission's
original message, contexts, JSON and downloadable attachments, including
submissions older than the latest 50 event summaries. Names, contact details
and source URLs are displayed as text. Retrying an existing feedback ID keeps
its first payload and receipt time; a new attachment can still be added.

An error arriving later can bind a pending reference through the same
application process. The association records a specific stored error receipt.
If that error expires, the feedback remains readable and reports the error as
unavailable. Reusing the error's SDK ID does not replace the old association.
Expired feedback removes its own attachments without deleting the associated
error. An issue with no retained submissions keeps its category and triage
history. **Inspect associated error** shows the retained error and links to
its current issue; file downloads belong to the selected feedback submission.

## Legacy user feedback

GoodSentry receives legacy `captureUserFeedback` reports through Sentry SDK
envelopes and `POST /api/0/projects/{organization}/{project}/user-feedback/`.
Official Sentry Browser `7.120.4` passes with its normal network transport.
Modern `captureFeedback` submissions use the separate **Feedback** view above.

Open an issue's **User reports** panel to read the reporter's name, contact
text and comments. Reports have 25 rows per page. **View event** opens the
actual reported error, including older errors outside the latest 50 summaries,
with its stack, metadata and downloadable attachments. Report reads preserve
comment and policy drafts. Supplied contact details are displayed as text.

A report can arrive before its error. It stays pending for up to 30 minutes,
then the application removes it if no matching error arrives. A linked report
expires with that stored error. Submission is accepted through 30 minutes
after the error's server receipt; a report can be changed through five minutes
after its first receipt. Later submissions return `409`. SDK timestamps and
updates do not extend these windows.

SDK writes use the project's DSN key. REST writes also accept an authorized
session with CSRF or an explicitly scoped `project:write` token. DSN keys do
not grant report reads. Project listing uses `project:read`; issue listing
uses `event:read` or `event:write`. Existing token defaults stay unchanged.
