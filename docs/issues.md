# Issues and event APIs

[Documentation](README.md) · [Project home](../README.md)

Investigate and triage captured errors. These native workflows and compatible API routes share the same stored issues.

## Issue triage

Assign an issue to a workspace member or team, or clear its assignment. Search people and teams from issue detail. Comments and activity have independent pages of 25 entries. Authors and workspace owners/admins can edit or delete comments; editing preserves their original creation time.

Simple Resolve records the current receipt time. A new unique error reopens the issue and records a regression, even when that error occurred earlier. Retrying an already accepted event ID preserves its count, resolution, activity and release metadata.

In the Resolution panel, choose an existing project release or use the issue's observed release. A newer release reopens the issue. GoodSentry uses semantic version order for recognized project histories and compatible package names, ignoring build metadata; other identities use recorded release creation dates. The selected reference and comparison stay fixed when metadata changes or is deleted.

**Resolve in next release** waits for a different open project release created or finalized after resolution. It needs a valid observed baseline. The first selected next release, or a newer release, reopens the issue when an error arrives. The application worker can select it from metadata without another event. Saved pending and selected states survive restart. Errors without a valid release reopen either policy. Applying the same policy preserves its progress; clear to simple resolution or reopen before starting a fresh next policy.

Errors that remain resolved are still captured and counted, but do not create new issue notifications. Activity shows the selected boundary and the reason for a regression. The compatible individual and bulk issue APIs share these native rules. Commit-triggered resolution remains pending.

Ignore pauses new issue notifications while collecting every error. In issue detail, mute indefinitely, for a number of minutes, until a number of additional unique errors, or until either condition is met. The last additional error reopens the issue before normal alert eligibility runs. Retries never consume the count, and applying the same active condition preserves its deadline and progress. New captures and the maintenance worker evaluate elapsed time using the database receipt clock; SDK timestamps and timezone changes do not move the deadline. Activity records why a mute ended.

The maintenance worker runs inside the application, including when uptime and log ingestion are disabled. It catches up after restart. Lists may show an expired mute until its next maintenance pass. Captures still contribute to project-wide alert windows, so a threshold crossed during mute is not replayed afterward. Deliveries already queued or in progress retain their status. Native conditional mute does not yet implement the compatible API's affected-user or sliding-window conditions.

## Merge and separate issues

Select 2 to 100 issues from one project and confirm **Merge selected**. Selections stay across pages with the same filters. The lowest issue ID retains its assignment, status and active mute or release policy. Absorbed roots clear their active policies and record the previous state in activity. Merging history does not consume mute progress, reopen a resolution or queue notifications.

In **Original groups**, search or page through the original fingerprints and separate selected groups, including the current root. Each selected group becomes standalone; unselected groups stay together. Active signatures follow their separated origin. **Historical only** origins retain their recorded events; separating them does not reclaim a signature already assigned to a newer issue. Counts and dates describe the resulting stored events. Comments and activity stay with their original group, including after separation.

Old issue links open the current group. If another user merges an issue while you edit it, the application preserves your drafts and provides an **Open current group** link. These controls implement native GoodSentry grouping. Compatible API v0 bulk merging uses the same rules.

## Reset future grouping

In **Future grouping**, select active signatures across pages and confirm **Reset selected signatures**. Existing events, assignment, mute and resolution policies stay on their recorded issue. The next new matching SDK event starts another issue. Retrying an already stored event preserves its original receipt and does not reactivate a reset signature.

Compatible clients can use `GET /api/0/organizations/{org}/issues/{issue}/hashes/` with `event:read` or `event:write`. Each signature has a stable public UUID and its own latest retained event sample, or null when no sample remains. Responses include cursor Links; full samples are enabled by default with at most 10 per page. Use `full=false` for summaries with up to 100 per page.

Send `DELETE` to the same URL with repeated `id` query values and an empty body to reset selected signatures. This requires `event:write`, or a session with CSRF. A successful response is 202 with no body after the reset commits. Foreign or inactive signatures are ignored. Writes to a merged alias return 409 with `mergedInto`; reads open its current group. The signature UUID remains stable when its next capture creates a new generation.

## Delete issues

Use **Delete issue** in detail or select up to 1,000 issues across list pages and confirm **Delete selected**. Deletion removes each whole current group, its recorded errors, attachments, comments, assignment and active triage policies. Future matching captures can create a new issue. Already queued notifications and shared release metadata remain available. Merge remains limited to 2–100 issues from one project.

Compatible clients use `DELETE /api/0/organizations/{org}/issues/{issue}/` with an empty body and the optional `event:admin` scope. Individual deletion returns 204 after commit; a merged alias returns 409 with `mergedInto`. Organization bulk deletion uses the same collection URL with repeated `id` values and returns 200 with an empty JSON object. If any supplied ID is missing or outside the credential's access, the whole bulk request makes no changes. The UI reports completion without inventing a deleted count and offers a read retry if refreshing the list fails.

`event:admin` grants deletion only and stays unchecked by default. It does not grant issue reads or other writes. Deletion is not permanent discard: retrying an actually deleted event ID can create a new receipt; retries of retained events remain idempotent. The official CLI 3.8.0 has no issue deletion command.

## Issue search

The Issues view combines project, status, environment and release filters with text and tag queries:

```text
region:eu timeout
region:[eu,us] !browser:Safari
release:"web@1" level:error
tags[release]:"build *"
```

Terms intersect; list values are alternatives. Bare `*` is a wildcard, while quoted values match literally. `tags[key]` searches a custom tag whose name collides with a reserved field. Select **All statuses** to include resolved and ignored issues.

Environment, release, tags and text must match the same stored event, including historical events. The displayed count, dates and sorting describe the issue's whole history. Choose Last seen, First seen or Event count; each page contains 25 issues. Filters persist through reload, paging and detail/back navigation. Clicking an event tag opens an exact search, including literal stars and punctuation.

Invalid queries keep the filter form editable. Broader query syntax remains separate work.

## Issue analytics and compatible reads

Issue detail shows identified users from retained events and a real 24-hour event chart, including an accessible table of hourly counts in UTC. Anonymous events add to event counts without creating an identified user. Historical counts remain visible even when recent activity is empty.

An explicit `event:read` or `event:write` token can read organization/project issue lists, organization/global issue detail and organization `issues-stats` under `/api/0/`. Reads honor its project grants. Omitted `query` selects unresolved issues; `query=` includes every status. Environment and occurrence filters return matching metrics alongside historical totals. Organization `statsPeriod` selects a relative time window; on the deprecated project list it selects the chart (`24h`, `14d` or empty to disable). `groupStatsPeriod` controls list charts separately.

Affected-user counts use the first nonblank valid ID, email, username or IP in each retained event. Numeric IDs share an identity with their equivalent decimal string; attributes stay separate and case-sensitive. This is GoodSentry's explicit counting rule. Unsupported query options return 400. Compatible sliding-window conditions remain pending. Issue category filters distinguish errors from modern feedback, including retained issues with no remaining events.

## Compatible individual issue changes

Create a token with the optional `event:write` permission and select its projects. It can update `/api/0/issues/{issue}/` or `/api/0/organizations/{org}/issues/{issue}/` with `PUT`. Use the trailing slash and replace `123` with an accessible issue ID:

```sh
curl --request PUT http://localhost:8000/api/0/issues/123/ \
  --header "Authorization: Bearer $SENTRY_AUTH_TOKEN" \
  --header 'Content-Type: application/json' \
  --data '{"status":"resolved","statusDetails":{"inRelease":"latest"}}'
```

The response contains the saved compatible issue detail and real analytics. `latest` requires an existing release associated with that project. Use an exact release version instead, or `"status":"resolvedInNextRelease"` to apply the native next-release policy. Plain `resolved`, `unresolved` and `ignored` follow the native rules; `muted` is an alias of `ignored`.

`assignedTo` accepts a member's ID, `user:ID`, `team:ID` or primary email. Null or an empty string unassigns. Omitted assignment and policy fields follow the native omission rules. For an ignored issue, `ignoreDuration` and `ignoreCount` select elapsed minutes and additional unique errors; zero disables that condition. These fields may also appear inside `statusDetails`. Identical policies preserve their existing progress.

An explicit `statusDetails:null` or `{}` clears the current condition while keeping the effective status. An empty `inRelease` or `inNextRelease:false` clears only a resolved condition and is rejected for other statuses. Invalid targets or incompatible policies roll back the whole request. A merged alias returns 409 with `mergedInto`; open the current group before retrying. An empty request object reads the authorized issue without expiring or changing its policies.

Writer tokens inherit issue and issue-event reads and can reset future grouping. Project event routes still need `project:read`, release routes need `project:releases`, and native mutation routes require a session. Compatible issue deletion is described under [Delete issues](#delete-issues). Username handles and user/window/commit conditions remain pending; unsupported fields return 400.

## Compatible bulk changes

Use the same optional `event:write` token with `PUT /api/0/projects/{org}/{project}/issues/` or `PUT /api/0/organizations/{org}/issues/`. Repeat `id` for explicit selection. Changes share one transaction and honor every project grant. If any explicit ID is absent or outside that scope, the whole request returns an empty acknowledgement without changing issues.

```sh
curl --request PUT 'http://localhost:8000/api/0/projects/my-workspace/my-project/issues/?id=123&id=456' \
  --header "Authorization: Bearer $SENTRY_AUTH_TOKEN" \
  --header 'Content-Type: application/json' \
  --data '{"merge":true,"status":"unresolved"}'
```

Merging requires explicit IDs from one project and retains the lowest current root. `merge:1` is also accepted; false, zero and null do not separate groups. Assignment and policy-only changes require IDs too. A status change can select by current `status`, project, environment, search and occurrence time. Without IDs or a query, the project route includes all statuses; the organization route selects unresolved issues. Explicit `query=` includes every status. Operations select at most 1,000 roots by default; an explicit `limit` accepts 1–100. Release selectors require one project. The response acknowledges supplied changes and the actual merge parent/children; issue GETs show each saved state.

The real official sentry-cli3.8.0 has passed resolve, mute, unresolve, next-release and selection by IDs, status or `--all`, followed by authenticated state checks. Bulk `snoozeDuration:null` is accepted, and a nonnull value aliases ignoreDuration in minutes. SDK capture, retries and future routing remain intact.

## Compatible event reads

Issue event routes accept an explicit `event:read` or `event:write` token restricted to the selected projects. Project event routes use `project:read`. The token form keeps `org:read` and `project:read` as defaults; both event scopes start unchecked.

Read errors from `/api/0/projects/{org}/{project}/events/`, `/api/0/issues/{issue}/events/` or `/api/0/organizations/{org}/issues/{issue}/events/`. Append the SDK event ID for full detail; issue routes also accept `latest`, `oldest` and `recommended` (latest). Project detail can retrieve a stored transaction by ID. The organization issue route additionally exposes `{event}/json/` as a legacy JSON projection.

Lists support environment, occurrence dates, `statsPeriod`, bounded search, `limit`/`per_page` and cursor Links. Dates without an offset use UTC. Omitted time filters retain all history; `full=true` limits each page to 10 events. Reads retain the stored SDK payload, current group ownership and original receipt time.
