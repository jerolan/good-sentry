# GoodSentry tasks and evidence

Design authority: ../SPEC.md. Ordering: plan.md. An unchecked item is incomplete, including its verification. User approved the module map and authorized execution on 2026-10-14. Product name: GoodSentry.

## Foundation and first durable SDK event

- [x] Prepare native Go and a working PostgreSQL/container verification environment in WSL.
  - Go 1.27.2 is installed at /tmp/sentrygo-toolchain/go; official archive SHA-256 `ecbadb99091a3f46e31f5f934b068b1864eafa7995211b39eaddf76996045fe5`. PostgreSQL 18 accepts real integration traffic; Docker 29.8.1 works through WSL. No shell profiles changed.
- [x] Initialize Git and commit the approved specification package.
  - Commit ca40c22 owns the initial package; subsequent changes are intention-focused commits. Remote publication is verified below.
- [x] Validate runtime configuration and build the executable with health endpoints.
  - Files: go.mod, cmd/goodsentry/main.go, internal/config/config.go and its tests.
  - Verify invalid environment rejection, go test and go build.
- [x] Add PostgreSQL migrations and lifecycle.
  - Files: internal/store/store.go, migrations/*.sql, store_test.go and command integration.
  - Verify clean database migration, repeated startup, database readiness and real persistence.
- [x] Implement account setup/login/session and organization/project bootstrap. Current public Chromium onboarding creates the first owner/workspace/project and signs out/in.
  - Files: internal/server/server.go, auth.go, workspace.go, auth_test.go and store additions.
  - Verify first-owner race/closure, login/logout/expiry, CSRF and cross-organization access denial against PostgreSQL.
  - Setup/login/logout, CSRF, project DSN and eight concurrent first-owner attempts pass. Complete membership/expiry coverage remains pending.
- [x] Implement SDK envelope/store framing and authentication.
  - Files: internal/protocol/envelope.go, transport.go, protocol_test.go and go.mod dependencies owned by primary.
  - Verify public wire grammar, binary payloads, unknown types, four compression formats, legacy store and invalid request limits.
- [x] Implement pure event normalization and stable fingerprinting.
  - Files: internal/event/event.go, event_test.go.
  - Verify exception/message grouping, custom/default fingerprints, missing IDs and timestamp variants.
- [x] Integrate durable ingestion, issue list/event detail and original responsive UI.
  - Verify real Go/Python/Node/browser SDK errors through authenticated reads, idempotent duplicate events, grouping and restart persistence.
  - Durable errors and UI are implemented. Real Go/Python/Node/browser exceptions pass through authenticated reads. Eight concurrent duplicate events count once. Compose restart preserves an acknowledged event and session.
- [x] Add the non-root Dockerfile and PostgreSQL compose deployment with embedded assets.
  - Verify docker compose up --build from a clean database, readiness, SDK capture and persisted data after restart.
- [x] Compile the user-authorized Tailwind CSS into the embedded UI.
  - Verify a pinned reproducible stylesheet build, browser workflow and responsive widths; retain a single Go runtime image.
- [x] Integrate actual pinned daisyUI components and the original Nexus-inspired dashboard theme.
  - Official standalone plugins compile with Tailwind; their checksums and MIT notice are verified in the non-root image. Existing workflows, component states and responsive access pass with the compiled assets.

## Issue workflow and artifacts

- [x] Deliver compatible bulk state/assignment/native-policy changes and merging.
  - [x] Extract the shared native merge body with unchanged native contracts and existing owning checks.
  - [x] Implement atomic grant-aware bounded selection, globally ordered project gates and shared real mutations.
  - [x] Add strict organization/project PUT admission, bulk-only CLI aliases and honest partial acknowledgements; preserve individual routes/default grants.
  - [x] Verify owning PostgreSQL/parser/HTTP and actual SDK+official CLI/browser, preserved260-event clone/restart and exact public source/CI.
- [x] Complete compatible hash listing and future-only hash detachment with real stable identities and historical preservation; native historical separation does not close this requirement.
  - [x] Add019 route identities/backfill and shared fresh-capture allocation, preserving001..018 rows and native history.
  - [x] Implement scoped snapshot hash samples and committed future-only resets with stable IDs, audit/no-op semantics and real lock ordering.
  - [x] Add exact compatible transport and native Future grouping controls with truthful historical-only originals and preserved drafts.
  - [x] Verify owning/full/race, real SDK/token/CLI/browser, six protected runtimes plus fresh270-event clone/restart, and exact public source/CI.


- [x] Deliver complete compatible individual/bulk issue deletion and usable native controls.
  - [x] Store normalized event and dependent bytes atomically; pin duplicate receipt identity and serialize standalone opaque inputs without losing independent log/span processing.
  - [x] Add020 event:admin scope/index migration preserving every prior row/receipt/default and implement atomic grant-aware complete-group deletion with real route-generation semantics.
  - [x] Add exact DELETE transport with strict admission, truthful committed responses, complete explicit-ID containment and unchanged write/read permissions.
  - [x] Add individual/list deletion confirmation, cross-page selection up to1000, retained merge limits, drafts/URL/focus/error/alias recovery and unchecked admin credentials.
  - [x] Verify owning/full/race/vet, actual SDK/retained CLI/browser, prepared312-event clone/restart with seven protected runtimes, exact public tree and matching CI.

- [x] Deliver individual Delete and Discard Forever for current error issues.
  - [x] Specify exact group fingerprints, independent quota admission, mixed-envelope behavior, category limits, route/scopes and irreversible marker retention in SPEC.md. Public references: Sentry's issue PUT and issue state/discard documentation; do not inspect upstream server internals.
  - [x] Add additive migration and atomically record every still-attached current-group fingerprint before reusing complete-group deletion. Keep ordinary DELETE and detached hashes unchanged; reject feedback and aliases after the project-gate wait.
  - [x] Check tombstones under the capture project gate before event or parented-child writes. Return the normal successful SDK acknowledgment without charging the parent; mixed logs/spans retain request-level project throttling and only admitted siblings consume quota.
  - [x] Add exact `discard:true` PUT parsing/transport and an explicit native detail confirmation. Permit event:write or event:admin for discard, but keep event:admin-only ordinary triage writes forbidden; preserve session CSRF and project/token containment.
  - Test-authoring gate: Store's real PostgreSQL owner must show that current group routes alone become tombstones, matching error/CSP captures leave no event or parented children, foreign-project/detached signatures still capture, feedback remains isolated, restart preserves markers, and a capture already holding the shared gate commits before discard while later matches are dropped. A separate server owner must prove parent-only throttle/quota bypass, mixed-request project throttling before sibling writes, independent log/span charging after admission, and actual response; Store cannot prove HTTP admission or mixed-envelope continuation. Extend the issue PUT HTTP owner for strict `discard:true`, admin/write scope separation, CSRF, feedback/alias rejection and exact204; existing DELETE and update owners do not exercise operation-dependent scope. Extend the existing issue detail browser owner only for the visible confirmation and resulting navigation; API tests cannot prove that UI action. No test-only production seam is needed.
  - [x] Verify owning Store/PostgreSQL and HTTP contracts for discarded multi-origin groups, detached controls, project/fingerprint isolation, error/CSP and feedback categories, restart, parent-only throttle/quota bypass, mixed request throttle and independent sibling charges, capture/discard ordering, API scopes, CSRF and response. Retain only tests with a distinct observable failure mode.
  - [x] Execute the Chromium issue-detail flow against a locally rebuilt single-image runtime with fresh PostgreSQL tmpfs and an actual `sentry-go v0.50.0` exception. The confirmation, cancellation, `204`, list navigation and issue disappearance pass at320/768/1440px in `test-results/acceptance-issue-discard-20261025/browser/report.json`; there are no page errors, failed requests or unexpected responses. The browser owner checks the response status; the HTTP owner separately checks its empty body. The first run caught the CSS `[hidden]` override; the corrected flow passes.
  - [x] Prior exact-source CI [40239347604](https://github.com/jerolan/good-sentry/actions/runs/40239347604) passed full PostgreSQL integration/race, formatting, vet and application-image build at `2226a903b660c4b67ad1a231eaca8877fe801b8b`; [40239723851](https://github.com/jerolan/good-sentry/actions/runs/40239723851) published `:main`, `:latest` and `:sha-2226a903b660c4b67ad1a231eaca8877fe801b8b` for linux/amd64 and linux/arm64 at OCI index `sha256:bc375ab42c3e628574302118f12c720003f53442b623becdbbec417aac0b586a`.
  - [x] Verify the CSS/browser correction at exact-source CI [40241095205](https://github.com/jerolan/good-sentry/actions/runs/40241095205) on `d4b7418ae6ead0dd36a7fa0b5c19520bc907becb`, then publish `ghcr.io/jerolan/good-sentry:main`, `:latest` and `:sha-d4b7418ae6ead0dd36a7fa0b5c19520bc907becb` for linux/amd64 and linux/arm64 in [40241627827](https://github.com/jerolan/good-sentry/actions/runs/40241627827), OCI index `sha256:6d5f4cf6041409273e2ce02f8c2034203492162088310b9ed343419c8a3d0fd6`. Local browser verification used the actual `sentry-go` demo and fresh PostgreSQL tmpfs; focused Go integration tests still require a configured test database.

- [x] Deliver compatible individual issue changes.
  - [x] Share native state/assignment/policy mutation with grant-aware writer authorization and additive event:write scope.
  - [x] Implement strict individual PUT contracts, policy/assignee normalization and opt-in credential UI without changing defaults.
  - [x] Verify owning PostgreSQL/parser/HTTP, real SDK/browser, preserved246-event image clone/restart and exact public source/CI.
- [x] Deliver compatible issue summaries and real analytics.
  - [x] Implement grant-aware RR lists/detail/stats with real user/occurrence/series/comment/release data and unchanged native summaries.
  - [x] Integrate pure public wire shapes and strict scoped routes/queries/Links, with explicit contract differences and pending compatibility flags.
  - [x] Add atomic native detail analytics and accessible affected-user/24-hour chart UI while preserving drafts/grouping/resolution flows.
  - [x] Verify owning tests, actual SDK/browser, cloned120-event image upgrade/restart and exact public source/CI.
- [x] Implement and locally verify the organization short-ID lookup route as a separate compatible issue-read increment.
  - [x] Return organizationSlug, projectSlug, canonical groupId, group summary and canonical shortId through the current membership/grant-aware RR provider.
  - [x] Verify decimal validation, merged-alias resolution, resolved-status lookup, missing/foreign 404s, query rejection and event:read scope. The new HTTP test passes in4.036s; the existing store grant/alias/current-membership test passes in3.847s against an ephemeral PostgreSQL16 instance; focused `go vet` passes for server/store.
- [x] Publish the issue short-ID and first-release commit route commits (`10244ec`, `7a0294e`) directly to `origin/main`, then verify their exact public source and CI. GitHub Actions run [39973299337](https://github.com/jerolan/good-sentry/actions/runs/39973299337) passed the full PostgreSQL/race suite, vet, formatting and application-image build in5m31s.
- [x] Implement compatible issue-first-release commit reads on the global and organization issue paths.
  - [x] Resolve aliases, current release association and ordered commits in one grant-aware read-only repeatable-read transaction; project only the documented five commit fields and return `[]` when empty.
  - [x] Verify malformed IDs/queries, both paths, first-release selection, projection, empty results and `event:read` scope. `TestCompatibleIssuesObservedReleasesGroupedCommentsAndCurrentPolicies` passes in0.555s; `TestCompatibleIssueCommitsHTTPContract` passes in0.395s against disposable PostgreSQL16; integration-tag `go vet` passes for server/store. `git diff --check` is clean.
- [x] Implement actual SDK user-feedback ingestion, issue report retrieval/counts and usable feedback UI.
  - [x] Add029 legacy report storage, receipt/update/pending clocks, generation-safe error binding and bounded orphan cleanup with real PostgreSQL proofs.
  - [x] Add scoped snapshot lists/counts/full-event reports and native/compatible paginated read routes.
  - [x] Receive prevalidated legacy SDK user_report envelopes with real parent/children transaction dispatch.
  - [x] Add strict DSN/session/token submission API with current provider authority and030 opt-in scope.
  - [x] Activate in-process expiry of pending reports through the shared lifecycle.
  - 2026-10-18 UTC: The real REST CORS/cache owner exposed a shared serializer bug: writeJSON replaced explicit private,no-store with its no-store default. Preserve an existing response policy and keep no-store as the fallback. Focused real report/token/attachment/OTLP consumers pass natively(server9.153s/store16.075s) after this repair. Existing API-token negative input used project:write as its unknown-scope fixture;030 now implements that scope, so the owner uses the still-unsupported unimplemented:write value instead. REST authorization/lifecycle acceptance remains open.
  - 2026-10-18 UTC: REST provider and transport prerequisite passes.030 preserves every old token/report/migration row over upgrade/reopen and grants project:write only when requested. Ordinary current members and restricted numeric-slug/project scopes are rechecked in the report transaction; DSN keys must match the actual organization/project and remain enabled. Real blocked membership deletion/key revocation, cancellation and first-receipt clocks pass; removing each authority row lock fails its owning test before exact restoration. The bounded test-engineer's missing-route405 RED precedes actual HTTP GREEN; primary inspects and corrects a POST mislabeled as a read, a masked missing/nonstring-field fixture and an unsupported surrogate expectation, then adds actual grant containment, media-header ambiguity, CORS/private-cache and409 dispatch checks. All session CSRF/origin, authoritative invalid Bearer/SDK sources, mixed-credential403, unsupported read permissions, strict JSON/query/media/encoding/size and actual DTO cases pass. Shared report decoding retains envelope extensions; current tokens/default scopes are not widened, and UI purpose text/README state the limited new scope. Native report/token/attachment/OTLP siblings server9.153s/store16.075s, Linux race server9.537s/store17.184s and final integration vet/JS syntax pass. Independent review returns no actionable provider/transport finding; primary separately catches and repairs the shared cache override. Evidence: ignored feedback-20261018/{rest-store-red,rest-store-first,rest-http-worker-baseline,rest-http-first,rest-siblings-green,rest-siblings-green-final,rest-race,rest-vet-final,mutation-authority-member-red,mutation-authority-key-red}.log. Sourcebd095e1 has successful full-race/image CI39110776766/job117626258313 with actual105209-character logs inspected(store92.479s/server40.766s/jobs9.743s). Seventeen protected runtime checkpoints and4447 accepted rows remain exact. Pending worker activation, report UI, final-image/official-SDK acceptance and modern feedback remain open.
  - 2026-10-18 UTC: StartPendingUserReports is the seventh real consumer of the shared maintenance loop and starts unconditionally with the common interval. Fixed30-minute pending age adds no configuration/service. A deliberate no-drain mutation fails the real601-row startup owner, then exact restoration passes the complete seven-provider backlog/idle/two-pool/SQL-retry/idempotent-stop matrix. Full14-package native integration passes(jobs16.373s/server65.368s/store240.367s), full LinuxGo1.27.2 race passes(jobs14.743s/server53.972s/store210.931s), and integration vet/JS syntax pass. Native and race use different databases on the disposable test PG so database-scoped receipt locks cannot interfere across runs; the added race database goodsentry_feedback_race_20261018_0234 contains only owned test schemas and no accepted source data. Evidence: ignored feedback-20261018/{pending-lifecycle-red,full-native-api-lifecycle,full-race-api-lifecycle,vet-api-lifecycle}.log. Legacy report UI and complete actual-image/SDK/browser/recovery/public acceptance remain open, independently of modern feedback and the full goal.
  - [x] Add usable safe report UI and verify full integration/race/vet, actual SDK/browser/copied-image restart/recovery and exact public source/CI.
    - 2026-10-18 UTC: Final native report UI passes on static nonroot LinuxGo1.27.2 imagef574f59e4a22a1e2e5ca5e84e07389369f14f2b518ded42c530dc9f52b0a1fcb,31,322,682 bytes,150 CA certificates, exact license and six source-exact assets. On a fresh logical4447-row copy, official Node11.7 sends61 errors and Browser7.120.4 sends29 reports using real transports across origins. Real session/DSN/Bearer and ordinary-member reads/writes pass;27 reports link to the60-event group, two start pending, and the first reported event is outside the native50 summaries. An official later Node parent binds one pending report without changing its ID/text/clocks, and the running worker removes only the separately aged owned orphan. Real parent expiry then removes only its parent/report and recounts the retained issue to0. Actual Edge tests25/2 paging, exact128/254/10000-character hostile fields, the correct historical stack and binary bytes, all drafts through triage, keyboard focus, delayed selection/page/SPA responses, empty/bound/expired states, and page/event/module errors and retries at320/768/1024/1440. The reusable owner is tests/compat/user_reports_browser.mjs; primary inspects screenshots. Focus loss, cached failed module import and nested grid clipping have concrete retained REDs before their fixes. A fixture-only early enabled assertion and late controlled-console classification are corrected without changing product expectations. The existing favicon404 stays visible and out of scope. Full14-package native integration passes(server58.129s/store208.086s/jobs18.033s), full Linux race passes(server74.498s/store209.284s/jobs34.469s), vet/JS syntax pass. Separate4569-row/601-event/27-report/30-migration/39-table recovery and actual binary download plus same-candidate restart are exact. Docker Desktop closes externally before final build; original checkpoints stay immutable, seven earlier accepted databases plus the4010-row reference and current SDK rows audit exact. Only two protected process states change, the existing PG containers restart in place, and changed bridge IPs cause a preserved failed candidate before final startup uses host.docker.internal:18094. Both earlier UI images/containers, failed final candidate and all data remain preserved. Evidence: ignored feedback-20261018/full-{native,race}-ui.log,vet-ui.log,browser/{initial,initial-resume,initial-green,initial-source-css,initial-final-stable,versioned-owner-verified,pending-bound,parent-expired,module-red,layout-overflow-red} evidence and image/{sdk-api,pending-runtime,parent-expiry,ui-final-start,recovery,external-engine-audit,accepted-rows,accepted-runtime}.json. Exact public source and matching CI still gate legacy closure; modern feedback and the full goal stay open.
  - [x] Specify and deliver modern captureFeedback separately, including standalone/associated submissions, attachments, category-filtered issues and usable triage. Legacy report success alone does not close this parent.
    - [x] Verify public API/client wire and current owned category/counter/grouping consumers; specify the complete modern consumer before implementation.
    - [x] Implement031, validated normalization, category-isolated capture, numeric-generation association and bounded binding with real preservation/rollback/locking/isolation proofs.
      - 2026-10-18 UTC: Five normalization owners and nine real PostgreSQL owners verify independent feedback identities, unchanged retry payload/receipt clock, binary children, old issue category projection and migration receipts, same-category merge/unmerge, cross-category atomic rejection, same/foreign/missing/nonerror targets, first numeric generation, target/own cascade, bounded parent250 and catch-up250/250/101, actual receipt/project waits, frozen targets, row/maintainer skips and statement/deferred-commit/cancellation rollback. A failed child on a reversed-arrival error also rolls back its prior feedback binding. Metadata omission, error-frequency leakage and numeric-target substitution mutations fail their actual owning assertions before exact restoration. Native siblings pass store41.124s; full14-package native integration passes store204.846s/server52.585s/jobs14.019s, focused Linux race passes store44.957s, and integration vet passes. Final added retry/fast-path and parent-rollback assertions pass10.979s/2.261s; the first clock fixture incorrectly queried received_at instead of the existing created_at, and the corrected fixture retains that failed attempt. Accepted4569 rows/current/recovery,4447 prior rows and eighteen protected runtimes remain exact, with031 never applied to originals. Evidence: ignored feedback-modern-20261018/{normalization-red,normalization-green,store-first,migration-siblings-first,migration-siblings-green,mutation-metadata-red,mutation-frequency-red,mutation-frozen-target-red,provider-siblings-green,provider-siblings-race,full-native-provider,final-owner-green,parent-rollback-green,provider-vet-final}.log and protected-state.json. HTTP dispatch, lifecycle, category/read/UI consumers and copied-image acceptance remain open.
    - [x] Receive modern envelopes, activate bounded association catch-up and add scoped category-aware native/compatible reads without changing legacy consumers.
      - [x] Add category-state search/projections and authorized feedback/associated-error readers with real snapshot and transport owners.
        - 2026-10-18 UTC: Native/compatible category terms now select durable issue state, including zero-event histories, with exact values, lists, negation and explicit literal-tag preservation. State/occurrence partitioning covers metrics and bulk PUT/deletion; native category selectors and the existing Issues request explicitly isolate errors. Feedback pages expose real text/receipt times and25-item paging; native detail shares its first page/count snapshot. The associated-error reader joins only the bound numeric receipt and current issue, never a recreated SDK identity. Pure event/issue projections preserve feedback type/category, actual message, contexts and null legacy report even with conflicting payload hints or no latest event. Actual parser/tag-collision, missing-route and empty-history projection REDs precede GREEN. Three deliberate state-partition/numeric-substitution/read-committed mutations fail owning assertions, with source restored exactly. Final owners pass server3.619s/store9.633s/eventview0.231s. Full14-package native integration passes store258.182s/server65.233s/jobs36.937s, full Linux race passes store221.044s/server53.545s/jobs22.636s, and integration vet/JS syntax pass. Read tests cover27 receipts, reverse payload-time order, aliases/unmerge, current membership/actual foreign captures, same-snapshot deletion, original versus recreated target, strict pages, private cache and bare-token denial, error totals and category-only bulk changes. Final HTTP source is rerun after mutation restoration; an overlapping diagnostic run is not its acceptance evidence. Worker fixtures and misplaced logs are retained/corrected; the earlier passing storage log is classified as baseline, not RED. Protected4569 current/recovery rows,4447 prior rows and eighteen runtimes remain exact. Evidence: ignored feedback-modern-20261018/{read-store-compile-red,read-store-first,query-worker-red,query-store-red,issue-projection-red,projection-worker-red,read-http-worker-red,read-final-owners,mutation-state-partition-red,mutation-associated-numeric-red,mutation-read-snapshot-red,full-native-reads,full-race-reads,vet-reads}.log and reads-protected-state.json. UI and actual SDK/copied-image acceptance remain open.
      - 2026-10-18 UTC HTTP/worker publication: dbaf9905342805572f88bf6fd7cefba4ff91e289 has366 exact public blobs/six unchanged Pages files and successful CI39152807917/job117749739059. Primary inspects111012 actual log characters; full PostgreSQL race store107.254s/server44.269s/jobs16.505s, formatting/vet/image pass. Saved http-worker-ci-success.log SHA256a434ac884ba5307b9e677594bed954cebc9d9df96a0e2ba6ddadffe256c54eeb and http-worker-public.json prove publication. DraftPR2 remains unmerged and main unchanged.
      - [x] Dispatch modern feedback parents and their dependent children through the real SDK HTTP writer.
      - [x] Activate and verify the eighth real in-process maintenance lifecycle for pending modern associations.
        - 2026-10-18 UTC: StartFeedbackAssociations runs unconditionally at the existing common interval, drains committed positive bindings and retries with static operation-only logs. The real lifecycle matrix covers601 pending submissions, idle catch-up, independent pools, declared UPDATE failure and repeated prompt stop. Primary corrects worker fixtures whose small batches were consumed entirely by error capture, whose retry seed could race ahead of failure installation and whose stopped-row check slept before making a row pending. Real row locks now make the actual parent skip pending candidates, preserving the worker's ownership without a test seam. The no-drain mutation fails its startup backlog assertion before exact source restoration. All eight cases pass15.806s; complete14-package native integration passes jobs33.439s/server62.578s/store242.802s, complete Linux race passes jobs22.177s/server53.653s/store212.403s, and integration vet passes. Current/recovery4569 rows, prior4447 rows and eighteen runtimes remain exact;031 and the new worker have not run on accepted originals. Evidence: ignored feedback-modern-20261018/{focused,lifecycle-green,lifecycle-no-drain-red,full-native-http-worker,full-race-http-worker,full-vet-http-worker}.log and api-worker-protected-state.json. Category/read/UI consumers and full official-SDK/copied-image acceptance remain required.
        - 2026-10-18 UTC: The official-wire HTTP owner first fails with400/attachment-requires-event, then passes typed/defaulted feedback, common event/store JSON, exact binary children, parent/type/ID rejection, invalid DSN, both kind-collision directions, unchanged first retry payload/clock and a late-child rollback. Primary corrects worker fixtures that assumed a nonexistent attachment receipt column, posted envelope framing to /store and expected normalized level inside raw JSON. Feedback bypasses error source-map rewriting to retain its supplied contexts. Full native server55.627s and focused Linux race server2.829s/event1.068s/protocol1.128s pass. Final snapshot assertions also pass in the complete native/race run recorded with worker activation. Evidence: ignored feedback-modern-20261018/{http-worker-red,http-first,http-siblings-green,http-full-native,http-focused-race,http-worker-vet}.log. Actual official SDK network/image acceptance remains required.
      - 2026-10-18 UTC storage publication: Commit52465b89f4c026cc6a9706542cb1c8a275627189 has365 exact public blobs and six unchanged live Pages files. Draft PR2 targets main while including accepted legacy PR1, whose merge permission is still pending. Matching CI39152001858/job117747395698 succeeds for formatting/vet, full PostgreSQL race(store86.864s/server39.981s/jobs9.667s) and image build. Primary inspects the actual110406-character job log; saved storage-ci-success.log has SHA25665e9e86725a670058c017a5006b0147f4d7a732110c6adf4feb3378eb167d10c. Public proof is storage-public.json. No modern consumer acceptance or main publication is claimed.
    - [x] Enforce and prove the specified target pin before feedback issue-route/own-receipt writes.
      - 2026-10-18 UTC: An actual route-table wait plus target FOR UPDATE NOWAIT probe fails on the prior implementation because no target pin exists yet. Capture now selects/pins behind its existing shared project gate and before own routing, retaining the chosen reference/receipt/pending state only until the atomic metadata insert. Sorted receipt gates and duplicate handling stay ahead of this stage; pending/error binding and rollback remain owned by the same transaction. The new order owner and feedback/report/capture/triage/group/lifecycle siblings pass natively store37.377s/server9.168s/jobs17.234s and Linux race store40.414s/server9.277s/jobs19.219s. Integration vet passes. Protected4569 current/recovery rows,4447 prior rows and eighteen runtimes remain exact. Evidence: ignored feedback-modern-20261018/{capture-pin-order-red,capture-pin-order-green,capture-pin-order-race,vet-capture-pin-order}.log and pin-protected-state.json. Native Feedback UI and actual-image/SDK acceptance remain required.
    - [x] Add the reusable official Node11.7 network runner and verify its fixed-ID feedback/attachment/retry phases on a fresh protected copy.
      - 2026-10-18 UTC: tests/compat/modern_feedback_sdk.mjs uses the ordinary client transport, explicit prepared IDs and per-operation flush. The actual image on18126/new database goodsentry_feedback_modern_20261018/new owned upload volume preserves all4569 accepted rows through031's explicit category:error projection, with601 existing events and31 migrations. All61 main feedback submissions persist, including exact10000/128/254/2048-character fields and binary bytes. Native merge yields25/25/11 pages with the special submission outside the first50. The first SDK retry preserves every receipt/metadata/issue field and clock while appending only a new binary child. A locked pending row makes real late-error capture skip it, then the actual eighth worker binds only its numeric target. Actual retention and separate intentional SDK-ID generations prove permanent unavailable association and independent own-feedback expiry, with full before/after preservation on the verified expiry generations. The initial expiry verifier incorrectly expected only an issue count change despite remaining events, and did not save its full preimage; that diagnostic is not whole-delta acceptance. Its subsequent generation captures a full preimage and verifies the existing recount contract without resetting the cohort. Known nonerror targets are unavailable; only genuinely missing targets are pending. Protected4569 current/recovery rows,4447 older rows and eighteen original runtimes remain exact. Image797d4dd1 is31409176 bytes, static Linux amd64 Go1.27.2, UID65532:65532,150 CA certificates, exact license and seven source-exact assets; memory remains unmeasured. BinarySHA2569a83ec0918457a17e00d2ee837937f6a7070524e24867f5338fa7ff108f7ee07. Windows Git archive first converted pinned vendor bytes; separate9bb0614 preserves them, and the canonical context verifies all373 committed blobs. Copy controller fixture/import/hash assumptions were corrected on the same database, preserving diagnostic logs. Evidence: ignored feedback-modern-20261018/{archive-pinned-bytes,runtime-context-canonical}.json and image/{image-proof,sdk-server-parents,sdk-server-aux,sdk-server-first,sdk-server-rest,late-binding,merged-pages,sdk-retry-preserves-first,expire-errorLate,expire-aux,recreated-sdk-id-intermediate,recreated-sdk-id-final,browser-cohort-ready}.json. These are SDK/image prerequisites; modern browser, restart, backup recovery, final public source/CI and the parent feature remain open.
    - [x] Verify the dedicated modern browser owner and the existing legacy owner on the same actual image and copied SDK cohorts.
      - 2026-10-18 UTC: Ordinary-member Edge passes modern-owner-final at320/768/1024/1440 for actual25/25/11 pages, the first receipt outside native50, exact hostile text and binary download, selected associated raw/current issue, missing/unavailable/nonerror references, retained zero-event feedback, simple triage and comment/assignment drafts, category/direct-detail/filter behavior, keyboard focus, page/detail/module retry and stale selection/mode/page/SPA responses. Primary reviews the worker owner, corrects fixture/selector/state assumptions and a test route-unregister race, bounds controlled waits and classifies only the explicitly injected module503 abort. Earlier failed logs/screenshots remain; no product change was needed to pass those harness corrections. Both wide/mobile actual screenshots are inspected. Existing user_reports_browser.mjs also passes legacy-modern-candidate with its original60-ID client plan,27 retained reports and older-than50 binary parent, at all four widths, without reseeding. A read-only readiness check disproves the explorer's unverified inference that the old cohort was unusable. Full pre/post legacy snapshots verify that only the copied legacy root resolved_at and two authorized status activity rows change; events/reports/all other rows stay exact. The declared exact digest projection preserves that copied-only change for later checks, while original18122/18124 remain byte/row exact. Runtime-image commit9bb0614 matches373 public blobs and six Pages files. CI39157923130/job117764906524 passes full PostgreSQL race(store137.931s/server48.270s/jobs16.561s), formatting/vet/image; primary inspects112356 actual log characters, saved SHA256c17299d80418eb25843c93da2fa7c3746b964b1e8b6ec8093c8c12f5527e7ff8. Evidence: ignored feedback-modern-20261018/{canonical-public.json,canonical-ci-success.log,browser/modern-owner-final.json,legacy-browser/legacy-modern-candidate.json,image/legacy-browser-ready.json,image/legacy-browser-delta.json,image/copy-legacy-issue-projection.json}. Same-app restart, separate backup recovery, final source/binary/public-CI and modern/parent closure remain open.
    - [x] Verify the completed modern cohort through a separate backup restore and same-application restart, with original-state protection.
      - 2026-10-18 UTC: The canonical image preserves4980 rows/665 events/61 modern submissions/27 legacy reports/31 migration receipts/40 tables across its own restart and a separate goodsentry_feedback_modern_recovery_20261018 database, owned upload volume and runtime on18128. Backup450306 bytes/SHA256dabfc336d1e040fcf318b89088ad2e874c6dee2eda6fbafc677d63338d0ffe1d restores exactly. Recovered ordinary-member reads verify25/25/11 pages, every extreme special field, numeric-bound current associated issue, unavailable404 despite recreated SDK ID and exact original/retry/legacy binary bytes. Upload archive manifests and runtime image/config/mounts remain exact. Accepted modern/current and recovery row/runtime snapshots are preserved under ignored image/{accepted-rows,accepted-runtime,recovery-runtime,recovery}.json; backup/journal remain private. All4569 original legacy current/recovery rows,4447 prior rows and eighteen protected runtimes remain unchanged. A bounded read-only UI reviewer withdraws its internally contradictory URL finding after primary source and actual browser verification; no product change follows. README now describes verified modern/legacy behavior and retains explicit parity gaps. Final published source, reproducible binary and matching CI still gate modern/parent closure.
    - [x] Deliver native Feedback navigation, paginated submissions/attachments/exact associated-error UI and complete actual-image/SDK/browser/recovery/public-CI acceptance on a new4569-row copy.
      - 2026-10-18 UTC closure: Functional097997b6d928cbc3769a9aa89d095d86e8b1373a has375 exact public Git blobs and six exact live Pages files. Matching CI39159883824/job117770761169 passes formatting/vet, full PostgreSQL race(jobs26.281s/server52.571s/store142.421s) and image build. Primary inspects111127 actual connector log characters, saved111128-character log/SHA256cc6dabcdd8e36747f6a7af39c101761a31bd932f8c21500fc959b9672cba2683. An independent network-disabled Go1.27.2 build from all375 exact committed blobs matches the actual image binary byte-for-byte atSHA2569a83ec0918457a17e00d2ee837937f6a7070524e24867f5338fa7ff108f7ee07. Both modern and existing legacy actual-browser owners, real official Node network receipts, generation-safe retry/binding/expiry, same-app restart and separate backup recovery pass. Accepted18126/18128 retain4980 rows/665 events/61 modern submissions/27 legacy reports/31 migrations/40 tables; backup450306 bytes/SHA256dabfc336d1e040fcf318b89088ad2e874c6dee2eda6fbafc677d63338d0ffe1d. Independent read-only audit verifies prior4447/4374/4342/4305/4258/4235/4209 checkpoints, both4569 legacy copies, immutable4010-row reference and all eighteen earlier runtime checkpoints. Ignored image/{functional-acceptance,evidence-index,all-prior-rows-audit}.json and reproducible-final-source.json retain proofs. Close modern and its parent feedback task only; native symbols, compatible attachment management, archival and all other unchecked full-goal requirements remain open. The user explicitly authorized PR1 merge; it is merged as e1f207d24fcd834360658bd9663c86f24f0c17ae with the exact accepted317d253 tree and preserved atomic history. PR2 merge is not authorized by that PR1-only approval. Pages assets were unchanged, so its path-filtered workflow needs no new deployment; the six live files still match.
      - [x] Integrate the category view, simple triage and lazy paged inspection module, preserving the existing issue consumer and regenerated styles; runtime acceptance remains open.
        - 2026-10-18 UTC source prerequisite: Feedback navigation and durable-category direct links share scoped lists/details while resetting stale category selections. Feedback triage omits error mute/release forms; own submission inspection uses exact selected raw JSON/binary attachments, and associated inspection uses the numeric reader without a target-file lookup. The bounded frontend worker owns only feedback.js; primary reviews it and fixes initial snapshot reuse, triage selection restoration, explicit refresh/retry focus, multiline text, response validation and unavailable wording before runtime acceptance. Primary integrates callbacks, safe tag/issue links, static allowlist and single-column nested CSS, retaining the legacy renderer through one shared event-content function. Syntax and integration vet pass; full14-package native integration passes store251.655s/server64.230s/jobs34.919s and full Linux race passes store218.830s/server55.007s/jobs26.301s. A clean372-file build context excludes unrelated worktree directories; the Dockerfile verifies pinned daisyUI files, Tailwind4.3.3 compiles, and the current generated app.css is regenerated with that verified compiler. This is source/build evidence only, not actual SDK/browser/image acceptance. Evidence: ignored feedback-modern-20261018/{ui-source-first,full-native-ui-source,full-race-ui-source,vet-ui-source,ui-build-initial,ui-css-current}.log and ui-context-initial.json. No original database/image has been upgraded and no modern cohort has been seeded.
    - 2026-10-18 UTC modern specification: Public Node/custom-widget/API documentation and exact official Node11.7 black-box wire show standalone feedback, optional name/contact_email/url/associated_event_id, hint tags and binary attachment. Seven cases include blank optional values, retained uppercase/UUID IDs and an empty message that the SDK still sends; no network acceptance is inferred. Primary independently verifies current normalization, ingestion, grouping, state/occurrence partitioning, error-frequency, project-event and trace consumers after a bounded read-only explorer. SPEC now owns category:error|feedback, one new issue per SDK feedback identity,031 old-field/default-category preservation, atomic children, optional numeric-bound association with permanent no-rebind after target deletion, bounded fast-path/catch-up and real same-project authority, simple feedback triage, category search/projections and native paged/selected/associated-error consumers. Existing nontransaction error predicates must exclude the new kind only where they count errors; retention/group deletion continue to include it. The captured public wire is ignored feedback-modern-20261018/official-client-wire.json and public sources/text have SHA256 provenance; it does not prove a server capability. All implementation and acceptance children remain open, all accepted4569-row/current/recovery and prior originals remain protected, and the full goal stays active.
    - 2026-10-18 UTC legacy closure: Functional cfa766f6dc30029dd8fbb98e246e5f889db9e9fd has357 exact public Git blobs,11 plaintext source/artifact checks and six unchanged live Pages files. Matching CI39147978078/job117735612268 succeeds for formatting/vet, full PostgreSQL race(jobs9.540s/server42.240s/store84.560s) and image build; primary inspects its actual108024-character connector log and saves the newline-normalized108025-character ui-ci-success.log/SHA256cf26656f4a42631f7f4611760d9db7edbf6eee55bc048c4efd3db3c0314d6f9d. The published source recompiles exactly to actual final-image binarySHA256c4b0cbc6b3179e01a822d1425c588d3e22d927fc65b5d0e96d32ca48abc99978. Final4569 rows/601 events/27 reports/30 migrations/39 tables and separate recovery remain exact after same-app restart. BackupSHA256f764754f080f1a497fc834ca020654545e914c3595952f6bcbf827b4791e87b7. All4447 original rows, seven earlier accepted databases,4010-row immutable reference and eighteen post-interruption runtime checkpoints pass. Evidence: ignored feedback-20261018/image/{public-functional,functional-acceptance,recovery,accepted-rows,accepted-runtime,external-engine-audit}.json and ui-ci-success.log. Only the legacy child closes. PR1 remains on codex/legacy-sdk-feedback, main1e241852 stays unchanged, and the parent/modern feedback plus every other unchecked product requirement remain open. Modern planning begins with public SDK/API documentation and exact client wire, then primary verification of the explorer's factual category/counter/grouping/attachment map before its SPEC contract.
  - 2026-10-18 UTC: Storage prerequisite passes eight real PostgreSQL owners, including merge/unmerge membership, parent cascade/fresh ID reuse, reversed arrival and expiry, same-envelope atomicity, receipt/update windows, a real blocked writer clock and cancellation, strict microsecond pending cutoff,601-row bounded backlog with held-row progress, maintainer skip, statement/deferred-commit/cancel rollback and029/reopen historical preservation. Three deliberate mutations fail their owning assertions before exact source restoration. Owning capture/error/release siblings pass22.922s, Linux race25.854s, full native integration across14 packages passes with store217.724s/server52.494s and integration vet passes. The first bounded fixture used unsupported parameterized multiple SQL commands; corrected test setup is retained in evidence. New029 exposed an existing release rollback snapshot restricted to028 compared with all-current migrations; that owner now compares all-current receipts on both sides. Primary independently reviewed the storage and inspected worker findings; no actionable storage defect remains. Protected accepted4447 rows/540 events/28 migrations and all sixteen earlier runtime states remain exact. Evidence is ignored test-results/feedback-20261018/{store-compile-red,store-first,store-owners,store-siblings-green-final,full-native-storage,vet-storage,mutation-clock-red,mutation-binding-red,mutation-cutoff-red}.log and storage-protected-state.json. No HTTP route, cleanup lifecycle, report UI or modern feedback acceptance is claimed from this storage-only commit; parent feedback remains open.
  - 2026-10-18 UTC: Storagef68c2ed2cdab15c6457568e0726b1a37839cfd87 is public on codex/legacy-sdk-feedback with344 exact Git blobs, six unchanged Pages files and successful PR CI39109375327/job117621990655. Actual logs inspected: full race store117.885s/server43.283s, formatting/vet/image pass; saved storage-ci-success.log and storage-public.json. Auto-review rejected a direct default-main push because the exact protected-branch mutation was not authorized. Primary used the safer feature branch and draft PR1, attached to this chat; main remains1e241852. Do not bypass that rejection through merge/ref/API or another execution path without new authorization.
  - 2026-10-18 UTC: Legacy read prerequisite now passes real scoped project/issue lists,25+1 pagination/totals, pending-project visibility, merged aliases and historical separation, membership/grants, read-only snapshots, native first-page/count and compatible full-event/detail count. The real blocked native reader remains consistent while a newer error/report commits; deliberate read-committed/count-zero/projection-aliasing mutations each fail their owning assertion. HTTP route RED precedes GREEN for exact paths/scopes/strict query/Links/native empty arrays and actual single/list event reports. The bounded test-engineer extends only the existing projection owner; primary inspects it and removes a redundant summary assertion while preserving null coverage. Native sibling suite store42.588s/server8.572s, Linux race store51.449s/server10.180s/eventview1.056s, full14-package native integration store208.511s/server57.366s and integration vet pass. Protected4447 rows and all seventeen runtime checkpoints remain exact. Actual black-box official Browser7.120.4 captureUserFeedback yields user_report with envelope event_id and event_id/name/email/comments payload; Node11.7.0 captureFeedback yields feedback. Python2.72/2.35 and Node7 entry points lack that legacy callable. Primary reruns and inspects actual SDK wire/provenance in ignored .tmp/feedback-sdk-{probe.py,wire.json}; this proves serialization only. Submission HTTP, pending lifecycle, UI and modern feedback remain open. Read evidence: ignored feedback-20261018/{read-compile-red,read-http-red,read-http-green,read-siblings-green,full-native-read,vet-read,mutation-read-snapshot-red,mutation-read-count-red,mutation-projection-copy-red}.log and read-race-observed.json.
  - 2026-10-18 UTC: Readfd2f888cf5333f771d849249e669ded54def9cdb has successful matching PR CI39110158510/job117624364491. Primary inspects its105479-character actual log: full race store119.663s/server45.209s/jobs10.286s, formatting/vet/image pass. The same branch remains a draft with main unchanged. Legacy SDK envelope dispatch now passes its actual unsupportedItems RED then real HTTP GREEN for standalone pending/report-before-error binding, combined error/binary attachment/report, missing outer ID with normalized UUID, repeated reports/identity/type/null/missing/UTF8/Unicode-length rejection, invalid key,409 overwrite expiry and a declared late database report failure that rolls back the fresh parent and every child. Rejected fresh identities preserve exact events/issues/attachments/reports/opaque rows. All legacy report fields are checked before ingestion writes; independent log/span/opaque transaction boundaries are explicit in SPEC, without changing their existing behavior. Shared NormalizeUserReport now has a production HTTP caller; all old internal callers move in the same increment. Full native HTTP42.243s, report/capture Store siblings10.571s, integration vet and focused Linux race(server2.002s/store9.806s/protocol1.105s) pass. No cleanup lifecycle, REST submission, UI or actual-image/SDK acceptance is claimed. Evidence remains feedback-20261018/{sdk-http-red,sdk-http-first,sdk-http-green,sdk-full-http-native,sdk-store-siblings,sdk-vet,sdk-focused-race,read-ci-success}.log; protected4447 rows and seventeen runtime checkpoints stay exact.

- [x] Deliver compatible event inspection through project and legacy/organization issue routes.
  - [x] Add event:read without changing credential defaults or old rows, and implement pure compatible event/JSON projection, including reachable public request/breadcrumb contracts.
  - [x] Add grant-aware snapshot event reads, ordering/time/environment/query filters, sibling IDs and compatible pagination.
  - [x] Wire strict routes/permissions and explain the implemented scope purposes in credential UI.
  - [x] Verify owning PostgreSQL/projection/HTTP, actual SDK/browser, copied-image upgrade/restart and exact-source publication/CI.

- [x] Deliver search, tag/environment/release filters, sorting and pagination with UI.
  - [x] Specify and implement bounded native search parsing with tag/quoted/list/negation/wildcard semantics and useful invalid-query errors.
  - [x] Select historical same-event matches with authorized snapshot totals/pages and deterministic lifetime-summary orders, preserving raw data.
  - [x] Connect strict native HTTP queries, release/All status/first-seen controls and safe clickable event tags with complete URL/history navigation.
  - [x] Verify PostgreSQL concurrency/reopen, HTTP grammar boundaries, actual SDK/browser25+1 workflows, final image restart and remote CI before closing search.
- [x] Deliver resolved/ignored/regression workflow, assignment, comments and activity history. Public browser and PostgreSQL owners pass resolve/reopen, comments, assignment, mute/regression and recorded activity.
  - [x] Native increment: transactional user/team assignment, simple-resolution regression and recorded activity; preserve unique captures and historical rows.
  - [x] Native comment edit/delete/moderation and bounded independent histories, strict authenticated routes and responsive UI.
  - [x] Verify real PostgreSQL concurrency/reopen, transport boundaries, actual SDK/browser workflows, final image preservation and exact-source CI.
  - [x] Native conditional mute: timed/additional-count conditions, new-notification suppression and always-on expiry, with provider/HTTP/worker/browser/SDK/image/publication acceptance.
  - [x] Complete native release-anchored/next-release resolution as a separately specified increment; full compatible issue management remains open.
  - [x] Implement release comparison/classification and immutable selected/pending/bound policies with issue-scoped candidates and additive014 preservation.
  - [x] Integrate staged ingestion/alert eligibility, metadata-only pending binding, strict native resolution requests and original responsive controls.
  - [x] Verify old/equal/new/late/retry release behavior, finalization and real lock/cancellation/replica paths, actual SDK/browser/final-image workflows and exact publication before closing native release resolution.
- [x] Deliver merge/unmerge with preserved event partitions and correct future grouping.
  - [x] Add migration015/private monotonic capture progress and prove existing mute/upgrade/reopen behavior independently of historical count changes.
  - [x] Consolidate authorized issue/event/comment detail in one snapshot while preserving the existing HTTP response and consumer assertions.
  - [x] Add constrained flat topology, project lock ownership, atomic partition merge/separation and unchanged policy/notification semantics.
  - [x] Canonicalize aliases, add origins/combined histories and deliver strict native merge/unmerge transport, including stale alias409 and retry no-ops.
  - [x] Deliver safe responsive list selection and origin separation with preserved drafts/navigation and existing controls.
  - [x] Verify owning PostgreSQL/concurrency/migrations, actual SDK/browser regressions, final-image upgrade/restart from an isolated copy of the100-event baseline and exact public tree/CI/site before closing the native child.
  - [x] Complete compatible API0 merge/hash operations and their required event permissions against public contracts; the native child does not close this parent.
- [x] Deliver releases, commit metadata and deploy records through compatible API.
  - [x] Define typed release contracts, shared organization/version storage and historical observed associations without changing original telemetry/maps.
  - [x] Implement scoped snapshot reads and transactional create/update/project-remove/delete, ordered complete commit sets/manual repositories and completed deploy records.
  - [x] Integrate fresh-only SDK/map observation and serialized artifact deletion, preserving duplicate suppression, existing mapped/raw stacks and255-character exact versions.
  - [x] Add compatible organization/project routes, native UI pagination and actual scoped project:releases credentials with existing cookie/CSRF protections.
  - [x] Deliver the responsive member/owner Releases workflow and unchanged read-credential regression.
  - [x] Verify real PostgreSQL/HTTP boundaries, official CLI lifecycle/local commits/previous-head/deploys, final image upgrade/restart, existing SDK/history and remote CI. Automatic repository refs/integrations remain separate acceptance.
- [x] Deliver release/debug-ID JavaScript artifact uploads and verified source mapping.
  - Native upload/list, mapped ingestion, modern CLI upload, their UI and final remote CI/publication pass; exact evidence is recorded below.
- [x] Deliver authenticated native source-map upload/list and mapped capture through API and UI.
  - Owners/admins upload; actual members read metadata. Release/dist and debug-ID selection, original stacks, source location/context, idempotent retries, immutable-content conflict and input/processing budgets are verified.
- [x] Research and implement modern sentry-cli artifact bundles, then test the actual CLI.
  - [x] Fix upload contracts and typed quota/expiry/materialization configuration; add migration011 without changing historical maps or migrations.
  - [x] Persist authorized raw chunks with atomic quota/expiry and snapshot ordered assembly reads; atomically save scoped multi-project map sets with existing release-delete ordering.
  - [x] Resolve bundle file identity and script/map URL aliases in the independent consumer while preserving native matching and source bases.
  - [x] Connect compatible negotiation/multipart/assembly routes, strict size/checksum/gzip boundaries and automatic staging cleanup.
  - [x] Verify PostgreSQL/HTTP and official CLI injection/upload/retry/resume plus a real JavaScript SDK mapped exception, image restart/history, browser regressions and remote CI.
- [ ] Implement native debug-file and minidump ingestion plus verified native symbolication.
  - [x] Reject overflowing Memory64 file ranges before slicing.
    - Contract: each Memory64 file offset must be within the dump and its declared byte count must fit in the remaining dump bytes. Validate by subtraction before slicing, return a parse error for invalid descriptors, and never panic or return partial memory ranges. Microsoft's public [`MINIDUMP_MEMORY64_LIST`](https://learn.microsoft.com/en-us/windows/win32/api/minidumpapiset/ns-minidumpapiset-minidump_memory64_list) defines the list-wide base RVA and cumulative descriptor sizes.
    - Test-authoring gate: the observable behavior is a parse error for a corrupt Memory64 descriptor with `base = ^uint64(0)` and `size = 1`; current `base + size` wraps to zero and reaches an invalid slice. Existing Minidump parser tests have no explicit Memory64 overflow owner. Extend the `ParseMinidump` owner with a minimal Memory64 stream fixture; no HTTP seam is needed.
    - Acceptance: wrapped and ordinary out-of-file descriptors return errors without panic; valid in-file ranges and all existing native Minidump behavior remain unchanged.
    - Verify: focused parser regression, `go test -race ./internal/native -count=1`, full `go test ./... -count=1`, integration-tag vet, trim-path app build, formatting, `git diff --check`, exact-source CI and amd64/arm64 image publication through `gh`.
    - Source: bounded Minidump stream parsing contract in this specification; Memory64 stream offset/size fields are 64-bit unsigned values.
    - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `internal/native/minidump.go`, `internal/native/minidump_test.go`.
    - Local evidence (2026-10-26): the regression reproduced the pre-fix panic at the maximum `uint64` RVA; weakening the guard also caused all malformed cases to panic. The focused/full native race tests, full Go suite with loopback access, integration-tag vet, trim-path build, gofmt and diff checks pass.
    - Public evidence (2026-10-26): exact-source [CI 40250564066](https://github.com/jerolan/good-sentry/actions/runs/40250564066) passed formatting, vet, PostgreSQL integration/race tests and application-image build for `47f87980192354365236c31ed1383c75cec7f335`. [Image workflow 40251114216](https://github.com/jerolan/good-sentry/actions/runs/40251114216) published linux/amd64 and linux/arm64 tags `main`, `latest` and `sha-47f87980192354365236c31ed1383c75cec7f335` at OCI index `sha256:e56c7a85c5650cf399a6625be708b0b54eeeeeecc79c8c1cea0f2b7a8d51fd94`.
  - [x] Add additive042 project-scoped debug-file storage and compatible multipart dSYM upload/list; preserve SHA-1 deduplication, metadata, exact bytes, owner/admin writes, member reads, project isolation, bounded pages and migration/reopen.
    - Chunked DIF negotiation/assembly now passes actual sentry-cli3.8.0 with missing-state, gzip chunk upload and durable `.sym` assembly. Legacy/Portable PDB and complete format coverage remain open. Evidence: `test-results/acceptance-native-minidump-20261020/cli-dif-success.log`.
  - [x] Reprocess stored native error frames after compatible project reprocessing requests.
    - Contract: preserve the empty `200` CLI response after durably enqueueing one coalesced project job. Snapshot the greatest project native-error row ID, restart the ordered cursor on repeated requests, and drain at most 10 matching native error events per locked transaction using only same-project debug files within the existing 25-file/64MiB cap. Fill only missing frame functions and available symbol source locations from existing `debug_meta.images` and `instruction_addr`; preserve unmatched/symbolized frames, all other event JSON, event identity, issue linkage and counts. Do not reconstruct frames without retained Minidumps. Commit event changes and cursor atomically, resume after process restart, and cascade jobs with project deletion.
    - Test-authoring gate: the observable behavior is an event captured before symbol upload gaining the matching function/source after an authenticated reprocessing request. Existing Minidump capture proves upload-before-capture only and does not exercise the route or stored-event mutation. Use the real PostgreSQL HTTP route and durable worker; add no test-only production seam.
    - Acceptance: verify empty `200` and durable enqueue, a late-uploaded matching PDB resolves a previously unsymbolicated frame, a mismatched project/object remains unchanged, repeated enqueue and worker restart resume without changing event ID/timestamp/context/issue linkage, and unrelated events are untouched. Verify migration/reopen, focused native and PostgreSQL HTTP races, full integration/race, vet, trim-path build, exact-source GitHub CI and amd64/arm64 publication through `gh`.
    - Sources: [Sentry CLI debug-file commands](https://cli.sentry.dev/commands/), [Sentry stored-event processing task](https://github.com/getsentry/sentry/blob/master/src/sentry/tasks/store.py), [Sentry event manager](https://github.com/getsentry/sentry/blob/master/src/sentry/event_manager.py).
    - Local evidence (2026-10-25): native race tests, full unit suite, integration-tag vet, trim-path build and compilation of server/store/jobs PostgreSQL integration tests pass. The HTTP/database and reopen tests are authored but cannot run in this environment because `DATABASE_URL` is unset and Docker access is denied. `gh auth status` reports the saved GitHub token invalid, so remote CI and image publication remain pending.
    - Public verification (2026-10-25): the later `gh` run [CI 40219341200](https://github.com/jerolan/good-sentry/actions/runs/40219341200) passes formatting, vet, the full PostgreSQL integration/race suite and application-image build for `27579adc85fa9c20f5aa0e805dc291457016f2c3`. Publication [40219897344](https://github.com/jerolan/good-sentry/actions/runs/40219897344) publishes `ghcr.io/jerolan/good-sentry:main`, `:latest` and `:sha-27579adc85fa9c20f5aa0e805dc291457016f2c3` for linux/amd64 and linux/arm64 at OCI index `sha256:5ed42fa5e0b86701d27ce0e1a10905236e1487290c1d00771b15403872415c4f`; this supersedes the earlier pending-publication note.
  - [x] First Minidump AMD64/x86/ARM64 slice: authenticated multipart ingestion, SystemInfo/Thread/Module/Exception/Memory parsing, context-based architecture selection, fatal event/debug-meta projection, raw-dump non-retention, bounded key quota, pointer-size-aware frame-pointer plus bounded DWARF CFI stack reconstruction, textual Breakpad and ELF/Mach-O DWARF/symbol-table symbolication with Debug-ID fallback. Uncommon CFI expressions, archive nesting beyond eight layers, symbol-server resolution and representative real crash receipts stay open.
  - [x] Accept official Sentry Native Crashpad Minidump requests that omit `sentry_version` and gzip-compress the multipart body.
    - Contract: omission is allowed only on `/api/{project}/minidump/`; a supplied version must be `7`, and all other SDK ingestion remains strict. Decode supported content encodings before multipart parsing and cap compressed and expanded bodies independently at `MAX_BODY_BYTES`.
    - Test-authoring gate: the former integration fixture supplied `sentry_version=7` and plain multipart, so it missed both Crashpad defaults. The updated protocol and PostgreSQL HTTP owners exercise the versionless `sentry_client`/`sentry_key` query, gzip multipart, unsupported versions and unchanged envelope authentication.
    - Local evidence (2026-10-25): `go test ./internal/protocol` and `go test -tags=integration ./internal/server -run TestNativeMinidumpSymbolicationAndIngestion -count=1` pass. The unmodified official `sentry-native` 0.17.4 `examples/crash.c` with Crashpad on linux/amd64 generated stored fatal event `1d2247673f066631b5c00359ed623c62` (`NATIVE-SDK-PROOF-1`); its Minidump frames include `libc.so.6` and `libsentry.so`. A redacted receipt is at `test-results/acceptance-native-crashpad-20261025/native-sdk-crash.json`. The test used a tmpfs PostgreSQL database and retained no dump in GoodSentry.
    - Public verification (2026-10-25): exact-source [CI 40243310451](https://github.com/jerolan/good-sentry/actions/runs/40243310451) passes formatting, vet, the PostgreSQL integration/race suite and image build for `c4829e334fcd50c9dd6da259a35811672d6eb553`. [Publication 40243811706](https://github.com/jerolan/good-sentry/actions/runs/40243811706) publishes `ghcr.io/jerolan/good-sentry:main`, `:latest` and `:sha-c4829e334fcd50c9dd6da259a35811672d6eb553` for linux/amd64 and linux/arm64 at OCI index `sha256:6e5cdd31bbaf6ebdc2b8e899d45549f3db0f9f5e82134f957838e30d985f0e7f`.
    - This closes one real linux/amd64 Crashpad path; it does not close the parent task's remaining architecture, CFI, archive, symbol-server or multi-architecture receipt gaps.
  - [x] Add a runnable official Sentry Native SDK demo that symbolicates its real Crashpad crash.
    - Contract: pin `sentry-native` 0.17.4 and Crashpad on Linux/amd64, compile a named non-inlined crash function with ELF/DWARF, upload the exact executable through the authenticated project `files/dsyms/` route, and trigger an intentional fault using that same binary and DSN. Require GoodSentry to return the named function, C source path and a positive line by the embedded ELF Debug ID; retain no minidump.
    - Acceptance: build the demo, run it against a disposable local GoodSentry/PostgreSQL instance, verify the real Crashpad event's symbolicated frame and absence of stored minidump bytes, then remove only the demo's temporary SDK data and disposable app/database state.
    - Test-authoring gate: protect the public SDK-to-HTTP-to-PostgreSQL contract that a real Crashpad fault from the uploaded ELF resolves `goodsentry_demo_crash` to `main.c` and a positive line, with no attachment or envelope bytes retained. A regression is a real SDK event that arrives but loses its own executable's symbols, or persists the raw dump. Existing fixture-minidump/ELF symbolication tests do not run sentry-native; the older unmodified SDK receipt proves only event and module ingestion, not this function/line. Use the existing PostgreSQL HTTP fixture, authenticated upload route and child process; add no production seam. A dedicated path-triggered GitHub Actions workflow builds the pinned SDK and opts into this test.
    - Source: [official sentry-native 0.17.4 README](https://github.com/getsentry/sentry-native/blob/0.17.4/README.md), [0.17.4 release](https://github.com/getsentry/sentry-native/releases).
    - Local status (2026-10-26): demo sources are committed at `bb93231`, with the integration test and path-triggered workflow at `78da02b` and the exact ELF Debug ID assertion at `339752f`. The pinned sentry-native 0.17.4 demo built with CMake in the pinned Go container. Its first real Crashpad receipt exposed two identity defects: uploaded ELF/Mach-O objects did not expose their embedded Debug ID, and Minidump modules ignored the Crashpad/Breakpad `BpEL` ELF build-id record. Both are corrected with GNU build-id, `LC_UUID` and `BpEL` parsing. The focused PostgreSQL/HTTP crash test now resolves `goodsentry_demo_crash` to `main.c` with a positive line and proves no attachment or envelope item retained the dump. Final local gates pass: full `go test ./...`, full `go test -race -tags=integration ./...` (server271.068s, store408.351s), integration-tag vet, trim-path build, gofmt and diff checks. Public CI and publication remain the closing gate.
    - Public verification (2026-10-26): source `a0cd17eda03e7e6cb9a8db8d2a679cb8a689bdb3` is on `main`. The exact-source [Native SDK demo workflow 40256935091](https://github.com/jerolan/good-sentry/actions/runs/40256935091) passes every build and crash step. [CI 40256935013](https://github.com/jerolan/good-sentry/actions/runs/40256935013) passes formatting, vet, the full PostgreSQL/race suite and application-image build. [Container image 40257491291](https://github.com/jerolan/good-sentry/actions/runs/40257491291) publishes linux/amd64 and linux/arm64 at OCI index `sha256:2d35a8afd9b0aded16d11506c049076bdc4331bb2f8d3cdc7b1369ea6cb208c9`.
  - [x] Reject AMD64/x86 Minidump contexts whose declared SystemInfo architecture conflicts with their context layout.
    - Contract: require architecture 9 for the 1,232-byte AMD64 context and architecture 0 for the 716-byte x86 context; reject either cross-pair before frame reconstruction.
    - Test-authoring gate: the mismatch cases fail before the parser change; the corrected x86 fixture declares architecture 0.
    - Local evidence (2026-10-25): native race tests, full `go test ./...`, integration-tag compilation and vet pass.
    - Public verification (2026-10-25): CI [40227196812](https://github.com/jerolan/good-sentry/actions/runs/40227196812) passes formatting, vet, the PostgreSQL/race suite and image build for descendant source `7dd351e444a8cf0d186246dfe63f634b1c4ac9e0`. Publication [40227609148](https://github.com/jerolan/good-sentry/actions/runs/40227609148) publishes amd64/arm64 `:main`, `:latest` and `:sha-7dd351e444a8cf0d186246dfe63f634b1c4ac9e0` at OCI index `sha256:8af9bb698deabce3e78762feebb3886340551c7d6ccf10b3d98aa53646d0da7b`.
  - [x] Accept bounded AMD64 XSAVE extensions written by Crashpad.
    - Contract: accept the 1,232-byte AMD64 base context plus up to 16,384 extension bytes only when context flags include XSTATE bit `0x40`; ignore extension data for register recovery and reject larger or unflagged extensions.
    - Test-authoring gate: verify a Crashpad-shaped extension and the exact maximum; reject a missing XSTATE flag and an extension one byte over the bound. Each case must be checked against the former parser.
    - Local evidence (2026-10-25): the former parser rejected flagged 1,376-byte and maximum-sized 17,616-byte contexts; both now pass while 16,385-byte extensions and missing flags fail. Native race tests, full `go test ./...`, integration-tag compilation/vet and trim-path build pass.
    - Public verification (2026-10-25): CI [40227196812](https://github.com/jerolan/good-sentry/actions/runs/40227196812) passes formatting, vet, the PostgreSQL/race suite and image build for exact source `7dd351e444a8cf0d186246dfe63f634b1c4ac9e0`. Publication [40227609148](https://github.com/jerolan/good-sentry/actions/runs/40227609148) publishes amd64/arm64 `:main`, `:latest` and `:sha-7dd351e444a8cf0d186246dfe63f634b1c4ac9e0` at OCI index `sha256:8af9bb698deabce3e78762feebb3886340551c7d6ccf10b3d98aa53646d0da7b`.
  - [x] Accept bounded x86 XSAVE extensions in 32-bit Minidumps.
    - Contract: accept the 716-byte x86 base context plus up to 16,384 extension bytes only when context flags include XSTATE bit `0x40` and SystemInfo declares x86 architecture 0; ignore extension bytes for register recovery and reject larger or unflagged extensions.
    - Test-authoring gate: verify flagged 860-byte and maximum-sized 17,100-byte contexts; reject a missing flag, one byte over the limit, and an x86 context declared as another architecture. Prove the old parser rejects valid extensions.
    - Local evidence (2026-10-25): the former parser rejected flagged 860-byte and maximum-sized 17,100-byte contexts; both now pass while a 17,101-byte extension, missing XSTATE flag, and mismatched SystemInfo architecture fail. Native race tests, full `go test ./...`, integration-tag compilation/vet and trim-path build pass.
    - Public verification (2026-10-25): descendant exact-source CI [40228893951](https://github.com/jerolan/good-sentry/actions/runs/40228893951) passes formatting, vet, the PostgreSQL/race suite and image build at `8f51d12dfc1a33ab2751baf66249617ebf308687`. Publication [40229543519](https://github.com/jerolan/good-sentry/actions/runs/40229543519) publishes `ghcr.io/jerolan/good-sentry:main`, `:latest` and `:sha-8f51d12dfc1a33ab2751baf66249617ebf308687` for linux/amd64 and linux/arm64 at OCI index `sha256:eb3cb7bb2c280cc76f0b54939f2fede24095cb6a716476dbcd668070b28cfda6`.
  - [x] Accept legacy Breakpad ARM64 Minidump contexts.
    - Contract: support SystemInfo architecture `0x8003` only with the exact 796-byte legacy context and integer-present flags `0x80000002` or `0x80000006`; read x29, SP and PC from offsets 240/256/264, preserve the existing ARM64 unwind and symbolication path, and reject a mismatched architecture, invalid flags or nonexact layout sizes.
    - Test-authoring gate: prove the old parser rejects a real-layout legacy fixture; verify both accepted flag forms and the decoded frame-pointer stack; reject architecture/flag mismatches and truncated or extended layouts.
    - Sources: [Breakpad's legacy ARM64 structure](https://raw.githubusercontent.com/google/breakpad/main/src/google_breakpad/common/minidump_cpu_arm64.h), [architecture value](https://raw.githubusercontent.com/google/breakpad/main/src/google_breakpad/common/minidump_format.h) and [context reader](https://raw.githubusercontent.com/google/breakpad/main/src/processor/minidump.cc).
    - Local evidence (2026-10-25): the former parser rejected the valid 796-byte context with both documented integer flags; the updated parser accepts both and recovers the expected two-frame stack. It rejects truncated/extended contexts, flags without integer state, unknown flags and the legacy/current architecture mismatch. Native race tests, the complete unit suite, integration-tag compilation/vet, trim-path build and `git diff --check` pass.
    - Public verification (2026-10-25): exact-source CI [40228893951](https://github.com/jerolan/good-sentry/actions/runs/40228893951) passes formatting, vet, the PostgreSQL/race suite and application-image build at `8f51d12dfc1a33ab2751baf66249617ebf308687`. Publication [40229543519](https://github.com/jerolan/good-sentry/actions/runs/40229543519) publishes `ghcr.io/jerolan/good-sentry:main`, `:latest` and `:sha-8f51d12dfc1a33ab2751baf66249617ebf308687` for linux/amd64 and linux/arm64 at OCI index `sha256:eb3cb7bb2c280cc76f0b54939f2fede24095cb6a716476dbcd668070b28cfda6`.
  - [x] Use the optional exception-stream thread context when symbolizing native Minidumps.
    - Contract: for public Exception streams of exactly 32 bytes, preserve the ThreadList fallback. Reject partial sizes 33..167. For streams of at least 168 bytes, read the context location descriptor at offset 160; both fields must be zero or both nonzero and in-file. Prefer a present context over ThreadList, reject a malformed or unsupported present context without fallback, and use matching-then-first ThreadList context when the descriptor is absent. Apply the declared-architecture context validation to the selected bytes.
    - Test-authoring gate: current fixtures only use 32-byte Exception streams and cannot catch the ignored descriptor. Exercise a conflicting embedded RIP, an embedded-only context, directory order independence, empty-descriptor fallback, partial stream boundaries, partial/out-of-file descriptors, unsupported context shape and architecture mismatch using public Minidump bytes; add no production test seam.
    - Acceptance: demonstrate the old parser ignores a valid context and accepts invalid descriptors/partial streams; pass focused native race tests, full unit tests, integration-tag compilation/vet, trim-path build, exact-source GitHub CI and amd64/arm64 image publication through `gh`.
    - Sources: [Microsoft MINIDUMP_EXCEPTION_STREAM](https://learn.microsoft.com/en-us/windows/win32/api/minidumpapiset/ns-minidumpapiset-minidump_exception_stream) and [MINIDUMP_LOCATION_DESCRIPTOR](https://learn.microsoft.com/en-us/windows/win32/api/minidumpapiset/ns-minidumpapiset-minidump_location_descriptor).
    - Local evidence (2026-10-25): pre-fix fixtures failed because the embedded RIP was ignored and malformed descriptors/partial streams were accepted. Reversing context priority makes the conflicting-RIP regression fail. Native race tests, full `go test ./...` with local listeners enabled, integration-tag compilation, integration vet, trim-path build and `git diff --check` pass.
    - Public verification (2026-10-25): exact-source CI [40230576100](https://github.com/jerolan/good-sentry/actions/runs/40230576100) passes formatting, vet, the full PostgreSQL/race suite and application image build at `3cb4036cec698b3641ac29574da8e5890383dc69`. Publication [40231068375](https://github.com/jerolan/good-sentry/actions/runs/40231068375) publishes `ghcr.io/jerolan/good-sentry:main`, `:latest` and `:sha-3cb4036cec698b3641ac29574da8e5890383dc69` for linux/amd64 and linux/arm64 at OCI index `sha256:ba9fd6ed50c79392e2479667c6585b652396b12f4f7aec36e3f7dc1176ce2a38`.
  - [x] Resolve instruction-accurate ELF and Mach-O DWARF source lines.
    - Contract: compile-unit line rows map half-open spans through the next row; `EndSequence` closes but does not map a sequence, and unmapped gaps stay unmapped. Intersect those spans with subprogram ranges while retaining a source-free full function range for name fallback. Cap line rows at 262,144 per compilation unit and emitted source spans at 262,144 per object; malformed/over-limit compilation units keep function names but emit no line mappings, and exhausting the object span budget stops further line mappings. Preserve virtual addresses until image-base normalization.
    - Test-authoring gate: protect the observable frame filename/line at an interior PC across a source-line transition. The existing ELF/Mach-O checks only resolve procedure entry PCs, so they miss the current entry-line-for-whole-function bug. Extend the native fixture owner for exact line 2/3/4 and 6/7/8 cases and the real PostgreSQL HTTP Minidump owner for persisted interior-frame line 3; add no production seam.
    - Acceptance: demonstrate the interior-line regression fails on the old parser, verify exact source transitions and half-open row boundaries against committed ELF/DWARF fixtures, and read the line 3 frame back from the stored Minidump event. Keep Mach-O, PDB, SymCache and reprocessing behavior intact. Run focused native/server integration races, full integration/race, vet, formatting, trim-path build, exact-source CI and amd64/arm64 image publication through `gh`.
    - Source: [Go 1.27 `debug/dwarf` line-reader contract](https://go.dev/src/debug/dwarf/line.go).
    - Local evidence (2026-10-25): the regression failed before the parser change at PCs `0x401111` and `0x401117`, both incorrectly inheriting line 2. Afterward `go test -race ./internal/native`, full `go test ./...`, integration-test compilation, `go vet -tags=integration ./...`, and `go build -trimpath` pass. The PostgreSQL HTTP test compiles locally and is executed in CI.
    - Public verification (2026-10-25): CI [40221791114](https://github.com/jerolan/good-sentry/actions/runs/40221791114) passes formatting, vet, full PostgreSQL integration/race tests and image build at `4e389684f85026087e13e7957a7e3b263ed3170c`. Publication [40222324391](https://github.com/jerolan/good-sentry/actions/runs/40222324391) publishes `ghcr.io/jerolan/good-sentry:main`, `:latest` and `:sha-4e389684f85026087e13e7957a7e3b263ed3170c` for linux/amd64 and linux/arm64 at OCI index `sha256:f032c9e75a0b2fc3b5a684d661e3189ac690df2d06c57dfe42c3672d0f10e928`.
  - [x] Parse optional Breakpad `m` markers without leaking malformed function line rows.
    - Contract: accept optional `m` after `FUNC` and `PUBLIC`, skip it when locating the existing address/size/parameter/name fields and preserve names containing spaces. Source line records attach only to an accepted `FUNC`; a `PUBLIC` or malformed `FUNC` clears the prior function association.
    - Test-authoring gate: the parser's existing unmarked fixture does not exercise `m` or the stale-current bug. Extend the `ParseBreakpad` owner test to resolve marked functions/public symbols, preserve a spaced name and confirm a source row after an invalid marked function is ignored; this directly fails on the old parser and needs no integration duplicate or production seam.
    - Acceptance: verify marked and unmarked `FUNC`/`PUBLIC`, names with spaces, line association, malformed-header reset, native race tests, full suite, formatting, vet, trim-path build, exact-source CI and amd64/arm64 publication through `gh`.
    - Source: [Breakpad symbol-file format](https://chromium.googlesource.com/breakpad/breakpad/+/HEAD/docs/symbol_files.md).
    - Local evidence (2026-10-25): the new parser-owner regression fails before the change because marked records are skipped; after the fix, `go test -race ./internal/native`, full `go test ./...`, integration-test compilation, `go vet -tags=integration ./...`, `go build -trimpath` and `git diff --check` pass.
    - Public verification (2026-10-25): exact-source CI [40223034532](https://github.com/jerolan/good-sentry/actions/runs/40223034532) passes at `d5abc493686d9af33d3e61e2269ff1b4ffe47e3e`; image publication [40223571467](https://github.com/jerolan/good-sentry/actions/runs/40223571467) publishes `ghcr.io/jerolan/good-sentry:main`, `:latest` and `:sha-d5abc493686d9af33d3e61e2269ff1b4ffe47e3e` for linux/amd64 and linux/arm64 at OCI index `sha256:b209e3ef25ccdc39c20fc9dc015ac60dfec6d75d455c915f22f09b809a037a3b`.
  - [x] Infer Breakpad `PUBLIC` fallback ranges through the next symbol address.
    - Contract: make each `PUBLIC` resolve from its address until the next distinct `FUNC` or `PUBLIC` start; let the captured Minidump module bound the final public range. Prefer any covering `FUNC`, preserve source-free public frames and names with spaces, and retain optional `m` parsing.
    - Test-authoring gate: current Breakpad coverage checks a `PUBLIC` only at its first byte and no fixture checks FUNC/public overlap or the module-end bound. Extend the native Breakpad parser/symbolicator owner to verify interior addresses, exact half-open next-symbol boundaries, FUNC precedence and the final module edge. This covers the visible frame outcome without duplicating the HTTP/PostgreSQL consumer or adding a production seam.
    - Acceptance: demonstrate an interior public frame resolves before the fix; verify two ordered public starts, FUNC overlap with and without source rows, end-boundary handoff, duplicate-address handling, final module bounds, `m` compatibility, native race tests, full suite, formatting, vet, trim-path build, exact-source CI and amd64/arm64 publication through `gh`.
    - Source: [Breakpad symbol-file format, `PUBLIC` records](https://chromium.googlesource.com/breakpad/breakpad/+/HEAD/docs/symbol_files.md#public-records).
    - Local evidence (2026-10-25): the new native symbolication regression failed before the change at interior public offsets, FUNC/public precedence and the final module edge; after the fix, focused Breakpad tests, `go test -race ./internal/native`, full `go test ./...`, integration-test compilation, `go vet -tags=integration ./...`, `go build -trimpath` and `git diff --check` pass.
    - Public verification (2026-10-25): exact-source CI [40223851521](https://github.com/jerolan/good-sentry/actions/runs/40223851521) passes formatting, vet, full PostgreSQL integration/race tests and image build at `89ec2135d5601104d69b54a215fb7ad243a220ec`; image publication [40224396825](https://github.com/jerolan/good-sentry/actions/runs/40224396825) publishes `ghcr.io/jerolan/good-sentry:main`, `:latest` and `:sha-89ec2135d5601104d69b54a215fb7ad243a220ec` for linux/amd64 and linux/arm64 at OCI index `sha256:2b5ad3e241fa3a0940c0c4955e279dc4cc724a82152b6cd4202034ec1a8adffb`.
- [x] Ingest Minidumps attached to official Sentry Native SDK event envelopes through the normal Sentry capture route.
    - Contract: accept one `event` item whose event `platform` is `native` and at most one `attachment` with `attachment_type:event.minidump`; run the same project-scoped parser, CFI unwinder and symbolicator as multipart minidumps. Preserve the SDK event identity, timestamp and all supplied event context, overlay the native stack on its first exception, fill missing native exception/debug metadata and deduplicate image metadata with Minidump modules authoritative. Never retain the typed Minidump bytes. Reject typed dumps with no native event, duplicate typed dumps or malformed bytes before any event/child write. Unmarked `.dmp` attachments and ordinary attachments keep current behavior.
    - Acceptance evidence: `TestNativeMinidumpEnvelopeAttachment` exercises authenticated envelope capture against PostgreSQL with an uploaded PDB fixture. The stored issue retains SDK identity, timestamp, message, tags and context, gains the PDB-symbolicated function/source line and module Debug ID, and does not retain the typed dump; rejection and ordinary-attachment cases remain covered in the same owner.
    - Acceptance: a matching uploaded PDB and a Minidump attachment produce the expected stack while preserving the SDK payload and event ID; duplicate/missing/non-native events and malformed Minidumps leave no event or dependent item. Unmarked attachments still persist. Verify with `go test -race -tags=integration ./internal/server -run 'NativeMinidump|CaptureEnvelope' -count=1`, full native and server integration/race suites, `go vet -tags=integration ./...`, trim-path build, exact-source CI and immutable amd64/arm64 image via `gh`.
    - Sources: [Sentry Native SDK crashpad envelope construction](https://github.com/getsentry/sentry-native/blob/master/src/backends/sentry_backend_crashpad.cpp), [Sentry Native SDK attachment API](https://github.com/getsentry/sentry-native/blob/master/include/sentry.h), [Sentry SDK envelope contract](https://develop.sentry.dev/sdk/envelopes/), [public Minidump attachment type](https://docs.rs/sentry-core/latest/sentry_core/protocol/enum.AttachmentType.html).
    - Public evidence (2026-10-24): the PDB-backed PostgreSQL/HTTP test passes locally with race detection. Exact-source CI [40145872740](https://github.com/jerolan/good-sentry/actions/runs/40145872740) passes formatting, vet, the full PostgreSQL integration/race suite and image build; image publication [40146315540](https://github.com/jerolan/good-sentry/actions/runs/40146315540) publishes `main` for linux/amd64 and linux/arm64 at OCI index `sha256:95579e831b365e4f109f1163c9c8497c69228c756fc9c3b999135bcb936d8f2d`.
  - [x] Parse the public ARM32 Minidump context layout and reconstruct a basic caller from r11 or LR.
    - Contract: require SystemInfo architecture5 and the exact 368-byte Crashpad or Breakpad context layout; decode FP/SP/LR/PC at offsets48/56/60/64. Follow a bounded r11 frame-pointer chain, or use LR for the first caller when the chain is absent; do not read a guessed return address from SP.
    - Verify: a synthetic 368-byte context fixture, malformed sizes and architecture mismatch; `go test -race ./internal/native`, integration-tag vet and trim-path build. Keep ARM32 CFI register restoration outside this slice.
    - Sources: [Crashpad ARM context](https://chromium.googlesource.com/crashpad/crashpad/+/refs/heads/main/minidump/minidump_context.h), [Breakpad ARM context](https://chromium.googlesource.com/external/omaha/+/e2c3f15816f1a394e56433de4fb58db30548fdb0/third_party/breakpad/src/google_breakpad/common/minidump_cpu_arm.h), [Minidump architecture values](https://chromium.googlesource.com/chromiumos/platform/google-breakpad/+/f1f5090c82314f281c6934d10494f1d0a66bc8a7/src/google_breakpad/common/minidump_format.h).
    - Local evidence (2026-10-22): the 368-byte Crashpad/Breakpad context fixture plus 364/360-byte rejection cases, r11 chain, LR-over-SP fallback, zero-LR no-guess, size rejection and architecture mismatch pass `go test -race ./internal/native`; `go vet -tags=integration ./...`, `go build -trimpath ./cmd/goodsentry` and `git diff --check` pass. DWARF CFI register restoration is implemented in a separate child below; exact-source acceptance and real ARM32 crash receipts remain part of the parent.
  - [x] Unwind ARM32 frames with DWARF CFI register restoration.
    - Contract: seed DWARF r0-r15 from the validated ARM32 context; use DWARF register numbers 0-15; apply CIE defaults of same-value for r4-r8, r10-r11 and r13 and undefined for other core registers, leaving r9 platform-specific unless explicitly described; carry each recovered register state into the next frame. This DWARF child does not cover VFP restoration; EHABI is specified in the separate child below.
    - Acceptance: `UnwindCFI` recovers a multi-frame chain where the second CFA depends on an r4 value restored from the first frame; parsing exposes all core context registers; missing or unavailable state stops unwinding without guessing.
    - Verify: `go test -race ./internal/native`, `go vet -tags=integration ./...`, `go build -trimpath ./cmd/goodsentry` and `git diff --check`; exact-source CI and real ARM32 receipts remain part of parent closure.
    - Sources: [Arm DWARF32 register numbers and CIE defaults](https://github.com/ARM-software/abi-aa/blob/main/aadwarf32/aadwarf32.rst), [AAPCS32 register preservation](https://github.com/ARM-software/abi-aa/blob/main/aapcs32/aapcs32.rst), [Crashpad ARM Minidump context](https://chromium.googlesource.com/crashpad/crashpad/+/doc/minidump/minidump_context.h).
    - Local evidence (2026-10-22): the pre-change regression returned no ARM CFI frames. A valid synthetic ELF32 ARM `.debug_frame` now unwinds three frames; its second CFA depends on r4 restored by the first CFI row, and unavailable saved r4 stops after the known top frame. Crashpad context parsing maps every r0-r15 value. Native race tests, integration-tag vet, trim-path build and `git diff --check` pass.
    - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `internal/native/minidump.go`, `internal/native/cfi.go`, `internal/native/minidump_test.go`.
  - [x] Unwind ARM32 ELF frames with bounded EHABI tables.
    - Contract: parse little-endian ELF32 `SHT_ARM_EXIDX` fragments linked to executable sections; resolve sorted function starts and compact EHT references with checked `prel31` arithmetic; honor `EXIDX_CANTUNWIND`; support inline personality 0 and `.ARM.extab` compact personalities 0, 1 and 2. Cap one file at262,144 index entries and16MiB of decoded operations, and one frame at256 operations. Execute bounded VSP adjustment, VSP-from-register, core-register pop and finish operations against captured memory, carrying restored r0-r15 into the caller. Ignore cleanup/catch descriptors. Reject malformed/unsorted/out-of-range tables, generic or reserved personalities, reserved instructions, VFP/WMMX and pointer-auth operations without inventing frames.
    - Acceptance: a synthetic ELF32 ARM minidump unwinds through inline and extab tables; stack restores PC/LR and caller SP correctly; CANTUNWIND and unavailable memory stop at the last known frame; malformed prel31 links, section bounds, opcodes and overlong ULEB values fail closed.
    - Verify: `go test -race ./internal/native`, `go vet -tags=integration ./...`, `go build -trimpath -o /tmp/goodsentry-build ./cmd/goodsentry` and `git diff --check`. Exact-source CI, clean-image restart and real ARM32 receipts remain part of parent closure.
    - Sources: [EHABI32 index and compact table formats](https://github.com/ARM-software/abi-aa/blob/main/ehabi32/ehabi32.rst), [EHABI32 frame operations](https://github.com/ARM-software/abi-aa/blob/main/ehabi32/ehabi32.rst#103-frame-unwinding-instructions), [AAELF32 ARM exception-index section](https://github.com/ARM-software/abi-aa/blob/main/aaelf32/aaelf32.rst).
    - Local evidence (2026-10-22): before implementation, both inline and `.ARM.extab` ELF fixtures produced no unwind table. They now recover caller LR/PC and SP; tests cover r4-r15 mask pops and low-register pops, positive/negative/large VSP changes, VSP-from-r4, CANTUNWIND, unavailable memory, truncated sections, invalid and unsorted `prel31` targets, generic/reserved personalities, unsupported instructions and overlong ULEB. `go test -race ./internal/native`, integration-tag vet, trim-path build and `git diff --check` pass. Implementation commit `1e83cb7` pushed to `main`; GitHub Actions and public-image results could not be queried after the push, and real ARM32 crash receipts remain open.
    - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `internal/native/cfi.go`, `internal/native/minidump_test.go`.
  - [x] Parse and unwind RISC-V64 minidumps using the Crashpad/Breakpad context v1 contract.
    - Contract: require SystemInfo architecture `0x8006`, the exact 528-byte little-endian context, version 1, and integer-valid flags `0x08000001` or `0x08000003`. Map context PC, x1-x31, x0=0, SP=x2, FP=x8, LR=x1 and pointer size 8. Follow frame records at fp-16/fp-8 with checked reads, monotonic caller FP, unique nonzero return addresses and a 64-frame bound; use x1 only as the first-caller fallback and never guess from SP. Parse only little-endian ELF64 `EM_RISCV` `.eh_frame`/`.debug_frame`, map DWARF x0-x31 and alternate return column64, keep active PC separate, honor the CIE return column and carry restored integer registers. Do not infer floating/vector/CSR state or platform signal contexts.
    - Acceptance: synthetic valid contexts prove register offsets, frame-chain order and x1 fallback; zero x1 with a plausible SP word yields no guessed caller. Wrong architecture, 524/536-byte lengths, invalid version, missing integer validity and unknown flags fail closed. Malformed/unavailable/nonmonotonic frame records, repeated return addresses and the 64-frame cap do not invent frames. A synthetic ELF64 RISC-V CFI fixture proves multi-frame register restoration, return column64 and architecture matching; a CFA that requires unavailable floating-point state stops at the last known frame. A PostgreSQL-backed HTTP capture is acknowledged and stored with both RISC-V frames.
    - Verify: `go test -race ./internal/native`, RISC-V-specific Minidump HTTP/PostgreSQL integration coverage, `go vet -tags=integration ./...`, `go build -trimpath ./cmd/goodsentry` and `git diff --check`. Exact-source CI, clean-image restart and real RISC-V crash receipts remain part of parent closure.
    - Local evidence (2026-10-23): the pre-implementation focused RISC-V tests failed on unsupported context size528 and the old unwind read only36 of70 synthetic callers. The parser, frame records, x1 fallback, zero-x1/no-SP control and ELF64 CFI pass focused tests; full `go test -race ./internal/native`, `go vet -tags=integration ./...`, trim-path build, integration-test compilation and `git diff --check` pass. Exact-source CI [40056049760](https://github.com/jerolan/good-sentry/actions/runs/40056049760) passed formatting, vet, the complete PostgreSQL integration/race suite and image build at `258c24c`; image workflow [40056482040](https://github.com/jerolan/good-sentry/actions/runs/40056482040) published both architectures successfully. The preceding CI stopped at gofmt; `258c24c` contains only that formatting correction. Clean-image restart and real RISC-V crash receipts remain part of parent closure.
    - Sources: [Crashpad RISC-V context](https://chromium.googlesource.com/crashpad/crashpad/+/refs/heads/main/minidump/minidump_context.h), [minidump architecture value](https://chromium.googlesource.com/crashpad/crashpad/+/refs/heads/main/minidump/minidump_extensions.h), [Crashpad context writer](https://chromium.googlesource.com/chromium/src/+/refs/heads/main/third_party/crashpad/crashpad/minidump/minidump_context_writer.cc), [Breakpad context reader](https://chromium.googlesource.com/breakpad/breakpad/+/HEAD/src/processor/minidump.cc), [RISC-V frame convention](https://github.com/riscv-non-isa/riscv-elf-psabi-doc/blob/master/riscv-cc.adoc) and [DWARF register mapping](https://github.com/riscv-non-isa/riscv-elf-psabi-doc/blob/master/riscv-dwarf.adoc).
    - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `internal/native/minidump.go`, `internal/native/cfi.go`, `internal/native/minidump_test.go`, `internal/server/minidump_test.go`.
  - [x] Add bounded TAR and gzip-compressed TAR debug archives without extracting files; process only regular entries and leave nested archives opaque.
    - Acceptance: Breakpad and ELF/Mach-O members are discovered and symbolicate a minidump from both archive forms; member-truncated, missing-end-block, non-zero-trailing-data, checksum-invalid, non-regular-only and over-budget archives return no partial symbols.
    - Verify: `go test -race ./internal/native`, the focused real PostgreSQL/HTTP archive symbolication test, full integration/race CI on the exact published source, `go vet -tags=integration ./...`, `go build -trimpath ./cmd/goodsentry`, and a clean-image Compose startup smoke for readiness, dashboard, open setup and cookie-free `/api/v1/me`.
    - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `internal/native/debug_objects.go`, `internal/native/minidump_test.go`, `internal/server/minidump_test.go`.
    - Acceptance: commit `b2878dac5fef1d51c689a3be5c2260ec66460154`, tree `43374cb51206ba0e50616e21d145c8704911737d`; local parser race, PostgreSQL/HTTP archive race, vet, build and final-image Compose smoke pass. Matching CI `39806196554` passes formatting, vet, all-package PostgreSQL integration/race tests (native1.161s, server172.790s, store229.285s) and image build. Image workflow `39807094597` publishes linux/amd64 and linux/arm64 to `ghcr.io/jerolan/good-sentry:main` and `:latest`, manifest `sha256:203b1b1e1207d3193429edd480c0773df3f14b69491efbc9018101186f295221`. Evidence: `test-results/acceptance-native-tar-20261022` and the matching GitHub Actions runs.
  - [x] Discover one nested archive layer while preserving the existing 256-entry and 128MiB expanded-byte bounds across the full tree. Permit ZIP, TAR and gzip-TAR at either layer; reject depth greater than two and malformed nested archives without partial symbols.
    - Acceptance: valid nested Breakpad fixtures resolve the captured function and source line; deeper archives and trees that exceed the shared entry or expanded-byte budget return no symbols. Verify `go test -race ./internal/native`, full exact-source CI, and published amd64/arm64 image checks through `gh`. Preserve the original uploaded bytes and never extract member paths.
    - Local evidence (2026-10-22): native race tests cover ZIP→TAR, TAR→ZIP and ZIP→gzip-TAR source-line symbolication, malformed/depth-three no-partial behavior, shared entry count, and a <64MiB archive whose two expanded levels exceed the shared128MiB budget. `go vet -tags=integration ./...`, trim-path app build, formatting and `git diff --check` pass.
    - Public evidence: commit `2b49d2ac8ba3527ad52f51e2af87aa5d5bfd09c4` is on `main`. CI [39896784376](https://github.com/jerolan/good-sentry/actions/runs/39896784376) passes formatting, vet, the full unit/PostgreSQL integration race suite and image build. Image workflow [39897260813](https://github.com/jerolan/good-sentry/actions/runs/39897260813) publishes `main`, `latest` and the full-SHA tag at OCI index `sha256:7ce0d9898a1feb40f682a73e976ff7667abb99fc4e4146333aa5a5e5e4c7e913`; amd64 and arm64 manifests are `sha256:66de1bae9e0f7b90dc1b29a343a5559b608a682070ea9f4e5807433e3c93d2a9` and `sha256:0f92042288034acd44477d554a126f7fe97b696f09ab61717a62902bda716ea0`.
  - [x] Expand bounded native archive discovery from two to four layers.
    - Contract: ZIP, TAR and gzip-compressed TAR may nest in any order to depth four. Keep the shared 256 logical-entry and 128MiB expanded-byte budgets, charging each archive member and its descendants. A fifth archive layer, malformed nested archive or shared-budget overflow rejects the whole tree without partial symbols. Never extract member paths.
    - Acceptance: a mixed four-layer archive resolves a captured function and source line; a fifth-layer tree with a valid sibling yields no objects; existing malformed, entry-budget and expanded-byte-budget cases still fail atomically.
    - Verify: `go test -race ./internal/native`, `go vet -tags=integration ./...`, `go build -trimpath ./cmd/goodsentry`, `git diff --check`, exact-source CI and published amd64/arm64 image checks through `gh`.
    - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `internal/native/debug_objects.go`, `internal/native/minidump_test.go`.
  - [x] Expand bounded native archive discovery from four to eight layers.
    - Contract: ZIP, TAR and gzip-compressed TAR may nest in any order to depth eight. Preserve the shared 256 logical-entry and 128 MiB expanded-byte budgets, charging nested containers and descendants. A ninth layer, malformed child, or budget overflow rejects the entire tree without partial symbols; never extract member paths.
    - Test-authoring gate: the observable behavior is a captured frame resolving its function and source line from an eight-layer mixed archive. Before implementation, the existing owner rejects valid depth-five-and-beyond inputs because `maxDebugArchiveDepth` is4. Extend `TestNestedDebugArchivesAndSymbolicate` to cover eight-layer success and ninth-layer rejection beside a valid sibling; it owns parsing, symbolication, and no-partial behavior, so add no production seam.
    - Acceptance: mixed eight-layer symbolication succeeds; a ninth-layer tree with a valid sibling returns no objects; malformed, shared-entry-budget and expanded-byte-budget controls remain unchanged.
    - Verify: `go test -race ./internal/native`, `go vet -tags=integration ./...`, `go build -trimpath ./cmd/goodsentry`, full `go test ./...`, `git diff --check`, exact-source CI and published amd64/arm64 image checks through `gh`.
    - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `internal/native/debug_objects.go`, `internal/native/minidump_test.go`.
    - Local evidence (2026-10-25): the old parser failed the new eight-layer case (`test-results/goal-native-archive-depth-red.log`). Eight-layer source-line symbolication and ninth-layer atomic rejection pass; existing malformed and shared-entry/expanded-byte controls pass. `go test -race ./internal/native -count=1`, `go test ./... -count=1`, `go vet -tags=integration ./...`, trim-path app build and `git diff --check` pass.
    - Public evidence (2026-10-26): commit `3d074beb43c8cf1b229e3db29c7e7a15c37d5d76` is on `main`. Exact-source [CI 40247403639](https://github.com/jerolan/good-sentry/actions/runs/40247403639) passes formatting, vet, PostgreSQL integration/race tests and image build. [Image workflow 40247847806](https://github.com/jerolan/good-sentry/actions/runs/40247847806) publishes `ghcr.io/jerolan/good-sentry:main`, `:latest` and `:sha-3d074beb43c8cf1b229e3db29c7e7a15c37d5d76` for linux/amd64 and linux/arm64 at OCI index `sha256:649a1581dfdadbec774db59c1ccbd801680c54dd074ab4977cb2b2f89838a3a3`.
    - Documentation reconciliation (2026-10-26): `SPEC.md` and `docs/compatibility.md` now describe the accepted eight-layer limit rather than the superseded four-layer limit.
    - Local evidence (2026-10-24): the four-layer mixed ZIP/TAR/gzip-TAR case failed against the old depth-2 parser and passes after the limit increased; `Symbolicate` resolves `CrashFunction` at `C:\src\crash.cc:42`. A fifth-layer tree with a valid sibling returns no partial objects. The full `go test -race ./internal/native`, integration-tag vet, trim-path build, formatting and `git diff --check` pass.
    - Public evidence: commit `da7136a452f7f2c97c9efa32233537f44783c0f1` is on `main`. CI [40113773309](https://github.com/jerolan/good-sentry/actions/runs/40113773309) and image workflow [40114251601](https://github.com/jerolan/good-sentry/actions/runs/40114251601) pass. GHCR tag `sha-da7136a452f7f2c97c9efa32233537f44783c0f1` resolves to OCI index `sha256:2f6ee10885e0032cb517e87436fbc1251d0d263dba94f0d64b57856b44c81ba2`, with linux/amd64 manifest `sha256:6aca1bd117999deda8fecd005b1f536791860166eec7695fdf519f4c8ccc9859` and linux/arm64 manifest `sha256:538b6714950df3267cb1f0934385ff60f0f67f45310b28260e522a4098226601`.
  - [x] Parse bounded native Microsoft PDB/MSF 7 module procedure symbols and match them by embedded GUID+age.
    - Contract: existing 64MiB file budget; validated 512/1024/2048/4096-byte MSF blocks, at most4096 streams,4096 modules,512 PE sections,200000 procedure records and1000000 CodeView records; module C13 procedure records plus DBI section headers map exact code ranges to image-relative addresses. Parse atomically and reject an uploaded Debug ID that disagrees with the embedded PDB ID. C13 source lines and stripped-public records are implemented in the children below; legacy/Portable PDB and demangling remain open.
    - Acceptance: standalone and ZIP/TAR PDB objects parse procedure ranges; the real HTTP/PostgreSQL AMD64 Minidump flow symbolizes only the matching RSDS identity; mismatched IDs never symbolize; malformed/truncated/over-limit MSF and procedure records produce no partial symbols. Verify parser race, full integration CI and a public image built from the accepted source.
    - Sources: [MSF](https://llvm.org/docs/PDB/MsfFile.html), [PDB Info](https://llvm.org/docs/PDB/PdbStream.html), [DBI/module streams](https://llvm.org/docs/PDB/DbiStream.html), [CodeView procedure records](https://llvm.org/docs/PDB/CodeViewSymbols.html), [Microsoft CodeView signatures](https://github.com/microsoft/microsoft-pdb/blob/master/include/cvinfo.h).
    - Verify: `go test -race ./internal/native`, PDB-specific and full server PostgreSQL integration/race tests, exact-source GitHub CI, `go vet -tags=integration ./...`, `go build -trimpath ./cmd/goodsentry`, and a clean-image startup smoke for readiness and open setup.
    - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `internal/native/debug_objects.go`, `internal/native/pdb.go`, `internal/native/minidump.go`, `internal/native/pdb_test.go`, `internal/server/minidump.go`, `internal/server/minidump_test.go`, `internal/testfixture/nativepdb/nativepdb.go`.
    - Local evidence (2026-10-22): native parser race, full server PostgreSQL integration/race, PDB-specific HTTP/PostgreSQL race, integration-tag vet, trim-path build and linux/amd64 image startup pass. The image reports ready, `/api/v1/setup` returns `required:false`, cookie-free `/api/v1/me` returns the implicit owner, and restarting preserves owner/org id `1`. Local image manifest: `sha256:390b019b2aa7122f1b96cbb1ca5555754c25b5755230c618f2fceb9f60d11685`.
    - Public evidence: source commit `0916283b4b020d099244faadeff98bab9d753158` matches GitHub main. CI `39824421607` passes formatting, vet, all-package PostgreSQL integration/race tests and image build; image workflow `39825175663` publishes amd64/arm64 at `sha256:249e19eba98051a45f75cbf8981c78924cee8b2fa63190e458b97cf5e14db062`. The public amd64 executable matches the local image at 23,650,464 bytes, SHA-256 `a7a25ca21f0b6d7e4a68d0cb8367927aa0933e85fa861ab6f31bdfb29f899ace`.
  - [x] Resolve C13 source lines to native minidump frames from classic PDBs.
    - Contract: after existing module symbols, validate `C13ByteSize` and parse bounded four-byte-aligned C13 subsection framing; consume lines/checksums/string tables, skip other framed kinds, and resolve filenames through module strings or stream 1's `/names` named stream. Validate line-block checksum references, section/contribution geometry and monotonic line offsets; emit filename/line ranges clipped to procedures from that module. Keep C11 and inline info unsupported. Caps: 65,536 subsections and 65,536 checksum file records per PDB, 1,000,000 line records and line-to-procedure ranges per PDB, 4,096 filename bytes; malformed input returns no partial symbols.
    - Acceptance: synthetic MSF7 fixture covers a multi-line source range, local and global string-table resolution, columns, multiple modules and line-to-function clipping; malformed subsection lengths, missing `/names`/checksum references, invalid segment/range, decreasing offsets and record budgets produce no PDB symbols. Existing standalone/ZIP/TAR matching and mismatching identity tests keep passing. A real HTTP/PostgreSQL AMD64 Minidump query returns the PDB filename and line for the matching RSDS identity and does not do so for a mismatched ID.
    - Sources: [PDB Info and named-stream map](https://llvm.org/docs/PDB/PdbStream.html), [DBI module stream sizes](https://llvm.org/docs/PDB/DbiStream.html), [CodeView line blocks](https://www.llvm.org/doxygen/DebugLinesSubsection_8h_source.html), [checksum entries](https://www.llvm.org/doxygen/DebugChecksumsSubsection_8cpp_source.html), [global PDB string table](https://www.llvm.org/doxygen/RawTypes_8h_source.html), [serialized hash table](https://llvm.org/docs/PDB/HashTable.html).
    - Verify: `go test -race ./internal/native`, PDB-specific PostgreSQL/HTTP test under race, full `go test -race -tags=integration ./internal/server -count=1`, `go vet -tags=integration ./...`, `go build -trimpath ./cmd/goodsentry`, local clean-image readiness/open-auth/restart smoke, then exact-source GitHub CI and both published image architectures via `gh`.
    - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `internal/native/pdb.go`, `internal/native/minidump.go`, `internal/native/pdb_test.go`, `internal/server/minidump_test.go`, `internal/testfixture/nativepdb/nativepdb.go`.
    - Local evidence (2026-10-22): native parser race, PDB-specific HTTP/PostgreSQL race, integration-tag vet and trim-path build pass. The exact public image digest starts ready, reports `/api/v1/setup` as `required:false`, serves the implicit owner without a cookie and preserves owner/organization id `1` after an app-only restart.
    - Public evidence: source commit `b83966049fe75012ce83922cc88827f69a716c64` matches `main`. CI `39828700505` passes formatting, vet, all unit/PostgreSQL integration race tests and image build. Container workflow `39829491246` publishes `linux/amd64` and `linux/arm64`; `main` and `latest` share OCI index digest `sha256:a5f798f90c61a1ed2e8d777a7d6cff192b22f6c1bf8d2f15f87b9fb82a18fcc2`.

  - [x] Expand classic PDB inline call sites into native Minidump stack frames.
    - Contract: parse MSF 7 module S_INLINESITE/S_INLINESITE_END scopes, C13 DEBUG_S_INLINEELINES entries and IPI LF_FUNC_ID/LF_MFUNC_ID names. Decode bounded CodeView binary annotations to select source line and file for the captured relative instruction. Emit nested virtual frames innermost-first immediately before the existing physical frame, repeat its instruction address and preserve current Minidump frame order. Do not demangle. Keep SymCache inline parents collapsed. Cap sites at100,000 per PDB, depth128, IPI records and annotations at1,000,000 each, and inline expansion at128 frames per physical frame. Missing references, malformed nesting/annotations/ranges and budget overflow reject the whole PDB without partial symbols.
    - Test-authoring gate: observable result is a Sentry event whose stacktrace contains the expected inlined function, exact source path/line and original physical frame; a generated valid fixture fails before implementation because current PDB parsing skips inline records; current PDB procedure/C13 tests do not assert inline frames; the canonical parser owner is internal/native/pdb_test.go, with BuildEvent serialization verified there; no test-only production seam is needed.
    - Acceptance: cover one and nested inline sites, mixed inline/physical ordering, source file changes and line deltas, matching/mismatching Debug IDs, malformed/missing IPI and C13 data, truncated/unknown annotations, malformed nesting, and site/depth/record/annotation/frame budgets. Keep existing procedure, C13, SymCache and minidump HTTP behavior.
    - Sources: [CodeView symbols](https://llvm.org/docs/PDB/CodeViewSymbols.html), [IPI/TPI streams](https://llvm.org/docs/PDB/TpiStream.html), [type records](https://llvm.org/docs/PDB/CodeViewTypes.html), [C13 inlinee lines](https://www.llvm.org/doxygen/DebugInlineeLinesSubsection_8h_source.html), [binary annotations](https://www.llvm.org/doxygen/SymbolRecord_8h_source.html), [LLVM inline line lookup](https://www.llvm.org/doxygen/NativeInlineSiteSymbol_8cpp_source.html).
    - Verify: go test -race ./internal/native, focused PostgreSQL/HTTP minidump race, go test -race -tags=integration ./internal/server -count=1, go vet -tags=integration ./..., go build -trimpath ./cmd/goodsentry, clean-image readiness/open-auth/restart smoke, exact-source GitHub CI and published amd64/arm64 image checks through gh.
    - Files: SPEC.md, tasks/plan.md, tasks/todo.md, internal/native/pdb.go, internal/native/minidump.go, internal/native/event.go, internal/native/pdb_test.go, internal/server/minidump_test.go, internal/testfixture/nativepdb/nativepdb.go.
    - Local evidence (2026-10-22): focused inline tests and `GOCACHE=/tmp/goodsentry-gocache go test -race ./internal/native -count=1` pass; `go vet ./internal/native` passes. The valid fixture exercises three inline levels through an S_BLOCK32 scope, source switches, scope offsets including the CodeView signature, and serialized Sentry frames. A fresh-volume Compose run of the published image returns readiness/dashboard200, `/api/v1/setup` `required:false`, and cookie-free `/api/v1/me` returns owner/org id1; an app-only restart preserves exactly one owner and organization.
    - Public evidence: source commit `1f5bf4484d6628714aaf6b9a461250e926b5bfd2` is on `main`. CI [39889867281](https://github.com/jerolan/good-sentry/actions/runs/39889867281) passes formatting, `go vet ./...`, the complete unit/PostgreSQL integration race suite and image build. Image workflow [39890349668](https://github.com/jerolan/good-sentry/actions/runs/39890349668) publishes `linux/amd64` and `linux/arm64`; the immutable OCI index is `sha256:ff2c8a49d05a46ffafa72bee8a0d9d41beb8dc6d10a8294b784bf05a5e4dee5`.
  - [x] Symbolicate functions from classic PDBs with stripped private records using public CodeView symbols.
    - Contract: only when DBI `ArePrivateSymbolsStripped` is set, scan DBI `SymRecordStream` (GSS), not the GSI/PSI indexes, for bounded `S_PUB32` (`cvpsfFunction`) records. Resolve segment/offset through the bounded DBI Section Map to executable PE sections, preserve raw names, and infer best-effort ranges through the next distinct public function or mapped descriptor boundary. Keep exact module procedure and source-line ranges more precise. Cap records, public functions, sections, and names; malformed framing, flags, ranges, unsupported selector/absolute/group mappings, or references return no partial PDB symbols. Require embedded GUID+age matching and do not demangle.
    - Test-authoring gate: observable result is `Symbolicate` assigning a public function to a frame inside its inferred range while a frame in another section remains unresolved; a generated stripped MSF fixture currently produces no object because module parsing yields no procedures; existing PDB tests cover private module procedure/C13 symbols only; extend the canonical native PDB parser fixture/test with no production seam.
    - Acceptance: a stripped fixture containing only valid GSS public function records maps names/ranges, ignores non-function public records, respects section-map translations and does not leak ranges into an adjacent executable section. Non-stripped PDBs keep current behavior, mismatched Debug IDs do not symbolize, and malformed/truncated/over-budget streams or invalid section references yield no partial symbols.
    - Verify: focused and full `go test -race ./internal/native`, `go vet -tags=integration ./...`, `git diff --check`, exact-source GitHub CI and published amd64/arm64 image checks through `gh`.
    - Sources: [CodeView S_PUB32](https://llvm.org/docs/PDB/CodeViewSymbols.html#s-pub32-0x110e), [DBI stream and Section Map](https://llvm.org/docs/PDB/DbiStream.html), [Microsoft DBI stream indexes](https://github.com/microsoft/pdb-rs/blob/main/docs/pdb/dbi_stream.md), [Microsoft GSS/PSI stream layout](https://github.com/microsoft/pdb-rs/blob/main/docs/pdb/globals.md), [CodeView framing](https://github.com/microsoft/pdb-rs/blob/main/docs/codeview/symbols/symbols.md#symbol-record-framing), [Microsoft CodeView header](https://github.com/microsoft/microsoft-pdb/blob/master/include/cvinfo.h).
    - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `internal/native/pdb.go`, `internal/native/minidump.go`, `internal/native/pdb_test.go`, `internal/testfixture/nativepdb/nativepdb.go`.
    - Local evidence (2026-10-22): `gofmt` and `git diff --check` pass. Local Go tests were unavailable because the Go cache path was read-only and the filesystem had only about 10 MiB free; the complete exact-source CI suite below covers native and PostgreSQL integration race tests.
    - CI follow-up: initial run [39894079877](https://github.com/jerolan/good-sentry/actions/runs/39894079877) found a `uint16`/`int` Section Map mask type mismatch in `go vet`; commit `9b626941a572bed9470f3cb01b4ab7f7b0520e1b` fixes it. Its container workflow was correctly skipped after failed CI.
    - Public evidence: implementation commit `5eab6ebfa0067b4162e4b5ef3a9a4c34786f8275` plus type-fix commit `9b626941a572bed9470f3cb01b4ab7f7b0520e1b` are on `main`. CI [39894186240](https://github.com/jerolan/good-sentry/actions/runs/39894186240) passes formatting, `go vet ./...`, the complete unit/PostgreSQL integration race suite and image build. Image workflow [39894754891](https://github.com/jerolan/good-sentry/actions/runs/39894754891) publishes linux/amd64 and linux/arm64. Tags `main`, `latest` and `sha-9b626941a572bed9470f3cb01b4ab7f7b0520e1b` share OCI index `sha256:6f1198cad23e3feb1eee74784f5f2207682e28a6c0212240a6cc8d5f52a579b2`.
  - [x] Consume SymCache v7/v8/v9 debug files in native Minidump symbolication.
    - Contract: accept little-endian `SYMC` versions7-9 under the64MiB file limit. Parse the72-byte header and8-byte-aligned file/function/source-location/range/string sections with v7 u32-length strings, v8/v9 LEB128 strings, v7/v8 12-byte files and v9 24-byte files. For v9, accept variable-header0 or version1 and validate each optional table extent without interpreting variable/type payloads. Cap each declared table and the number of strings at1,000,000 entries, check all arithmetic and references, validate bounded inline chains, and reject malformed or unsorted/empty ranges atomically. Use the embedded DebugId, map each range to the source-location suffix, and emit the innermost function plus optional composed path and positive line as one module-relative `SymbolFunction`; skip the final no-source-location boundary and do not expand inline parents.
    - Acceptance: independent binary fixtures for versions7/8/9 resolve the correct DebugId, function and source line through `Symbolicate`; a real authenticated multipart debug-file plus Minidump/PostgreSQL flow resolves only the matching ID. Wrong-endian/magic/version, truncation, overflowing or over-budget counts, malformed strings, invalid references, inline cycles, duplicate/unsorted ranges and unsupported v9 variable headers yield no partial object.
    - Verify: `go test -race ./internal/native`, focused native Minidump PostgreSQL/HTTP race test, `go test -race -tags=integration ./internal/server -count=1`, `go vet -tags=integration ./...`, trim-path build, clean-image readiness/open-auth/restart smoke, then exact-source `gh` CI and both published image architectures.
    - Sources: [SymCache format and lookup](https://docs.rs/symbolic-symcache/13.9.0/symbolic_symcache/), [v7/v8 records](https://github.com/getsentry/symbolic/blob/13.9.0/symbolic-symcache/src/raw/v7.rs), [v7/v8 sections](https://github.com/getsentry/symbolic/blob/13.9.0/symbolic-symcache/src/v7/mod.rs), [v9 records](https://github.com/getsentry/symbolic/blob/b4571fb2ba0fb3244edd0ed4e27fb7eff7475d33/symbolic-symcache/src/v9/raw.rs), [v9 section parsing](https://github.com/getsentry/symbolic/blob/b4571fb2ba0fb3244edd0ed4e27fb7eff7475d33/symbolic-symcache/src/v9/mod.rs), [DebugId byte layout](https://github.com/getsentry/rust-debugid/blob/6185e37d328fe9e3336444118b1e25780458ddb9/src/lib.rs).
    - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `internal/native/debug_objects.go`, `internal/native/symcache.go`, `internal/native/symcache_test.go`, `internal/native/minidump.go`, `internal/server/minidump_test.go`, `internal/testfixture/nativesymcache/nativesymcache.go`.
    - Local evidence (2026-10-22): `go test -race ./internal/native`, `go vet -tags=integration ./...`, trim-path build and `git diff --check` pass. An isolated fresh-volume Docker Compose build reports readiness200, dashboard200, `/api/v1/setup` `required:false`, and cookie-free `/api/v1/me` returns the implicit owner; restarting preserves owner/org id `1`. The local PostgreSQL HTTP test could not run without `DATABASE_URL`; the exact test is covered by CI.
    - Public evidence: commit `6f066ae409ded61fdde33f8d16ccc19a5bc04e6e` is on `main`. CI `39851413272` passes formatting, vet, the full unit/PostgreSQL integration race suite and image build. Container workflow `39852205391` publishes `linux/amd64` and `linux/arm64` as `main`, `latest` and `sha-6f066ae409ded61fdde33f8d16ccc19a5bc04e6e` at OCI index `sha256:b72e9a380d942dd399aeb7a65b1f1198f82377580f4dde98b5781d17c7bcdaa8`.
  - [x] Honor DWARF CIE `S` signal-frame return-address lookup.
    - Contract: recognize the `S` marker in sized `.eh_frame` CIE augmentation and use the return address restored from that FDE unchanged for the next FDE lookup. Ordinary CIEs retain return-address-minus-one. Do not infer platform-specific signal-context register layouts.
    - Acceptance: a parser test recognizes `zRS`; an `UnwindCFI` outcome test proves a signal-restored PC at the exact start of the next FDE unwinds into that frame, while ordinary lookup behavior remains unchanged.
    - Verify: `go test -race ./internal/native`, `go test -race -tags=integration ./internal/server -run 'Minidump|Native' -count=1`, `go vet -tags=integration ./...`, and `go build -trimpath ./cmd/goodsentry`. Exact-source CI and public-image acceptance remain part of parent closure.
    - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `internal/native/cfi.go`, `internal/native/minidump_test.go`.
    - Local evidence (2026-10-22): `TestCFISignalFrameUsesUnadjustedReturnAddress` proves the `zRS` parser flag, exact-start lookup for a restored signal PC and unchanged return-address-minus-one lookup for ordinary frames. The full native race suite, integration-tag vet, trim-path build and fresh-volume Compose readiness/dashboard/open-auth/restart smoke pass. The focused server test is covered by PostgreSQL-enabled CI because no local `DATABASE_URL` is configured.
    - Public evidence: source tree `582c22ab704f8884401bc3188687faf5a587acc9` passes CI `39852760898` with formatting, vet, all unit and PostgreSQL integration race tests and image build. Container workflow `39853558056` publishes `linux/amd64` and `linux/arm64` to `main` and `latest` at OCI index `sha256:19c305f1ec8d17148cf24e2591083fe5b846b99603df8e615cb69d43a8dfa76c`.

  - [x] Decode LEB128 DWARF exception-frame pointer formats completely.
    - Contract: `DW_EH_PE_uleb128` and `DW_EH_PE_sleb128` consume bounded variable-length values using the existing uint64/int64 CFI readers. Apply PC-relative bases after signed values are extended. FDE start and range fields must each consume the full encoded value; malformed or overflowing encodings reject the FDE.
    - Test-authoring gate: `readCFIPointer` must return the correct value and byte count for multi-byte ULEB/SLEB and negative PC-relative SLEB. A credible regression is the current single-byte read truncating `80 20` to 128 and `ff 7e` to -1, shifting the range and instruction cursor. No current pointer-format or unwind test covers LEB pointer encodings; extend the parser owner and add one synthetic `UnwindCFI` outcome using these encodings. No production seam is needed.
    - Acceptance: test one- and multi-byte values, full consumption, negative PC-relative application, truncated and overflowing inputs, and an FDE whose LEB start/range allow a caller frame to unwind. Preserve fixed-width pointer formats.
    - Verify: focused regression first fails against the current parser; then `go test -race ./internal/native`, full `go test ./...`, integration-tag compilation and vet, trim-path build, formatting and `git diff --check`; exact-source GitHub CI and amd64/arm64 image publication through `gh`.
    - Sources: [DWARF 5 §2.5.1.1–2 and §7.6](https://dwarfstd.org/doc/DWARF5.pdf), [LSB DWARF Exception Header Encoding](https://refspecs.linuxfoundation.org/LSB_2.1.0/LSB-Core-generic/LSB-Core-generic/dwarfehencoding.html), [LLVM DWARF EH pointer constants](https://github.com/llvm/llvm-project/blob/main/llvm/include/llvm/BinaryFormat/Dwarf.h).
    - Files: `SPEC.md`, `tasks/todo.md`, `internal/native/cfi.go`, `internal/native/minidump_test.go`.
    - Local evidence (2026-10-25): both new regressions fail on the former parser because it truncates multi-byte values, accepts malformed ones, and cannot select the valid FDE; they pass after decoding through the bounded readers. `go test -race ./internal/native -count=1`, `go test ./...`, integration-tag compilation, integration-tag vet, `go build -trimpath ./...` and `git diff --check` pass.
    - Public evidence (2026-10-25): source commit `8007be650b844563f3298560c900d3d46de1e10f` is on `main`. CI [40232641385](https://github.com/jerolan/good-sentry/actions/runs/40232641385) passes formatting, vet, full PostgreSQL integration/race tests and image build. Publication [40233163266](https://github.com/jerolan/good-sentry/actions/runs/40233163266) publishes `main`, `latest` and `sha-8007be650b844563f3298560c900d3d46de1e10f` for linux/amd64 and linux/arm64 at OCI index `sha256:4ebf4cc6be40ca8bc36af402244a3c725ed3123217a5971d5fe07ab522796c85`.

  - [x] Consume indirect CIE personality pointer encodings without resolving them.
    - Contract: for the `P` field in a sized CIE augmentation, consume its supported base/application encoding while ignoring only `DW_EH_PE_indirect`; discard the value without resolving or invoking it. Keep indirect FDE pointer encodings unsupported. Reject a truncated field or a field that crosses the declared CIE augmentation boundary.
    - Test-authoring gate: extend the existing CFI parser owner with a `zPR` CIE using personality encoding `0x9b` and FDE encoding `0x1b`; prove `UnwindCFI` recovers its caller. Add truncated and augmentation-boundary cases, plus a direct assertion that an indirect FDE encoding still errors. The positive test must fail against the current parser because it rejects the indirect personality pointer.
    - Acceptance: parse and unwind the valid CIE without dereferencing personality metadata; reject malformed or out-of-augmentation personality payloads atomically; preserve rejection of indirect FDE starts.
    - Verify: `go test -race ./internal/native`, full `go test ./...`, integration-tag compilation and vet, trim-path build, formatting and `git diff --check`; exact-source GitHub CI and amd64/arm64 image publication through `gh`.
    - Sources: [GNU `CFI directives`](https://sourceware.org/binutils/docs/as/CFI-directives.html#cfi_005fpersonality).
    - Files: `SPEC.md`, `tasks/todo.md`, `internal/native/cfi.go`, `internal/native/minidump_test.go`.
    - Local evidence (2026-10-25): the new positive unwind regression fails on the prior parser with `indirect CFI pointer is unsupported`; after the fix, the `zPR` CIE recovers its caller and malformed/truncated augmentation plus indirect FDE cases reject. Focused regressions, `go test -race ./internal/native`, full `go test ./...`, integration-tag compilation and vet, `go build -trimpath ./...`, formatting and `git diff --check` pass.
    - Public evidence (2026-10-25): source commit `ae0bb6adf00edd4cdf494ef2968da422f215b3fc` is on `main`. CI [40234951669](https://github.com/jerolan/good-sentry/actions/runs/40234951669) passes formatting, vet, PostgreSQL integration/race tests and image build. Publication [40235508558](https://github.com/jerolan/good-sentry/actions/runs/40235508558) publishes `main`, `latest` and `sha-ae0bb6adf00edd4cdf494ef2968da422f215b3fc` for linux/amd64 and linux/arm64 at OCI index `sha256:e5de2b35de40139a6b02bc037b8fefdbd9e933b975eebdce0ebf12a9ace262eb`.

  - [x] Parse DWARF CIE v4 address and segment sizes in specification order.
    - Contract: read `address_size` and `segment_selector_size` immediately after augmentation; accept address sizes4 and8, and reject unsupported sizes or nonzero segment selectors.
    - Acceptance: cover both supported address sizes, header and CFI instruction decoding, truncated size fields, unsupported addresses and unsupported segment selectors.
    - Verify: `go test -race ./internal/native`, `go vet -tags=integration ./...`, trim-path build and `git diff --check`; exact-source CI and public-image acceptance remain part of the native parent.
    - Sources: [DWARF 4 §6.4.1](https://dwarfstd.org/doc/DWARF4.pdf), [DWARF 5 §6.4.1.1](https://dwarfstd.org/doc/DWARF5.pdf).
    - Local evidence (2026-10-24): RED reproduced rejection for valid four- and eight-byte CIE v4 headers. The parser now reads fields in standard order, decodes alignment/return-register/CFA instructions correctly, and rejects truncation, unsupported addresses and segment selectors. Native race, integration-tag vet, trim-path build and `git diff --check` pass.
    - Files: `SPEC.md`, `tasks/todo.md`, `internal/native/cfi.go`, `internal/native/minidump_test.go`.
  - [x] Read DWARF dereference expressions with their declared widths.
    - Contract: `DW_OP_deref` reads one target pointer; `DW_OP_deref_size` consumes a one-byte size operand in 1..pointerSize, reads only captured bytes and zero-extends them. Invalid widths, truncated operands and addresses outside captured memory stop evaluation.
    - Acceptance: expression tests cover `DW_OP_deref`, every valid width from 1 through pointerSize for four- and eight-byte pointers, invalid zero/oversized/truncated sizes, unavailable memory, and the actual returned value.
    - Verify: `go test -race ./internal/native`, `go vet -tags=integration ./...`, and `go build -trimpath ./cmd/goodsentry`. Exact-source CI and public-image acceptance remain part of parent closure.
    - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `internal/native/cfi.go`, `internal/native/minidump_test.go`.
    - Local evidence (2026-10-22): the regression test failed before the fix because `DW_OP_deref` stopped evaluation and a missing `DW_OP_deref_size` operand was accepted. All widths from one byte through pointerSize now return the expected zero-extended values; invalid widths, truncated operands and reads beyond captured memory stop. Native race, integration-tag vet and trim-path build pass.

  - [x] Decode `DW_OP_addr` at the target address width.
    - Contract: consume four bytes for a four-byte CFI target and eight bytes for an eight-byte target; malformed or truncated operands stop expression evaluation.
    - Acceptance: expression tests verify the exact address for both widths and reject a truncated four-byte address.
    - Verify: `go test -race ./internal/native`, `go vet -tags=integration ./...`, and `go build -trimpath ./cmd/goodsentry`. Exact-source CI and public-image acceptance remain part of parent closure.
    - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `internal/native/cfi.go`, `internal/native/minidump_test.go`.
    - Local evidence (2026-10-22): the new test failed before the fix because four-byte expressions were read as eight-byte addresses. Four- and eight-byte values now decode exactly, and a truncated operand is rejected. Native race, integration-tag vet and trim-path build pass.

  - [x] Evaluate DWARF expression branches with bounded, validated control flow.
    - Contract: implement signed 16-bit `DW_OP_skip` and `DW_OP_bra` offsets from the byte after each operand; accept only expression opcode boundaries or the end position; stop after 4,096 executed operations so loops cannot run without bound. Malformed targets and truncated operands stop unwinding.
    - Acceptance: tests cover forward skip, taken and untaken branches, branch into an operand, out-of-range targets, truncated operands and an infinite loop.
    - Verify: `go test -race ./internal/native`, integration-tag vet, trim-path build and `git diff --check`; exact-source CI/public image remain part of parent acceptance.
    - Sources: [DWARF 5](https://dwarfstd.org/doc/DWARF5.pdf).
    - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `internal/native/cfi.go`, `internal/native/minidump_test.go`.
    - Local evidence (2026-10-22): RED showed forward `DW_OP_skip` returned the skipped literal and malformed targets were accepted; signed forward skip and taken/untaken `DW_OP_bra` now pass, while operand-entry/out-of-range targets, truncation and an infinite loop fail closed. `go test -race ./internal/native`, `go vet -tags=integration ./...`, trim-path build and `git diff --check` pass.
  - [x] Evaluate DWARF shift expressions with explicit bounds.
    - Contract: support `DW_OP_shl`, `DW_OP_shr` and `DW_OP_shra` with the former top entry as shift count and former second entry as value; counts of 64 or more and stack underflow stop evaluation.
    - Acceptance: cover left, logical right and arithmetic right shifts for four- and eight-byte target CFI, zero shift, invalid large shift counts and operand underflow.
    - Verify: `go test -race ./internal/native`, integration-tag vet, trim-path build and `git diff --check`; exact-source CI/public-image acceptance remain part of parent closure.
    - Sources: [DWARF 5](https://dwarfstd.org/doc/DWARF5.pdf).
    - Local evidence (2026-10-22): RED showed all three shift operations were rejected. The evaluator now handles left, logical-right and arithmetic-right shifts, plus zero shift; counts >=64 and underflow fail closed. Four-/eight-byte target cases pass `go test -race ./internal/native`; integration-tag vet, trim-path build and `git diff --check` pass.
    - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `internal/native/cfi.go`, `internal/native/minidump_test.go`.
  - [x] Evaluate bounded signed arithmetic and comparisons in DWARF expressions.
    - Contract: support `DW_OP_abs`, `DW_OP_neg`, `DW_OP_not`, signed `DW_OP_div`, and signed `DW_OP_eq/ge/gt/le/lt/ne`; division by zero and signed minimum overflow stop evaluation.
    - Acceptance: verify unary and binary operand ordering, negative values, all six comparison outcomes, zero divisors, minimum signed overflow and stack underflow.
    - Verify: `go test -race ./internal/native`, integration-tag vet, trim-path build and `git diff --check`; exact-source CI/public-image acceptance remain part of parent closure.
    - Sources: [DWARF 5](https://dwarfstd.org/doc/DWARF5.pdf).
    - Local evidence (2026-10-22): RED showed the evaluator rejected all requested operations. Signed abs/neg/division, bitwise complement and all six signed comparisons now pass, including negative operands; zero division, signed-min overflow and stack underflow fail closed. Native race, integration-tag vet, trim-path build and `git diff --check` pass.
    - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `internal/native/cfi.go`, `internal/native/minidump_test.go`.
  - [x] Evaluate DWARF modulo using the accepted truncated-division semantics.
    - Contract: `DW_OP_mod` computes signed 64-bit remainder using truncation toward zero; zero divisors and signed minimum divided by -1 stop evaluation.
    - Acceptance: cover negative numerator and denominator, exact division, zero divisor, signed overflow and stack underflow.
    - Verify: `go test -race ./internal/native`, integration-tag vet, trim-path build and `git diff --check`; exact-source CI/public-image acceptance remain part of parent closure.
    - Source: [accepted DWARF clarification](https://dwarfstd.org/issues/250924.2.html).
    - Local evidence (2026-10-22): RED showed `DW_OP_mod` was unsupported. Truncated signed remainders for negative numerators/denominators and exact division now pass; zero divisor, signed-min overflow and underflow fail closed. Native race, integration-tag vet, trim-path build and `git diff --check` pass.
    - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `internal/native/cfi.go`, `internal/native/minidump_test.go`.
  - [x] Complete DWARF stack rotation and no-op operators.
    - Contract: `DW_OP_rot` rotates the top three values in the standard order; `DW_OP_nop` leaves all values unchanged. Rotation with fewer than three values stops evaluation.
    - Acceptance: verify the three-value rotation result, nop preservation, underflow, and continued rejection of unsupported opcodes.
    - Verify: `go test -race ./internal/native`, integration-tag vet, trim-path build and `git diff --check`; exact-source CI/public-image acceptance remain part of parent closure.
    - Source: [DWARF 5 operations](https://dwarfstd.org/doc/DWARF5.pdf).
    - Local evidence (2026-10-22): the focused regression failed before implementation because both operations were unsupported, then passed for the standard three-value rotation, no-op preservation and underflow rejection. Native race tests, integration-tag vet, trim-path build and `git diff --check` pass.
    - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `internal/native/cfi.go`, `internal/native/minidump_test.go`.
  - [x] Honor terminal `DW_OP_stack_value` in CFI rules.
    - Contract: `DW_CFA_expression` returns an immediate register value when its final opcode is `DW_OP_stack_value`; otherwise its expression yields a captured-memory address. `DW_CFA_val_expression` uses the result directly. Empty or nonterminal stack-value opcodes stop evaluation.
    - Acceptance: register-rule tests distinguish immediate values from memory addresses, cover val_expression, and reject empty/nonterminal stack-value expressions.
    - Verify: `go test -race ./internal/native`, `go vet -tags=integration ./...`, and `go build -trimpath ./cmd/goodsentry`. Exact-source CI and public-image acceptance remain part of parent closure.
    - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `internal/native/cfi.go`, `internal/native/minidump_test.go`.
    - Local evidence (2026-10-22): the regression failed before the fix because an immediate CFI value was treated as an address, and an interior stack-value opcode was accepted. Tests now prove immediate `DW_CFA_expression`, address-based `DW_CFA_expression`, direct `DW_CFA_val_expression`, and rejection of empty/nonterminal markers.
  - [x] Decode bounded `DW_OP_implicit_value` scalar blocks in CFI expressions.
    - Contract: consume an unsigned-LEB128 nonzero length no greater than the target pointer width, read exactly that many little-endian bytes and push the zero-extended scalar. Preserve `DW_CFA_expression` address semantics unless terminal `DW_OP_stack_value` is present; `DW_CFA_val_expression` returns the scalar directly. Reject empty, truncated and over-width blocks and branch targets inside the block.
    - Test-authoring gate: observable behavior is a valid implicit scalar reaching the register-rule result, with an unmarked `DW_CFA_expression` still dereferencing its address. The credible regression is the evaluator rejecting opcode `0x9e`; existing expression and stack-value owners do not cover block operands. Extend the native expression and register-rule owners; no production seam is needed.
    - Acceptance: test every supported block width for 32- and 64-bit targets, little-endian zero extension, expression/value-expression behavior, and empty/truncated/over-width operands plus a branch into block bytes. Unsupported and malformed expressions stop unwinding.
    - Verify: focused and full `go test -race ./internal/native`, `go vet -tags=integration ./...`, trim-path build, `git diff --check`, exact-source CI and published amd64/arm64 image through `gh`.
    - Source: [DWARF 5 `DW_OP_implicit_value`](https://dwarfstd.org/doc/DWARF5.pdf).
    - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `internal/native/cfi.go`, `internal/native/minidump_test.go`.
    - Local evidence (2026-10-24): the focused regression failed before implementation because opcode `0x9e` was rejected. Scalar widths1..4 and1..8, little-endian zero extension, stack-marker bytes, malformed widths/truncation, branch-boundary rejection, and both register-rule modes now pass under native race tests; full native race, integration-tag vet, trim-path build and `git diff --check` also pass.
    - Public evidence (2026-10-24): commit `b84cc42e98ea21d34ef6b0b1417654eee5309019` is on `main`. [CI 40151025290](https://github.com/jerolan/good-sentry/actions/runs/40151025290) passed formatting, vet, PostgreSQL integration/race tests and image build. [Image workflow 40151457897](https://github.com/jerolan/good-sentry/actions/runs/40151457897) published `linux/amd64` and `linux/arm64`.
  - [x] Support DWARF `DW_OP_xderef` and `DW_OP_xderef_size` in native CFI expressions.
    - Contract: treat the top stack item as the address and the preceding item as the address-space identifier. Map identifier0 only to captured process memory; reject other identifiers. Read pointerSize bytes for `DW_OP_xderef` and 1..pointerSize little-endian bytes for the one-byte `DW_OP_xderef_size` operand; reject malformed stacks, sizes, branches into operands and unavailable memory.
    - Test-authoring gate: the observable contract is a CFI register value read from captured memory through address-space identifier0. Before repair, the owner rejects opcodes0x18/0x95. Existing dereference tests do not exercise address-space stack order or the size-operand boundary. Extend the existing native expression evaluator owner, with no production seam or duplicate HTTP test.
    - Verify: focused native race tests, full native race and PostgreSQL integration/race suite, integration-tag vet, trim-path build, formatting, `git diff --check`, exact-source GitHub CI and published amd64/arm64 image through `gh`.
    - Source: [DWARF5 extended dereference operations](https://dwarfstd.org/doc/DWARF5.pdf); address-space-to-memory mapping0 is the explicit GoodSentry contract in `SPEC.md`.
    - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `internal/native/cfi.go`, `internal/native/minidump_test.go`.
    - Local evidence (2026-10-25): expected RED recorded in `test-results/goal-cfi-xderef-red.log`; implementation passes expanded pointer-width and malformed-input cases under `go test -race ./internal/native`. `go test ./...`, `go vet -tags=integration ./...`, `go build -trimpath ./cmd/goodsentry`, and `git diff --check` pass.
    - Public evidence (2026-10-25): commit `71a957e9a0bdd2014031f12c417946cf1cf34a3c` is on `main`. [CI 40245600629](https://github.com/jerolan/good-sentry/actions/runs/40245600629) passed formatting, vet, PostgreSQL integration/race tests and image build. [Image workflow 40245972410](https://github.com/jerolan/good-sentry/actions/runs/40245972410) published amd64/arm64 at manifest digest `sha256:8f39fbce5c657b1e8fe852001d334ce163e670f73007f3d95b091062d1f8ebc5`.
  - [x] Reject overflowing CFI LEB128 values and narrowing register-ID aliases.
    - Contract: decode unsigned and signed LEB128 values only when they fit `uint64` and `int64`; reject overflow and unterminated encodings. Validate every encoded CFI register number against `math.MaxUint32` before conversion. `DW_OP_regx` and `DW_OP_bregx` must not truncate an out-of-range register into an existing register key.
    - Test-authoring gate: the current expression owner has no register operand above `uint32` or 10-byte LEB overflow case. Extend the native CFI expression/cursor owners to verify that the current implementation aliases `1<<32` to register zero, while preserving valid `uint64`/`int64` extrema and rejecting overflow, unterminated and truncated sequences. No production seam or duplicate transport test is needed.
    - Acceptance: cover valid min/max signed and unsigned encodings, invalid tenth-byte payloads, continuation past the maximum width, truncation, `DW_OP_regx`/`DW_OP_bregx` values above `math.MaxUint32`, and unaffected valid register lookup. Run focused and full native race tests, integration-tag vet, trim-path build, formatting, `git diff --check`, exact-source CI and amd64/arm64 publication through `gh`.
    - Source: [DWARF 5 §2.5.1.1–2 and §7.6](https://dwarfstd.org/doc/DWARF5.pdf); `DW_OP_regx` and `DW_OP_bregx` use ULEB128 register operands.
    - Local evidence (2026-10-25): regressions failed before the change because 10-byte ULEB/SLEB overflow was accepted, `DW_OP_regx`/`DW_OP_bregx` aliased `1<<32` to register zero, and three CFI instruction operands did the same. Focused regressions, `go test -race ./internal/native`, full `go test ./...`, integration-test compilation, integration-tag vet, `go build -trimpath` and `git diff --check` pass.
    - Public verification (2026-10-25): exact-source CI [40225344293](https://github.com/jerolan/good-sentry/actions/runs/40225344293) passes formatting, vet, full PostgreSQL integration/race tests and image build at `86cff5a3322779a9c59718c99d776fe61a622972`; image publication [40225856561](https://github.com/jerolan/good-sentry/actions/runs/40225856561) publishes `ghcr.io/jerolan/good-sentry:main`, `:latest` and `:sha-86cff5a3322779a9c59718c99d776fe61a622972` for linux/amd64 and linux/arm64 at OCI index `sha256:e5c924d7ca35a10dadc0dd5c8052ef2968801d9192c913e4a8187abd67d6cb3c`.

  - [x] Apply the CIE data-alignment factor to signed CFA offsets.
    - Contract: `DW_CFA_def_cfa_sf` and `DW_CFA_def_cfa_offset_sf` set their CFA offsets to `signed_operand * cie.dataAlign`, as already required by `DW_CFA_offset_extended_sf` and `DW_CFA_val_offset_sf`. Reject signed 64-bit multiplication overflow before storing any signed-factored rule.
    - Test-authoring gate: the observable behavior is recovery of a caller from the correct captured stack address after parsing each signed CFA opcode. With factor `-8` and operand `-2`, the CFA displacement must be `16`; old code uses `-2` and reads the wrong address. Existing fixtures do not exercise either CFA `*_sf` opcode. Extend the native CFI parser/unwind owner with a real CIE/FDE and captured stack; add overflow controls for all four signed-factored rules. No production test seam or duplicate HTTP layer is needed.
    - Acceptance: both CFA opcodes recover the expected caller; all four signed-factored opcodes reject multiplication overflow; existing unsigned rules and ordinary `DW_CFA_def_cfa` behavior remain unchanged.
    - Verify: focused native race test, full native race suite, `go test ./...`, integration-tag vet, trim-path app build, formatting, `git diff --check`, exact-source CI and amd64/arm64 image publication through `gh`.
    - Source: [DWARF 5 §§6.4.2.2–6.4.2.3](https://dwarfstd.org/doc/DWARF5.pdf).
    - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `internal/native/cfi.go`, `internal/native/minidump_test.go`.
    - Local evidence (2026-10-26): the CFA recovery cases fail against the old parser because both rules use the unfactored `-2` displacement; all four signed-factored rules previously accepted overflowing products. After the fix, the focused regressions, `go test -race ./internal/native -count=1`, `go test ./... -count=1`, integration-tag vet, trim-path build, gofmt and `git diff --check` pass. The unprivileged full-suite attempt could not open local test listeners; the same suite passed with loopback permission.
    - Public verification (2026-10-26): exact-source CI [40249096930](https://github.com/jerolan/good-sentry/actions/runs/40249096930) passed formatting, vet, PostgreSQL integration/race tests and image build at `5b8ad1915557fc748dc4efbee06cc72191cc961c`; image publication [40249517100](https://github.com/jerolan/good-sentry/actions/runs/40249517100) published linux/amd64 and linux/arm64 at OCI index digest `sha256:34a6507b2ee05c07714547139a089270797faf9d74caad53b00dfbf8feb0a441`.

For each item above: add a consumer-facing test at the owning boundary, verify the actual UI/API outcome with stored data, inspect the diff and commit a single coherent increment. Do not mark a mapped-stack task complete based only on an upload response.

## Observability and operations

- [x] Accept current SDK span v2 batches atomically, deduplicate retries and aggregate only segments.
  - Preserve child relationships and typed attributes; verify real Node 11.7.0 error, trace and logs after the initially failing trace check.
- [x] Deliver transaction/span storage, time-window aggregates, average/p50/p95, errors and detail UI.
  - Legacy transactions and standalone spans persist; selectable-window counts/average/p50/p95/error aggregates, occurrence detail, operation statistics and the complete specified trace waterfall pass through API/UI and image acceptance.
- [x] Open exact stored legacy/segment occurrences from aggregates with authorized detail, honest span scope, raw context and 50-row pagination through API/UI.
- [x] Add aggregate child-operation statistics and complete trace reconstruction/waterfall without assuming the per-envelope batch limit bounds a trace.
  - [x] Add atomic normalized legacy-span projections, typed standalone attribution index and resumable concurrent historical processing inside the application.
  - [x] Add snapshot-based operation statistics and native authenticated routing with exact counts, continuous p95, nullable timing, canonical filters and explicit pending-history responses.
  - [x] Add the responsive operation breakdown with preserved filter/identity navigation and real browser pagination/empty/indexing acceptance.
  - [x] Verify actual pinned Go/Node child metrics, final PostgreSQL/race checks, image migration/restart persistence and remote CI for the statistics increment. Full trace waterfall remains separate.
  - [x] Upgrade legacy projection to version2 with normalized root/child IDs/times, preserving raw JSON, version1 statistics and resumable atomic history processing.
  - [x] Reconstruct the specified forest from narrow metadata with explicit duplicate/parent/cycle policies, timing bounds, paginated summaries/raw and related error/log counts.
  - [x] Add authenticated trace/node/error routes and responsive waterfall navigation with preserved origin state, project scope, parent-page access and exact raw JSON inspection.
  - [x] Verify real PostgreSQL deep/mixed/multi-envelope traces, HTTP boundaries, browser states, official SDK ancestry/timing and final-image upgrade/restart/CI before closing trace acceptance.
- [x] Deliver SDK logs with typed attributes, filters, trace correlation and detail UI.
  - SDK ingestion, deduplication, project/text/level/environment/trace/service/time filters, 25-row pagination and typed-attribute UI pass. Exact max-int64 decimal display has a real browser RED/GREEN regression. Native OTLP remains independently pending below.
- [x] Add the specified transaction/log absolute time windows and exact log-service filter, preserving defaults and URL/pagination behavior.
  - Typed store intervals, PostgreSQL boundary tests, HTTP validation, UI presets/active UTC bounds and real browser combined-filter evidence pass. Trace detail remains separate.
- [x] Deliver native OTLP /v1/logs and verify an actual exporter.
  - [x] Specify the public OTLP1.11 contract, explicit GoodSentry authentication/projections, lossless batch storage and acceptance owners in SPEC.md. Primary inspected official sources and owned log/schema/auth/UI callers, repeated49/49 official codec checks and reviewed six resolved design findings. Research/offline encoding is not network or product acceptance.
  - Factual provenance: `test-results/verification-otlp/public-contract-facts-20261015.md` separates normative requirements from local choices. `go-codec/primary-result.json` SHA256 `8a3a89637d080d1c9a8c8c1c2f01564e2dd21276c1bb38ce7308f756f4056f81` proves49/49 checks with Go1.27.2/OTLP1.11.0/protobuf1.36.11; the actual signal-only dependency list has118 packages and zero compiled grpc/gateway packages. The287-byte synthetic protobuf matches ExportLogsServiceRequest/LogsData bidirectionally. Offline official Python SDK1.45.1/JSON0.66b1 encoding has3 records/8 AnyValue kinds,524 protobuf bytes/1672 JSON bytes; summary SHA256 `5b4a92d8bf2856878ee45ddd74f3cbb8e32f0e22374b41912b2eabb3e2ebe51d`. Primary read actual codec probe and client endpoint/header/serialize/success owners; stock protojson requires the specified adapter and exporter SUCCESS/flush does not prove HTTP200/retention. No product network traffic or runtime mutation occurred in this research increment.
  - [x] Add the bounded OTLP binary/JSON/gzip adapter and validated record/node configuration, including preallocation limits, exact values, canonical projections/detail and small response encoders. Verify at the pure consumer/config owners before storage. Files: internal/otlp, internal/config, go.mod/go.sum; split the codec/config into bounded commits.
    - [x] Validate record/node deployment capacities in582a9c1 through the existing Load owner: six previously ignored-invalid-value RED cases, GREEN default/minimum/maximum and complete configuration checks. No HTTP receiver is enabled by this prerequisite.
    - [x] Implement and locally verify the wire decoder, schema-aware protobuf/streaming JSON preflight, identity/gzip framing, canonical-size admission and deliberate OTLP JSON adapter. Five pure consumer owners cover full typed values/presence, unknown/merge semantics, malformed/encoding boundaries and early budgets. The primary inspected the worker's302-line json.go and owns the rest; a separate focused advisor found no material wire/preflight defect. No test-only exports, mocked acknowledgments or private call-order assertions were added.
    - Local evidence is retained under `test-results/verification-otlp/wire-foundation`: valid-request stub RED and real config ignored-value RED are distinct from the local race harness's missing-C-compiler failure. Native functional checks and Docker Go1.27.2 race checks pass; the complete14-package PostgreSQL integration/race run passes (OTLP1.112s, config1.060s, protocol1.191s, server39.696s, store117.887s), with all-package vet and formatting. The actual original Python encoder bodies (524-byte protobuf/1672-byte JSON) decode through the new Go receiver into equal complete3-record values; canonical protobuf SHA256 remains `827589cbaf839a68601e1da821e367feb3a5efcfaa1b139b08d30a75abec3362`. This is offline wire interoperability, not network export or durable ingestion. Exact public-tree/matching-CI evidence belongs in `test-results/publication/otlp-wire-foundation-*` after publication; no new local application image/restart is claimed here.
    - Publication proof: dd70af583f37dcce314e2b14537a0e89ba06e447/tree6e2f2506f2afbee0fa150ee481d7b8c297e0c71b matches all285 public paths. CI38804817676/job116662804237 completes formatting/vet, PostgreSQL integration/race (otlp1.039s, server35.900s, store80.159s) and image build. The CI image dca6068f is distinct from the preserved accepted local623d6782 image; no accepted runtime changed.
    - [x] Complete bounded search projections, shared canonical batch preparation, selected-record JSON detail/representation admission and static success/warning/error encoders before Store/HTTP integration. Prepare plans exact raw/quoted projection bytes, memoizes resource/scope measurements, checks JSON depth and admits the stored wire payload before materialization. Select emits only its indexed record/context, preserving empty/typed values and exact digits. Static responses are a separate intention-focused commit a698dac. This remains a pure boundary; it activates no receiver or storage path.
    - The test-audit gate assigns five preparation/detail consumer owners and two independent public response-wire owners; no private counters or test-only exports are asserted. A compiling-stub preparation RED and30-case response RED precede implementation. Fresh review found a JSON/canonical-wire node asymmetry, reproduced in canonical-reopen-red.log and repaired before projections; it also prompted exact projection planning before expanded allocation. The reviewer checked both repairs, and the exact-fitting/one-byte-smaller representation test uses actual encoding/json bytes as its oracle.
    - Evidence under test-results/verification-otlp/preparation includes local-acceptance.json SHA256 `6cc18dce60053620748410bdcc71fa51562c4557de06daf3dfabfefc6b48f187`, full14-package PostgreSQL integration/race (otlp1.137s, server40.874s, store121.797s), final OTLP race1.081s after the fixed-wrapper count correction18-to17, all-package vet and207-file gofmt. The original Python encoder bodies pass Prepare with equal complete3-record projections/canonical524-byte payload and six equal selected reads; canonical SHA remains `827589cbaf839a68601e1da821e367feb3a5efcfaa1b139b08d30a75abec3362`, and actualHTTPExports remains0. The primary also repeats14/14 public-schema presence checks and10003 finite-double token comparisons, preserving the proto3 explicit-zero limitation. Final public-tree/matching-CI evidence is recorded under publication/otlp-preparation-* after publication; no new local application image/restart is claimed.
  - [x] Implement021 batch/reference migration and atomic Store.IngestOTLP with duplicate multiplicity, preserved SDK data and same-project selected-record detail/list semantics. Primary owns SQL/migration; workers own only isolated-schema ingestion/read tests. Real PostgreSQL covers canonical bytea NUL, shared payload, ordinal mapping, six two-record replays including four concurrent/two pools, empty/invalid project and actual later-insert/deferred-commit/cancel rollback after prior writes. Each failure preserves preexisting rows and has a positive retry. Three native-read owners prove SDK DTO/data preservation, OTLP25+2/filter/typed-selected behavior, present flags0, reduced limits and current membership/containment/revocation. Migration021 explicitly guards NULL-pair logic, nonnegative ordinal, same-project FK/unique identity and all prior values/null new references through020-to021/reopen.
    - Test-audit gates precede all three test owners. Compiling-stub ingest/read RED and actual missing-column migration RED are distinct from the first full-matrix failure: old020 compared a filtered001..019 snapshot with all receipts including021. Isolated diagnosis shows only schema_migrations differs. The separate fix5cabe6c captures every current receipt for rejected operations while retaining the historical upgrade comparison. Review also removed physical insertion-order assumptions from external failure/cancellation fixtures; no production seams were added.
    - Final fresh14-package PostgreSQL integration/race passes (store126.729s/server40.288s/otlp1.127s), with integration-tag vet and211-file format checks. Evidence: test-results/verification-otlp/store-foundation/local-acceptance.json SHA256 `270e428fc8d276ae97924c24ce3248d10d32a0cb2b2210f07d92e4f81b8d135a`. The actual application compiles310 packages and zero grpc/gateway packages. Exact public-tree/matching-CI evidence follows under publication/otlp-store-*; this provider child activates no HTTP endpoint, official network export, new local app image or accepted-runtime upgrade.
  - [x] Wire actual enabled SDK-key lookup, exact prefixed /v1/logs transport and authenticated native log detail. Verify representations, boundaries, body/record/node limits, gzip, CORS, disabled feature and no cookie/Bearer escalation at real HTTP. Files: workspace key lookup, shared protocol key syntax, server/otlp.go, server.go and owning HTTP tests; primary owns authentication/integration.
  - [x] Add the original Projects exporter recipe and Logs OTLP/source/full typed-detail UI, preserving existing filters, SDK attributes and pending-navigation behavior. Verify loading/retry, schema/trace/type precision and safe hostile text in actual Chromium320/768/1440. Files: app.js and focused external browser owner; bounded worker, no backend/spec edits.
  - [x] Execute official Python1.45.1 protobuf identity/gzip and JSON0.66b1 exporters on a fresh18089 image copy, observe actual HTTP200/wire/response and independently verify canonical values, bytea retention, scopes/multiplicity/isolation. Verify existing Sentry SDK log/error/attachment and every baseline4010 row/524 event plus uploads and nine protected runtimes. External harnesses live under tests/compat; private credentials and wire remain outside tracked files.
      - 2026-10-17 first real traffic: a fresh disposable compose stack (project gsotlpv, image goodsentry:otlpsmoke built from this tree, http://127.0.0.1:18091) accepted a real protobuf OTLP/HTTP batch from the official Python 1.45.1 OTLPLogExporter through BatchLogRecordProcessor force_flush; the exporter reported SUCCESS and both records became visible through the authenticated API. tests/compat/otlp_python_smoke.py now covers bootstrap (setup/login + project DSN key extraction), INFO/WARN emission with typed attributes (string/integer/boolean/double), API list check with lowered severity, per-record detail read back with source=="otlp" and native logRecord.attributes (stringValue/intValue/boolValue/doubleValue). Two runs passed (exit 0): run1 IDs 857b5872174116d6, run2 evidence at test-results/verification-otlp/python-otlp-exporter/result.txt. go vet ./cmd/... ./internal/... exit 0. Not yet covered by this harness: gzip transfer, JSON content-type exporter variant, bytea retention against a reduced later limit, baseline-row verification of the nine protected runtimes.
  - [x] Prove all-current-row/canonical-batch app-only restart, final image/source assets, full integration/race/vet, exact public tree and matching remote CI before closing native OTLP. Preserve failed-attempt provenance and keep full parity/memory claims open.
- [x] Deliver the compatible retained-log API at `054_log_host`: hourly statistics, distinct resource names by last use, keyset paging with a cursor Link, published level spelling, inverted severity numbers and unwrapped attribute data, over session or `event:read`/`event:write` Bearer authority with project containment. Additive054 projects `host.name` from Sentry attributes and OTLP resources; historical rows keep an empty host. Real PostgreSQL and HTTP cover filters, half-open windows, cursor paging, containment, 404/400 admission and the disabled-feature route. Numeric log identities stand in for GlitchTip UUIDs and the published stats window defaults to seven days.
- [x] Deliver configurable retention with actual removal/recovery checks. Event, log, transaction/span, file, monitor, release, pending-report and OAuth-secret owners pass actual expiry/recovery checks.
  - Native logs, transaction/standalone spans, error events, uptime checks, completed files and release metadata are accepted independently below. Cold archival remains required; this parent stays open.
- [x] Deliver HTTP HEAD/GET/POST monitors and stored checks with UI.
  - Real probes verify each method/status/body policy. Browser GET down/recovery, configuration and stored history pass at 320/768/1440 pixels.
- [x] Deliver TCP checks and heartbeat expiry/recovery with UI.
  - Real TCP connections, anonymous heartbeat POST, deadline expiry/down/recovery, history pagination and pause/resume pass through the running application.
- [x] Verify scheduler leases and recovery across simultaneous replicas/restarts.
  - Eight concurrent claims, superseded-token fencing and expired-lease reopen recovery pass on PostgreSQL. Two actual application containers share one due completion; session/history persist across application restart.
- [x] Deliver SSL certificate monitors after verifying their public behavior. Controlled CA/host/expiry failures, public scheduler state and responsive monitor UI pass; current image restart preserves the monitor.
  - Current public OpenAPI includes SSL; the initial HTTP/TCP/heartbeat slice does not establish certificate-monitor parity.
- [x] Implement the specified uptime network policy, transition history and owner/member UI.
  - [x] Public uptime status pages accepted at `b98caa2229e25f33b5b875f28ccaa9afc5f1a6da`: additive046 pages/monitor selection, compatible list/create, native edit/delete UI and public HTML/JSON pass real PostgreSQL, HTTP and Chromium at320/1440 without overflow or console errors. Public output exposes monitor names/status only; URLs/heartbeat keys remain hidden. Anonymous registry has exact amd64/arm64 revisions and shared main/latest index `sha256:519fe63de8934784ba4ce7e5e33b02c7389c50e8f7b3981c9c5fd187dd5eee48`; public amd64 binary matches local at22,765,728 bytes, SHA-256 `4c2efe74aab14a3cf4547d16f1f33d9f5d5c19df232e15de86c27f3468ed966f`. Evidence: `test-results/acceptance-status-pages-20261020`.
  - Default private-target rejection and explicit opt-in pass in the built image. Actual member reads, hidden write controls, disabled-feature API/UI and transition/all-check pagination pass in Chromium. Scheduling and expected-body semantics are explicit GoodSentry decisions in SPEC.md.
- [x] Deliver frequency alert rules, cooldown and durable delivery intents.
  - Project-scoped native rules, receipt-time threshold crossings, environment filtering and transactional event/check outbox pass real PostgreSQL, HTTP capture, browser and restart checks. Automatic delivery is verified separately below.
- [x] Deliver SMTP/consolemail and webhook retries, plus uptime down/recovery alerts.
  - User explicitly authorized configured automatic delivery on 2026-10-14, resolving the previous automatic approval rejection. Startup composes sender/workers and exposes their availability. Actual-image SMTP/webhook retries survive restart and coordinate across two replicas; browser sent/attempt views pass with real local receipts. External provider receipts and remaining product parity stay open.
- [ ] Verify provider-specific alert delivery with authorized external receipts.

## Remaining parity and release verification

- [x] Publish reviewed code to the user-provided jerolan/good-sentry repository without overwriting remote work.
- [x] Replace the public README with a polished English introduction, original visual assets, actual quickstart and honest compatibility/status.
- [x] Build and browser-verify an original responsive static landing under site/ with project-base-path-safe assets and real product/demo context.
- [x] Add verified/pinned GitHub Actions CI and Pages workflows, push, fix actual workflow failures and verify the deployed public site URL.

- [x] Complete scoped API tokens, team memberships, invitations and email password recovery.
  - [x] Store hashed expiring/revocable organization/project-scoped read credentials and consume them through authorized compatible project reads, preserving current memberships and explicit all-project semantics.
  - [x] Add session/CSRF-protected credential management and bearer/scope/header/prefix transport boundaries without widening native or SDK authentication.
  - [x] Deliver the responsive one-time-secret creation/list/revoke UI with real owner/member, pagination, input retention and safe text acceptance.
  - [x] Verify actual issued credentials with pinned sentry-cli project listing, revoked rejection, PostgreSQL/race checks and final-image restart/remote CI. Release/write consumers and the remaining account capabilities stay required.
  - [x] Add the OpenAPI `limit` alias to `GET /api/0/organizations/{org}/projects/` while preserving `per_page`, current cursor and access behavior. Same-valued aliases are allowed; conflicting or repeated page-size values and malformed numbers are rejected. Extend the project pagination HTTP owner to verify `limit` and the alias in emitted/followed links.
    - `TestAPITokenHTTPContract` passes under PostgreSQL integration/race. Exact-source CI [39979744432](https://github.com/jerolan/good-sentry/actions/runs/39979744432) and multiarchitecture image publication [39980675495](https://github.com/jerolan/good-sentry/actions/runs/39980675495) pass for `4839d937525da8f78e7f6048e7cda52ad44629af`.
  - [x] Implement and verify generation-bound session/API-token issuance and upgrade preservation before password mutation. Additive035 and real stale-generation owners pass.
  - [x] Implement authenticated own-password change and Account UI, with atomic all-session/personal-token revocation and actual concurrency/HTTP/browser/image acceptance.
  - [x] Deliver account email, single-use password recovery, invitations and team/membership management with separate account authorization.
  - [x] Account mail and password recovery accepted on the public8160b3 binary: additive036, encrypted token outbox, SMTP MIME sender,60-second request floor, worker, public request/reset routes and auth forms pass real SMTP, HTTP and responsive Chromium. Current-image restart preserves all56 rows/47 tables. Evidence: `test-results/acceptance-account-current-20261020/{recovery-fresh,account-before-restart.json,account-after-restart.json}`.
  - [x] Organization member roles and team management accepted on the public8160b3 binary. Native member/team APIs, last-owner locking, owner/admin rules, membership cascades, default-team protection, project unassignment, PostgreSQL/HTTP and responsive Chromium pass; restart preserves all54 rows/47 tables. Evidence: `test-results/acceptance-account-current-20261020/{people-ssl,people-before-restart.json,people-after-restart.json}`.
  - [x] Email invitations accepted on the public8160b3 binary: additive037, encrypted single-use links, seven-day expiry, owner/admin rules, retries, cancellation and new-account acceptance pass actual isolated SMTP, responsive browser, replay refusal and restart.
    - The first full-suite run exposed a fixture race: the console mail was visible before the worker committed the invitation's `sent` state. The HTTP inspect owner now waits for that durable state; three focused repetitions and the complete server suite pass. No production behavior changed.
- [x] Complete supported Sentry-compatible management API routes, OpenAPI/admin flags and endpoint matrix.
  - Exact-source CI run [40024980583](https://github.com/jerolan/good-sentry/actions/runs/40024980583) passed on `b521f7d85054024697725ba999f0fb308bd629ad` with the complete PostgreSQL integration/race suite, formatting, vet and application image build. All route-specific children below have their HTTP/store/browser evidence; OpenAPI is generated from registered routes. This acceptance uses GoodSentry's local HTTP fixtures and does not require a live third-party installation.
  - [x] Opt-in OpenAPI and administration accepted at `f391fbfc4ad7141c8fae273fd3be2c5c40b22ad4`: `ENABLE_OPENAPI` serves a generated 3.1 document plus endpoint matrix from the exact registered routes, and `ENABLE_ADMIN` serves a read-only owner/admin overview that rechecks current membership and hides credentials. Both default off and return404 when disabled; Compose passes both variables. Focused HTTP, full integration/race, vet/build, Compose validation and Chromium1440/390 pass. CI39723127726 and image39723624107 pass; public digest `sha256:dcd35b1ac1f813f945b2e38a7ea7743ab92228cc4d760de4cb82f78422f76ae5` publishes amd64/arm64, and the public image responds200 with flags enabled and404 by default. Exact amd64 binary matches local at23,576,736 bytes SHA-256 `9b9a43d3db13b5004225aae3627f8079f69a6082682ca7dfbd029b70299eb0c6`; arm64 is21,758,112 bytes SHA-256 `80557af1d35099c4cadaeaeb9a274d3b7a2d904b5cea03b0d4d30ed57ea9d7c4`. Evidence: `test-results/acceptance-openapi-admin-20261021`.
  - [x] Compatible project update/delete and native UI: immutable slug/creation metadata, owner/admin or project-scoped `project:write`, strict name/platform validation, permanent cascade warning and compatible/native route coverage.
  - [x] Add GlitchTip-compatible project `eventThrottleRate`: persist0..100 with default0, return it from project DTOs, and sample each authenticated Sentry/OTLP intake request once with429 retry headers. Verify fixed sampling boundaries, project create/update/read, migration defaults, and that per-key quotas remain independent.
    - `TestProjectEventThrottleDecision`, project create/update/read HTTP coverage and `TestProjectThrottleRejectsSentryAndOTLPRequests` cover rate boundaries, default/persistence, all intake families, retry headers and unchanged per-key usage. CI39966001083 passes formatting, vet, the complete PostgreSQL integration/race suite and image build. Container publish39966916653 publishes amd64/arm64 `main`, `latest` and revision tags at manifest `sha256:f4fd50fc026cb1605fe14952c10a42b13d061a20c65d2145e2a15ad4fd7d9dbe`.
  - [x] Compatible issue comments: global/organization routes, event read/write scopes, POST/PUT data.text bodies, CommentSchema mapping, cursor pagination, native activity reuse and author/owner/admin mutation boundaries.
  - [x] Compatible issue tags accepted at `f07647d732eea946813c3d10ed40adf678f9a03f`: merged-alias visibility, object/array aggregation, string-only values, unique/total counts and top-5 IssueTagSchema values pass PostgreSQL and HTTP. CI39577171692 and image39577809686 pass; anonymous amd64/arm64 use `65532:65532` and exact revision with shared index `sha256:75036a90c280785968d650897c1768a7b1b295a7833e459ee4946a0e21c027db`. Public amd64 binary matches local at22,810,784 bytes, SHA-256 `36edc63f56ac715079c9db111bde4f5f1cd89b4d97293f578994d614abaaee06`;648 blobs match source.
  - [x] Add optional issue-tag `key` filtering at `e088d8e`: both route aliases return the exact case-sensitive key, including URL-decoded literal keys; absent returns all, empty/unmatched returns `[]`, and unknown/repeated/malformed queries return400. The existing PostgreSQL HTTP owner covers both paths and aggregation. CI [39976040595](https://github.com/jerolan/good-sentry/actions/runs/39976040595) passes format, vet, full PostgreSQL/race suite and app image build; image workflow [39977008284](https://github.com/jerolan/good-sentry/actions/runs/39977008284) publishes both architectures.
  - [x] Compatible uptime monitors accepted at `82148dd5803cfb10151688494a9441b6942dfc57`: paginated list/detail/checks, MonitorIn create/update/delete, org read/write and owner/admin authority, type aliases and explicit rejection of numeric environment IDs. CI39584500072 and image39585018434 pass; anonymous amd64/arm64 use exact revision and shared index `sha256:f682cfa7bcdd6e403c3c1ac74913d4e1e107e2fdf75c3c039b4529c867bd7277`. Public amd64 binary matches local at22,835,360 bytes, SHA-256 `8dc3291c81d9e70489d22bbe679836fc5e18c5f384db0e39365895fc944a384f`;662 blobs match source.
  - [x] Compatible project alerts accepted at `c5e3eb8491738f68a35f038b18f98420f9e2a802`: slug-to-ID grant resolution, paginated list, ProjectAlertIn create/update, delete/test, native owner/admin rules, email-to-team mapping, webhook providers and explicit rejection of unsupported recipient modes. CI39587223866 and image39587810785 pass; anonymous amd64/arm64 use exact revision and shared index `sha256:7a0a31084b5d6827ed1cf8d05120fa0267820eeae00cb1502d21c60fddca9a9c`. Public amd64 binary matches local at22,868,128 bytes, SHA-256 `9955303a2161399ee82b5923fcb96985e1e0ef99a7c043d595d893eb3116754a`;677 blobs match source.
  - [x] Compatible user profile/options and notification preferences accepted at `93e6b4218a4d5ea36cc2748f804e07ba3eeca5fe`: additive047, self/me authorization, primary verified email listing, persisted subscribeByDefault and explicit unsupported secondary-email verification. CI39590333583 and image39590924892 pass; anonymous amd64/arm64 use exact revision and shared index `sha256:322eb2a2002d5e553825aac7d1faf1e393d09668e0671cf9c1dec51ab25fd387`. Public amd64 binary matches local at22,880,416 bytes, SHA-256 `0caa12fa4ec01fdfa31b402de88e0af00fe31b7980e1a1b968af099bb24b9fcd`;694 blobs match source.
  - [x] Compatible organization member detail, role update/deletion and team-membership mutations accepted at `c96e2e74d0a221cdc87c1e923804abc2ed75f026`: active-member ordered teams, self/`me`, last-owner and org:write controls, idempotent team assignment and cross-organization denial. CI39593733782 and image39594213227 pass; anonymous amd64/arm64 use exact revision and shared main/latest index `sha256:2ab78c2275620538bcb524c04ceb088fd9dcabd4f46be5bae1ffa4be472200ad`. Docker-built amd64/arm64 binaries match published bytes exactly at22,904,992 bytes SHA-256 `0cac0829d74d29fc8a4df5c237757179fbcc79e200196c611f3763ef949c9604` and21,233,824 bytes SHA-256 `5f19037f2b60aa8aaddc3f36961e1930abe7b3aa6ac7f7d3f899a6765fabcc6d`;699 blobs match source. Evidence: `test-results/acceptance-compat-organization-members-20261021`.
  - [x] Compatible team project creation accepted at `5fb0901c28e36e6057ffcc5eadeb5b967e460596`: ProjectIn validation, canonical/derived slug, duplicate and team isolation, atomic first key and creator team membership, session/org:write controls and immediate project/key visibility. CI39597895832 and image39598544192 pass; anonymous amd64/arm64 use exact revision and shared main/latest index `sha256:81f6469480303f6ec18562027fbaa2f1b98a4bc66ed2ba21f91b8bc150c228f7`. Docker-built amd64/arm64 binaries match published bytes exactly at22,917,280 bytes SHA-256 `3e3ce5fa6b08d4671748d92a5173356d72707fb3cd2ada7120fd75c9eecf4d6d` and21,233,824 bytes SHA-256 `43ad70211de4034b27075f89cb8a01711afe7859cacd6337c9095f25c19d1810`;721 blobs match source. Evidence: `test-results/acceptance-compat-project-create-20261021`.
  - [x] Compatible single-relation project-team attach/detach accepted at `06f012b2ac38873c9045c457d2c743e37fe7a3f6`: project:write and owner/admin authority, idempotent same-team/clear operations, explicit409 for a second team and grant-contained missing/foreign rejection. CI39601927772 and image39602594737 pass; anonymous amd64/arm64 use exact revision and shared main/latest index `sha256:384fd3b92309e10291c9122073ec5ec48a26cb0c2dd69bf7b3c12f4e9a9f7cc7`. Docker-built amd64/arm64 binaries match published bytes exactly at22,929,568 bytes SHA-256 `a6b2eec9ab44db9a4d78be3c08568e5a22e06145ebff389cf1bbb97d7fccc209` and21,233,824 bytes SHA-256 `118b62113bca4ae26b823b903f1ba17763f591e6716cd63558dd0d76f15c4ea8`;742 blobs match source. Evidence: `test-results/acceptance-compat-project-team-20261021`.
  - [x] Additive048 many-to-many project-team associations accepted at `75cdef09a555c5475d4cfcee83da087eb0d57528`: cross-organization FK containment, legacy relation backfill/reopen, deterministic multi-team reads, idempotent attach/detach without replacement, team-delete repair, project counts/team-email expansion across all teams and preserved legacy primary mirror. CI39609348551 and image39609982813 pass; anonymous amd64/arm64 use exact revision and shared main/latest index `sha256:34e2e9cf5cd6e63b6cdf381772a04c061eec2a3786fa389dcfe94b05f82f89ce`. Docker-built amd64/arm64 binaries match published bytes exactly at22,933,664 bytes SHA-256 `1e1c452695586915c1fc00ccbbd45af411aca9bc1e243fa72a3c75b9538af156` and21,233,824 bytes SHA-256 `af06d5263051a5a8df4809fe97cb23e08f90df7118e6fa85df8eda904d86fa5c`;764 blobs match source. Evidence: `test-results/acceptance-project-teams-multi-20261021`.
  - [x] Additive049 compatible organization/project environments accepted at `aef6a02ea3d4029bb938461baecb084bb90e678f`: historical event/monitor/log/span backfill and reopen, live union, visibility aggregation, project detail/hide updates, null public IDs and organization/project grant containment. CI39616416386 and image39617552818 pass; anonymous amd64/arm64 use exact revision and shared main/latest index `sha256:2391c395f566a89bc8d6b48aa2fedc1b32d1d2f79e8ef7cdcd2c4a8f1ba00f80`. Docker-built amd64/arm64 binaries match published bytes exactly at22,966,432 bytes SHA-256 `3899f45149de74ee6d00206ce93ca461438e7936a4f24de44268ea3d125a3f1a` and21,299,360 bytes SHA-256 `62e7f866da4a87b8ef99bd2ec3a3740e3a02ec0c67242f5dc01f9215f0577731`;791 blobs match source. Evidence: `test-results/acceptance-environments-20261021`.
  - [x] Opt-in compatible organization creation accepted at `678f956823e1f7763af9c5086c6a0b73b5c18b35`: `GOODSENTRY_ENABLE_ORGANIZATION_CREATION`/`ENABLE_ORGANIZATION_CREATION`, owner/admin session or source-org `org:write` authority, atomic owner/default-team creation, duplicate/name/body admission and response detail. CI39621440652 and image39622275658 pass; anonymous amd64/arm64 use exact revision and shared main/latest index `sha256:3be03ed5aeb67bacd893dc71fee4fd40defcb21d4b6f60ba0597e70bdfa0a287`. Docker-built amd64/arm64 binaries match published bytes exactly at22,974,624 bytes SHA-256 `56ef43a4af1ad4e62a997e6eec0d374dc74813397df6c1e8098c4fe4269f869c` and21,299,360 bytes SHA-256 `f1f0f6781ac3c8cae05f4102ca407c14d00a8d19dd275254e41e89e61d2dc83c`;815 blobs match source. Evidence: `test-results/acceptance-org-create-20261021`.
  - [x] Compatible pending-invitation member detail/cancellation accepted at `59a5aa0a27ff95a27102d501517a737a74880664`: owner/admin-only visibility, pending DTO shape without fabricated user/teams, role-change rejection and link invalidation. CI39624388121 and image39625109768 pass; anonymous amd64/arm64 use exact revision and shared main/latest index `sha256:d17d5f1d3dec7b5b0180f6da7d3b2a38c42c0aca239954cb71a6ba8e80af359c`. Docker-built amd64/arm64 binaries match published bytes exactly at22,982,816 bytes SHA-256 `959659bf970f502ee163d50d293eef4add6acd1f71d2c84f3dd2e20dbfa7c9c2` and21,299,360 bytes SHA-256 `7e3c74cf8a28a5e84e2f3aac12d989f70addeb1b6258ded2edacc3dc20f3998d`;836 blobs match source. Evidence: `test-results/acceptance-invitation-item-20261021`.
  - [x] GlitchTip `set_owner` transfer accepted at `41f7a3345aea43e258d69052a260ab7dea9d3f84`: current-owner authority, target membership locking, sole-owner demotion semantics, active-member response and admin/missing/malformed rejection. CI39627836401 and image39628661807 pass; anonymous amd64/arm64 use exact revision and shared main/latest index `sha256:6a26cfd05b49776dd7144ed993d5a2cc25255072ddd57b3e40f63a9661c94daa`. Docker-built amd64/arm64 binaries match published bytes exactly at22,986,912 bytes SHA-256 `61879eef29afe4e937207dee3a630aab91a77d34f4124a96a21c6d47bb43e7fd` and21,299,360 bytes SHA-256 `cf62df2cea5e779bd32d4d4a96fa1fcc376ca5278f6d6aae7f41bf361c14dae3`;858 blobs match source. Evidence: `test-results/acceptance-set-owner-20261021`.
  - [x] Additive050 verified secondary emails accepted at `a37a987454d9f57258d4eb2c69e8d9c70cf8932e`: global uniqueness, one-use email tokens, resend, verified login/recovery/OIDC/invitation lookup, atomic primary swap, primary deletion protection and public confirmation link. CI39633063993 and image39633675336 pass; anonymous amd64/arm64 use exact revision and shared main/latest index `sha256:9cc418b573cf1e55f2d011b23fdcfb3f81176691bc6b82af1b6fb64b10175eb9`. Docker-built amd64/arm64 binaries match published bytes exactly at23,027,872 bytes SHA-256 `dae2f96f1c53f19bcdc4df9fd568e4c8b40d09df69fd9831983962ad123d6759` and21,364,896 bytes SHA-256 `6bcdf45c6c42972eb079597a3474ac320122629b89b4435a9ca0878c74ad7cce`;885 blobs match source. Evidence: `test-results/acceptance-user-emails-20261021`.
  - [x] Account email-management UI accepted at `0979c84817bd4ec263a84e1c9a7f7766c81cd388`: primary/verified/pending state, add/resend/promote/remove, inline recovery and skip-link/dialog fixes. Chromium1440/390 passes; CI39644156239 and image39644952701 pass with932 exact blobs and shared index `sha256:421391ba44dcd0f3852a8ae19a9b4d6c9ee3847207e3fe3befa98ed0db14dd67`. Exact binaries: amd6423,031,968 bytes `df9eb29f9466ba61da318ee4405c0c1f8f5a0ceee6e34489a57aafec7bb7f4d9`, arm6421,364,896 bytes `cb63dac0a0ad0ca876ecfadab3e1cd612f715263314f6f524a0fafad8dcd2ee7`.
  - [x] Compatible repository registry accepted at `b8d680cbf45a01869a134f45e7c9ccd58cce80f9`: org-scoped manual create/list, owner/admin and org:write controls, duplicate/name/body admission and project-token containment through commit links. CI39648033558 and image39648698492 pass with952 exact blobs and shared index `sha256:f9e136e6ccef6b8245c4ccd08e3f8bdea684d3af5336c9f11a5c640f8e97c16b`. Exact binaries: amd6423,040,160 bytes `f30b7242fd5e254df933a06ed3d30a29272275528f31d4084aaffdbcd46040d2`, arm6421,364,896 bytes `37a1cea515e5535ca833d0cc9289275ba57a80505a76bd0f628d634a8890e8c8`. Automatic provider synchronization remains open.
  - [x] Compatible current-account deletion accepted at `83d02b691557379e5642b2ddcc05d4914c9b24e2`: session-only `me`/exact-ID deletion with CSRF, cross-account404, sole-owner409 and atomic account credential/membership/email/identity cleanup. Organization row locks serialize concurrent owner deletions and retain one owner. CI39651302571 and image39652109500 pass with970 exact blobs and shared index `sha256:449bca6b7cc2b7ec81b5debab827bc2e710836261801571c531250eecbebda33`. Exact binaries: amd6423,044,256 bytes `9fcfa2419358a7a6d1f3d8805e876671cdddefc9e934972bb329c8b5463d2120`, arm6421,364,896 bytes `458cf568abbd4b16402d13fe2a4877de2f533baea0cda4dd73b610d5c3725740`. Evidence: `test-results/acceptance-user-delete-20261021`.
  - [x] Account-page deletion accepted at `be7022d524f3f2492f68645715d9f7d5b99dc48f`: typed-email confirmation, cancel/session retention, sole-owner409 recovery and successful signed-out state. Reusable Chromium owner/member flow passes at1440/390 on both local and anonymous public images, with the existing password regression and PostgreSQL cleanup checks green. CI39655431789 and image39656195302 pass with989 exact blobs and shared index `sha256:c5b8b7801a6fc90fc1ed8a85f89575772966fe570e6697d03855215ebafa40cb`. Exact binaries: amd6423,048,352 bytes `e4e6796a5152860b1ae9174b4ceb84935861e4c3afcb03ecf9db6a95c11e9758`, arm6421,364,896 bytes `c8701039ceb5eca7aa296681b4f9c74a4fe7caab16b354144d1310f4ff3932b8`. Evidence: `test-results/acceptance-account-delete-ui-20261021`.
  - [x] Official `sentry-wizard` handoff accepted at `9740b434db868c2641e541e983817339834ccfa5`: signed ten-minute stateless hash, authenticated organization selection, encrypted transfer, current project/full-DSN payload, one-day `project:releases` credential and hash consumption. Actual `@sentry/wizard8.0.1` completed login/project selection, created `.sentryclirc` and source-map script, exited0 and authenticated. Migration051 preserves existing credentials and reopens; owner/admin and bearer-source boundaries pass. CI39661318427 and image39662056750 pass with1031 exact blobs and shared index `sha256:0cbb29c66c630b5aed4acb671dc9e77a61c1a26f3fd876d66314e3c33ff7b43c`. Exact binaries: amd6423,081,120 bytes `cda91a4c863aa546c5f0b627a776b5a91f9482c51300a10b742c37c2a2984532`, arm6421,364,896 bytes `e53678931e1a715bcef2a8843f5725008ee09596008cf34824ccafd3afbe2ebc`. Evidence: `test-results/acceptance-wizard-20261021`.
  - [x] Compatible performance span-groups and N+1 patterns accepted at `0ce94b0522b3f41f3f6fe0403ef803263e376f43`: session/event-scope reads, repeated authorized project filters, paired time windows, bounded sorting and exact public fields. N+1 aggregates repeated per-trace children before transaction/op/description grouping. Real PostgreSQL and the anonymous public image pass multi-project aggregation, Bearer containment and strict admission. CI39665461859 and image39666199710 pass with1066 exact blobs and shared index `sha256:0ce99b3721a6d14a19b9bed6c1aeb4a2a12ac7c7a47b25590718a39a0485de13`. Exact binaries: amd6423,109,792 bytes `c3e93f6e3833ddc3bbe9577cde40129f9023766c982380ee9b3219d616a783e2`, arm6421,364,896 bytes `6313a09748fc920dab490482046c667cdc6e776abbe39939010c59cf1c427f04`. Evidence: `test-results/acceptance-compat-performance-20261021`.
  - [x] Complete compatible transaction-group family accepted at `4552fb83d4382c3ff1840919f760ee7b54bef664`: additive052 stable project/transaction/op IDs, list/detail/trend/per-group spans, retained-history metrics, public fields, cursor Links, repeated project containment and strict admission. CI39670433437 and image39671227816 pass with1092 exact blobs and shared index `sha256:68e520313ae0a9f06a90b08a24a65b961f8c112ecc8727aa984461928ebdb2d5`. Exact binaries: amd6423,154,848 bytes `6c726be3c86df5233b9465b7a5b4ea7fd6816289f406828f2258f20ad0e40dd8`, arm6421,495,968 bytes `16053a714f9a0d0f3900fd7f9884c62dcafe0bdfc04dd98c7a98bb21f3f21ae7`. Evidence: `test-results/acceptance-compat-transaction-groups-20261021`.
  - [x] Cookie-free open authentication accepted at `74daf30d1357e1e270a1a07caf2ceb0f6b9a18bb` with explicit user authorization: `GOODSENTRY_AUTH=disabled` default, idempotent implicit owner/org/default team, cookie-free `/api/v1/me`, stable CSRF and same-origin mutation enforcement. Empty-volume Compose, restart, full open/required browser workflows and public image pass. CI39676341485 and image39677173073 pass with1116 exact blobs and shared index `sha256:706915732d975ba9eaa99ea81a7d1398d43faedd2ed3e4b2cb8780dd0186d375`. Exact binaries: amd6423,171,232 bytes `2233fbc84a9639fa7c72164ee1c6d8b00eb2097227456e56badc39f613cd427a`, arm6421,495,968 bytes `2b83f88322fbb8437a4bcc8c2dcd92d1cccafaa5564486641cb1fd4e086cd171`. Evidence: `test-results/acceptance-open-auth-20261021`.
  - [x] Keep open-mode invitation acceptance on the implicit identity. PostgreSQL/race HTTP acceptance rejects invitations for other emails without creating accounts, lets the implicit owner accept an invitation to its configured email with the existing owner password, and confirms cookie-free `/api/v1/me` retains that owner and its new organization. Required-mode invitation acceptance remains covered by `TestOrganizationInvitationHTTPContract`. Evidence: `go test -race -tags=integration -run '^(TestOpenAuthenticationUsesImplicitOwnerAndKeepsCSRF|TestOpenAuthenticationInvitationsStayOnImplicitOwner|TestOrganizationInvitationHTTPContract)$' ./internal/server` against disposable PostgreSQL on 2026-10-23. The current full untagged suite `go test ./...` and integration-tag `go vet -tags=integration ./...` also pass.
  - [x] Reject native login and setup submission while authentication is disabled. The real HTTP owner observes `/api/v1/setup` required false, POST setup409 and valid-owner POST login409; required login and two-factor enrollment still pass. Before the fix, the open-mode test reproduced login200. PostgreSQL/race proof: `go test -race -tags=integration -run '^(TestOpenAuthenticationUsesImplicitOwnerAndKeepsCSRF|TestOpenAuthenticationInvitationsStayOnImplicitOwner|TestTwoFactorEnrollmentAndLoginHTTP|TestOrganizationInvitationHTTPContract)$' ./internal/server`; full `go test ./...` and `go vet -tags=integration ./...` pass on 2026-10-23.
  - [x] Landing feature showcase published at `bad461e919d356d96faa3d1e5c963816bf3d93e1`: seven actual-product panels for error tracking, source context, performance, logs, uptime/alerts, people/access and account security. Pages39638909802 and CI39638909782 pass; live HTML contains seven cards and all seven screenshots return HTTP200. Chromium140 captures pass at1440,390 and full-page desktop without overflow. Evidence: `test-results/acceptance-landing-features-20261021`.
  - [x] Implement compatible event attachment list/retrieve/download metadata contracts, including documented numeric attachment identities/checksums, filters and project-scoped bearer behavior. Accepted through the compatible attachment and public5dd302f evidence below; the broader management API remains incomplete. Official public list/retrieve docs are captured under ignored file-retention-20261017/attachment-api-*.html/json.
  - [x] Add read-only compatible API root, current user, organization/project discovery, public settings, per-organization login settings, instance license and support link.
    - Real HTTP covers trailing-slash routes, anonymous rejection, session/Bearer admission, current-membership and token project scope, unknown users, strict/repeated query rejection, DTOs and prefixed pagination links. Real PostgreSQL covers owner/search filtering, ordering,25+1 pagination, foreign membership isolation and the user creation instant. Full non-integration/integration race, vet and build pass. Evidence: goal-compat-discovery/{all-race.log,full-integration-race.log,vet.log,source-hashes.txt}.
  - [x] Add compatible organization rename/delete with owner/admin and owner-only boundaries, immutable slug and cascade behavior.
    - Real PostgreSQL covers rename preservation, invalid names, owner/admin/member limits and deletion cascade. Real HTTP covers PUT/DELETE roles, org:read/org:write Bearer, malformed bodies, admin delete denial, owner deletion and subsequent404. Evidence: goal-compat-mutations/{organization-store.log,organization-http.log,full-suite.log}.
  - [x] Add the native Organization view with owner/admin rename and owner-only delete confirmation.
    - Responsive Chromium covers real id/slug/role, rename/persistence, cancel/confirm deletion and the no-organization state at320/768/1440 with zero page/console errors; one navigation-aborted request is recorded explicitly. Evidence: goal-browser-organization/{report.json,PNGs}.
  - [x] Complete broader management API acceptance.
    - Exact-source CI run [40023245722](https://github.com/jerolan/good-sentry/actions/runs/40023245722) passes on `55530603a1cd4d37ab50017a20775fad1ec9a1fa`, including the PostgreSQL integration/race suite and application image build. Route-specific HTTP/store coverage exercises organization and project mutations, SDK-key CRUD, memberships and teams, session/Bearer scope rules, cross-organization/project isolation, strict inputs, and pagination. The OpenAPI test verifies the generated document follows registered routes; Sentry client route compatibility is exercised against GoodSentry's local HTTP fixture without a live third-party server.
  - [x] Add additive039 team creation instants and project-key UUID/label metadata, compatible organization detail, and project-key list/create/update/delete with DSN output.
    - Real PostgreSQL covers039 preservation/reopen, deterministic UUID/team-clock backfill, relation/access DTOs, key CRUD and active/inactive filters. Real HTTP covers owner/admin/member controls, project:read/project:write Bearer scopes, strict inputs/queries, DSN shape, pagination links, cross-organization isolation and deletion. Evidence: goal-compat-management/{store-migration.log,store-crud.log,http-management.log,full-suite.log}.
  - [x] Expose Sentry `isActive` on project-key DTOs and support activation through PUT.
    - Focused HTTP/store integration checks prove omitted `isActive` preserves state, deactivated keys reject envelopes, and reactivation restores ingestion.
  - [x] Add responsive Projects SDK-key management for copyable DSNs, label/rate-limit editing, activation, deactivation and confirmed deletion.
    - Chromium passes at320/768/1440 with no page or console errors; real HTTP-backed browser flow verifies key create/edit/status/delete, DSN path/key shape and forward/backward pagination. Evidence: `tests/compat/project_keys_browser.mjs`.
  - [x] Define the SDK-key rotation workflow with overlapping keys and a reversible rollback window.
    - `docs/compatibility.md` documents create, DSN cutover, ingestion confirmation, old-key deactivation/reactivation and final deletion. Existing key API/browser acceptance covers these operations; Sentry documents the matching create, `isActive` update and delete endpoints.
  - [x] Add additive041 per-key fixed-window usage, accept positive rateLimit configuration and enforce it on Sentry envelope ingestion.
    - Real PostgreSQL covers atomic charge/denial, unlimited keys, short-window reset,041 preservation/reopen and bounded14-day cleanup. Real HTTP covers key rateLimit create/validation, envelope200-to429 with Retry-After/X-Sentry-Rate-Limits, OTLP protobuf429/google.rpc.Status code8 and successful empty exports. Maximum window is604800 seconds; count is capped at1000000000. Evidence: goal-compat-rate-limit/* and otlp-rate.log.
  - [x] Add compatible organization-member, team, team-member, team-project and project-team read routes with pending-invitation privacy and token organization/project isolation.
    - Real PostgreSQL covers current/pending people visibility, team member counts, assigned projects and member/token boundaries. Real HTTP covers trailing-slash routes, session member reads, owner data, org/project scopes, missing teams, strict pagination and prefixed links. Evidence: goal-compat-people/{store-people.log,http-people.log,full-suite.log}.
  - [x] Add additive040 `org:write`, compatible member invitations and team create/rename/delete with session owner/admin and Bearer scope controls.
    - Real PostgreSQL covers040 token preservation/reopen, org:write issuance/authentication, team rename/project preservation and default-team protection. Real HTTP covers invitations, duplicate/malformed/unsupported request rejection, pending invitation privacy, team create/rename/delete and org:read versus org:write. Evidence: goal-compat-mutations/*.
- [x] Verify Sentry SDK compatibility and Grafana datasource integration.
  - [x] Grafana13.0.2 with the official `grafana-sentry-datasource` passes a real health check (`16 projects found`) and a real ten-row issue query, including executed URL `api/0/organizations/{org}/issues/`. Evidence: `test-results/acceptance-integrations-current-20261020/{datasource.json,grafana-query.json}`.
  - [x] Add compatible `GET /api/0/organizations/{org}/stats_v2/` and verify a real Grafana stats panel: Grafana13.0.2 with `grafana-sentry-datasource`2.2.8 renders hourly error counts from three stored events, and the published image serves the same frame. Category, field, interval, repeated project, `-1` and window validation, outcome/reason/groupBy rejection and hidden-project404 pass on both builds. Evidence: `test-results/acceptance-stats-v2-20261021/*`.
  - [x] Add and run a standalone demo app using the official Sentry Go SDK; its captured exception is delivered to a GoodSentry project using the project's DSN.
    - 2026-10-23: `sentry-go v0.50.0` sent event `b940592796c945448b11d148fea31136`; `POST /capture-error` returned202 and the GoodSentry issue API returned the same event ID, exception value and issue title. The temporary app and PostgreSQL container had no persistent volume and were removed.
    - 2026-10-23 published-image run: source `672a0f34a56caceafdfbafb2b9b7350f1adfd70a`, CI [40059516157](https://github.com/jerolan/good-sentry/actions/runs/40059516157), image [40059866543](https://github.com/jerolan/good-sentry/actions/runs/40059866543), digest `sha256:978c7fae1f2fd21192b81957075bfb827543f6539456515e4e514b3a651ae370`. The official demo page returned200 and `/capture-error` returned202; event `ab3b999b50014cc1be9661d872cdec0a` appeared in the issue API with its exception value intact. After an app-only restart, that event remained visible and the same SDK app delivered event `8b91ab18a55c449a8a202b45e3ba5485` into the same issue. No external service was contacted; task-created app, Compose volumes/network, image and Go caches were removed.
    - 2026-10-24: `tests/compat/sdk_smoke.go` now reads `/api/v1/me` first, using password login only after a required-auth `401`. Before the change, the pinned Sentry Go smoke failed on the default-open image with login `409`. Fresh non-root amd64 image `sha256:063c3c61847c436c8901bd5fe532617e53bc6c4d15a13bc86a6f3b3a87e411fb` with an empty PostgreSQL database and no auth override passed actual `sentry-go v0.50.0` exception, transaction, trace and typed-log retrieval. A separate required-auth database passed both first-owner setup and a second-run password login with the same SDK workflows. Event IDs: open `1db67e4948cc4af994c80eed75df95a4`, required setup `575f066c44bf4761952dc6178fe0e84b`, required login `e0b3d65e00c34f3cb95451d49ef51d82`. All containers, image and task-local caches are removed after verification.
- [x] Deliver optional authenticated MCP with tool-level scope enforcement. Accepted at public8160b3 after all17 tools, native/OAuth grants, PKCE/refresh/revocation, default-off behavior, version42→44 upgrade and exact public binary checks.
- [ ] Verify optional social OAuth/OIDC against external identity providers.
  - Local security follow-up (2026-10-24): OIDC callback responses now set `no-store` and `no-referrer` before state validation. The integration test failed on the prior `same-origin` response, then passed against disposable PostgreSQL after the fix. External-provider receipts remain open.
  - [x] Generic OpenID Connect sign-in accepted at public `645f15ee63ff1b18cb6ed6f19b138509ea5271a2`: all578 published blobs match source, CI39562509301 and image39563179201 pass, and anonymous amd64/arm64 images use `65532:65532` with the exact revision. The public amd64 binary matches local exactly:22,712,480 bytes, SHA-256 `9e6b203e9279253e6e48047f97a8468b6c42729755b8a0d60fdb38c8f0e39557`. Local RSA-signed discovery, S256 PKCE, state/nonce, verified-email linking, auto-create policy, tampered callback rejection and real sessions pass; the clean image completes the browser callback and Chromium verifies a session at320/1440 without overflow. Evidence: `test-results/acceptance-oidc-20261020/{integration-race.log,targeted-oidc-race.log,local-gates.json,image-local.json,image-oidc-flow.json,browser.json,public-source.json,public-ci.json,public-ci.log,public-image.json,public-image-ci.log,public-manifest.json,registry.json,public-pull.log,public-binary.json}`. Vendor-specific deployments and external identity-provider receipts remain open.
  - [x] Deliver organization-managed OpenID Connect applications with additive055: owner/admin or `org:write` CRUD at `/api/0/organizations/{org}/social-apps/`, sealed client secrets, https-only issuers, stable provider identities, per-application PKCE callbacks, a public `/login/{org}` page and `/api/v1/organizations/{org}/sso` discovery, and sign-in that links or creates the verified subject as `social:{providerID}` and ensures member membership. Real PostgreSQL and HTTP cover CRUD containment, validation, cascade cleanup, bearer scopes, the full synthetic-IdP sign-in and cross-application flow rejection; Chromium covers creation, editing, login-page discovery, deletion and 390/1440 layout. Vendor-specific providers and external identity-provider receipts remain open.
  - [x] Add Microsoft, GitHub, GitLab, Gitea and DigitalOcean organization social sign-in.
    - Contract: additive070/071/072 permit the hosted/self-hosted OAuth2 provider values without rewriting rows; Microsoft keeps `openid_connect`, defaults to Entra ID v2.0 and requires a tenant plus issuer-backed email/preferred username. GitHub, GitLab, Gitea and DigitalOcean use PKCE OAuth2, sealed client secrets, bounded requests and the existing organization membership/session path; DigitalOcean requires an active verified account and uses its UUID as subject.
    - Acceptance: synthetic TLS providers for all presets complete sign-in and identity linking; unverified GitHub/DigitalOcean, unconfirmed/inactive GitLab and inactive Gitea profiles create no identity or session; Microsoft accepts its tenant-bound claims while generic OIDC still rejects unverified email; API creation/update and provider immutability remain strict; migration/reopen, full PostgreSQL/race, vet, build and UI syntax pass.
    - Local evidence (2026-10-26): Microsoft tenant-bound OIDC plus GitHub, GitLab, Gitea and DigitalOcean synthetic TLS providers complete sign-in, profile handling and organization membership. Microsoft accepts its issuer-backed email/preferred username without `email_verified`, while generic OIDC still rejects unverified claims; unverified/unconfirmed/inactive OAuth2 profiles create no session or identity. DigitalOcean uses its UUID subject and requires verified active status. API admission covers hosted defaults, required Gitea URLs, self-hosted HTTPS URLs and provider immutability. Focused social/OIDC tests pass in3.424s and migration070/071/072 preservation/reopen passes in1.834s. Full `go test ./...`, `go test -race -tags=integration ./...` (server199.450s, store371.904s), integration-tag vet, trim-path build and UI syntax pass.
    - Public verification (2026-10-26): complete at source `556e3b857e68c5514591852b85711e9c3edafbf0`. [CI 40268935017](https://github.com/jerolan/good-sentry/actions/runs/40268935017), [Native SDK demo 40268935030](https://github.com/jerolan/good-sentry/actions/runs/40268935030) and [image publication 40269547906](https://github.com/jerolan/good-sentry/actions/runs/40269547906) pass. The linux/amd64 and linux/arm64 OCI index is `sha256:5be8e0377ef3eed9916b0b0651240708f98f4a171ed32773d3b5d79082fc4244`.
  - [x] Add instance-admin and active-account storage in migration064, first-owner bootstrap, and an idempotent CLI grant for existing installations. Preserve existing account state through migration/reopen and verify bootstrap only promotes users on an empty database. The real PostgreSQL migration, bootstrap, CLI and reopen tests pass.
  - [x] Enforce inactive-account rejection across password login, sessions, API tokens, OIDC and MCP credential flows. Suspension revokes extant sessions/tokens and reactivation never restores them; covered by the full race-enabled integration suite.
  - [x] Add bounded paginated/searchable global user administration API. Require current instance-admin state in storage, use strict PATCH input, and serialize updates so the last active administrator cannot be disabled or demoted. The HTTP/storage and account-concurrency tests pass, including the regression that fails when duplicate detection takes a user row lock.
  - [x] Add the user-management section to the optional Administration UI and document bootstrap, CLI grant, `ENABLE_ADMIN` and `GOODSENTRY_AUTH=required` before external exposure. Open-auth CSRF and feature-off HTTP checks pass.
  - [x] Final acceptance: responsive Chromium verifies filtering and mutations; exact-source CI [40111843491](https://github.com/jerolan/good-sentry/actions/runs/40111843491) and amd64/arm64 image publication [40112327603](https://github.com/jerolan/good-sentry/actions/runs/40112327603) pass at `43b2d67dde26eaea57cdf449b962db349a154c0f`. The published OCI index is `sha256:d5fac345372ee09d12581e650aa1c8e97704d91f9be841feb6827237ef1dd7e4`.
    - Chromium verifies email/name filters, promotion, demotion, suspension and reactivation at320/768/1440px with no page errors, console errors or horizontal overflow. The inline SVG favicon removes the browser's missing `/favicon.ico` request. Evidence: `test-results/admin-user-browser-20261024/report.json` and `admin-users-{320,768,1440}.png`.
- [x] Deliver optional local/S3 Parquet cold archival with verified historical/span queries.
  - [x] Add validated opt-in archive settings and additive manifest migration059; preserve the existing total transaction/span retention deadline and default the archive threshold to min(30 days, half the configured lifetime, with a one-day minimum).
    - 2026-10-22: Config owner covers disabled/local defaults, explicit archive→GoodSentry legacy→GlitchTip alias precedence, derived and explicit thresholds, S3 endpoint/bucket/region and malformed settings. Enabling the incomplete path still fails the startup availability gate. `go test ./internal/config` and exact trim-path app build pass. The real PostgreSQL migration owner reopens version59, checks the checksum, compares every pre-059 table/receipt snapshot, and proves project deletion preserves a null-project manifest through another reopen; focused integration test passes against a disposable PostgreSQL18 database. The local PostgreSQL container is stopped. The Parquet data path remains incomplete and disabled.
  - [x] Add a bounded Parquet span codec plus atomic local and S3-compatible object storage with deterministic retry keys.
    - 2026-10-22: Pinned parquet-go v0.32.0 preserves every spans column, microsecond timestamps and raw JSON/attribute bytes. The 250-row and 64-MiB limits, deterministic row ordering and SHA-256 cohort key, malformed/corrupt Parquet rejection and lossless round trips pass. Local writes are fsynced and atomically hard-linked, reject different bytes on an existing key, and pass concurrent same-key, read/delete, path and cancellation cases. AWS SDK v2 uses default credentials, seekable known-length PutObject requests, custom-endpoint path-style addressing, conditional immutable puts and identical-content retry recovery; a local S3-compatible HTTP server passes Put/Get/Delete and collision checks. `go test -race ./internal/spanarchive`, `go vet ./internal/spanarchive`, full `go test ./... -count=1`, `go build ./...`, `go vet ./...` and `git diff --check` pass. Archival remains disabled until the PostgreSQL lifecycle/query paths are complete.
  - [x] Archive spans through the existing bounded transaction-retention lifecycle and expire cold objects at the original server-receipt deadline.
    - 2026-10-22: The archive-aware worker drains one bounded project cohort per pass under the existing PostgreSQL advisory lock. It uploads before inserting the manifest and deleting hot rows in the same transaction; retry keys keep a successful upload reusable after SQL rollback. Cold objects expire against the original `created_at` cutoff, and deleted-project manifests are cleaned as orphans. Real PostgreSQL18 tests cover same-deadline mixed-age rows, preserved independent errors/logs, upload and manifest failures with retry, locked rows, cancellation, concurrent retainers, expiry and project deletion; the public worker starts and stops with an actual readable Parquet object. Focused store/job integration race tests, full `go test ./... -count=1`, `go build ./...`, `go vet ./...` and `git diff --check` pass. Historical reads and the application availability gate remain pending.
  - [x] Add additive migration060 with an indexed row catalog, a manifest-complete flag and durable ready/deleting state; existing manifests start incomplete.
    - 2026-10-22: Focused PostgreSQL18 upgrade tests verify version59 manifest data survives, old objects reopen as ready/incomplete, duplicate retained identities are rejected, project deletion cascades catalog metadata while preserving its orphan object manifest, and the060 checksum is stable. `go test -tags=integration ./internal/store -run 'TestSpanArchive(CatalogMigrationKeepsOldObjectsIncomplete|ManifestMigrationPreservesRowsAndReopens)$' -count=1` passes.
  - [x] Implement bounded, serialized, restartable catalog backfill for existing Parquet manifests; missing, corrupt, mismatched or inaccessible objects leave the manifest incomplete and return an error.
    - 2026-10-22: A real PostgreSQL18/Parquet integration owner verifies recovered row metadata, manifest completion only after a full object, repeat-run idempotence, and fail-closed missing-object behavior. Focused command: `go test -tags=integration ./internal/store -run TestBackfillSpanArchiveIndexesExistingObjectsBeforeCompletion -count=1`.
  - [x] Wire backfill into application startup before reads or ingest; reject disabled startup with existing manifests and backend changes that cannot read existing objects. A real PostgreSQL integration test proves disabled startup fails with a manifest, enabled startup backfills before the first span query, and a backend mismatch leaves the manifest incomplete; `go test -tags=integration ./cmd/goodsentry -run '^TestConfigureSpanArchiveRejectsDisabledManifestAndBackfillsBeforeRead$' -count=1` and the focused store backfill test pass.
  - [x] Publish complete row-catalog identities atomically with each new archive cohort, and check retained catalog identities while span ingest holds the shared ordered identity advisory locks.
    - 2026-10-22: PostgreSQL archive retention tests now verify every newly archived row is indexed before the manifest is complete; an SDK retry after archive returns zero new rows and leaves exactly one cold identity with no hot duplicate. Archive and ingest acquire the same sorted project/trace/span advisory keys before locking or inserting hot rows. `go test -tags=integration ./internal/store -run 'TestSpanArchive|TestTransactionRetention' -count=1` passes. A forced in-flight archive/ingest interleaving and cold expiry cleanup remain pending.
  - [x] Expire individual catalog identities at the total receipt-age cutoff and delete fully expired/orphaned objects through a durable, idempotent deleting state.
    - 2026-10-22: PostgreSQL18 verifies mixed-age objects keep their live row and bytes after only the older catalog identity expires; failed object deletion leaves a retryable deleting manifest; a forced manifest-delete failure after successful external deletion also leaves durable state, and retry safely removes the missing object plus manifest/catalog rows. `go test -tags=integration ./internal/store -run 'TestSpanArchive|TestTransactionRetention' -count=1` passes.
  - [x] Verify forced concurrent ingest/archive identity interleavings and stale-manifest read retries under concurrent expiry. PostgreSQL18 gates an archive after it locks/uploads the row, starts a separate archive-enabled ingest against that exact identity, verifies it waits behind the shared identity lock without reaching the hot insert, and confirms the single cold catalog identity remains. A second real RR transaction holds its snapshot while expiry marks a selected manifest `deleting`; the stale `FOR SHARE` conflicts with SQLSTATE40001 and the full read retries against a fresh snapshot. Both focused cases pass five consecutive runs.
  - [x] Make every current span read query hot PostgreSQL rows and matching cold objects through one transaction-scoped relation.
    - Resolve and quote the hot spans table selected by the connection's `search_path`; use a primary-pinned Repeatable Read/Read Write transaction and a full-shape temporary relation that is dropped before the connection returns. Load selected objects one at a time, expand combined hot+cold trace seeds for unbounded root/child joins, filter rows by the existing total receipt-age lifetime, and retry the complete read up to three times after SQLSTATE 40001 without returning partial results.
    - [x] Route direct span lists through the archive-aware transaction and preserve the existing authorization, project/trace filters and raw payloads. A PostgreSQL18 integration test reads one hot and one archived span through `Store.Spans`, then verifies a hot seed expands its trace to an archived row.
    - [x] Route transaction detail and trace graph/metadata/raw reads through the same relation and repeatable-read retry boundary. Focused PostgreSQL18 archive, detail, graph, and raw-node integration tests pass. The full store integration run was attempted but PostgreSQL exhausted the host filesystem (`SQLSTATE 53100`), entered automatic recovery, and caused later tests to fail; no code assertion failed before that resource error.
    - [x] Route native transaction groups and Stats V2 through archive-aware time/project selectors. The PostgreSQL18 archive read test proves both endpoints include an archived segment; rerun passes with `go test -vet=off -tags=integration ./internal/store -run TestSpanArchiveReadCombinesHotAndColdRowsAndExpandsSeedTraces -count=1`.
    - [x] Route transaction span groups through scoped root selection and combined hot/cold trace-seed expansion. PostgreSQL18 tests put the root and child in separate cold objects and verify exact child grouping, plus the existing root-window and organization/project regression cases.
    - [x] Route scoped transaction/span groups, transaction trends and both N+1 readers through archive selectors; N+1 readers seed from selected hot/cold children, then expand roots across the trace. The PostgreSQL18 archive test verifies archived aggregate results and cold root lookup. It passes with `go test -vet=off -tags=integration ./internal/store -run 'TestSpanArchiveReadCombinesHotAndColdRowsAndExpandsSeedTraces|TestTransactionSpanGroupsCombineProjectedLegacyAndTypedStandalone|TestTransactionSpanGroupsRootWindowsReferenceAndOrganizationScope' -count=1`.
    - [x] Route compatible transaction groups and their detail wrapper through archive-aware selectors. PostgreSQL18 verifies the cold segment group and its persisted project/name/op identity. Stable group-ID assignment runs only after the retried read transaction returns.
    - [x] Route project/organization environment discovery and visibility checks, and release environment filtering through the relation. Preserve scope, filtering, ordering and pagination, including updates to visibility for cold-only environments. PostgreSQL18 verifies cold-only project/org discovery, hiding a cold-only environment, and release selection by a cold-only environment while the existing visibility/grant/release tests pass.
  - [x] Publish Compose/README/configuration/retention and backup/restore instructions. Compose defaults archival off, persists local Parquet under `/data`, forwards standard S3 credentials and region, and documents backend continuity, startup backfill, paired PostgreSQL/uploads backups and the unchanged `GOODSENTRY_TRANSACTION_RETENTION_DAYS` deadline. `docker compose -f compose.yml config --quiet`, `go test -p=1 ./... -count=1`, `go build -p=1 ./...`, `go vet -p=1 ./...` and focused PostgreSQL startup/backfill tests pass.
  - [x] Verify a new local archive volume, the S3-compatible endpoint fixture, app-only restart, same-deadline expiry and isolated backup recovery. The published image started with fresh Compose volumes and archive enabled; `/api/v1/setup` returned `required:false`, cookie-free `/api/v1/me` and the dashboard returned 200, and the owner/workspace IDs remained stable after restarting only the app. `/data/archive` was created as UID 65532. A paired PostgreSQL dump and uploads tar restored into a second empty Compose project; the restored archive marker, owner/workspace and open dashboard were readable. Store integration verifies real Parquet objects and manifest backfill/query behavior; the S3-compatible HTTP fixture passes Put/Get/Delete and collision checks. PostgreSQL expiry tests verify cold rows use the same receipt-age cutoff.
  - [x] Verify exact-source CI and publish the public amd64/arm64 image through `gh`. Commit `ff5c27e` passed CI run `39910278392` (format, vet, unit and PostgreSQL integration tests with race detection, image build); image workflow `39910903910` passed and `ghcr.io/jerolan/good-sentry:main` resolves to OCI index digest `sha256:197aa94bea2774be965224217550108b5518a6a8f423a7775b66ce0eed2938af` with linux/amd64 and linux/arm64 manifests. The paired docs commit `a0942b6` passed CI `39912042968` and image workflow `39912701287`; the public index is `sha256:62b10daf9595d43a939ae2837f8f493663989abad5753b0a47ded2bac8ac9123`.
- [x] Verify true/false feature flag and GlitchTip alias behavior, including PostgreSQL-only mode.
  - 2026-10-20: Config tests cover explicit GoodSentry precedence, false aliases, legacy retention aliases, unsupported `SERVER_ROLE`, empty Valkey defaults and PostgreSQL-only operation. At that checkpoint runtime availability rejected enabled MCP, DuckDB, admin, OpenAPI and nonempty Valkey instead of silently claiming support. MCP later passed its full OAuth/tool/image/public gate at8160b3; the archive feature now accepts the DuckDB aliases, and the optional OpenAPI/read-only admin surfaces are implemented. Valkey remains unavailable. Evidence: goal-config-flags/config-race.log and the later archive/OpenAPI/admin evidence above.
- [x] Run pinned real SDK matrix for Go, Python, Node and browser, then publish actual results. Public8160b3 passes Go0.50.0, Python2.72.0, Node11.7.0 and Browser11.7.0 errors/transactions/typed logs; restart preserves144 rows/47 tables.
- [x] Run browser onboarding, DSN setup, issue triage and responsive/accessibility checks. Public8160b3 Chromium passes first-owner onboarding, project/DSN/copy, accessible control names, real event/source/breadcrumbs/tags, search/environment reload, resolve/comments reload, transaction/trace/logs, source maps and320/768/1440 overflow checks; restart preserves100 rows.
  - Decision request outside the uptime-retention increment: the existing monitor heading reserves the tablet-width action row beside long names, causing excessive title wrapping at768px. Both before-768.png and final-empty-768.png under ignored uptime-retention-20261017/browser show the same layout; it is not introduced by retention and neither page overflows horizontally. Revisit header composition in the broader responsive UI pass, without mixing that redesign into child-history expiry.
- [x] Run full tests, race detection, vet, fuzz/parser checks, PostgreSQL concurrency/recovery and container acceptance.
  - [x] Build and run the current single-image candidate with a disposable PostgreSQL/volume, real browser workflows, official Node SDK capture and app-only restart.
    - Image `goodsentry:goal-final` is static Linux amd64, 32,110,682 bytes, UID65532:65532 with `/goodsentry` entrypoint and exact served app.js/app.css. Provider Chromium passes directly on the image; organization Chromium passes on the same source-asset bytes served by it. Official `@sentry/node 11.7.0` stores exception, log and transaction; an app-only restart leaves the issue/event snapshot byte-identical. Evidence: goal-image-final/{image-config.json,image-identity.txt,asset-hashes.txt,node-sdk.log,before/after-restart.json,providers/,report.json}.
  - [ ] Obtain authorized real delivery receipts from provider destinations.
    - Public8160b3 passes exact-source CI/Pages/images, isolated SMTP recovery/invitations, account/people/SSL Chromium, version42→44 backup recovery and a 24.4 MiB measured workload. Provider encoders and loopback receipts are verified; external Slack/Discord/Teams/Google Chat/Feishu/ntfy/Zulip receipts require outside credentials.
- [x] Measure application RSS under the documented repeatable workload. Concurrent official Go/Python/Node/Browser SDK traffic produced24.367 MiB maximum application VmRSS (25,550,848 bytes) with PostgreSQL excluded, well inside the256–512 MB target.
- [x] Publish quickstart, upgrade/backup/restore procedure, SDK and endpoint matrices in README.md and linked guides.
- [ ] Audit every specification requirement against current artifact/runtime evidence; only then complete the goal.
  - 2026-10-24 audit slice: corrected the team-delete mirror contract, described attachment bytes and generated storage IDs instead of paths, and reconciled older issue-scope text with implemented deletion-only `event:admin`. OIDC callback containment is recorded in `97eaf12`. The full requirement audit remains open.
  - [x] Reconcile the frictionless-start requirement against current configuration, HTTP tests, Compose and deployed-image evidence. `GOODSENTRY_AUTH` defaults to `disabled`; the configured implicit owner values are passed through Compose and documented with `required` as the pre-exposure mode. `TestOpenAuthenticationUsesImplicitOwner` covers setup, cookie-free `/me`, CSRF/origin-gated mutation, restart identity and required-mode rejection; invitation and MCP OAuth tests cover their open-mode owner behavior. The 2026-10-25 fresh-volume Compose receipt below verifies dashboard access, one owner/org/team, stable restart state and a separate `required` setup/login flow. This closes only the user's open-auth startup requirement; native Crashpad, external provider/OIDC receipts and the full SPEC audit remain open.
  - [x] Reconcile the public README, compatibility guide, release guide and landing page with accepted native-debug and cold-span-archive evidence. Commit `3fb588b` passed exact-source CI `39914660976` (formatting, vet, PostgreSQL integration/race suite and image build), and Pages deployment `39914661053` succeeded. The complete requirement-by-requirement audit remains open.
  - [x] Reconcile the management API status in `SPEC.md` with route, OpenAPI and optional administration acceptance. Exact-source CI [40023245722](https://github.com/jerolan/good-sentry/actions/runs/40023245722) passed on `55530603a1cd4d37ab50017a20775fad1ec9a1fa`; route coverage uses GoodSentry's local HTTP/PostgreSQL contracts.
  - [x] Reconcile MCP, OIDC and administration status in `SPEC.md` with accepted evidence. MCP's 17 tools, OAuth, default-off behavior, version42→44 upgrade and published image are accepted at public `8160b3` (CI [39538713048](https://github.com/jerolan/good-sentry/actions/runs/39538713048), image [39539169221](https://github.com/jerolan/good-sentry/actions/runs/39539169221)). Generic OIDC and organization-managed apps pass synthetic-IdP/browser acceptance. Global user administration passes responsive browser acceptance and exact-source CI/image at `43b2d67dde26eaea57cdf449b962db349a154c0f` (CI [40111843491](https://github.com/jerolan/good-sentry/actions/runs/40111843491), image [40112327603](https://github.com/jerolan/good-sentry/actions/runs/40112327603)). Vendor-specific providers and external identity-provider receipts remain open.
  - [x] Recheck the current local Go baseline during the final audit. At `c16e09d5d7bbc920887ef66077d47f4baecc363c` on 2026-10-24, `GOCACHE=/tmp/goodsentry-gocache go test -race ./...`, `GOCACHE=/tmp/goodsentry-gocache go vet -tags=integration ./...` and `GOCACHE=/tmp/goodsentry-gocache go build -trimpath -o /tmp/goodsentry-build ./cmd/goodsentry` all pass. The first sandboxed race run could not bind loopback for SMTP/HTTP/S3 fixtures; the same suite passed with loopback enabled. Exact-source CI [40114917798](https://github.com/jerolan/good-sentry/actions/runs/40114917798) passed on `3221bce3e4d9a44e0fe6c1d351f3dcafb23acc4f`, including formatting, vet, PostgreSQL integration tests with race detection and image build. Image workflow [40115414205](https://github.com/jerolan/good-sentry/actions/runs/40115414205) published linux/amd64 and linux/arm64; immutable OCI index digest `sha256:7dc2504dfb8c66925f36050b4ef2d832d72457a4953b6b27f16fc4b64a84de26`. Browser acceptance, external-provider receipts and the remaining requirement-by-requirement audit stay open.

  - [x] Reconcile the native Minidump architecture summary in `SPEC.md` with delivered RISC-V64 support. The parser, CFI, HTTP/PostgreSQL acceptance and public amd64/arm64 image evidence are recorded under the RISC-V64 task above; exact-main CI [40112638468](https://github.com/jerolan/good-sentry/actions/runs/40112638468) passed formatting, vet and the complete unit/PostgreSQL race suite. The remaining limitation now names RISC-V64 as supported.

## Evidence log

- 2026-10-15 UTC: Verified issue-search source e9586d59213839a0ec4e5072ddba854278d47646 is published with170 files and tree3b0c10da44fa15a8a74177f9d590b8562ee136c9 independently compared against GitHub's recursive tree API. CI run38690025488/job116273552766 passes formatting, vet, actual PostgreSQL integration/race and image construction; primary inspects all completed steps and retains its log. Pages38690025549 package116273552937/deploy116273607542 succeeds. The six deployed files match site/ exactly; actual Chromium320/768/1440 verifies keyboard/skiplink, anchors,16links, real clipboard/focus and screenshot image with no page/console/request/HTTP errors. The unchanged landing harness SHA93742f09b34771e15d150493c93a9a77794d99ace9c7f77230100a79b34cac95 and actual reports/assets/captures are under ignored test-results/landing-issues-deployed; primary inspects the report and all six hashes. This closes native search, not API0 listing or complete GlitchTip parity.
- 2026-10-15 UTC: Publication incident: despite the reviewed13-path staged diff, commit2dde118 records an empty tree and was pushed before the primary checked the resulting commit. The source files remained intact; corrective fast-forward e9586d5 restores the exact verified source using a private index, without history rewriting. The restored tree differs from good design26294fa on only the13 reviewed paths and deletes none. Default index was65bytes/0entries, then rebuilding index metadata only yields170entries/17294bytes and a clean worktree. The underlying staging-loss cause is unproven; do not attribute it to iCloud or another process without evidence. The private publication helper now requires explicit reviewed commit/tree SHAs and required application objects before any push. Future commits must use an isolated index and check the recorded tree before publishing. Evidence: ignored test-results/verification-issue-search/index-recovery.json and actual GitHub tree comparison; no runtime source changes during recovery.
- 2026-10-15 UTC: Final application image sha256:de4c438fa6f9bdb4a6ddfcbe3b3cc9fc40fea2dbd5731dd5029c63a63fac40f2 passes the unchanged Node11.7.0 issue-search workflow with33 real SDK events/31issues/26cohort matches. An app-only restart3.447s preserves those33 raw event rows,31 lifetime issue records and16 exact HTTP responses, all three orders and25+1+0 pages. The same owner cookie works without login, the old revoked token5 stays401, original52-event/spans/operations subsets and4map bytes/checksums/receipts/aliases stay unchanged, traces35/39/41 and2canceled-zero-attempt deliveries remain intact; total85events is expected after SDK traffic. Primary independently compares all16 live response bytes/status/hashes, actual running image/non-root identity and served UI against the worktree. Image25,335,146bytes is not RSS. Go1.27.2/CA/MIT/no-Node runtime audit passes with unchanged migrations. Evidence: ignored test-results/container-issue-search/{report,primary-verification}.json and issues-sdk-image-final/report.json.
- 2026-10-15 UTC: The first private image verifier expected0 for selecting the deliberately populated second project; the actual SDK seed requires1 there and excludes that issue only when selecting the first project. The verifier is corrected to1 and the old failed attempt remains classified as harness-only. No production data/code change or SDK replay is used. The two additional final-image UI rebuilds are justified by demonstrated environment and context-link REDs; earlier image audits remain historical. Only the owned native QA18006 process is stopped after acceptance; existing18001/18004/18080 services remain available.
- 2026-10-15 UTC: On the final frozen UI7c81e3f03b7a8a3348ba2924b6af2d02bd7a5f53a14ff9fd4db53738097921b1, issue search33/general27/releases21 browser captures pass (81 total) with no uncaught errors or failed requests. Intentional validation/auth responses remain separate. A real DOM repro finds stale release on project View issues and trace Open issue after their other filters reset; two explicit clears repair both while trace Back retains its original release. Final DOM observations, fresh-project click and screenshots confirm the behavior. Primary inspects actual hrefs/reports. Evidence: ignored test-results/issues-browser-{context-final,general-final,release-final} and issues-context-{red,green}.
- 2026-10-15 UTC: Final-source full native integration passes server28.687s and full race integration passes server29.574s; unchanged store checks correctly reuse the passing51.067s/47.349s cache. Vet, source formatting and diff checks pass. The SDK harness uses pinned Node11.7.0; image fixture preflight initially detects127.0.0.1 versus the configured localhost DSN before any capture, and a sanitizer empty-string guard repairs noisy redaction only. Fixture base is aligned with configured localhost while original DSNs/runtime remain unchanged. Historical preflight report remains under ignored test-results/issues-sdk-image; it is not a product RED. Final-source logs: ignored test-results/verification-issue-search/{native-final,race-final}.log.
- 2026-10-15 UTC: Native issue search has an explicit independent grammar and historical contract. The parser's compiling pass-through baseline fails actual AST/malformed-query assertions0.010s, then grammar/Unicode/quoted literal/list/negation/bounds pass0.008s; vet/format pass. No SQL, new dependencies or test-only seams enter the parser. Evidence: /tmp/goodsentry-issue-search-parser-{red,green}.log.
- 2026-10-15 UTC: Historical PostgreSQL RED0.543s returns0 when the matching production/v1 occurrence precedes a staging/v2 latest sample. The same test passes0.317s after moving combined predicates onto one stored event. Five provider cases cover24 structured combinations, object/pair/scalar/missing tags, literal SQL characters, quoted/wildcard identity,25+1 pages, deterministic three orders, current grants and reopen; full focused suite with durable capture passes4.838s. A natural real SetIssueStatus writer/three readers sees both resolved/unresolved states; ReadCommitted overlay fails2.043s with total0 and one returned row, while the repeatable-read provider remains coherent. Primary inspects the actual overlay and actor assertions. Evidence: /tmp/goodsentry-issue-search-evidence.
- 2026-10-15 UTC: Actual HTTP reproduces permissive unknown/repeated/page/sort input, invalid UTF-8/NUL reaching storage503 and missing historical grammar wiring. Strict query handling and typed provider errors repair these; full new HTTP boundary passes0.346s with the original authenticated session, canonical page metadata, exact Unicode selector boundaries, missing project/org404 and past-end pages. The first fixture forgot sentry_version and failed SDK403; that is corrected as setup error before the genuine query RED. Logs: /tmp/goodsentry-issue-http-{red,green}.log.
- 2026-10-15 UTC: Complete native Go integration passes server28.037s/store51.067s and full Docker race integration passes server29.351s/store47.349s; vet passes. These checks precede subsequent UI-only literal-environment/navigation refinements; unchanged backend validation remains covered and final browser/image/remote checks own the resulting assets. Evidence: ignored test-results/verification-issue-search.
- 2026-10-15 UTC: Official Node SDK11.7.0 sends33 real captureEvent messages with generated IDs through the owned native process. Authenticated retrieval preserves SDK metadata, exact tags/timestamps/environment/release/fingerprint.31issues include26 selected historical matches on25+1 pages with independently seeded date/new/frequency order; wrong project/release/environment/tag and split-event controls remain excluded.33 captured raw payloads/checksums are retained. Two separately classified raw pair-tag envelopes add resolved literal-star and negative controls; they are not claimed as SDK output. Primary inspects actual first/latest metadata and live served asset. Evidence: ignored test-results/issues-sdk-native.
- 2026-10-15 UTC: New real-browser search acceptance initially detects environment trimming (production versus the submitted surrounding spaces). A one-line form repair preserves the exact environment and passes30captures at320/768/1440, including historical26/count2, three orders,25+1, URL/history, exact tag/pair/star navigation and editable400/retry. Existing general27 and release21 regressions also pass on the same runtime with a real member. Primary inspects actual mobile pair-tag and desktop error-form screenshots. The harmless first harness duplicated region:eu; final harness consumes the exact supplied query. Evidence: ignored test-results/issues-browser-{first,final,general-regressions,release-regressions}; further navigation acceptance remains tracked before publication.
- 2026-10-15 UTC: JavaScript artifact source a199aaa0e32770b10eeffa9e8256872373baafae is published on main. GitHub CI run38686089260/job116261141232 succeeds for formatting, vet, actual PostgreSQL integration/race and image construction; primary inspects every completed step and retains its log under ignored test-results/publication. Pages run38686089286 package116261141374/deploy116261191828 succeeds. Its six public files match site/ byte-for-byte; Chromium320/768/1440 verifies exact viewport widths, keyboard/anchors,16links, real clipboard/focus and product image with no page/console/request/HTTP errors. Primary inspects reports and actual mobile screenshot. The unchanged harness SHA25693742f09b34771e15d150493c93a9a77794d99ace9c7f77230100a79b34cac95 and manifests are under ignored test-results/landing-artifacts-deployed. This closes only JavaScript artifact uploads; native symbols and full GlitchTip parity remain open.
- 2026-10-15 UTC: After image restart and fixture token revocation, primary independently confirms the running017dcb4f image/non-root identity, six live owner-authenticated map/issue HTTP bodies against recorded SHA256/size snapshots and exact external-client report/harness hashes. No login or replay occurs. Evidence: ignored test-results/container-artifact-uploads/primary-verification.json.
- 2026-10-15 UTC: Final Docker image sha256:017dcb4f1f9dd5cd06fbad7998ec6cadbc895e862562dd8f00049b8bf0c9b90d passes the unchanged official CLI3.8.0/Node SDK11.7.0 six-exception workflow. After an app-only restart, three maps retain exact bytes/checksums/receipt/aliases, six events retain raw/mapped data and eight authenticated HTTP responses remain byte-identical. The original cookie/restricted token work without login; excluded project404 and historical map/trace/raw/projection/canceled-zero-attempt delivery subsets remain unchanged. Migration011 checksum b219554477ccfefded6c6582afa638a6a3c1bed1e56d3e6e886bf55422101ae5 applies without changing old migrations. Runtime audit confirms Go1.27.2, non-root65532:65532, exact embedded assets/MIT and no Node/build toolchain. Image size25,292,455bytes is not RSS. Primary independently inspects image identity, report snapshots and actual SDK results. Evidence: ignored test-results/container-artifact-uploads and artifact-cli-sdk-image.
- 2026-10-15 UTC: Only the two explicitly created artifact fixture credentials (native token1 and image token5) are revoked through the owner API with original CSRF/session. Each returns204, changes negotiation200→401 and makes the actual CLI upload fail with401/exit1. Logs contain no credential values. The owned native QA process18005 is stopped; existing services18001/18004/18080 remain. Evidence: ignored test-results/artifact-credential-revocation/report.json.
- 2026-10-15 UTC: Modern artifact upload contracts connect exact negotiation/chunk/assembly routes and owner/admin project:releases consumption. Configuration validates expiry/quota/materialization limits before startup; six ignored-invalid-policy cases fail before the repair and the existing table plus default/minimum/large-deployment cases pass afterward. Independent public docs/OpenAPI and actual pinned CLI traffic own provenance in SPEC.md. General telemetry retention and native debug files remain separate work.
- 2026-10-15 UTC: Eight real PostgreSQL artifact cases plus native-map/release regressions pass14.064s: atomic concurrent staging quota, expiry/refresh/reclaim, ordered/repeated/distinct-missing chunks, bounded SKIP LOCKED cleanup, current roles/organization/project grants, multi-project rollback/conflicts/alias union, release-delete races, migration010→011 and reopen. Original map bytes/checksums/receipt remain unchanged. The natural-expiry/real-cleanup concurrency case observes complete and missing states with valid immutable chunks; its ReadCommitted overlay survives, so no mutation sensitivity is claimed. The earlier direct-DELETE interleaving failure is retained only as artificial ordering evidence. Logs: /tmp/goodsentry-artifact-mutations.
- 2026-10-15 UTC: A real storage RED rejects duplicate selector rows as2maps/102bytes under a51-byte budget; logical-map coalescence repairs it to1map/51bytes and preserves103 original selector rows, aliases and DebugID-only semantics. Distinct source bases/IDs/content remain distinct. Pure consumer REDs cover script aliases/source bases, full header query/fragment identity, UTF-8 and typed limits. Metadata/file/bundle identities remain separate; contradictions fail, unambiguous inheritance works. Focused package tests and vet pass.
- 2026-10-15 UTC: Actual HTTP absent-route RED404 versus401 is repaired; the full new HTTP boundary passes1.146s with issued credentials, current roles/CSRF/header precedence, prefixed negotiation, raw/gzip filename/digest/size/expansion limits, strict assembly, missing/error/ok shapes, isolated atomic project writes, immutable retries and no implicit release creation. An unknown POST descendant405 is the preexisting fallback, not a product regression; the test's initial404 assumption is corrected. Logs: /tmp/goodsentry-artifact-http-*.log.
- 2026-10-15 UTC: Native full integration initially passes server28.087s/store42.723s and Docker race passes server29.461s/store38.496s. After the actual Node-frame repair, artifact tests pass0.590s and race6.069s; complete race/integration passes server28.654s with unchanged store checks correctly cached. The redirected final native log omits its last three package lines; an independent same-code recheck confirms server/store/uptime all pass with valid Go cache, saved separately. Vet and formatting/diff checks pass. Evidence: ignored test-results/verification-artifact-uploads, including native-node-fix-package-recheck.log.
- 2026-10-15 UTC: First actual CLI→Node SDK capture fails to map despite correct file DebugID because SDK11.7.0 emits filename without abs_path. Consumer-boundary RED confirms absent/null/empty abs_path cases; fallback to filename repairs them while valid abs_path remains authoritative and malformed values stay unmapped. The unchanged external capture is not adapted to manufacture abs_path. Historical external RED is under ignored test-results/artifact-cli-sdk-red; /tmp/sentrygo-node-filename-{red,green}.log records the focused repair.
- 2026-10-15 UTC: Pinned official sentry-cli3.8.0 SHA25613f8cb34ae01a6a272d7d7c22e277a105286615b4020de900ea95a8de47cdbb6 and official Node SDK11.7.0 pass the full native runtime workflow: inject, upload/retry, DebugID-only capture, same build with release/dist, wrong-project isolation, an uninjected release/dist capture and wrong-version/wrong-dist controls. Six genuine SDK exceptions are retrieved; positive frames match independently authored original line7/column1/context and preserve minified V8 frames. Incorrect selectors remain unmapped. The repository-owned tests/compat/artifact_cli_sdk.mjs creates no accounts or mocked envelopes and consumes a private0600 fixture. Evidence: ignored test-results/artifact-cli-sdk-green/report.json.
- 2026-10-15 UTC: A real owned process restart preserves a staged498-byte first chunk, its bytes/receipt/expiry, the same session/CSRF/token and one historical capture hash. Assembly still asks only for the missing second498-byte chunk, then accepts it and remains idempotent. Startup cleanup removes only an explicitly expired fixture chunk before any HTTP write. Expiry was induced through fixture SQL; natural24-hour passage is not claimed. This bundle map already existed, so the case proves resume/idempotence rather than first map creation. Evidence: ignored test-results/artifact-native-resume/report.json.
- 2026-10-15 UTC: Browser general14groups/27captures, Releases2/21 and a real SDK/CLI UI inspection6captures pass at320/768/1440 (54 final captures), with no uncaught page errors or failed requests. Expected auth401 and validation400 remain separate. Primary inspects actual mobile original-line7/context and desktop CLI artifact metadata. The earlier general false0 count races async metadata refresh; one awaited visible row fixes only the existing harness while preserving its exact count assertion. A separate probe locator error is classified as harness-only. Historical first attempts remain separate; final reports: ignored test-results/artifact-browser-{general,releases,sdk-ui}.

- 2026-10-15 UTC: Release source1190bec31307e85cbe75518433e51a6526e70bc8 is published on main. GitHub CI run38681320812/job116246303075 succeeds for formatting, vet, actual PostgreSQL integration/race and the application image build. Primary inspected every completed step; the job log is retained under ignored test-results/publication. Pages run38681320608 packages and deploys the matching site successfully. This closes the specified release/commit/deploy increment; modern CLI artifact uploads, automatic repository integration and full GlitchTip parity remain open.
- 2026-10-15 UTC: Public HTTPS landing acceptance for1190bec passes the unchanged Chromium harness at320/768/1440, including keyboard navigation, actual clipboard, all anchors/links and product image. Primary inspects the report and live desktop viewport. All six canonical HTTPS assets return200 and are byte-identical to committed site/; page, network, HTTP and console error arrays are empty. Evidence: ignored test-results/landing-releases-deployed/{report,asset-integrity}.json and six captures. QA18005 stops with exit0 and its port closes; prior18001/18004/18080 remain available.
- 2026-10-15 UTC: Release storage and migration010 add organization/version identity, scoped project associations, ordered commit replacement, actual manual-repository metadata and completed deploy records. Fresh-only SDK/map observation and historical backfill preserve events, spans, raw JSON and existing maps. Ten focused PostgreSQL cases pass under America/Mexico_City, including concurrent creation/replacement, membership/scope changes, ordered upload/delete, 255-character exact identities and reopen. An isolated Read Committed mutation fails the summary-coherence assertion; repeatable-read providers pass. Raw commit JSON retains int64, escaped NUL in additional metadata and exponents beyond float64. Historical focused evidence: /tmp/goodsentry-releases-store-proof.json and /tmp/goodsentry-releases-store-green.log. Later full suites include the additional CLI project-assertion case.
- 2026-10-15 UTC: Actual HTTP regressions reproduce missing routes, local-zone serialization, rounded conflicting aliases, configured CLI projects on PUT, archive/restore POST with empty projects, and deployment project objects rejected by the CLI. The repaired contract normalizes new metadata to UTC, compares numeric aliases exactly, treats PUT projects as assertions, permits existing-only status POST, and adapts API0 deploy projects to slug strings while native pages retain project metadata. Issued read/write tokens, current roles, cookie/CSRF/Bearer precedence, shared-release coverage, hidden resources, strict query/paging/prefixed Links and native25-row pages pass. Two actual unregistered project descendants initially return200; exact route matching repairs both to404. Focused tests and their RED/GREEN logs are under /tmp/sentrygo-release-http-*.
- 2026-10-15 UTC: Chromium verifies original Releases list/detail/forms, independent25+1 histories, safe text/URLs, exact int64/extreme-exponent previews, UTC, member permissions, source-map navigation and matching artifact deletion. A genuine isolated UI filter mutation sends a numeric ID shadowed by another slug and returns0 rows, whereas the production slug request returns25. Earlier selectOption string matching and removal timing failures are classified as harness errors, not product REDs. A separate real Back-navigation regression leaves the old create form open; owned-dialog cleanup and guarded completion callbacks repair create/edit Back/Forward while preserving ordinary400 input retention. Historical reports remain under ignored test-results/browser-releases-*.
- 2026-10-15 UTC: Final browser acceptance passes Releases2 groups/21 captures, credentials3/21 and unchanged general14/27, totaling19 groups/69 captures at320/768/1440 with no uncaught page errors or failed requests. Intentional validation400 and anonymous401 responses remain separately recorded. Primary inspected actual reports and desktop release/mobile deletion screenshots. Final assets are app.js367219f17288ff2ef6a0db56291218713ab622c2872507747bfce266cd11ae39, releases.js2825c9737c1699948e3a7a0dfa1cb6246e9961f5169f76a4b2a18bfa7df48cb4 and app.css8be4d7fb345a1d56033268501d8d12317aeb3e2d18c947c42dcdce407b74bed7. Reports: ignored test-results/browser-releases-dialog-final, release-dialog-token-regressions and release-dialog-general-regressions.
- 2026-10-15 UTC: Complete native PostgreSQL/integration tests pass server28.455s; Docker race/integration passes server28.077s. Unchanged store checks are correctly cached after earlier full runs passed store48.906s and race store43.955s. Vet, formatting and diff checks pass. Final logs are under ignored test-results/verification-releases/{native-final-assets,race-final-assets}.log; historical runs retain the original uncached store timings.
- 2026-10-15 UTC: Actual final application image sha256:a201adbde8b3c154b75d0eddd844d621280b5bc6fc45a8b4393907fcaea32133 passes pinned official sentry-cli3.8.0 create/duplicate208/finalize/archive/restore/list/info, local Git2 commits then previous-head1 commit, one completed deploy and delete. Fresh issued token4 is limited to project34; the same cookie/token and byte-identical release/commit/deploy snapshots survive a real container restart, then revocation causes CLI rejection/API401. Official Go SDK0.50.0 captures error+transaction and observes metadata; deleting that metadata preserves both stored captures and raw hashes. Historical trace/raw statistics and canceled delivery history remain unchanged; migration010 backfills two original identities without repeat resurrection. Earlier actual CLI400/response-shape failures and their fixes remain in the chronological report. Evidence: ignored test-results/container-releases/report.json and per-command proof/log files.
- 2026-10-15 UTC: Primary verifies the exact final image ID, non-root65532:65532 entry/goodsentry and current readiness. An independent streamed image audit confirms all four served assets byte-identical to the workspace, the exact daisyUI MIT notice, Go binary and CA bundle, with no Node/npm/Tailwind tools or loose frontend files. Image metadata reports25065271bytes; this does not measure RSS or establish the256–512MB target. Evidence: ignored test-results/container-releases/runtime-audit.json. No external test recipients were created; all acceptance credentials were revoked.

- 2026-10-14: Credential source8f9325257855795771e0fb26e387ea741dac4122 is published on main. GitHub CI run38673907845/job116223050942 succeeds for formatting, vet, actual PostgreSQL integration/race and the application image build. Primary inspected the exact-head completed steps. QA runtimes18005/18006 stop cleanly after browser inspection; existing18001/18004/18080 remain available. This closes only the workspace read-credential prerequisite. Release/commit/deploy operations, their write scopes and the remaining full-product acceptance stay open.
- 2026-10-14: Official sentry-cli3.8.0 first fails against the old image's missing route404, then succeeds with an actually issued read credential restricted to project30, excluding project31 and another organization. It succeeds again after app-only restart with the same credential/session and byte-identical project list/detail/metadata. Native revocation returns204, persists revoked metadata and makes that same CLI fail and the API return401. Final image sha256:5c3f54fe1a33d42036e32d7a48b85a6f6c5df3c4bc2d85090c71441a80210352 runs as65532:65532, reports24,775,187 image bytes (not RSS), excludes Node/build plugins, retains the exact MIT notice and serves JS8df14e0ab7e0eac8d53b80a1d996bf1cbcaefcd5fce7f4f8af7be9670e876bed/CSS69414746b10573e4b699ef3f8f45df778d0357006945761eaad9420ee31295a5. Migration009 checksumc37e107341f2f2a28bfc483ab6973f24c121ce4f126b3c70722ed8516700c890 is applied. Primary independently checked runtime/assets, corrected400 responses, live revoked metadata and PostgreSQL history35/39/41 raw checksums/version2; their traces/projections and project15 canceled zero-attempt delivery history remain intact. Ignored test-results/container-api-tokens/report.json passes; its reports contain no credential values or HMACs, and the driver's private secret is deleted after revocation. Only test-owned credentials were revoked.
- 2026-10-14: The automation-credential prerequisite passes real PostgreSQL for hash-only persistence/reopen, current owner/admin/member permissions, idempotent retained revocation, expiry and permanent invalidation on membership removal/re-enrollment. Foreign/absent selected projects leave no partial credential/grants. All-current-and-future access stays distinct from restricted access after its last project is deleted; literal wildcard characters, numeric ordering/lookahead and25-row metadata history pass. An isolated overlay removing the expiry predicate fails the owning regression in0.235s; the real focused storage suite passes1.844s. The initial reversed literal-substring fixture was corrected without changing the provider.
- 2026-10-14: Real HTTP issuance/consumption initially returns404 instead of the required anonymous401, then passes scope checks, authoritative malformed/duplicate Bearer headers with valid-cookie controls, current tenant/project isolation, session-only credential management with CSRF/origin checks, SDK-key separation, null/type/bound validation and prefixed public Link navigation. Fresh review identifies raw malformed query encoding silently defaulting and a generated next cursor beyond its accepted bound. Both fail at the public HTTP boundary before repair and pass afterward in0.751s. Strict ParseQuery and one shared cursor bound repair the owners; a follow-up focused review finds no remaining scope violation.
- 2026-10-14: Credential Chromium acceptance passes3 behavior groups/21 captures; unchanged general regression passes14 groups/27 captures at320/768/1440, totaling48. Real400 preserves selected projects/scopes/lifetime, actual keyboard copy/paste matches the issued credential, compatible project reads succeed before visible revocation and return401 afterward. One-time reveal clears on dismissal/leaving/Back/refresh, browser storage and metadata never recover it, telemetry names remain text and real member controls are absent. Every report has zero uncaught page errors or failed requests and all fixture credentials are revoked. Primary inspected desktop metadata and mobile permission captures under ignored test-results/browser-api-tokens-final and api-tokens-regressions. Two earlier harness-only scroll-position and unavailable HTTP clipboard-read failures are preserved separately; no product changes were made for them.
- 2026-10-14: Full integration before the focused HTTP repairs passes server25.923s/store31.206s; full Docker race/integration passes server26.293s/store29.131s. After repair, complete integration passes server23.683s and race/integration server24.982s, with unchanged store checks correctly cached; final vet and format/diff checks pass. Logs and earlier results remain under ignored test-results/verification-api-tokens. CLI3.8.0 binary checksum13f8cb34ae01a6a272d7d7c22e277a105286615b4020de900ea95a8de47cdbb6 is independently checked. Remaining release/write scopes and full management/account parity are not complete.

- 2026-10-14: Trace source commit85dee826d4cf22cef9e37efe8c66694e84e87996 is published on main. GitHub CI run38670405270/job116211953505 succeeds for formatting, vet, actual PostgreSQL integration/race and the application image build; primary inspected the completed steps and downloaded job log under ignored test-results/publication. This closes the specified trace increment, not the full GlitchTip parity objective. Release/deploy/CLI, native OTLP, retention and the remaining unchecked capabilities remain required.
- 2026-10-14: Trace browser acceptance passes four behavior groups/32 captures; unchanged general, time-window, occurrence and operation harnesses pass 14/27, 5/12, 4/33 and 3/27, totaling 30 groups/131 captures at 320/768/1440. The actual 71-node mixed-project forest, independently known waveform geometry, zero/partial/reversed timing, parent-page access, full selected int64/raw/XSS safety, 26-error pagination, canonical uppercase logs, origin restoration, disabled logs and real404 pass. Trace and operation indexing503 states use real pending PostgreSQL row locks; normal workers recover after release. Every report has zero uncaught page errors or failed requests; only expected auth/validation/not-found/indexing responses occur. Primary inspected actual reports and desktop/mobile waveform/relationship/raw screenshots under ignored test-results/browser-trace and trace-{regressions,windows,occurrences,operations}. Both QA servers stop cleanly afterward; prior runtimes and the acceptance image remain available.
- 2026-10-14: Trace reconstruction passes real PostgreSQL for mixed legacy/standalone sources, explicit project-local parent preference, cross-project ambiguity, every cyclic edge, missing/invalid IDs, numeric ordering and a 2001-node deep chain across three envelopes through 41 pages. Nullable/reversed/zero timing, Unicode summary bounds, selected raw scalar/null/int64 records, 25-row errors, logs-disabled scopes, actual membership and repeatable-read graph/metadata/count consistency pass. Isolated mutations removing local preference, cutting only one cycle edge or using Read Committed fail the owning regressions. The version1 upgrade test proves uncommitted replacement statistics stay available, cancellation rolls back atomically, and reopen restores version2 metadata without changing raw JSON or statistics.
- 2026-10-14: Trace HTTP tests first fail on missing authenticated routing, then pass canonical IDs/pages/projects, real capture-to-graph/raw/errors, tenant/project isolation and 503/Retry-After: 2 for pending legacy metadata. A primary-added uppercase-log regression initially returns zero related logs; explicit traceMatch=canonical fixes it while default exact matching remains intact. Full integration passes server 23.350s/store 24.574s; Docker race/integration passes server 24.866s/store 24.959s and final vet is clean. Logs: ignored test-results/verification-trace. Representative load/RSS measurements remain unperformed.
- 2026-10-14: Official Go 0.50.0 and Node 11.7.0 extended clients first fail with trace-route404 on the previous image, then pass 6/6 against sha256:bc991d9e3298f3fc32f5c08e1298f75c6aa0ee00d85269a0a4dc733102e56eea, including SDK-emitted ancestry/depth/timing, selected raw records and related logs. This image runs as 65532:65532, reports 24,677,952 image bytes (not RSS), excludes Node/build plugins and serves exact JS 4da36fc44bdfceb515ceda274d9db98a811213362dce5db04e2b6de4a34fc37f and CSS 7bd3dfa7a84ad8aa8455ea9a2ed06a740dea5aec91a150a9d821f4b6d99cea6c. Primary independently fetched both live traces and inspected PostgreSQL event35/39/41 IDs, times, projection version2 and original raw checksums. Migration008 checksum is 19724cb7ad4fc974e6a9cf7175e6f90860c25363dff7cf2865bd8c11e6aae9c5.
- 2026-10-14: Actual-image history35/39 upgrades from version1 to2 with raw JSON and prior operations/detail/aggregate bytes preserved. With only the app stopped, fixture41 retains statistics but resets its trace metadata/version1; the same image's startup worker restores exact version2 metadata. Same-cookie traces, selected raw nodes, logs, operations, occurrence and absolute-window aggregate responses remain byte-identical after restart, and canceled project15 delivery history stays intact. Primary inspected ignored test-results/container-trace/report.json and direct stored/live responses. The same session survives without reauthentication.

- 2026-10-14: Child-operation statistics pass real PostgreSQL for combined legacy/standalone groups, exact typed segment references across projects/traces, continuous p95/null/zero metrics, historical Go microsecond normalization, root time-window boundaries and 1060 children across two envelopes through 53 groups. Eight concurrent projection workers process unlocked history once; cancellation while COPY waits on a real table lock leaves marker 0 and no partial rows. Reopen resumes it. Deferred database constraints force commit failures and prove both ingestion and historical projection roll back atomically before successful retries. Removing SKIP LOCKED in an isolated Go overlay makes the owning lock regression fail; shared production files stay unchanged.
- 2026-10-14: New HTTP contract first fails anonymous 404 versus required 401, then passes both actual capture formats, immutable group reference, canonical query validation, real membership/isolation and descriptive 503/Retry-After: 2 without partial metrics. Primary-reviewed browser acceptance passes 3 new groups/27 captures plus unchanged 14/27 general, 5/12 window and 4/33 occurrence checks, totaling 26 groups/99 captures at 320/768/1440. Only intentional 503/auth/validation responses occur; zero uncaught page errors or failed requests. The historical 503 uses a real locked pending event, and normal in-process processing restores the visible statistics after release. Reports are under ignored test-results/transaction-span-groups-{browser,regressions,windows,occurrences}. The exercised fixture helper is retained with the harness under tests/compat/tools for reproduction.
- 2026-10-14: Primary full integration passes server 22.624s/store 22.622s; Docker race/integration passes server 24.879s/store 23.862s. Unit tests and vet pass. Logs live under ignored test-results/verification-transaction-span-groups. Official Go 0.50.0 and Node 11.7.0 checks fail with 404 on the prior image, then pass 6/6 against image sha256:c56f5cc5b62d02b5a79b5347a6f75986058a99af30f743877789b891f8e4fd8a. Both verify child metrics against SDK-emitted timing. Primary independently fetched Go event 35/project 24 and Node segment 15/project 25 statistics and inspected their persisted rows/typed segment association. The image is 24,569,563 bytes, not RSS, runs as 65532:65532 and serves exact JS 6c21eda41d097d6f697efdd43f9e54d56db763e919a3e10e3f539d00edea1f96 and unchanged CSS 2bdf63adf87792ed41d352ded4ba25a512414657af39ff20effe13f35d0109eb.
- 2026-10-14: Actual-image restart acceptance leaves only fixture event 35's derived projection pending while the application is stopped. Startup restores marker 1 and its exact ordinal row automatically, preserving original raw SHA256 ef8e0f62b34650b02e455c5721cff6e3222b96f30a5bbc752d1f2fa2af1b451f. Same-cookie statistics/detail/aggregate responses with absolute bounds remain byte-identical; historical events 27/32 also backfill and canceled project 15 delivery history remains intact. Primary checked applied migration 007 checksum 51dea804e1aaa4451887049a83705f7ee1c640ed9d147efc2ac6ae62a3f28d61 directly in PostgreSQL. Runtime report test-results/container-transaction-span-groups/report.json passes. QA ports 18005/18006 stop cleanly after inspection; existing services remain. Source commit 65d7247 is published on main and GitHub CI run 38666528522/job 116199641976 passes formatting, vet, PostgreSQL integration/race and the image build. Complete trace reconstruction/waterfall is still pending.

- 2026-10-14: Occurrence browser acceptance passes four behavior groups/33 captures; unchanged general regression passes 14 groups/27 captures and time-window regression five groups/12 captures at 320/768/1440. Actual all-project selection opens the stored sibling project; source/id and UTC bounds survive 50+1 paging/reload/back and related-log navigation. Real members and a second logs-disabled server pass; invalid/missing/reversed timing stays unavailable, raw telemetry is text and max-int64 digits remain exact. Zero page errors or failed requests; intentional preexisting auth/validation statuses are recorded separately. Primary inspected desktop legacy metadata, standalone related-span scope and mobile disabled-log context. A focused DOM probe confirms all 32 trace characters fit at 320px without clipping or overlap. Evidence: ignored test-results/transaction-detail-{browser,regressions,windows}; temporary native check servers stopped cleanly.
- 2026-10-14: Extended the pinned official Go 0.50.0 and Node 11.7.0 clients to verify capture-to-aggregate-to-detail root/child timing, parent identity and original SDK context. Both matrices pass 3/3. The first Node duration assertion used the legacy callback timestamp field; primary independently checked the installed SDK's public StreamedSpanJSON/options declarations, confirmed end_timestamp and accepted the test-only correction. No product change or client-pin change was needed. The prior failing harness output is retained separately; final SDK logs are copied under ignored test-results/container-transaction-detail. Primary independently read the real stored Go child acceptance flag/parent and Node typed segment ID from PostgreSQL.
- 2026-10-14: Final occurrence image sha256:f121277d5e911d19644624894dd944a47dadff57b0619c108fa6e9b4c203eae6 reports 24,491,927 image bytes, not RSS, and runs as 65532:65532. Primary checked actual image identity and byte-exact served JS ad544e927619e5523a67809fdd0431fcaac7033291f56f7a6a8138d0db109542/CSS 2bdf63adf87792ed41d352ded4ba25a512414657af39ff20effe13f35d0109eb. Full report test-results/container-transaction-detail/report.json passes license/no-Node checks and actual application restart: the same owner cookie and both SDK aggregate/detail responses remain byte-identical, including source/id/raw/parent/timing. Acceptance-owned canceled delivery history is unchanged. Aggregate span statistics and a complete trace waterfall remain incomplete independently of this verified occurrence detail.

- 2026-10-14: Occurrence-detail store tests first fail with not found, then pass against real PostgreSQL for legacy raw/nullable timing/ordinal children, actual membership and foreign-kind isolation, reopen persistence and 1003 standalone spans across envelopes through 22 pages. An implementation check caught lexical ordering of the projected id::text alias; explicitly ordering by numeric spans.id fixes the same paging regression. HTTP capture-to-reference-to-detail initially fails at anonymous 404 instead of 401, then passes with both real capture formats, canonical source/id/page validation and actual member/foreign/nonsegment/nontransaction cases. Primary extended the owning aggregate tests without changing cross-project grouping; a mutation removing source precedence fails the exact-reference tie regression and real code passes.
- 2026-10-14: Integrated occurrence backend passes go test -tags=integration ./... (server 21.596s/store 14.293s), go vet ./... and Docker golang:1.27.2 go test -race -tags=integration ./... (server 23.337s/store 13.226s). These results cover storage and HTTP behavior; responsive browser, actual SDK detail and final-image acceptance are recorded separately above.

- 2026-10-14: Published main to the user-authorized public https://github.com/jerolan/good-sentry without force; remote HEAD matched reviewed commit a45b89c93ba2433cdeca2792ae20e376b921bc55. GitHub CI run 38660203471 passed formatting, vet, actual PostgreSQL integration/race tests and Docker build. Pages run 38660203483 initially failed because the new repository had no Pages site; enabling workflow-based Pages through the authorized repository API and rerunning attempt 2 passed package/upload/deploy. Public HTTPS https://jerolan.github.io/good-sentry/ returns 200. Primary independently compared all six served files byte-for-byte with site/, inspected the live desktop screenshot and reviewed public Chromium acceptance at 320/768/1440: exact widths, keyboard navigation, real clipboard, links and zero console/page/network/HTTP errors. Evidence: ignored test-results/landing-deployed and test-results/publication. Temporary demo/preview/theme-check servers are stopped; the acceptance Compose stack remains available. This completes publication, not the original feature-parity goal.

- 2026-10-14: The original static landing and matching README use an actual 1440x810 GoodSentry screenshot from an isolated fictional Northstar Demo workspace, SHA-256 f54f196d204ca716a611dc8362130136e0817bc89c47b60b786198bccacda38e. Local Chromium acceptance passes relative assets at /good-sentry/, exact clipboard commands, keyboard skip/navigation, named links and responsive widths 320/768/1440 with zero page/console/network/HTTP errors. Primary inspected the actual screenshot and desktop/mobile landing viewports. Evidence: ignored test-results/landing-final/report.json. Copy explicitly distinguishes verified capabilities from incomplete GlitchTip parity and unmeasured memory/registry status.
- 2026-10-14: Reviewed official CI/Pages workflows pin checkout v7.0.1, setup-go v7.0.0, configure-pages v6.0.0, upload-pages-artifact v5.0.0 and deploy-pages v5.0.1 to immutable commits independently checked through GitHub's ref API. CI uses disposable PostgreSQL 18 for formatting/vet/integration/race and builds the actual image. Pages publishes only site/ from main with separated read/package and pages-write/OIDC deploy permissions. Local YAML/shell checks pass; actual remote execution and public deployment are recorded separately.

- 2026-10-14: Actual daisyUI 5.7.50 components/theme compile with Tailwind 4.3.3 from vendored official standalone modules. Primary review caught a variable collision and then a cascade-layer conflict; GoodSentry owns its color variable and final app layer after plugin utilities. Computed primary contrast is 5.115:1, with white text, distinct error/resolved/warning badges, visible keyboard focus, disabled controls, native dialogs and conditional hidden fields verified. Four unchanged browser harnesses pass 30 groups and 87 captures at 320/768/1440; three focused state groups add three login captures. Zero page errors or failed transport requests; intentional anonymous 401/validation 400 are recorded separately. Reports: ignored test-results/dashboard-daisyui-{browser,windows,uptime,alerts,finish}. Primary inspected desktop/mobile issues and alert dialogs.
- 2026-10-14: Final dashboard image sha256:c86e3ab4c93ee1bc7438ac81c2f7e8ab9c6329095088c4f56d2b9c3efc20812d is ready, runs as 65532:65532 and reports 24,454,389 image bytes, not RSS. Primary inspected test-results/container-dashboard-daisyui/report.json: CSS aecce572389dd301f9f15430272761f86985fca55382d1353286991e6afba0fd and JS 896391d1354f74b8ed7ab9e10e67c0d54bf51050f0c2a2dcf4fe1284e1463139 match served bytes; both build-input checksums pass. Runtime MIT notice matches the official dependency bytes, with no Node or plugin modules in the exported image. Session and acceptance-owned canceled delivery history survive app replacement; automatic-delivery features are active, consolemail is configured and private-network opt-ins remain false.

- 2026-10-14: Time-window/store tests first failed with ignored explicit intervals and service, then passed half-open boundaries in both transaction transports, combined aggregates without child inflation, historical/default behavior, exact service, other filters and tenant/project isolation. HTTP regression first returned 200 for an incomplete interval; the final real capture-to-filter workflow passes with offset normalization, malformed/empty/microsecond-collapsed range rejection and service validation.
- 2026-10-14: Time-window browser acceptance passes 5 groups/12 screenshots; unchanged UI regression passes 14 groups/27 screenshots at 320/768/1440 pixels. Verified 2 mixed legacy/segment transactions, boundary exclusion, 26 combined-filter logs with exact int64 and second-page start record, all four presets, fixed bounds across pagination/filter/reload, UTC microseconds, trace links, default/clear behavior and real invalid-range 400. Zero page errors, failed requests or document overflow. Primary inspected reports, harness assertions and desktop transaction/mobile log captures under ignored test-results/browser-telemetry-windows-final and browser-telemetry-windows-regressions.
- 2026-10-14: Final time-window integration, Docker race/integration and vet pass with stdout under ignored test-results/verification-telemetry-windows. Integration server 21.739s/store 15.191s; race server 22.888s/store 13.692s. Exact non-root image sha256:05777f4071cae8ec106f767a277cdae622a08a583a387ccc3746537744d59295 serves all three assets byte-for-byte, preserves the session and two canceled zero-attempt delivery histories, and returns real 400/200 for invalid/valid time queries. Official Go SDK error/transaction/typed-log smoke passes after recreation. Primary reviewed test-results/container-telemetry-windows/report.json; image bytes 24,352,211 are not RSS.
- 2026-10-14: After explicit user authorization, actual application image sha256:2e1864f7ab2d5ee92c72e09f06bb649a4f928c169d5786c65943ab20e66617ca composes automatic delivery and reports alertDelivery=true. An isolated Compose project with local HTTP/SMTP fixtures records 503/451, persists 15-second retries across restart with the same session/identifiers, then delivers 204/250 while two actual app replicas run. Primary independently queried all seven sent records and attempt histories: two frequency deliveries with retry+success, two down and two recovery deliveries, one manual SMTP test. Evidence is under ignored test-results/container-alert-delivery. No external recipients were contacted.
- 2026-10-14: Active-delivery browser acceptance passes six groups and 24 screenshots at 320/768/1440 pixels in ignored test-results/browser-alerts-delivery-final/report.json. Its own HTTP receiver records 29 matching IDs: 26 tests, one captured error, one confirmed down and one recovery. Sent state, HTTP 204 and completed attempt rows are visible; disabled banner is absent. Zero page errors or failed requests; only expected anonymous 401/validation 400. Primary inspected the report, receiver IDs and desktop capture/mobile recovery screenshots. Temporary native worker and receiver were stopped after verification.
- 2026-10-14: Following startup composition, go test -tags=integration ./... passes (server 21.171s), go vet ./... is clean and the image builds successfully. The full component race suite already passed on these sender/worker/store implementations; startup adds composition and shutdown ordering without changing them.
- 2026-10-14: Pinned official Go SDK v0.50.0 error/transaction/typed-log smoke passes after restart against that isolated automatic-delivery image. Primary independently queried error 756fe6c1d65e4a4d93d968211ad07b70, transaction 459ef37c124e4122979ebc6d24e0fe4f and one correlated log for trace ebea1a32a05cafea6df103a52f208ece before fixture teardown.
- 2026-10-14: Transactional alert storage passes receipt-time crossings, concurrent ingestion, duplicate suppression, cooldown, environments, team resolution, tenant isolation, replacement cancellation and retained snapshots against PostgreSQL. Private count queries use numeric project predicates; an actual EXPLAIN fixture with 10,000 events confirms project/environment/time index conditions. Payload environment is bounded to 128 Unicode characters and exhausted-lease cleanup to 128 records per transaction. The stale-token regression fails under a mutation removing its comparison and passes with the real implementation.
- 2026-10-14: The real HTTP integration exercises SDK-shaped capture through a test-owned local webhook: 503 retries after the specified 15-second backoff, then 204 persists sent with two attempts and a stable delivery identifier. Real heartbeat expiry/recovery produces down/up notifications. HTTP 600 initially failed to persist under the old status constraint; forward migration 006 fixes the same regression. No production dispatcher was started.
- 2026-10-14: Alert browser acceptance passes 6 groups and 18 screenshots; existing application regression passes 14 groups/27 screenshots and uptime regression 6 groups/27 screenshots. The 72 captures cover 320/768/1440 pixels with zero page errors or failed transport requests. Expected anonymous 401 and intentional invalid-input 400 remain separate. Evidence: ignored test-results/browser-alerts-final, browser-alert-regressions and browser-alert-uptime-regressions reports. Primary inspected desktop rule and mobile delivery captures. Delivery assertions cover queued state, not automatic browser-visible sending.
- 2026-10-14: Built-container queue acceptance in ignored test-results/container-alert-queues/report.json proves atomic intent, duplicate suppression, queued test, exact embedded assets and session/payload/attempt persistence after restart. Go SDK error/transaction/log smoke passes afterward. Only acceptance-owned rules were deleted; their two delivery records persist as canceled with zero attempts, independently checked by the primary. Both runtimes report alerts=true and alertDelivery=false.
- 2026-10-14: Final alert integration and race suites pass after the cancellation SQL was consolidated into one transaction-local statement. go test -tags=integration ./...: server 21.275s/store 14.665s. Docker golang:1.27.2 go test -race -tags=integration ./...: server 22.754s/store 14.538s, no race warnings. go vet ./... passes. These results cover the library dispatcher with local test-owned recipients; production startup authorization remains outstanding.
- 2026-10-14: Exact final application image sha256:45f19f972e30f6f868bce1911d21f8a83bbe5946995b5da3aed05154a29e8986 is ready after recreation, runs as 65532:65532 and reports 24,213,746 image bytes. Primary inspected ignored test-results/container-alert-final/report.json: all three embedded assets match worktree bytes/SHA-256, alerts=true/alertDelivery=false and the two acceptance-owned canceled deliveries retain unchanged payloads with zero attempts. This is image size, not RSS. The unapplied startup proposal is available under ignored test-results/proposed-alert-startup.patch for authorization review; production sources remain disconnected.
- 2026-10-14: Alert rule and deployment validation pass bounds, recipient/environment deduplication, safe mail URL parsing, header injection rejection and configuration errors without credential disclosure. Official SMTP/Go and public GlitchTip API contracts were checked independently; unspecified retry/cooldown/payload behavior is documented as GoodSentry design.
- 2026-10-14: Local-only delivery library passes actual HTTP status/identity/private-network/redirect/timeout checks, SMTP MAIL/RCPT/DATA, transient/permanent replies and QUIT failure after acceptance. Fresh test subprocesses trust a test-owned CA through SSL_CERT_FILE and prove authenticated SMTPS and required STARTTLS, including stable Message-ID. Full integration, race detection and vet pass. Production startup remains disconnected following the automatic approval rejection recorded above.
- 2026-10-14: Independently authored uptime probes pass actual HTTP/TCP tests for HEAD/GET/POST, expected status and bounded literal body matching, TLS verification, redirect limits, private/mapped-IP blocking, timeout and caller cancellation. Network validation resolves and pins checked addresses at dial time and uses no environment proxy. Initial missing-API and invalid TCP-host cases failed before implementation/fixes. Probe code adds no dependencies or database access.
- 2026-10-14: PostgreSQL uptime state/history and random fenced leases pass eight-contender one-winner claims, consecutive-failure threshold/reset/recovery, pause/configuration/heartbeat invalidation, expired-lease recovery after reopen, real member/foreign-organization/project isolation and 25-row pagination. An isolated mutation removing the token comparison accepts an old lease under a newer active lease and fails the regression; the real implementation passes. Database time after row locking owns receipts/completions, with UTC microsecond persistence.
- 2026-10-14: Native uptime routes and in-process workers pass authenticated HTTP setup/CRUD/history/capability heartbeat, CSRF, feature gating, validation and oversized heartbeat rejection. Real HTTP plus two independent PostgreSQL pools produce one down/recovery history, survive reopen and cancel active probes without recording shutdown as an outage. Full PostgreSQL integration, race detector and vet pass. MONITOR_WORKERS bounds and private-network alias precedence are validated before startup.
- 2026-10-14: Final uptime browser acceptance has 6 passing groups and 27 screenshots in ignored test-results/browser-uptime-final/report.json. Original project/issue/transaction/log/source-map regression has 14 passing groups and 27 screenshots in test-results/browser-uptime-regressions/report.json. Zero page errors or failed transport requests; anonymous 401 and intentional invalid-input 400 are recorded separately. Primary inspected desktop down/list and mobile heartbeat/form captures.
- 2026-10-14: Single-image container acceptance passes in test-results/container-uptime/report.json. Default destination_blocked, explicit private-network opt-in, real HTTP 200, one completion for a rearmed deadline across two actual replicas, heartbeat expiry/recovery and session/full-history persistence after restart are verified. Primary independently queried the seven persisted checks. Official Go SDK error/transaction/typed-log smoke passes after restart. Image runs as 65532:65532 and reports 24,039,132 bytes, which is image size, not measured application RSS. Temporary replica removed and only the acceptance monitors paused.
- 2026-10-14: Workspace verified empty. WSL2 runs approved /bin/sh commands. Normal sandbox process creation fails before execution. Initial Docker wrapper reports missing WSL integration. Go installation preparation has started in /tmp/sentrygo-toolchain. No application behavior is implemented yet.
- 2026-10-14: Public-documentation research established the GlitchTip baseline and Sentry ingestion contracts. No upstream server implementation source was inspected. Source links and known compatibility gaps are recorded in SPEC.md.
- 2026-10-14: `go test -tags=integration ./...`, `go vet ./...` and `git diff --check` pass against PostgreSQL 18 through commit ad5b209. Full race tests passed before the final source-map parser fixes; rerun after the next integrated increment. Envelope fuzzing completed 31,840 executions without a failure. These checks do not establish unfinished feature parity.
- 2026-10-14: Official sentry-go 0.50.0 passes exception/transaction/typed-log retrieval against native WSL and Compose. Python sentry-sdk 2.72.0 passes the same matrix against Compose. Node 11.7.0 exceptions and typed logs pass; its real streamed-span aggregate initially fails. The new HTTP regression reproduces `unsupportedItems:[span]`; keep the trace check failing until the specified fix lands. SDK packages are isolated external test clients. Node's installation used an explicitly authorized exception to npq's publication-age gate, confined to temporary test tooling.
- 2026-10-14: Tailwind CLI 4.3.3 SHA-256 `dc61b3ac6b8c9ca874c0cc4c57b2409791a64c5540404ca5f5367360babc313a` produces a repeatable stylesheet. Browser onboarding/project/DSN/errors/filters/resolve/reopen/comments/login pass at 320/768/1440 px with no document overflow, JS errors or transport failures. Expected anonymous /me 401 is recorded separately. Local ignored evidence: test-results/browser/report.json and responsive screenshots. Compose serves exactly that compiled stylesheet; image/runtime verification currently covers Linux amd64.
- 2026-10-14: Independent pure source-map tests prove original file/line/column/context, debug-ID precedence and preserved original stacks. Upload/storage/API wiring remains incomplete. Official sentry-cli 3.8.0 SHA-256 `13f8cb34ae01a6a272d7d7c22e277a105286615b4020de900ea95a8de47cdbb6` exercised against our recorder establishes request negotiation, SHA-1 chunks, multipart gzip and SYSB-v2 bundle layout. Synthetic server responses are not real Sentry server evidence. The bundle decoder is the next bounded artifact increment.
- 2026-10-14: Span v2 regression now passes at HTTP and real PostgreSQL boundaries. Atomic rejection, eight concurrent retries including reversed batch order, preserved typed attributes/parent relationships and segment-only aggregation pass. Combined legacy/segment fixture: count 4, average 375 ms, p50 375 ms, p95 606.25 ms. Full `go test -tags=integration ./...`, `go vet ./...` and Docker `go test -race -tags=integration ./...` pass against PostgreSQL 18. The unchanged Node 11.7.0 matrix passes through Compose; primary independently retrieved its two related spans and single aggregate.
- 2026-10-14: Browser Sentry SDK 11.7.0 runs in Chromium 156.0.8078.4 using the official CDN tracing/logs bundle with fixed SRI. Exception, transaction and typed log retrieval pass from a separate HTTP origin; actual POST Origin and Access-Control-Allow-Origin are checked. No browser runtime errors or failed ingestion requests. This establishes the current error/transaction/log SDK matrix, not attachments, artifacts, all SDK versions or complete feature parity.
- 2026-10-14: SYSB v2 decoder is implemented and independently verified against the actual bundle generated by the official CLI recorder experiment. Full Go integration/race/vet checks pass with parser cases for unsafe/duplicate/nonregular paths, missing members, malformed metadata and expansion budgets. Bundle/source debug IDs remain distinct. Chunk upload endpoints and complete CLI symbolication are still pending.
- 2026-10-14: Telemetry browser workflow passes: visible 5 transactions / 1 error / average 400 ms / p50 300 ms / p95 880 ms; 25+1 log pagination; project/text/level/environment/trace filters survive reload; trace copy/link and typed disclosures work at 320/768/1440 px. A literal JSON `9223372036854775807` initially displayed incorrectly as `9223372036854776000`; after lossless log display parsing the same test passes with the exact digits and integer label. Evidence is kept separately under ignored test-results/browser-int64-red and browser-int64-green. No JS runtime errors, failed requests or unexpected console errors.
- 2026-10-14: Native source-map HTTP workflow first failed with 405 before routes existed, then passed upload-to-mapped-event checks. Review reproduced invalid indices accepted at upload and panicking on lookup; an independently authored ECMA-426 consumer now rejects those maps and preserves valid unmapped segments. A second HTTP regression fails under an overlay restoring old Debug-ID selection and passes with frame-specific eligibility. Indexed same-URL source context also has a public RED/GREEN regression; exact mapped source records now own content. The former source-map library dependency was removed.
- 2026-10-14: Source-map consumer fuzz campaigns passed 182,860 executions and then 77,653 on the final context implementation. Full PostgreSQL integration, race detection and vet pass. PostgreSQL tests cover restart persistence, eight concurrent identical uploads, conflicting bytes, real member/foreign-organization identities and exact/exceeded count/byte budgets. HTTP tests reject invalid mapping indices, malformed maps, oversized files and NUL metadata with 400/413, and prove maps cannot cross distributions. NUL metadata previously returned storage 503; its boundary regression now passes.
- 2026-10-14: Final browser source-map workflow passes with actual owner/member identities and 27 screenshots at 320/768/1440 px: multipart boundaries/CSRF, retained invalid-upload inputs, 201 new/200 identical, metadata reload, mapped file 3:5/source context and original stack. Evidence: ignored test-results/browser-sourcemaps-final/report.json; zero JS errors, transport failures or unexpected console messages. Intentional /me401 and invalid-map400 are recorded separately. Primary inspected screenshots. Compose upload/mapped capture, artifact checksum and session/event persistence after application restart also pass; Go SDK exception/transaction/log smoke remains green.
- 2026-10-14: OTLP factual research confirms GlitchTip 6.2 release notes announce native /v1/logs while its current logs guide and published OpenAPI do not document that receiver. Product auth/project routing/encoding details remain unverified. OTLP 1.11 defines HTTP JSON/protobuf/gzip, resource/scope/record attributes, nanosecond timestamps, AnyValue types and success/partial/error responses. Candidate external exporter pins: opentelemetry-sdk and opentelemetry-exporter-otlp-proto-http 1.45.1. No OTLP receiver or exporter proof is implemented yet.

- 2026-10-15 UTC: Native triage provider and transport are implemented, with final browser/image/publication acceptance still open. Automatic approval review rejected the proposed legacy assignee column deletion as potentially destructive. The revised additive migration 012 retains every legacy value under its existing FK, copies only valid current targets to the new typed assignment table, and never falls back to legacy data. The owning real 001..011 upgrade test preserves legacy foreign/valid/null targets, comments/activity/receipts and earlier migration checksums; membership removal/re-enrollment cannot resurrect an assignment.
- 2026-10-15 UTC: Seven owning PostgreSQL triage tests plus durable capture and search snapshot pass in 7.705s with TZ=America/Mexico_City. Genuine REDs expose repeated Resolve changing time/history, lexicographic candidate IDs and non-UTC regression data. Repairs preserve same-value mutations, explicitly order numeric candidates and normalize the prior resolution instant before serializing activity. Real issue lock receipt ordering, eight duplicate retries, fresh one-day-late regression, ignored captures, role changes/moderation/content deletion,25+1 histories and reopen pass. A natural 100 Add/Delete and 160 reads checks comment totals/pages without a production seam; this is not claimed as deterministic mutation sensitivity. Primary inspected the worker diff/assertions and exact file checksum 45b689930a9d3508d58cb232b1d1c610440299808d931f20f508c0fb6b56f79e.
- 2026-10-15 UTC: Native HTTP routes first fail anonymous assignees with 404 instead of 401, then pass three groups in 1.058s in the Mexico timezone. Exact partial JSON/null/target admission, raw UTF-8 validation, strict query/page parsing, prefix/session/CSRF/origin controls, bounded detail comments and commentTotal,200/201/204 contracts and nullable system activity are verified. Provider tests own the larger atomicity/role/concurrency matrix. Primary reviewed response assertions and exact HTTP test checksum 30020b493ce35ac68f497e0bf3f5ee4b4ebadbc7a185d09c86482c4e21dd1a35. Original RED/GREEN logs are preserved under ignored test-results/verification-triage.
- 2026-10-15 UTC: Complete integration passes server 29.341s/store 50.890s; full Docker race/integration passes server 30.688s/store 44.912s and vet is clean. The official Node SDK 11.7.0 exercises its actual makeNodeTransport and receives five 200 acknowledgements for three unique captures. Original and late-ID retries preserve resolution/history/count; a distinct occurrence one day earlier creates one regression with exact UTC receipt/activity identity; ignored capture remains stored without a status transition and original latest-event JSON stays unchanged. SDK evidence is test-results/triage-sdk-native-green/report.json. Earlier 403 from a wrong CSRF header and an assertion assuming UTC formatting for the preexisting firstSeen field were test-driver errors, corrected without product changes; their reports remain. This does not close final-image/browser acceptance or full triage parity.

- 2026-10-15 UTC: Final triage browser acceptance passes seven behavior groups and 27 captures, plus the unchanged general regression's 14 groups/27 captures, at 320/768/1440. Primary inspected the actual reports and mobile unedited-comment/assignment plus desktop SDK-regression captures. Real failures exposed loss of edits typed while PATCH was pending, clipped long assignee text and a literal null rendered for unedited comments. Fixes retain newer drafts, compile existing wrap/min-width utilities and omit absent native DOM arguments; assignment notifications use a short fixed message. The final source is app.js f0c87051d5a0775c10b8487d7c9c94e9529fa952618a35cec5a253f7b4886c35 and app.css ac3b1d1f3c95dc2837422f1d9dfef050b347902f02160ce7c20a3afb10b9f077. Browser harness 67fe3a684a417d216dcdaabcb056d5514a6f003f3fb498acd8c7ac1fc35d860c verifies safe user/team selection, independent pages, member/owner moderation, a genuine CJK byte-limit 400 retaining editable input, same-turn SPA navigation and actual SDK regression. Final reports have no uncaught errors/failed requests and only the documented validation/auth responses. Earlier incorrect SDK mount, resolvedAt retention and full-navigation draft assumptions were test-driver errors; a filesystem EINVAL during screenshot writing was avoided with /tmp output. Historical issue-search 33 captures passed on the preceding c889 asset and were not relabeled as final-source evidence.
- 2026-10-15 UTC: All Go integration/race checks pass with final triage providers. After the last visual-only changes, the affected server package passes integration 28.943s and race 29.687s; unchanged store results retain their earlier complete 50.890s/44.912s runs. Vet, syntax, formatting and diff checks pass. Logs are preserved in ignored test-results/verification-triage/{native,race}-accepted-assets.log; the complete earlier runs remain separately recorded.
- 2026-10-15 UTC: Image 93b02a4a9476e66c02d00e2cdf5fa6195b0f54d4f7ff05af18894931f6b0dae1 passes real official Node SDK 11.7.0 with five acknowledged envelope sends for three unique captures, native assignment/comment/activity smoke and app-only restart. Fresh own project 39/issue 71 ends with 26 comments in 25+1+0 pages and 39 activities. Original 85 events/20 spans/15 operations/4 maps, trace 35/39/41 raw/projections, legacy triage, canceled project 15 deliveries (two, zero attempts), original cookie/CSRF and revoked token 5 HTTP401 stay intact. Additive 012 retains earlier migration history and legacy values. SDK report test-results/triage-sdk-image/report.json is tied to harness 8a3ca1738be406fa2e6ae87ca7a8d34ac66053d7b5f22b131c29079a0901d6e5.
- 2026-10-15 UTC: The accepted visual fixes are incorporated in final image 394fe8ddf8120206b5af78d1583db0240fdfbcecb8305f0cfc2a254da5933322 through an app-only refresh/restart, without replaying SDK captures or creating new fixtures/accounts. Exact backend/migration/harness identities, original 85+new 3 events, issue 71/comments 26/activity 39, eight native HTTP response bytes, session/configuration/volumes and migration 012 receipt/FKs stay unchanged. The earlier actual SDK acceptance remains explicitly tied to image 93b and preserved data; no fresh SDK run on394f is claimed. Primary independently fetched all eight responses and four actual served assets and inspected the running non-root 65532 image. Runtime audit verifies Go 1.27.2, exact MIT notice/CA roots and no Node/build tools. Image metadata 25437974 bytes is not RSS evidence. Reports: ignored test-results/container-triage/{report,runtime-audit,primary-verification}.json, with prior 93b evidence retained. Helper checksum decb0e2cb5037a53b453814295ba7c3974b0932c544f9d40a584fc08fc25747b. Final public source/CI and deployed landing verification remain open.

- 2026-10-15 UTC: Native assignment/comments/activity source 23f4b97d2144ab6435063d47e2b73f38c0cf2822 is published on main with reviewed tree ee2b26d673943f79b12ab80017ce1999e7401287. A private-index commit guard verified exactly 20 changed paths, 178 complete tracked files, no file deletions and final asset identities before push; primary independently matched GitHub's public ref/commit/recursive tree to all 178 local paths. CI 38696612235/job116294535684 succeeds for formatting, vet, real PostgreSQL integration/race and image build; primary inspected completed steps and downloaded the log. Pages 38696612114 package 116294534397/deploy 116294582235 succeeds for this source. Deployed landing 6/6 files are HTTP200 and byte-identical, including index 9645 bytes/SHA efa5823f497768cb958f2c0b5b6fbcbd7eae6f4ea4c90554a16915be4443ed60; real Chromium 320/768/1440 verifies links, skip/tab/anchors, exact clipboard with retained focus and actual product image, without errors. Primary inspected the actual report/manifest and desktop capture. Evidence: ignored test-results/publication/triage-public-tree.json and landing-triage-deployed/{report,asset-sha256}.json. The final temporary 18006 QA process stops after acceptance; original 18001/18004 and final image 18080 remain. This closes only native assignment/comment/activity/simple regression. Release resolution variants, conditional mute, merge/unmerge, compatible management and every other unchecked product capability remain required.

- 2026-10-15 UTC: Native conditional mute local acceptance is complete; publication/remote CI/landing remain open. Additive013 stores positive int32 minute/count conditions, receipt-clock UTC deadlines and occurrence baselines without rewriting legacy rows or001..012 receipts. Native PATCH has exact null/omission/integer admission; same active policy preserves deadline/progress/history, distinct late captures count, transactions/retries do not. Ignore suppresses only new issue frequency deliveries; raw project window counts and existing queue/history remain. Store suppression first fails with one unwanted delivery; HTTP first fails400 Unknown field:ignore in0.374s, then passes combined mute/triage1.602s and final mute-only0.576s. Configuration invalid-input REDs pass after typed interval default1/range1..3600 admission. PostgreSQL upgrade/reopen preserves old indefinite issues and history.
- 2026-10-15 UTC: Fresh review found a real drain race: a stale joined condition renewed before the issue claim's fresh recheck returned false, making the worker idle despite other due rows. Public PostgreSQL18 SELECT documentation confirms named-table locking leaves other joined reads under their statement snapshot. A real natural-concurrency overlay first witnesses false with3958 immutable other due issues in0.867s. The primary changes the internal bool to claimed processing progress, including renewed candidates, and preserves fresh recheck/transaction/error semantics. The exact retained regression also fails against an overlay restoring only the old return, at claim4 with3997 still due in0.456s. It passes500 required claims over4000 fixed other deadlines after repair. Baseline interleaving sensitivity is naturally nondeterministic; no deterministic trigger or exhaustive concurrency proof is claimed. No production clock/callback seam was added. Logs: ignored test-results/verification-ignore/drain-retained-red.log and temporary /tmp/goodsentry-ignore-drain-proof.json. Final store test SHA99767289c0b3f3dca96ae91ab1ba57190d74e1a58ec67402a1d6a97224cb5458.
- 2026-10-15 UTC: All main-module Go packages pass integration and race, with vet/format/syntax/diff checks clean. Initial local go test ./... encountered an ignored test-results entry listed by scandir but returning ENOENT to stat/lstat; the underlying filesystem cause is unproven. Explicit cmd/internal roots cover all11 main-module packages; tests/compat is a separate SDK module. Main-module complete results include server30.643s/store60.428s native and server31.405s/store48.077s race before the worker-only progress fix. Final affected-package reruns pass store59.242s/jobs2.034s native and store51.980s/jobs3.106s race; vet all main packages passes. The running worker test observes a real blocked activity insert, cancels through production stop and verifies full status/policy/history rollback, then persisted catch-up after reopen. Logs and exact repository/package inventory are in ignored test-results/verification-ignore.
- 2026-10-15 UTC: Official Node SDK11.7.0 native acceptance acknowledges six real transport sends for four unique captures, preserving same-policy deadline and duplicate counts, counting a one-day-late occurrence once, ending the OR condition by count with one system activity, retaining raw latest event and accepting indefinite mute. A separate native receipt-clock duration proof moves only that own persisted deadline into the past: the actual main worker expires it without another capture while uptime/logs are disabled. The81 actual browser captures at320/768/1440 cover27 mute,27 existing triage and27 general UI states. They verify combined deadline/remaining, count and duration System activity, genuine400/newer in-flight drafts, navigation, comment drafts, indefinite clear and responsive layout, with no unexpected errors. Primary inspected actual duration mobile and combined desktop images plus reports/manifests. JS c2d2edfa79cd99adef48b8ecc395b48571ad6fb695d3a228ecca2249ffef5be0, unchanged CSS ac3b1d1f3c95dc2837422f1d9dfef050b347902f02160ce7c20a3afb10b9f077; browser harness d55ac90bc1d2117f730123b8c4162558af50795cea3324368ee9f42c5f6ea66d; manifest test-results/ignore-browser-manifest.json SHA9da88e77aec36fd117310328b4d8d0c036037e63647c3767768f58fd30f18c7d. Browser/native SDK evidence precedes only the separately verified worker-progress return repair; final-image SDK below exercises the final backend. Temporary18007 QA stops cleanly; original servers and18080 remain.
- 2026-10-15 UTC: Actual final image f3c74764a29425e6ed0a39bf10ae721dbeaa5dd986b8273e7e6f7944eefd550e contains final backend digest5aadf20082a97f06b95a9bf66b3764af7ddfb8a52e5a7890a3b98eeb22bd093b and exact served assets. The first a0 conditional-mute image is preserved as historical evidence; it received no SDK captures before the worker repair rebuild. Final official Node11.7.0 harness e504e5f4154d42f929d7264e37b6ec4fc5d35a894b243a5087cb627727b0dadc passes4 checks/6 acknowledgements/4 unique events in own project40/issue74, ending ignored indefinitely with count4 before native acceptance actions. A120-minute/count3 policy then preserves deadline, baseline4, remaining3, raw telemetry and byte-identical responses from four HTTP endpoints through app-only restart. A stopped-app fixture moves only issue74 until into the past; startup maintenance records exactly one new duration unignored activity45, null user/no eventID, remaining3 and no regression or new capture. All88 original+4 new events,20 spans,15 operations,4 maps, legacy issue71/comments26/activity39/assignments/FKs, canceled delivery history, revoked token5HTTP401, original cookie/CSRF, volumes/config and001..013 receipts stay unchanged. Primary independently fetches11 exact native/legacy HTTP responses, all4 served assets, actual non-root65532 Docker identity and SQL snapshot hashes. Runtime audit confirms Go1.27.2, MIT notice/CA roots and no Node/build tools. Image25472046 bytes is metadata size, not RSS. Evidence: ignored test-results/ignore-sdk-image/report.json and container-ignore/{report,runtime-audit,primary-verification}.json; helper SHA1e8d2c49c36d277db0c231f841c7e6d0c7d937b8646eb8f4b152368c640c186a. Remote publication acceptance remains open.

- 2026-10-15 UTC: Native conditional mute is accepted and published as7e05cdc8e8a1b817ddb2e667abfaea331ed83322/tree80e9467ef2161984c87467558317ca5d5a4efa44. Private-index commit proof verifies exactly22 changed paths,186 tracked files and no deleted files; primary independently matches every public blob path/mode/SHA and main/local/origin identity. CI38703271640/job116316356345 succeeds for formatting, vet, all PostgreSQL integration/race tests and image build; primary inspects completed steps and downloaded log, including server27.910s/store33.441s. PostgreSQL cancellation/projection_guard log entries belong to intentional existing rollback tests. Pages38703271498 package116316355911/deploy116316412219 succeeds for this exact source. Actual deployed6/6 files are HTTP200 and byte-identical; index9661bytes/SHA6a2efbc6708ddb985d88a87c5efbe15b40d43207c3e7aca2926d9f7981d96f77. Real Chromium320/768/1440 verifies16links, skip/anchors, native clipboard with retained focus and actual product image without errors. Primary inspects actual report/manifest/desktop capture and independently refetches all6files. Evidence: ignored test-results/publication/ignore-public-tree.json, publication/job-116316356345.log and landing-ignore-deployed/{report,manifest,primary-verification}.json; landing manifest SHA2ecb83da3ab4d7a1bad7f1cb96c67aeca78d9395ab7b2a0ee2d8c8226268f146. This closes only native conditional mute. Release-anchored/next-release resolution, merge/unmerge, compatible API0/window/user conditions and every other unchecked product capability remain required. No registry image or verified RSS claim is added.

- 2026-10-15 UTC: Native release-resolution implementation is integrated but acceptance remains open. Pure releaseorder owns grammar/precedence/classification; additive014 stores immutable chosen/pending/bound references, exact native PATCH and issue-scoped candidate pages feed the original UI. Ingestion now retains issue→rules→release→outbox ordering and evaluates regression before notification eligibility. Owning policy RED0.468s (nil policy) and HTTP RED0.421s (unknown resolution field) pass after integration; focused14 PostgreSQL cases pass11.348s, HTTP five groups0.773s and running maintenance/lifecycle3.495s. The worker test also fails5.313s against the previous mute-only loop. Primary inspects target/no-op, semantic/created fallback, finalization, scope and real cancellation assertions. The initial primary release-lock wait used cached activity observations inside its test transaction; clearing PostgreSQL's statistics snapshot makes the real cancellation/retry proof pass0.444s, without changing production. Full native main-module suite passes, including server31.058s/store69.144s/jobs5.553s; vet/format/JS syntax pass. Local package scope remains ./cmd/... ./internal/... because the ignored test-results directory still contains an unreadable entry; no filesystem cause is claimed.
- 2026-10-15 UTC: The original release-resolution UI passes five actual Chromium groups and30 captures at320/768/1440, including an ordinary member,25+1 issue-scoped candidates, archived choices, exact255-codepoint unsafe/astral release text, a real400 retaining drafts, held real PATCH and stale navigation, metadata-only binding and immutable references after deletion. Primary inspects the actual report and mobile exact-release/desktop regression captures. Assets are app.js b2b3c4cb887e58cfc93c6078de362d0530b1683e835c2a94ef163be00fcf37b8 and app.css29ca23580d65fcffd6ae2771d23b065702a2a763fb4624f229276fadd2ad2b74. Official Node SDK11.7.0 passes ten acknowledgments/eight unique events for late/older/build-equal/newer/retry/next/invalid/missing release behavior and original raw-event preservation in its own native project16/issue77. The first driver attempted a nonexistent separate events endpoint only after all five behavior groups passed; the corrected driver reads the existing detail.events response. Its failed report/eight prior captures are preserved separately rather than relabeled. Evidence: ignored test-results/resolution-browser and resolution-sdk-native{,-driver-failure}. Final-image, race, publication and deployed-site acceptance are still pending.

- 2026-10-15 UTC: Final local release-resolution acceptance passes. Full main-module race suite passes server31.781s/store58.472s/jobs5.603s; the sole later Go change expands the existing overlap test to a second independent Store/pool, then passes native0.518s/race1.462s. Integration-tag vet and syntax/format checks pass. A bounded fresh review finds no approved-scope defect in policy/no-op/binding/scope or issue→rules→release→outbox lock ownership; primary inspects the implementation and assertions. Unchanged mute/triage/general browsers add7/7/14 groups and81 captures to the new panel's30, with zero unexpected errors. The first mute regression's Docker fixture mount EACCES is retained separately; changing that mount path resolves the driver precondition without changing source. Reports and hashes are in ignored verification-resolution and resolution-regressions-manifest.json.
- 2026-10-15 UTC: The final single app image is420c4249be78c856188c72cf9edf917b2ff8bb81b14a0e293a687de5249f1c8b,25,550,002 Docker metadata bytes, not RSS. It contains the reviewed93-file backend digest eb3076b47bd7304b9e83f6e8172a232204fbaba1c98e9149784965c4f63db620, exact embedded assets, Go1.27.2, CA/MIT notice and nonroot65532 with no Node/build tools. Additive014 checksum cdf41b82ba660941ce6f4cdf30335a4dab46c02082460dd66f3d328732f59620 preserves all92 old events/68 issues,20spans/15operations/4maps, issue71's26comments/39activities, issue74's prior catch-up state, old assignments/mutes, canceled zero-attempt history, revoked token5, original session/CSRF/config/volumes and001..013 receipts. Official Node SDK11.7.0 adds only eight unique events from ten acknowledgments in own project41/issue78, leaving100 total. Chosen references/receipt/history survive actual app stop/start. A release precreated through API0 becomes eligible when only its date_released is set to the database clock while the app is stopped; startup maintenance binds it without a new capture, with one new System activity58 and no false regression. This SQL recovery fixture is not claimed as CLI execution. Primary independently rereads22 native response hashes, all4 assets, actual Docker identity/config/volumes, raw SDK IDs and latest JSON hash, all14 receipts, immutable old SQL snapshots and final resolved/next-bound/count8 state. Evidence: ignored container-resolution/{report,runtime-audit,primary-verification}.json and resolution-sdk-image/report.json. Native18008 exits cleanly after acceptance; final image18080 remains active. Exact public tree, remote CI and deployed Pages acceptance remain open before closing this child.

- 2026-10-15 UTC: Native release-resolution is accepted and published as1da8adcc78e06d39669dd7c933e75858631f4e02/tree19eba65001098ce3e963c2324566da0ec4bb705f. Private-index proof verifies exactly24 changed paths,198 tracked files and no deletions; primary independently compares every public path/mode/blob SHA and local/origin/main identity. CI38713113170/job116349155868 succeeds for formatting, vet, complete PostgreSQL integration/race tests and image build. Primary inspects completed steps and the downloaded log, including server32.732s/store40.511s/jobs5.798s; projection_guard messages belong to existing intentional rollback tests. Pages38713113132 package116349155928/deploy116349231812 succeeds for that exact source. Actual deployed6/6 assets are HTTP200 and byte-identical, including index9674bytes/SHA6a8b4bd7bf120523f3dcf54d091591cfb5b90c2f56bab89a442776800566da57. Chromium320/768/1440 verifies16links, anchors/keyboard, native clipboard with retained focus and the product image without errors. Primary inspects the actual report/manifest/desktop capture and independently refetches all6files. Evidence: ignored publication/resolution-public-tree.json, publication/job-116349155868.log and landing-resolution-deployed/{report,manifest,primary-verification}.json; landing manifest SHA61f4db14bf8e5e604ed7b3bca294475c3d3764f5cc0527c6499dc363f13d375d. This closes only native release-resolution. Merge/unmerge, compatible API0 issue management/scopes/windows/user conditions and every other unchecked requirement remain mandatory. Registry publication and RSS verification remain unclaimed.

- 2026-10-15 UTC: Native merge/unmerge design is recorded in SPEC.md and published as5981c3e; matching CI38717872928 succeeds. The first foundation,015/private receipt_count, separates mute progress and first_seen admission from historical issue totals without changing the DTO or adding queries to the capture path. Its owning PostgreSQL RED reproduces count100 incorrectly yielding remaining0 instead of3; the separate upgrade case is explicitly a missing-feature RED42703. Final focused GREEN passes1.274s, including historical count100/0, no-op/assignment, duplicate/transaction exclusion, exact expiry, one first_seen and a new policy after historical count3 diverges from progress5. An isolated Go overlay restoring only the wrong baseline source fails on the first new-policy capture0.708s, confirming the review-driven coverage gap is closed; this negative control is not relabeled as an old product bug. The upgrade fixture captures7 errors, mutes8, then captures3 more before reconstructing the absent015 column/receipt. Open backfills10 and preserves remaining5, every historical table row/column and001..014 receipts; a fresh capture and reopen preserve progress11 with historical count0 and remaining4. Full main-module integration passes server31.143s/store74.841s/jobs5.364s; full race passes server31.636s/store61.928s/jobs5.786s; integration-tag vet, format and diff checks pass. Local scope remains ./cmd/... ./internal/... because of the previously recorded unreadable ignored test-results entry, with no new filesystem-cause claim. Primary inspects worker tests/diff and repeats the separate PG18 composite-FK/partial-index proof in a rolled-back schema; that proof does not establish an implemented grouping writer. Evidence: ignored test-results/verification-merge/capture-progress-*.log and primary-fk-proof.log. Only the counter foundation is closed. Native merge/unmerge operations, API/UI, SDK/browser/final-image upgrade and publication acceptance remain open; the100-event image baseline is not replaced by this foundation.

- 2026-10-15 UTC: Foundation015 source b39fb264/tree50ab267 is published; primary verifies all200 public paths/modes/blob SHAs and clean local/origin/main identity. Remote CI38719918426/job116371655994 fails only the existing TestIssueMaintenanceCatchUpAndStop at its final restart wait; server30.869s/store43.081s pass, image build is skipped, and the failed log remains preserved. Twelve focused race repetitions pass6.314s, so primary investigates instead of rerunning CI to hide the failure. Installed pgx5.11 documents asynchronous cancellation cleanup; a separate isolated observation sees cleanup pending and a skipped row in30/30 attempts without transport delays, but does not claim to reproduce the original test. Primary then runs that unchanged test through a local PostgreSQL proxy delaying only CancelRequest750ms: it reproduces the exact final wait failure5.592s. The test had treated Go worker join as synchronous database rollback. Add a bounded real SELECT FOR UPDATE barrier after releasing the activity blocker, before comparing canceled state or starting the next hour-idle SKIP LOCKED worker. This waits for actual row-lock release, retains all rollback/history assertions and the unchanged5-second startup deadline, and adds no production seam or product behavior change. The same delayed-cancel test now passes3/3 in4.349s; complete jobs passes4.893s and jobs race5.588s, with vet/format/diff checks clean. The diagnostic proxy is stopped. Test-audit ownership is the existing cancellation/restart lifecycle proof; the credible regression is assuming server cleanup completed on goroutine return, and no other owner observes that transition. Evidence: ignored verification-merge/capture-progress-ci-worker-*.log, goodsentry-pg-cancel-proxy.go and goodsentry-cancel-lock-observation.{go,json}; publication/job-116371655994.log. Matching repaired-source remote CI remains pending. The detail refactor is paused until this failure is closed.

- 2026-10-15 UTC: The rollback-test repair is published as68e294b995f9e7eca9de18096225ff448142cf28/treeaa480602404c7676b6021aa9a55688508cd2318f. Primary verifies every200 public paths/modes/blob SHAs and clean local/origin/main before resuming work. Matching CI38721246271/job116376061422 succeeds for format/vet, full integration/race and image build; primary inspects completed steps and downloaded log, including jobs5.946s/server30.142s/store40.748s and image58e6774316d67b06083f28967f3e6c764436eb169c444777282a40d8afbfcf5c. Expected database lock/cancel/projection-guard errors belong to intentional tests; the job has no failing tests. Public-tree and run/job evidence remain under ignored publication/capture-progress-fixed-* and job-116376061422.log. This closes foundation015 publication, preserves the failed first CI evidence and resumes the specified coherent-detail increment. No site assets changed, no runtime100-event baseline was upgraded and no registry/RSS claim is added. Native merge/unmerge remains open.

- 2026-10-15 UTC: Coherent detail is consolidated in Store.IssueDetail: current membership/role, issue state, latest50 event summaries/raw latest and first25 comments/commentTotal use the existing readonly repeatable-read transaction. Server consumes the same JSON shape through that result; obsolete Issue/Events production methods are removed after all callers migrate. The shared comment reader has two real production consumers and retains ordering, empty arrays/raw{}, counts and moderation. Existing assertions remain unchanged when extracting result.Issue in store and worker tests, including the proved rollback barrier. The new owning test uses40 timestamp-ordered distinct captures, two independent Store pools and468 real concurrent reads; initial focused GREEN is1.790s with411 before/57 after commit observations. A temporary overlay weakening only the real read helper RR to ReadCommitted fails0.529s at count1/events2. This deliberate negative control is not claimed as a preexisting API or product RED. Test-audit protects this distinct detail consistency contract, with ordinary public-call channels and no productive hooks/exports. Primary inspects source, worker diffs/logs and all caller migration; bounded simplifier/review finds no approved-scope defect. Full main-module integration passes server30.024s/store73.112s/jobs5.211s; full race passes server31.893s/store62.319s/jobs5.659s; integration-tag vet/format/diff checks pass. Evidence is ignored verification-merge/issue-detail-{focused,readcommitted,full,race,vet}.log. Only the coherent-read foundation is closed locally; its public-tree/CI acceptance is pending. Alias canonicalization, groupSize, migration016, grouping writers, origins/combined histories, API/UI and complete native merge acceptance remain open. No site assets, runtime100-event baseline or registry/RSS claim changes.

- 2026-10-15 UTC: Coherent detail is accepted and published as18b59d0cd4efe8ef3fed19ce07c7237b78f0a855/tree74f4912aae1101432ceb955af6cc50ab56440e2f. Private-index proof records exactly11 changed paths,202 tracked files and no deletions; primary independently verifies every public path/mode/blob SHA and clean local/origin/main identity. Matching CI38722828509/job116381344014 succeeds for formatting, vet, full PostgreSQL integration/race and application image build. Primary inspects the completed steps and downloaded log, including jobs5.704s/server28.938s/store47.699s and CI-only image0ba0cec2abd8c19ad187a3a94f366d3594a28fa8f82978ea230c4d80b1bd4ee1. Evidence: ignored publication/issue-detail-{public-tree,runs,run}.json and job-116381344014.log. No landing assets changed, so this backend foundation does not trigger a new Pages deployment. The100-event acceptance image is not replaced and registry/RSS remain unclaimed. Focused preparation for016 also identifies valid old unowned message rows and legacy roots without matching raw counts; SPEC now explicitly preserves these and validates only topology/existing ownership. Native merge/unmerge, compatible API0 management and every other unchecked goal requirement remain open.

- 2026-10-15 UTC: Before grouping implementation, the primary fixes typed Store result/error contracts and exact merged/unmerged activity JSON in SPEC. The per-project topology key includes schema identity; preflight rejects active-state aliases while preserving valid unowned history and counter divergence. Execution ownership is disjoint: primary Store/migration/integration, bounded HTTP route/transport worker, owning PostgreSQL test worker and original UI worker. No worker may expand scope, delegate or commit. This is a design checkpoint, not implemented merge behavior.

- 2026-10-15 UTC: Native grouping is implemented locally with migration016 preflight/composite topology constraints, a schema-scoped per-project shared/exclusive gate, descending row locks, immutable event origins, summary recounts and unchanged receipt progress. Merge retains the lowest root's policy and assignment, clears absorbed active policies with previous-state audit, and supports subset/root/all separation and lost-response no-ops. Alias reads, combined original comment/activity history, strict session/CSRF transport, origins25+1 and stale-write409 are wired. HTTP feature RED405 instead of401 passes0.830s after routing. A real empty-partition RED0.366s exposes stale retained dates/text on its first fresh capture; the fix initializes current summaries when count=0 while first_seen activity still uses receipt progress. Ten owning PostgreSQL tests pass11.529s, including raw/history/policy preservation, original partitions/future default/custom routing, current membership, real two-pool waits/cancellation, concurrent capture/triage/split, valid legacy alias upgrade/reopen and seven atomic invalid preflights. Public worker controls use explicit inconsistent SQL fault fixtures after Open; they prove alias skipping with root positive controls, not a natural topology race after a row wait. The primary inspects the fresh topology guards in the productive workers separately. Full main-module native integration passes server32.755s/store89.818s/jobs5.347s; subsequent final owning extensions pass the focused suite, and full final-source race passes server32.802s/store85.824s/jobs5.971s. Vet/format/diff checks pass. Local ./... remains unavailable because of the previously recorded ignored-directory walker error; ./cmd/... ./internal/... covers all12 main-module packages. Native SDK11.7.0 sends8 unique events and2 retries through its unchanged transport; primary independently verifies the final two count4 groups and raw hashes over HTTP and SQL. Grouping Chromium acceptance passes6 workflows/33 captures at320/768/1440, with only the intentional409; primary inspects actual desktop/mobile screenshots and manifest. The first browser run's locator failure is retained, corrected only in the new harness and rerun with new projects. Evidence: ignored verification-merge/groups-*.log, grouping-sdk-native/{report,primary-verification}.json and grouping-browser/{report,manifest}.json. Existing browser regressions, image upgrade/restart and exact publication/CI remain open. Native grouping and the overall parity goal are not yet closed.

- 2026-10-15 UTC: Automatic approval review rejects starting the new runtime on the preserved native QA schema because startup migrations would change that protected database. No process starts on it. The primary instead creates fresh isolated schema goodsentry_groups_acceptance_9ed22a0c9cf8 and new0600 owner/member fixture for18009. The100-event image remains unchanged, independently read as100 events/20 spans/15 operations/4 maps, migration014 and bound issue78. A read-only pg_dump and read-only upload-volume copy restore into a new database and new volume for candidate-image acceptance. Backup SHA256 is1b0ce4b21ce6aa5a6d9087ab3f9d4ffe678f3a335e5d452fef41d6237b729404. This avoids mutating the protected original; it does not claim that original was upgraded. The isolated copy's database URL, listen port and public domain must differ intentionally. No registry publication or RSS measurement is claimed.

- 2026-10-15 UTC: Existing resolution_browser.mjs passes unchanged against the fresh native QA fixture, with5 checks/30 captures at320/768/1440 and only the intentional400. It covers candidate pages, exact versions, saved pending/bound policies, ordinary-member access and draft/navigation guards. It does not open View trace or Original event JSON; triage/issues browser suites are not rerun because their established fixture fields are absent, and no old fixture is reset. Primary independently hashes all33 grouping screenshots and the report, inspects desktop/mobile output and compares all4 served assets directly to source. Add two owning TraceErrors reads to the existing partition test to verify current issue links while merged and after full separation. The case passes0.932s and race1.565s. An intentional overlay reading original_issue_id instead of current issue_id fails at the trace link comparison; this is a deliberate negative control, not a claimed old product bug. Existing grouping SDK harness initially rejects copied-image port18081 before any HTTP write or SDK capture; preserve that guard RED and admit only isolated ports18009/18081 while retaining loopback, private fixture and DSN-origin checks. Production source and candidate image are unchanged by these test-only additions. Evidence: ignored grouping-resolution-regression and verification-merge/groups-trace-*; container-groups/sdk-port-red.

- 2026-10-15 UTC: Native grouping is published as a9a23f27301ef76bdb6e3a8843674ed692b04204 with tree b482be1538e09d340d8aab3549a90dc7e6d69fdc. The private-index commit records exactly22 changed paths,212 tracked files and no deletions. Primary independently verifies all public paths, modes and blob SHAs, matching local/main/origin and a clean tree. Matching CI38729441627/job116403293730 succeeds for format, vet, full PostgreSQL integration/race and image build. Primary inspects the completed steps and downloaded log: server31.075s, store53.129s, jobs5.654s and CI-only image2fe56f65e11508efea7d6c160a7045161d40406736437fe8aadfbbe81717f77b. All6 deployed landing files remain byte-identical to source, so the unchanged site requires no new deployment or browser run. Evidence is under ignored publication/groups-feature-*, groups-site-unchanged.json and job-116403293730.log.

- 2026-10-15 UTC: The single runtime image57c433c0c8cb3063c529154419bc65bd14cc5ef813c444463231d7103a954848 passes upgrade and restart on the isolated copy of the100-event baseline. Its size is25,668,922 Docker metadata bytes, not RSS; the actual process is UID65532, the Go1.27.2 binary is static and the MIT notice/CA certificates are present. The immutable014 checkpoint contains36 tables and665 rows. Migration015 backfills counters once,016 applies with matching checksums, and every old row and001..014 receipt remains unchanged. Original issue78 keeps its bound next-release policy69/count8;71/74, assignments, comments, activity, maps, spans, operations, canceled delivery history and copied session/CSRF persist. The copied upload volume was empty and remains empty; all4 source maps are preserved in PostgreSQL. Actual Node SDK11.7.0 adds8 events with10 acknowledgements in its own project42 and proves merged/future/retry/split routing. The same final container restarts with108 events,20 spans,15 operations and4 maps; its new issue86 release policy and issue87 standalone state remain intact, and the original session/CSRF accepts a no-op. Primary independently compares every original table against the saved checkpoint and checks its old row-hash multiset remains present in the copy, then verifies both SDK group states/raw hashes, all4 served assets, original configuration/mounts and revoked token5 HTTP401. The protected original stays on its previous image with100 events. The copy intentionally uses a new database/volume, Docker peer address172.17.0.2:5432, port18081 and domainhttp://localhost:18081. Initial host networking was healthy inside Docker but unreachable from WSL; primary reproduces ready200 inside that network, then authorizes replacing only the new container with bridge networking and an explicit localhost port. The failed readiness attempt and driver errors for Docker top PID/temporary-directory reuse remain preserved; no checkpoint is reset or overwritten. Evidence: ignored container-groups/{report,primary-verification,runtime-files-primary}.json and grouping-sdk-image/report.json. Acceptance script SHA256 is0fb05d3af6474361b65e758bb414f9ede367858ee838450ddab5e30f1957f175; its report SHA256 is5807eb9d1175ddd9874b6b110dd86f896c99c8d6a40c0121f30a6c29df7810b0. Native grouping acceptance is closed. Compatible API0 grouping/issue management, every other unchecked capability, registry publication and measured RSS remain required.

- 2026-10-15 UTC: The previous goal turn is verified functional progress: native grouping source/acceptance is public at1b2bff0 with212 paths and a clean tree. Closing-documentation CI38730555968/job116406985112 remains active on the first authoritative poll. Next work researches only public Sentry docs/OpenAPI and GlitchTip public OpenAPI, plus own repository symbols. Public documents distinguish event scopes for issue paths from project scopes for project-event paths, and GlitchTip retains global legacy issue URLs. Primary verifies those contracts and defines the event-inspection vertical increment in SPEC before build. Public sources disagree on some mutation details and do not define hashes DELETE as unmerge; no guessed private behavior is accepted as fact. Those mutations/hash/window/user/commit requirements stay open. Public JSON evidence is under /tmp/goodsentry-{api0,glitchtip}-public-*.json; no upstream server code, tests, internal schema or assets were read.

- 2026-10-15 UTC: Compatible event inspection is locally accepted; exact publication/CI remains open. Seven exact GET routes cover project events, global/organization issue events and the organization legacy JSON view. Migration017 changes only the scope constraint; event:read is opt-in and issue-scoped, while project:read follows documented project-event reads. Current membership, project grants, alias topology, release association, raw records and siblings share one readonly RR snapshot. Native transaction tag syntax remains unchanged. This increment leaves all compatible issue listing/mutation/merge/hash/window/user/commit work mandatory.
- 2026-10-15 UTC: Owning tests pass: pure projection, six PostgreSQL cases including017 row/catalog/receipt preservation and real two-pool merge/split, plus strict HTTP/Links/auth. Missing-route RED is401 expected versus404 before registration; missing pure module is explicitly compile RED. A deliberate ReadCommitted overlay produces lost partitions/not-found; definitive RR passes6.263s with unchanged productive hash. A deliberate first-exception overlay detects wrong metadata. Fixture setup errors are preserved separately. Full13-package native suite passes server35.078s/store110.627s/jobs6.093s; full race passes server34.181s/store96.189s/jobs6.992s. The final native-versus-event transaction search consumer assertion passes0.634s and race1.510s. Vet, format, JavaScript syntax, reproducible Tailwind and diff checks pass. Evidence: ignored verification-api0-events and verification-events-store.
- 2026-10-15 UTC: Official Node11.7.0 SDK acceptance passes three actual exceptions plus one transaction,4 acknowledgements and23 compatible reads with limited credentials in own project47/issue100. Primary independently verifies all4 raw SQL payload hashes and current ownership, native count3, compatible routes/JSON/siblings and distinct project/event scopes with newly issued credentials before and after restart, then revokes them. Initial SDK attempts confused default streamed spans with static transaction envelopes, then compared raw SDK timestamps to PostgreSQL occurrence precision; only the harness changes, and both failures/captures remain preserved. The explicit static lifecycle is documented by the official SDK migration guide. A browser fixture has an existing session rather than login credentials; the initial login failure is retained and the final harness imports that cookie. Chromium passes3 workflows/9 captures at320/768/1440, only intentional403/401, with no overflow/page errors/request failures. Primary inspects actual mobile/desktop scope screenshots and hashes all9. Evidence: events-sdk-image-third, events-browser-image and container-api0-events/primary-{before,after}-restart.json.
- 2026-10-15 UTC: Single image0205c773dab6bf9ea69247392467d79119de35c7cc9450678e9467636990f759 upgrades a fresh isolated copy of the accepted108-event/36-table/689-row baseline through017 and restarts the same containerac90667224daf1fa90e59e422a9d3576768c58b1d8aff23f8b0e093f97014836. Every old row/receipt/native response/session/CSRF/upload and revoked token5HTTP401 survives. Protected original100-event and grouping108-event runtimes/configuration/volumes remain exact. Final copy has119 events (including retained earlier SDK-attempt captures),22 spans,17 operations,4 maps and17 migrations. Actual UID65532, static ELF, CA certificates and exact MIT notice pass; binary SHA1687b805992c12793a6c1f22194721b56911e2b0bdd09e28204c130210075e70. Image25,773,893 bytes is Docker metadata, not measured RSS. An acceptance-helper readiness URL error is corrected to actual /readyz and resumed on the same new container without resetting references. Report: ignored container-api0-events/report.json, with independently verified SDK SQL data and protected source row multisets. Public registry and RSS requirements remain open.
- 2026-10-15 UTC: This increment uses public API docs/OpenAPI and permitted official SDK client material. A public search incidentally returned an unrelated Sentry frontend platform-category snippet after production implementation/testing had frozen; it was discarded and informed no change. No upstream server repository or file was opened or copied. The earlier native-grouping closing-documentation CI38730555968/job116406985112 is now authoritatively successful.

- 2026-10-15 UTC: Public tree4cfcfe5/226 paths and CI38735979006/job116425263022 pass, but final schema review finds an in-scope wire defect: SDK request header objects need public pair arrays plus required nullable inferredContentType, and numeric breadcrumb times need public UTC dates. Reachable public Request/APIEventBreadcrumb contracts now explicitly own these conversions in SPEC before correction. The read increment remains open. Existing captures and all protected checkpoints stay preserved; no server storage or normalizer change is authorized by this projection fix.

- 2026-10-15 UTC: Focused public-reference audit confirms Request and APIEventBreadcrumb are reachable event response schemas, while EventUser/ClientSDKInfo are not (event responses allow free-form user/sdk). Primary corrects that inference in SPEC before accepting any extra conversion; the fix remains request/breadcrumb only. No other concrete valid-SDK shape defect is found. A malformed optional processing-error name remains a recorded nonblocking edge.

- 2026-10-15 UTC: Nested request/breadcrumb correction passes real RED against4cfcfe5, then pure GREEN0.013s, with a separate wrapper RED and UTC year-bound cases. Headers/query map to valid ordered pairs; inferred MIME is required nullable; numeric timestamps use exact decimal digits, bounded UTC years and nearest-nanosecond rounding. Breadcrumb wrapper emits only values. Unknown breadcrumb fields remain extensible; User/SDK/Legacy/storage remain intact. Full final13-package native passes eventview0.019s/server42.807s/store119.930s/jobs6.633s; race passes eventview1.102s/server38.185s/store101.957s/jobs9.213s. Vet/syntax/format/diff pass. Only two productive paths differ from the first accepted image: eventview/events.go and new nested.go.
- 2026-10-15 UTC: Actual SDK HTTP integration passes on final imagef8cae96c58b7d63ee7cd5768e95bfe518ca395dc67903cf5efbba42322a71d86. A real local POST and nested GET generate one error plus automatic/manual breadcrumbs in project49/issue103, SDK11.7.0 ACK200. Object headers, duplicate query and numeric breadcrumb instants project to public pairs/MIME/UTC; LegacyJSON/native payload SHA53be7595bec7c2ecd1abc6d1d5c247403c6549b0b1ee6137800efc26b2fa410a stays exact. The SDK does not attach request.data although the actual server consumes a body; no real-SDK body projection proof is claimed. Pure tests cover arbitrary retained body data. Harness/token/server cleanup passes. Evidence: events-request-sdk-image/report.json SHA1c5f3d7e81446d22a331868f9663726fdcf75e327bd975995bb101a5843c8888 and run.json; harnessSHAdf463782523129e5692b7a26a04c98e8c6a69aec1c100fcd05fee65cee3b4377.
- 2026-10-15 UTC: Before refreshing only the owned QA container, freeze its119 events/36 tables/753 rows and preserve the earlier candidate stopped. The final image starts on the same copied database/volume with no row or receipt changes, receives the new real HTTP error and restarts the same final container26cdca46248e20363e2ba8fafab3a869495d180354cd4aeae4575cd89dd46fbd. Every earlier119 event and old row remains; final counts120 events/22 spans/17 operations/4 maps/17 migrations. Both protected100/108-event runtimes remain running with exact rows/configuration/mounts. Final static Go1.27.2 binary SHA38d0d838cf0387c2ad8749089d99f20c406dfcf9abac693d752637ec275234ea, actual UID65532, CA/MIT and unchanged4 served assets pass. Primary independently verifies5 SDK raw SQL hashes, actual public request pairs and2 precise UTC breadcrumb instants, legacy raw shapes, old/new counts3/1 and fresh restricted credential revocation401 after restart. Final25,798,220 bytes is image metadata, not RSS. Existing9-browser-image proof is retained because all UI assets and native credential/auth sources are unchanged; no fresh Chromium run on the corrected image is claimed. Evidence: container-api0-events-final/{report,primary-verification,source-delta-verification}.json. Exact corrected public tree/CI acceptance remains open.

- 2026-10-15 UTC: Compatible event inspection acceptance is closed at corrected source81efc123a454764db52de620cb97d0075e93adbb/treec87be87a0c5b89e625b4a02369808947d97bfa1f. The private-index guard verifies exactly7 correction paths,229 tracked paths and no deletions; primary independently matches every public path/mode/blob and main/local/origin with a clean tree. Matching CI38738533236/job116433929484 succeeds for formatting, vet, all PostgreSQL integration/race and image build; inspected log records eventview1.056s/server28.975s/store40.628s/jobs5.294s. CI-only imagea0a23ee3fe09fd1acc518962acdf5a49b3b5da8f530f8dedae58526490abed90 is distinct from the locally exercised final imagef8cae96c. All6 deployed landing assets stay byte-identical and no new site deployment is needed. Evidence: ignored publication/events-corrected-{public-tree,terminal}.json, job-116433929484.log, events-site-unchanged.json and verification-api0-events-nested/acceptance.json. This closes only compatible event reads; issue listing/detail/mutations, compatible grouping/hashes/window/user/commit behavior and every other unchecked requirement remain mandatory. Registry publication and measured RSS remain open; the overall goal stays active.

- 2026-10-15 UTC: Previous goal turn is verified progress: compatible event reads are public atcbc4542/229 paths, with corrected functional81efc123 CI accepted. Closing-documentation CI38739176908/job116436103911 is authoritatively live on the initial poll (tests complete, image building). Primary revalidates clean current source and public contracts before specifying compatible issue summaries and real native analytics. Factual workers confirm native issue wrappers open their own userID-only snapshots, missing grant-aware user/stats/comment/release reads; no implementation starts before the new SPEC. Public identity inputs exist but precedence/buckets/metadata selection are undocumented choices. GT statusDetails GET and Sentry typed policy fields conflict; SPEC explicitly chooses the Sentry policy wire instead of asserting impossible simultaneous schema conformance. Every remaining capability stays mandatory.

- 2026-10-15 UTC: Compatible issue summaries are implemented under their frozen SPEC contract. Five exact GET routes use explicit event:read, including project issue lists; membership, project grants, alias ownership, comments, observed release associations and all metrics share readonly repeatable-read snapshots. Historical native counts/dates stay unchanged; SQL counts known retained users and matching occurrences, explicit unhandled evidence and zero-filled rolling24h/14d series. Native detail adds only analytics in its existing transaction. No migration or historical rewrite is introduced. API0 mutations/grouping/hash/window/user/commit behavior, actual feedback and every other unchecked capability remain required.
- 2026-10-15 UTC: Owning test evidence distinguishes causes. Missing issue routes fail404 against401 before registration. A real next_release fixture exposes the wire's mistaken next value, then passes after repair. Unicode NBSP-only identity produces17 instead of16 users before the explicit25-codepoint White_Space fix, then passes1.030s. Deliberate ReadCommitted and unnormalized-number overlays fail coherent capture and identity assertions; these are mutants, not original product failures. Two driver defects are retained separately: reused JSON maps kept chart keys after decoding{}, and preexisting whole-response grouping equality included the new clock-dependent analytics. The latter now compares every previous field and excludes only analytics; new owning snapshot checks remain intact. Evidence: ignored verification-issues/{http-wire-initial,http-wire-final}.json, full-go{,-final}.log and verification-issues-store/mutants-and-unicode-result.json.
- 2026-10-15 UTC: Full main-module PostgreSQL tests pass across13 packages (server36.308s/store121.519s/jobs6.457s); full race suite passes (server35.952s/store103.698s/jobs6.509s). Integration-tag vet, Go formatting, JavaScript syntax, Tailwind compilation and diff checks pass. Provider tests own grants/alias/identity/lifetime/filter/release/comment/policy/chart matrices,54 captures beyond the native50-event page and two-pool merge/split snapshots. Pure wire and real HTTP tests own public projection, route admission/defaults and prefixed cursor Links. Primary reviews worker changes and their actual checks. Evidence: verification-issues/{full-go-accepted,full-race-final}.{json,log} and verification-issues-store.
- 2026-10-15 UTC: Official Node SDK11.7.0 passes seven actual error acknowledgments against the final layout image in its own project56/root223, plus historical root228. Numeric42/string42 share an ID; lifetime known users are3 with4 recent events, while the historical group has2 users/count2/chart0. Five compatible route families, restricted event credentials, query defaults/empty values, filters, paging, raw/legacy preservation and revoked401 pass. Primary independently checks all7 persisted SDK identities/timestamps, native latest versus SQL payloads, both API summaries and charts, all raw hashes after reads and SQL revocation. Evidence: issues-api-sdk-layout-final/report.json and verification-issues/primary-final.json. Earlier two successful SDK runs are retained; they are separate projects, not retries relabeled as the final proof.
- 2026-10-15 UTC: Final analytics Chromium passes3 workflows and9 captures at320/768/1440 with real native snapshots, known SDK counts, SVG geometry/fill, accessible24-row UTC data, empty intervals, Resolve/Reopen, Refresh, drafts and navigation. Every chart uses its figure width and places its caption below; no unexpected console/network/page failures occur. Primary inspects actual mobile and desktop captures. Original grouping/resolution scripts separately pass6/5 workflow groups and33/30 captures on prior image052bf190, in a fresh fixture organization with owner/member users; only deliberate409/400 occur. The final layout image changes only chart CSS after that run; those entire regression scripts were not rerun on it. Earlier browser failures remain recorded: inline bars violated existing CSP, then manual screenshot review found DaisyUI figure flex compressed the graph. SVG attributes preserve CSP; a real layout gate fails the old image at320px before the CSS fix. Evidence: issues-api-browser-{image,final,layout-red,layout-final}, issues-regressions and verification-issues/browser-*-run.json. Missing-analytics fallback is source-guarded, not simulated as runtime evidence.

- 2026-10-15 UTC: Final local issue-read image acceptance passes imageadb3719fc20a6164f0ca5fc3d457fd963c446a1c560b26b9a39848eb030bb390, container2ecac89bf04a051253e10b32c7f83d6afea2a057770b7e1c4703e0b2b8864ebd before/after real restart. The new copy began with120 events/765 row hashes across36 tables and unchanged001..017 receipts; subsequent refreshes preserve all127/790 and239/1278 prior candidate event/row totals. Final copy has246 events,22 spans,17 operation rows and4 source maps. Original100-event, accepted108-event and accepted120-event runtimes retain their exact containers/images/config/mounts/rows and remain running. Existing native responses differ only by the new analytics field; old cookies/CSRF and revoked401 remain valid, uploads/maps remain intact. Static Go binarySHA022ce9fe19cdfe3b1fe3b0b62f5cb5a3069422f57312ff45e7be60ab7f26435c runs as UID65532 with CA/MIT files verified. Docker metadata size25,947,395 bytes is not RSS; registry publication and measured memory remain open. Evidence: container-api0-issues-layout/{before,report}.json, primary-final.json and all prior candidate reports. The layout verification driver's inherited phase flag initially rejected start before any stop/run/write; the corrected phase owns its own marker and resumes without data reset. Public source/CI acceptance is still pending.

- 2026-10-15 UTC: Compatible issue summaries/analytics acceptance is closed at source8b54c85b6d0cb4870cbf4dbf3256f8bafa6d2c8b/tree246a66c0643f409cd15bb0982604ccd75170b6ab. The private index verifies exactly26 intended paths,241 tracked paths and no deletions; primary independently matches every public path/mode/blob, main/local/origin and clean checkout. All107 exercised production files match committed blobs. Matching CI38747554716/job116465089604 succeeds for format, vet, full PostgreSQL/race and image build; primary inspects actual log (eventview1.069s/server31.100s/store57.027s/jobs5.625s). The locally exercised imageadb3719f is separate from the CI-only image; neither is claimed as a published registry artifact. The six deployed landing files remain byte-identical. Evidence: ignored publication/issues-feature-{commit,public-tree,terminal}.json, job-116465089604.log, issues-site-unchanged.json and verification-issues/{runtime-source-commit,acceptance}.json. This closes only the specified read/analytics child; compatible mutations/grouping/hashes/window/user/commit, feedback and every other unchecked product requirement stay mandatory. The overall goal remains active.

- 2026-10-15 UTC: Previous turn is verified progress: accepted compatible issue reads/analytics are public ated8caef/241 paths; both functional8b54c85 CI38747554716 and closing CI38748364030 succeed, with actual logs inspected. Current checkout is revalidated clean. Public update research confirms partial-update prose versus contradictory required fields, GT integer merge versus Sentry boolean, and undocumented null/empty/zero/false semantics. The individual-write SPEC makes those choices explicit and leaves bulk/deletion/merge/window/user/commit mandatory. Native wrappers are userID-only and open their own transactions; the primary requires shared business logic and grant-aware authorization instead of post-filtered native calls. No production implementation begins before this contract.

- 2026-10-15 UTC: Native mutation-body extraction is accepted locally as a separate refactor. Primary inspects guards/body/wrapper diff;29 existing native triage/ignore/resolution tests pass before29.044s and after30.211s with no test change. validateIssueUpdate preserves guards, applyIssueUpdate preserves the mutable body and has no transaction lifecycle, native UpdateIssue retains begin/rollback/commit and errors. Authorization is intentionally unchanged at this point. Evidence: verification-issue-changes/native-extraction-{baseline,green}.log and native-extraction-structure.json. The new246-event source clone is prepared separately (1298 hashed rows), with all four accepted runtimes unchanged; no write feature is claimed complete yet.

- 2026-10-15 UTC: Individual-write review catches lost policy intent before acceptance: explicit inNextRelease:false or inRelease:"" must clear only a resolved condition, while null selector values convey no concrete selector. SPEC now preserves that distinction with CompatibleIssueClearResolutionPolicy. The initial false/empty normalization could clear a conditional mute and requires correction plus regression evidence. HTTP first-run fixture expectations for native IssueDetail envelope and pinned Go1.27.2 ServeMux307 were corrected without product changes; missing-route405 RED remains distinct.

- 2026-10-15 UTC: Individual issue writes pass local acceptance. The shared writer explicitly uses ReadCommitted and materializes the issue and analytics before commit. The new scope adds no capability to existing tokens. Eight owning PostgreSQL tests cover current grants, real assignments, policy lifecycles, cancellation, two independent pools and every pre018 row/receipt. Parser and HTTP tests pass. The false/empty resolution-selector bug has real HTTP RED followed by GREEN; absent-route405 RED is separate. A disabled project-grant filter is caught by the tests. Disabling only the outer ReadOnly guard is an equivalent mutant because the shared transaction guard still rejects it; it is not claimed as detected. Existing native focused tests pass in store29.760s/server3.109s. Full integration passes all13 packages, including server38.457s/store115.491s/jobs5.830s; full race passes server35.747s/store95.516s/jobs5.937s. Vet,171 Go formatting checks, JavaScript syntax and exact Tailwind CSS reproduction pass. Evidence is in ignored verification-issue-changes.

- 2026-10-15 UTC: Official Node SDK11.7.0 acceptance passes with seven unique captures and eight acknowledgements in project58/issue237. It exercises actual member/team/email assignments, additional-count retries, a real61.519-second mute, chosen/latest/next-release regression, inherited reads, native token-only401, compatible Bearer precedence and revocation. The first seven-capture attempt exposed an incorrect harness assumption: a valid native session authorizes a native request even when it carries an unused Bearer header. Only the test was corrected; project57 and all its rows remain preserved. The successful run uses a fresh project. Primary independently verifies SQL counts, SDK metadata, latest raw, all seven payload hashes and both revoked project credentials. Chromium passes the writer workflow in18 screenshots at320/768/1440, preserving all five drafts and URL filters. Primary inspects phone, tablet and desktop screenshots; no unexpected network/page/console error occurs. Evidence: issue-write-sdk-image, issue-write-browser-image and verification-issue-changes/primary-{sdk-baseline,final}.json.

- 2026-10-15 UTC: The single application image3e5e2c8f passes upgrade and real restart on the new18084 copy. It preserves all1298 baseline rows and001..017 receipts, adds only018 at startup, and retains both SDK attempts afterward. Final copy has260 events,22 spans,4 source maps,18 migrations and1388 rows across36 tables. Four prior accepted runtimes remain byte-identical in container/image/config/mounts and all row hashes. The static Go binary76d4adf3 runs as UID65532 with CA and daisyUI MIT files verified. Image metadata is25,993,785 bytes, not measured RSS or a registry publication. All131 production/build inputs remain identical to the exercised manifest. Public source/CI acceptance is still pending. Evidence: container-api0-issue-write/{prepared,before,report,all-attempts-preserved}.json and verification-issue-changes/runtime-source.json. This closes no other baseline requirement.

- 2026-10-15 UTC: Compatible individual issue changes are accepted and published at d5bebed267a063fc40afa7815ec667d4f7e901cb, tree e4d2e94a39fd60929e242152d7862129eb2fc885. The private index records exactly23 intended paths and11 additions with no deletion. Primary verifies all252 public paths, modes and blob SHAs, matching main/local/origin and a clean checkout. Every exercised production/build input matches the published commit. CI38756190770/job116495137457 succeeds for formatting, vet, full PostgreSQL/race and application image; primary inspects the actual log, including eventview1.080s, jobs5.950s, server33.307s and store68.742s. The six landing files remain byte-identical. The exercised local image is distinct from the CI build; neither is claimed as a registry publication. Evidence: publication/issue-write-feature-{commit,public-tree,ci-terminal}.json, job-116495137457.log, issue-write-site-unchanged.json and verification-issue-changes/{runtime-source-commit,acceptance}.json. This closes only the specified individual-write child. Bulk/merge/hash/delete/window/user/commit, feedback and every other unchecked product requirement remain mandatory; the overall goal stays active.

- 2026-10-15 UTC: Next bulk contract is specified before implementation. Public Sentry project/org contracts and official sentry-cli3.8.0 source at b602219e19ba29562996597a4d01b4a3bec4e7fc are corroborated by actual binary wire observation: repeated IDs, status and snoozeDuration:null, muted alias, status/--all selectors, no-selection silence and200/204 exit behavior. Official release binary SHA13f8cb34ae01a6a272d7d7c22e277a105286615b4020de900ea95a8de47cdbb6 is verified against release asset metadata. Sanitized ignored proof verification-bulk/cli-public-contract.json distinguishes observed requests from server state acceptance.
- 2026-10-15 UTC: Isolated unmodified GlitchTip6.2.6 image a3d8eb1b36c1e1603d55ab32711ea3dd8115874742a781a57391a62a16e0dff6, own PostgreSQL/network and public HTTP only supply external contract observations. Seven genuine official Node11.7.0 seed captures form four issues;101 later real captures form101 separate issues. Project PUT returned405 despite hosted-current OpenAPI describing that method. Organization status PUT changed D4 outside project=3 and, separately, outside101 explicit IDs5..105; observers stopped each matrix at containment failure without restoration, so limit=1/101 remains unobserved. All108 acknowledged SDK events retain their original projects. These are upstream6.2.6 observations, not GoodSentry test results or requirements to copy. merge:true/1 actually combines hashes; false/0/null does not separate. Hash DELETE202 removes a real hash while the historical event remains group3; repeated/unknown hashes return202, malformed UUID422. Private responses are never published. Sanitized proofs are ignored verification-bulk/gt-oracle-{seed,limits,explicit-limits}.json; root retained each HTTP phase separately. No server source/tests/internal schema/assets were inspected. GoodSentry will follow the explicit Sentry containment contract and preserve its existing lowest-ID native merge winner; future-only hash detachment remains a separate mandatory storage/behavior increment.

- 2026-10-15 UTC: Bulk foundation shares the native merge body in7ea400097257cba0881b3f4b6cb24ccaf909f231. Primary inspected the diff;53 mutating lines remain identical, SHA c89703dcfe3ce9729663935d380eed8bf859fe056ceb4141bd41868e94f59ce8. Existing17 top-level grouping/related tests pass before20.471s/after21.249s. Native membership,100-ID admission, locks, commit and unmerge remain unchanged. Contract637b520 was written first; later latest-release clarification was written before its implementation.
- 2026-10-15 UTC: Bulk organization/project state/assignment/native-policy writes and merging pass10 owning PostgreSQL tests11.052s, including101 actual roots, whole-ID containment before limiting, same-occurrence/time selection, two-project assignment/mute and actual signed-key inversion across independent pools. Gate cancellation preserves native actors; an external SQL row-writer fixture proves status recheck after issue-row waiting with no cohort backfill. Combined merge/invalid target rolls back every row; native partitions, raw data, receipt/mute progress and future grouping remain intact. The row-writer proof models maintenance's lock shape and does not claim execution of its worker. Full13-package integration passes server37.194/store128.193/jobs6.014s; full race passes server36.758/store103.083/jobs5.814s. Vet,177 Go formatting and JS syntax pass. Fresh bounded review reports no blocker, independently runs owning tests10.195s and kills an isolated whole-ID-containment mutant. HTTP/parser owners pass with existing compatible siblings3.197s; a separate zero-duration alias mutant is killed. Missing-route405/401 RED, worker compile/assertion mistakes and a primary read-only metadata KeyError are retained separately; no genuine product-regression RED is claimed. Evidence: ignored verification-bulk owning/transport/full logs, transport-evidence.json and acceptance.json.
- 2026-10-15 UTC: Actual candidate image b393ba64377cb91c7f7abe0260d02711e2bbaa6a830de025348fd9eb378ae99f passes official NodeSDK11.7.0 plus official sentry-cli3.8.0 first-attempt acceptance. Ten unique SDK errors produce11ACK200;11 CLI invocations include9 successes and intended hidden/revoked failures. Newly issued event:write-only token26 is restricted to project59; project60's real issue remains isolated. Repeated IDs, status/muted/--all/no-selection, next-release regression, boolean/integer merge, native historical split, future routing, retry preservation and raw projections are observed through real reads, not CLI exit alone. Primary independently compares all10 stored payloads exactly with their first acknowledged SDK payloads,9 retained HTTP hashes and401 Bearer precedence over the existing cookie. Token26 remains revoked. Browser then intentionally incorporates control248, leaving root244 with9 conserved occurrences/3 original groups, while excluded249 stays separate. Evidence: ignored bulk-sdk-cli-image/report.json and verification-bulk/primary-{sdk-baseline,final,http-final}.json.
- 2026-10-15 UTC: Real Chromium passes5 workflows with27 PNGs/dimension checks at320/768/1440. UI token27 is event:write-only/project59 and revoked at completion. Defaults/keyboard/reveal/clipboard, real bulk mute/assignment/selected release, five drafts and URL filters, native separation246, stale alias409/current-group recovery and boolean/integer remerge are verified without new captures. Final root244 is unresolved/unassigned/no active policy, with4+4+1 actual original occurrences. No unexpected page/console/request/HTTP failures; expected negative statuses remain explicit. Primary inspects tablet mute, phone alias error, desktop three origins and masked secret screenshots. Evidence: ignored bulk-browser-image/report.json.
- 2026-10-15 UTC: The fresh18085 image copy preserves all1388 original rows/260 events and all18 migration receipts before new captures. Actual app restart1.721s preserves all1455 current rows/270 events byte-for-byte, old cookie/credentials/maps and five protected earlier runtimes; no old runtime is restarted or migrated. Primary also compares16 actual native responses preserving every business field and identified-user count, omitting only existing clock-dependent chart series. Runtime is static Go1.27.2/non-root65532 with CA/MIT, binarySHA a7be1d76db5690446169f7cd4bb3755dfe2cef85b98e4d21aca1ad46731174a0;132 production/build inputs match the image. Metadata size26060798 bytes is not RSS or a registry publication. Final270events/22spans/17operations/4maps/18migrations are expected. Evidence: ignored container-api0-bulk/{prepared,before,report}.json and verification-bulk/primary-native-final.json/runtime-source.json. Exact public tree and matching remote CI remain required before closing this child.
- 2026-10-15 UTC: External GlitchTip hash-detachment follow-up uses exactly two further real SDK captures without SQL/source introspection. After removing A's hash, newA joins new issue106, controlB remains3 and oldA still belongs to3; both ACK200/flush pass. This establishes future-only routing and historical retention in observed6.2.6, not native historical unmerge equivalence. Evidence: ignored verification-bulk/gt-oracle-detach-future.json. Hash identity/storage/detachment, deletion and every other unchecked full-parity task remain mandatory.

- 2026-10-15 UTC: Compatible bulk state/assignment/native-policy changes and merging are accepted and published as50f183c6dae506f797fa0a9e459206e5a4d42e44, tree36fc13b01e7c13199317b25263db416e7ca00207. The private-index guard records14 intended paths/eight additions/no file deletions and260 total files. Primary independently compares all260 public paths/modes/blob SHAs and all132 exercised production/build inputs with this commit. Matching CI38766455860/job116530808848 succeeds for formatting, vet, real PostgreSQL integration/race and image build; actual downloaded log is inspected (eventview1.057/jobs5.079/server33.879/store68.408s). All six deployed Pages files remain HTTP200/byte-identical; site inputs are unchanged so no redeployment is needed. Evidence: ignored publication/bulk-feature-{commit,public-tree,ci-terminal}.json, job-116530808848.log and bulk-site-unchanged.json. This closes only the specified bulk/merge child. Hash listing/future-only detachment, deletion, window/user/commit/flags/feedback and every other unchecked product baseline requirement remain mandatory; the full goal stays active.

- 2026-10-15 UTC: Previous bulk turn is verified progress, with clean authoritative HEADcff554,260 published paths,132 matching runtime inputs and successful functional/closing CI38766455860/38767182758. Primary revalidates current source/evidence before the next contract. Own database catalog confirms issues_project_id_fingerprint_key; native origins currently have no active-route metadata. Hash/reset design is fixed in SPEC.md before implementation: independent stable signature routes, immutable historical generations, future-only detach and retained native historical separation. No migration/ingestion/UI implementation is claimed yet. The next base is the accepted270-event/1455-row/18-migration imageb393ba64377cb91c7f7abe0260d02711e2bbaa6a830de025348fd9eb378ae99f; clone to18086 and protect all six earlier runtimes. Every unchecked requirement and the full goal remain active.

- 2026-10-15 UTC: Hash listing and future grouping reset implementation/local acceptance pass. Migration019 backfills stable UUID routes without changing1455 baseline rows or receipts001..018; fresh captures allocate a new historical origin after reset, while retries return before route allocation. Native separation cannot reclaim a retired route from a newer generation. Scoped RR samples use each active origin's actual latest event; reset commits only owned active routes and one sorted actual-change audit, preserves policies/history/outbox, and returns empty202. Store7 owning checks5.418s/race5.278s, transport0.877s/siblings7.539s, full13-package integration(server37.311s/store133.807s) and full race(server41.280s/store132.928s) pass; vet,184Go formatting and four JS syntax checks pass. The old016 reconstruction now retires019's dependent catalog before restoring015; old-table snapshots retain their full contract. A prior HTTP no-op check removed analytics at the wrong nesting level; a delayed overlay reproduced analytics-only change, and the correction excludes only clock-driven stats while retaining userCount and every business field. That correction is separate commit1a07c42. The isolated same-group-containment mutant is killed, not a prior product defect.

- 2026-10-15 UTC: Actual Chromium found and verified two UI repairs: replaceChildren received an Array instead of expanded Nodes; a confirmed202 followed by failed native refresh left stale selection controls. The final module renders real controls and synchronizes selection/notification before refresh. Final Chromium passes8 checks/27 screenshots at320/768/1440, including10+1 Links, cross-page selection, five drafts/URL/focus, declared GET503 and post-commit nativeGET503 recovery, real alias409 after merge, empty state, historical-only labels and revoked tokens34/35. Final image sha256:a78064bfaa3e5a3421d79ec190a9be2a7c687db31762d7d9c60eef31eae28ea5 passes a fresh official SDK11.7.0/CLI3.8.0 run21 unique/22ACK, independently compared against all first wire payloads. An earlier21-event run remains exact. The312-event clone at18086 restarts in1.828s preserving all1885 current rows,4map payloads,22spans,17operation rows,19checksummed migrations, original cookie and six earlier runtimes. Static ELF/UID65532/CA/MIT/five served assets verified;26,114,813 image metadata bytes is not RSS or a registry publication. Evidence: ignored verification-hashes, hashes-sdk-image, hashes-final-sdk-image, hashes-browser-image and container-api0-hashes. Exact public source and matching CI remain required before closing this child; the full parity goal stays active.

- 2026-10-15 UTC: Compatible hashes/future-only grouping reset accepted at7064f82fbf60bad4a755a9bd0ca6c269fe240479, tree8e71d1e14567638ec30e0b8ecc5b787bc1ce6371. Public recursive GitHub tree independently matches all269 paths/modes/blobs; all138 runtime/build inputs match the accepted a78064bf image. CI38773269521/job116554317815 completes successfully; primary inspected formatting/vet, full PostgreSQL integration with race(eventview1.068s/jobs5.800s/server34.923s/store72.592s), image construction and the actual retained log. The six deployed landing files remain HTTP200 and byte-identical, so no Pages redeploy is needed. After restart, primary rechecks all42 first SDK wire payloads and immutable origins from both runs, old1455 rows and six protected runtimes; independently verifies final root269 has16 events/13 historical-only origins/zero active hashes, retained assignment/policies, control270 has4 events and hidden271 has1. Two primary-verifier setup errors(container-owned0600 snapshot and incorrect Anew key spelling) were corrected without application changes or mutations. Evidence remains in ignored verification-hashes, container-api0-hashes, hashes-final-sdk-image, hashes-browser-image and publication/hash-* reports plus CI log. This closes only the hash/reset child; issue deletion, further conditional policies, feedback and all other unchecked parity requirements remain mandatory.

- 2026-10-15 UTC: Previous goal turn is verified progress: published a297b43/74584da tree269, exact source and CI38774023933/job116556879351 success; hash image a78064bf remains active. New deletion contract is fixed in SPEC.md after public Sentry/GT docs, official client-only CLI3.8.0 research and unmodified GT6.2.6 HTTP observations. CLI has no DELETE command. GT individual204/recurrence and bulk200 were observed only in newly created oracle project4; seven public guards preserved110 earlier events/106 issues. Observed legacy alias/count and missing-ID behavior is recorded, not copied. One oracle SDK observer promise/flush error yielded no event in three GETs and was abandoned without replay;9 of10 attempts have real ACK200. Evidence: ignored verification-deletion/public-contract-research.json(SHA36140cc18cc58c45d854572f174061f36b14b7473afdcd2035cea692d1614720), cli-public-contract.json(SHA581d724e5f95b6f71824a6f4f9804e1b4061f42c0b147fba0d07579883690f77), gt-oracle-delete.json(SHAaaa0cb11a5e869069f1b501ad8bb506389f56e1170be90550ad1dc4d68717c67). A fresh312-event/1885-row/19-migration DB/uploads clone is prepared at18087; seven accepted runtimes remain unchanged. No new app is started there and no deletion product code is implemented yet. Independent advisory review found the post-commit child ABA and standalone opaque gate gaps; the fixed contract stores normalized child bytes in the capture transaction, pins duplicate numeric receipt identity and orders standalone opaque writes. A fresh reviewer spawn hit agent capacity; the independent reviewer had prior factual CLI context and no participation in this design. Cross-model review is skipped in this autonomous continuation. All CaptureItems batches must be consumed. These are design findings, not executed product REDs.
- 2026-10-15 UTC: The capture foundation is implemented and locally verified before adding the destructive provider. Ingest consumes all CaptureItems batches inside the event transaction, preserves content identities, pins duplicate numeric receipts across gate waits and does not replay counters/raw/outbox. Standalone opaque writes share the project gate; HTTP continues independent logs/spans after collecting parented bytes. The sole SaveAttachment caller is migrated and its superseded public method removed. Primary reviewed the actual diff and both workers' owner assertions; an independent read-only review found no defects. Five PostgreSQL owners/six subcases pass3.826s and focused race4.165s, including real22P02/post-write cancellation rollback, invisible parent/children, transaction children, observed gate waits and removed/replaced receipt ABA. One HTTP owner/four cases passes0.905s and existing capture sibling21.377s, covering binary/empty attachments, unknown headers, order/retry and retained independent400/415 behavior. These are hand-built HTTP envelopes; attachment bytes are observed in isolated PostgreSQL, not a claimed SDK/download proof. Full13-package integration passes(server41.895s/store160.021s), full race passes(server42.409s/store133.406s), vet and188-Go-file format/whitespace checks pass. No product, compile or fixture RED occurred. Evidence: ignored verification-deletion/capture-{evidence,http-report,primary-source-review}.json and actual owning/full/race/vet logs. No020 migration, DELETE provider/route/UI or new18087 application image is accepted yet; parent deletion and full product goal stay open. Exact publication/CI for this coherent foundation follows next.
- 2026-10-15 UTC: Capture foundation is published as396b0442685abec162673c58822afa3e9ac12f0c/treea2b80647bc5c46fb91add6259345b19db4b3de14. The private index records exactly7 reviewed paths/two additions/no deletions; primary independently verifies all271 public paths/modes/blobs. Matching CI38781634347/job116583042372 succeeds for formatting, vet, full PostgreSQL/race and image build. Actual inspected log records eventview1.042s/jobs5.662s/server32.546s/store64.541s and CI-only image4a092f1973f9dc4613affca4322cf17e79551da535c615e7d76db82b4352aa87. This image is distinct from the accepted a78064bf runtime; neither registry publication nor measured RSS is claimed. Landing sources are unchanged. Evidence: ignored publication/capture-foundation-{commit,public-tree,ci-terminal}.json and job-116583042372.log. Deletion provider/UI/image acceptance remains open.
- 2026-10-15 UTC: The local deletion writer review identifies a concrete stale-alias race: native Unmerge holds the gate while awaiting a row, bulk selects aliasB's old rootA and awaits the gate; after separation the old selector deletedA or changedA while leavingB. Primary adds the observed two-pool PostgreSQL owner before fixing production: both DELETE and PUT genuinely fail in1.009s, including DELETE retaining only1 of2 roots/events. SPEC is clarified before the shared retainExplicitIDs selector fix. It now requires both frozen-cohort membership and current canonical ownership of an original selected ID; an independent read-only review finds no further defect. The two cases and existing native merge-wait sibling pass2.037s. New destructive transaction owners additionally pass actual23503/all-table rollback, cancellation during attachment cascade after route/opaque writes, complete-ID recheck after competing real deletion and no-backfill after a row predicate changes; combined with020 migration they pass4.423s. These are actual Store operations and observed PostgreSQL blockers, without production seams. Evidence: ignored verification-deletion/delete-detached-alias-{red,green}.log and delete-transactions-first.log. An advisor's potential018 fixture incompatibility was an unexecuted inference; primary inspection and the actual owning run1.143s confirm it still passes, so no old test was changed. UI contract now explicitly distinguishes individual deletion204 from bulk200{} that may be a containment no-op: consume submitted selection, preserve drafts absent authorized404, and never claim a deleted count. Full deletion acceptance remains pending.
- 2026-10-15 UTC: Backend deletion is locally verified before publication. Four core PostgreSQL owners pass3.058s/race3.641s with nonempty independent telemetry/metadata/outbox/attempts and exact row preservation.020 upgrade passes0.875s/race1.682s, preserving every old row and receipt001..019 while proving exact opt-in scope, real23514 rejection, index/checksum/reopen. Its initial incomparable-User compile error belongs to the fixture and is retained separately. Strict parser passes0.010s; eight real HTTP cases pass1.720s with actual issued credentials, grants, scope noninheritance, authoritative Bearer, CSRF, framing/307, committed204/200{} and actual default/explicit-ID behavior; existing token/PUT siblings pass3.866s. The four primary transaction owners pass focused race4.053s. Three additional real-DELETE ordering owners/four scenarios pass final2.174s/race2.684s, including exact advisory key/mode/wait checks, both capture cuts, deleted duplicate bytes and reversed signed project-gate ordering. Primary inspects each worker diff, assertions and actual logs; independent code/test reviews find no further concrete backend defect. Full13-package integration passes(server45.857s/store185.539s), full race passes(server44.927s/store155.438s), vet and197-Go-file formatting pass. The full local runs compiled before the final order-owner wait-type strengthening; its final focused runs verify that change, and exact-source CI remains required. Evidence: ignored verification-deletion/delete-{backend-primary-checks,store-evidence,migration-evidence,http-report,order-evidence}.json and raw logs. UI, official SDK/retained CLI, copied-image upgrade/restart and parent deletion acceptance remain pending; all seven accepted runtimes and the prepared312-event copy are untouched.

- 2026-10-15 UTC: Backend deletion is public at de74b0aeed073dad1643a9be0daa92b4ce02c4be/tree2e64217ec11ce244c28e59b748e075e3c4eb82e6. All281 public paths/modes/blobs match. Verification explicitly permits only the separately unstaged app.js UI and records workingTreeClean=false; the original clean-tree verifier is unchanged. CI38787009598/job116601485947 succeeds for formatting/vet, complete PostgreSQL integration with race(eventview1.058s/jobs5.633s/server35.906s/store76.814s) and image build. Primary inspected the actual downloaded log SHA5edbfecd17f4867ad6e95ae20ff0aab796c4b1119dbd55be38d84c99ff1091be. The CI image d6b91c1f is distinct from the local acceptance image. An abandoned shared index.lock was verified byte-stable and equivalent to HEAD, privately backed up and quarantined without losing staged work; subsequent commits use the private-index helper.
- 2026-10-15 UTC: Candidate image sha256:1b0f5501d8fb80a798f493ac6e1cab15b8432bae528cb2189c0cc1eaf2c6c212 starts on the prepared18087 copy and applies020 without rewriting any of312 old events/1885 rows/receipts001..019. Static ELF, actual UID65532, CA/MIT, uploads and five exact served assets pass. Image metadata26,183,211bytes is neither RSS nor registry publication. All143 runtime/build inputs remain exact; app.js SHA8cce87dbd7b3bbe9da60d95202e1dc8f067af86bcdbf4c1b835988de45f4a545. Official NodeSDK11.7.0 executes one run:8 actual wire captures/ACK200 with binary attachments,6 unique external IDs and7 fresh numeric receipts. Primary independently parses every captured envelope/attachment, compares retained C/D raw against first wire payloads and observes exactly2 retained events/attachments,4 stable routes/2 inactive, unchanged hidden control and1885 old rows/seven protected runtimes. Merge/reset/deletion preserve the newer generation; deleted external-ID replay becomes a fresh receipt while retained retry remains idempotent. Attachment bytes are observed through readonly PostgreSQL, not a public download endpoint. Evidence: deletion-sdk-image/report.json SHA20c0cc62b3ef4f425484ce4dc7e1692e6fa9f27eb42becdfb9803cc023ba44b2 and verification-deletion/delete-sdk-primary-proof.json SHAeb382c4225f52ff6afbe2757d59c79f6f2f637524d306b1488bdbd0035e04872.
- 2026-10-15 UTC: Official CLI3.8.0 executes8 real invocations with issued restricted credentials:6 successes(list, resolve, unresolve, mute, unresolve, retained reader list), intended read-only403 and revoked-writer401. Native reads after each command verify actual status/count and exact SDK raw/binary/routes; only the new writer is revoked. CLI has no deletion command. Baseline1885 rows and all seven protected runtimes remain exact. Primary inspected actual logs; evidence deletion-cli-image/report.json SHA217d394f407a23a18fbf406decc22dbd7e8133d810f43fd9fe7957b0473d92c0.
- 2026-10-15 UTC: Deletion UI passes8 actual Chromium scenarios across48 verified viewport captures at320/768/1440, with provenance across retained attempts. These include five drafts/cancel/focus, declared precommit503, held204/shared pending controls followed by GET503/read-only retry, actual merge409/Open current group, unknown-owner paginated comment edit preserved after unmerge/root deletion, real containment no-op retaining drafts, DOM selection across41 pages with100/101 merge and1000/1001 delete bounds, exact1000-ID deletion and unselected survivor/page clamp, and unchecked admin-only token39 reveal/revocation403-to401. Primary inspects real narrow/detail/comment/bulk/token screenshots and independently queries PostgreSQL: browser fixtures retain3 exact raw events/issues(294,296,298),27 comments including26 on294, and1009 stable routes with1006 inactive. All1000 submitted IDs are absent; the actual unselected ID remains. SDK C/D,1885 old rows and seven protected runtimes remain exact. Browser events are manual HTTP fixtures, not an SDK claim. Three harness failures are preserved separately: unavailable Chromium204 body, literal JSON newline comparison, and historical-title assumption after merge. The last made zero mutations. No product repair, reset, reseed or repeated deletion hid these failures. A fresh read-only reviewer also corrected counter-format and intentionally rejected checkbox assumptions before execution. Final report continuation3/report.json SHA537d8e36256a54d199880bd1c90ddb8980a95393a468b7e9bd350e88d8301fc7; aggregated accepted-report.json SHAb5e5bd0a2b43d2697bd7ca2d765430dfc3ea975bc198b5637ff02c54df0520dd; primary SQL proof SHAc1edef3fa413969e1e33031c863c509eedfde3524eaffd7677d111ead19c0081. SPEC clarifies the existing empty-JSON-object response allows normal JSON whitespace. App-only restart, exact final public source and matching CI remain required before closing deletion. All other unchecked parity requirements remain mandatory.
- 2026-10-15 UTC: The accepted deletion app alone restarts in1.812s with exactly all3009 current rows preserved,317 events,22 spans,4 source maps and20 checksummed migrations. The1885-row original subset, old native responses/cookie/CSRF, revoked-token401, uploads, exact five served assets and seven protected runtimes remain unchanged. Primary inspected the actual restart report SHAa585e4fb9693de6a14c7487d4c4a5df7c3a2a21cf640263253c0f8c5f49521c2. The six live Pages files are still HTTP200 and byte-identical(SHA50e6f1219d2b4d80ccc43beb7dc163c072c5be76b04dde2c858232c87b27ac91); no landing change or redeploy is needed. Local complete deletion acceptance passes. Publish the reviewed five-path UI/documentation increment and inspect its exact public tree and matching CI before checking the parent task. Full parity, public attachment-download acceptance, registry publication and measured RSS remain unclaimed.
- 2026-10-15 UTC: Complete compatible issue deletion is accepted and public at d47ac3a672c077a9153a6acc4902d29e0217f383/tree6de7decbb33dd0819f90850df923cd31b0649a56. Primary independently verifies all281 public paths/modes/blobs, clean local/origin/public identity, and every143 committed runtime/build input against the actual1b0f5501 accepted image. CI38792648398/job116620874874 succeeds for formatting/vet, full PostgreSQL integration with race(eventview1.065s/jobs5.824s/server36.389s/store81.498s) and image build. Primary inspected actual completed steps and retained log SHA1884efb4e88846cf9f93494085a84b3c678b7b784efac394a35085efc6ec1867; intentional JSON/scope/rollback/cancellation/group/route/projection errors belong to passing owning cases. CI-only image8640c736 is distinct from the locally accepted image. Public README now describes whole-group removal, future recurrence, exact empty-object/no-op semantics, selection limits and opt-in deletion scope without claiming full parity or CLI DELETE support. Evidence: ignored publication/deletion-ui-{commit,public-tree,runtime-source,ci-terminal}.json, actual CI log, verification-deletion primary wire/SQL proofs, accepted-report.json and container-api0-deletion/report.json. This closes only compatible deletion; native OTLP/retention, remaining conditional policies/feedback and all other unchecked baseline capabilities remain mandatory. The accepted317-event/3009-row runtime at18087 joins the seven earlier protected runtimes; do not reset or mutate any accepted baseline for later increments.
- 2026-10-15 UTC: Closure-only source b50b0cb/tree255d96a is public with exact281-path verification and successful CI38793352056/job116623307126. Primary inspected actual log SHA5dd63e6c24851335351dab1eddb20561eebed40d1c3eb02b835ecb946d3c6e06(eventview1.054s/jobs5.647s/server37.266s/store74.841s). A subsequent retained-driver check found its old accessible label/100-selection expectation stale under the approved neutral1000-selection contract. The existing grouping browser owner is aligned, preserving merge<=100/one-project behavior. One actual run on a NEW317-event/3009-row copy at18088 with two newly seeded users/org passes6 scenarios/33 captures/103 manual events; primary verifies every new eventID,103 standalone issues/active routes and all3009 old rows/uploads/eight accepted runtimes. Foreign old-login-fixture precondition and cookie-requiring readiness helper errors are preserved separately; no preparation reset or baseline rewrite hid them. Evidence: deletion-grouping-regression and verification-deletion/retained-grouping-*.json.
- 2026-10-15 UTC: Primary visual inspection of that actual merge confirmation finds a real introduced UI defect: replaceChildren stringifies its optional null child, displaying bare null during merging. A focused real Chromium reproduction fails with visibleNullPlaceholder=true and zero issue mutations on image1b0f5501. This is product RED, distinct from the earlier harness failures. The owning UI now spreads an empty child list when no deletion warning applies, and the retained grouping driver explicitly rejects the placeholder. Deletion acceptance is reopened until this correction is verified and published. Corrected candidate623d6782bc21a88e09609a31cd387ed09b5484b367cefbd053e6fae3b065e3f3 replaces only the test copy's app, preserving all3509 current rows/420 events/database/uploads/environment and all eight accepted runtimes. Old test container is stopped/preserved as goodsentry-deletion-grouping-ui-red-check; do not restart it.142 of143 runtime/build inputs are byte-identical; only app.js changes. Static ELF/UID65532/five served assets pass. Run corrected-image confirmation probe, complete retained browser owner with fresh owned fixtures, one fresh official SDK binary capture, all-row restart and exact publication/CI before closing again. Keep every other unchecked parity capability required.
- 2026-10-15 UTC: Corrected image623d6782 passes the focused actual merge/delete confirmation probe at320/768/1440, visibleNullPlaceholder=false and zero issue mutations. Primary inspects both narrow screenshots; real RED report SHA8745aa866609ee6fa5e6b0e2b3637e928814efb003af6d0fa0a368e321c68d8f becomes GREEN SHA faa3a787d4c725fde3f58c396d62139ec8e40ca0659da9395f4a18e85659931c. The strengthened retained driver executes once with fresh owned projects71/72:6 real merge/separation/comment/draft/navigation scenarios,103 new events,33 viewport captures, only declared409. Primary independently observes all103 eventIDs/standalone origins/active routes and every3509 pre-replacement row unchanged. Official NodeSDK11.7.0 executes one rebuilt-artifact capture in new project73, actual ACK200,2239 real wire bytes and eight binary attachment bytes. Primary parses that actual envelope independently and compares its full event against current PostgreSQL and its associated exact filename/content-type/binary data. Worker SDK report SHA3fac10435a0425737da4c93a7e42e4ef64bd50bd078864dd6956d8d5ec27a0c2. An app-only restart takes1.654s and preserves all4010 current rows/524 events/22 spans/4 maps/20 migrations, uploads, actual five served assets, CA/MIT and all eight prior accepted runtimes. Primary final report SHA405e0ad1a976acb0ac448c4575976208bb5da66831b836a193bd36ea16879a20. This is a narrow UI repair: existing full eight-capture SDK/recurrence/CLI/deletion backend evidence applies to the unchanged142 runtime/build inputs, while the fresh one-capture smoke proves the rebuilt artifact. Browser events are manual fixtures; no public attachment-download, RSS or registry claim is added. Publish this coherent repair/retained-driver alignment and inspect exact public tree and matching CI before reclosing deletion.
- 2026-10-15 UTC: Deletion is accepted again after the actual merge-confirmation repair at bd242c28cab39768f97749cab59ca978b0e8f263/treeeb7c0dafd19f426a704a8e27f64a44283c2e4689. All281 public paths/modes/blobs and all143 committed runtime/build inputs independently match the corrected623d6782 image. CI38797408903/job116637329129 succeeds for formatting/vet, full PostgreSQL integration with race(eventview1.053s/jobs5.984s/server36.730s/store83.596s) and image build; primary inspects actual steps/log SHA b35022f19642c67db302c9c6f548f7dcb36aa0ce5f3d9dae011d4104ac0455db. CI-only imagef425c07c is distinct. The retained grouping owner now proves actual native controls under the approved selection contract and rejects the reproduced null placeholder. Its corrected-image6 scenarios/33 captures plus focused6 confirmation captures, fresh official SDK wire/binary smoke and all4010-row/524-event restart pass. Eight prior accepted runtimes remain exact; corrected18088 now joins them as the ninth preserved baseline. Evidence: publication/deletion-confirmation-fix-*.json, actual CI log and deletion-grouping-regression/final-acceptance.json. This also reconciles the stale generic grouping checkbox: native a9a23f2, compatible bulk/merge50f183c and hashes7064f82 already passed their separate contracts/permissions; no additional grouping capability is claimed by a task-state correction. Next specify native OTLP from the refreshed public facts and existing log owner; retention, remaining conditional policies/feedback and every other unchecked baseline capability remain mandatory. Full parity is still active.

- 2026-10-15 UTC: Primary implements the OTLP HTTP boundary and session detail over accepted Store021. Exact configured POST /v1/logs resolves only one enabled SDK public key, authenticates before application body reads, preserves at-least-once occurrences and commits before200; static google.rpc.Status, Retry-After1, CORS, methods, gzip/media and configured byte/record/node capacities pass their real owners. Native GET organization/log-row detail returns the direct selected DTO under current session/membership/project containment; SDK canonical data and OTLP zero flags/exact typed selected context survive without sibling leakage or an extra wrapper. Exported protocol.ValidPublicKey reuses existing SDK syntax, with unchanged SDK call sites; the OTLP text boundary additionally rejects invalid UTF-8. Real missing-route REDs precede registration. Two official-Go routing proofs found repeated-prefix redirects and Unicode RawPath fallback aliases; the final production guard passes126 raw HTTP targets (6 canonical POST403/OPTIONS204,109 direct404,11 unrelated/deep redirects ending404/405). Eight HTTP ingestion owners/70 initial subcases plus one primary auth-before-unsupported-media case, three native detail owners and real network/independent-PG acknowledgment/rollback pass. Fresh review found no product defect; its one concrete coverage gap is repaired and a temporary unsupported-media-before-auth mutation is now detected, distinct from product RED. All14 application packages/12 tested packages pass fresh integration with race (server42.645s/store111.014s), vet and214-file format/whitespace checks. The recreated WSL environment lost earlier /tmp tools: official-checksummed Go1.27.2, relocated GCC15 and isolated localhost PostgreSQL18.6 were restored without system package installation. Initial complete non-race/race runs exposed only two legacy snapshot comparisons rendered in the new service's America/Mexico_City timezone versus UTC; setting only that disposable PostgreSQL service to UTC repairs both without weakening tests or changing application code. Retain those initial logs and toolchain/fixture diagnostics. Evidence: ignored verification-otlp/http/local-acceptance.json SHA96047f47a7ed54a564201c2240a23c135eb3aad368070c5d1b6182337ca7d737, final-full-race.log SHAc8df0b4375db4c5e419d4bda33c6604177bf5d20901949e70a0e99a998432f93, owner RED/GREEN/gates, auth-media-mutant metadata and route-facts. No UI, actual Python exporter traffic, new local image/restart or current-state re-verification of the nine accepted runtimes is claimed; Docker Desktop's engine remains unavailable after the environment recreation. No accepted runtime was mutated. Publication/CI follows; the full OTLP child, retention and all other unchecked baseline requirements remain open.

- 2026-10-17 UTC: Publication repair. Remote commits 10836be and 3a7807c stored internal/server/web/app.js and tests/compat/otlp_browser.mjs as single-line base64 blobs (7fbe70d, 76813f8) instead of UTF-8 text, so the public tree carried an unusable embedded app.js while local builds from the correct working tree hid the defect. Both blobs decode byte-exact to the locally verified files (app.js hash-object e21eff1, node --check passes on both), so no content is lost. This increment re-points main at origin/main, stages the plain-text files and publishes them so the public tree matches the intended source. Exact public tree identity and matching remote CI verification follow this push.

- 2026-10-17 UTC: Repair published as 359395d. Exact public tree matches; plain-text app.js served from the public raw URL and GitHub Actions CI run 39070843224 completed successfully on that commit. The runtime protections (.gocache, .gopath, .tmp) are ignored locally. The restored sources match the decoded base64 blobs byte-exactly; no product behavior changed.

- 2026-10-17: Continued OTLP acceptance from3a01005. Actual Chromium reproduces the Projects recipe pointing at/observe/74/v1/logs instead of/observe/v1/logs, with nonexistent BatchLogRecordExporter and no gzip example. The corrected recipe is copied from the running UI and executes with official Python1.45.1 protobuf/gzip. A fresh read-only volume reference independently matches every original4010-row/524-event hash; existing startup cleanup removes exactly three artifact_chunks whose original deadlines expired2026-10-16. All4007 other historical rows remain exact, including every524 event and completed map; the reference retains the original chunks. SPEC now explicitly accounts for elapsed staging expiry without rewriting deadlines or restoring/resetting data. Initial port mismatch and local ./... evidence-directory package conflicts are environment/harness failures, not product RED. Full cmd/internal PostgreSQL suite passes14 packages; integration-tag vet passes. Linux race, actual official exporters, full Chromium and restart/publication acceptance remain pending. No parent checkbox closes from this note.

- 2026-10-17: OTLP UI/exporter acceptance is public in76649c8 andcca6919. Independent public-tree comparison matches all302 paths/modes/blob SHAs. Image82674a571b7ea30f640082587e1e26ec1608233991c173b0b792cab8f6384925 is the actual final Linux/amd64 Go1.27.2 static binary, UID65532, exact MIT notice and five matching served assets. Its31,084,845-byte Docker metadata size is not RSS. Real RED/GREEN browser proofs catch the wrong project-suffixed endpoint and omitted-empty-key rendering. Final Chromium/Edge155 executes10 scenarios and12 viewport captures at320/768/1440, with no page overflow, page errors or request failures. It covers exact recipe, typed int64/bytes/NUL/nonfinite/duplicate/empty keys, lazy caching/loading, declared503 retry, pending close/reopen/navigation, text/service filter, pagination, trace, existing SDK detail, member and disabled behavior. Root favicon404 is separately observed, along with the single intentionally injected503; neither is a telemetry acknowledgment. Primary inspects the actual mobile typed-detail capture. The copied final Python recipe sends a real log.
- 2026-10-17: Pinned official Python SDK1.45.1, protobuf exporter1.45.1 and JSON exporter0.66b1 execute identity/gzip variants against the fresh18089 copy. Final own project81 receives8 actual HTTP200 acknowledgments and24 occurrences, including duplicate records, replay and8 empty-byte bodies. Consumer reads preserve resource/scope schema, exact signed64 and nanosecond digits, base64 bytes, nested/empty attributes and trace/span values. Primary independently decodes every recorded wire and compares the eight bytea batches plus24 exact ordinal rows to PostgreSQL. OTLP hex identifiers are deliberately converted to base64 only for the independent stock ProtoJSON parser; the initial unadapted parser mismatch is a harness failure. Wrong key403, foreign-organization detail404, historical1474-byte batch under reduced1KiB capacity413 and original full-detail200 all preserve every canonical byte. The initial empty-byte-body text expectation was corrected to the specified compact AnyValue JSON projection; both real exporter runs remain retained.
- 2026-10-17: Actual final-image NodeSDK11.7.0 project80 captures errora3c42fb00bdf4ef0959cda3a1da2738b, its eight-byte binary attachment and a typed log with two real HTTP200 acknowledgments. Primary SQL comparison proves full original SDK payload equality, exact filename/content-type/bytes and null OTLP references on the SDK log. Earlier driver mistakes used a nonexistent native event URL and a repeated project name; they remain recorded, with no database reset. Full main14-package PostgreSQL integration passes (server75.354s/store206.311s); Linux Go1.27.2 full race passes (server68.669s/store252.587s), vet/format/JS checks pass. Ignored evidence Go experiments initially broke ./... discovery; ignored test-results/go.mod now isolates that private evidence as a separate local module without modifying product module ownership.
- 2026-10-17: App-only final restart takes1.562s and preserves every4209 current row in38 tables,527 events and28 canonical OTLP batches byte-exact. Every protected original application/database state/configuration/mount remains identical, including the immutable4010-row reference and4007 live old rows plus3 separately proven expired chunks. No accepted original container is restarted, reset or written. The exact six live landing files remain HTTP200 and byte-identical. Final matching remote CI39080618212 forcca6919 is still running on the authoritative check; closure remains unchecked until its real result passes. Evidence is ignored test-results/otlp-acceptance-20261017/{public-tree,exporters-primary,boundaries-primary,sdk-primary,restart,baseline-preservation,runtime-assets,site}.json and browser-complete-final/report.json. Source/runtime scope and historic failures remain explicit. This closes only UI and real-exporter children, not the overall OTLP/GlitchTip parity goal, registry publication or memory target.

- 2026-10-17: Native OTLP acceptance is closed against exact public537e8633f27e37fe4ff0e61a0b51a06cae3d4618,302 paths and actual final image82674a57. Matching remote CI39080717495/job117537123339 succeeds for formatting, vet, full PostgreSQL integration/race and application image build; primary inspects each completed step and saves ci-terminal.json. Earlier39080618212 was canceled by the later evidence push, not a successful CI claim. Raw public app.js/styles/CSS/browser/exporter/README files are independently fetched as plain UTF-8 and byte-match their local commit, preventing recurrence of the previous base64-blob publication defect. Primary validates actual static ELF Go1.27.2 SHAed3fc6dd693ef32b0a28ca5cb1699f76fc9e46c7a10eab9331a306cfab9f4a72,150 CA certificates, exact MIT and empty unchanged uploads; image-primary.json records this proof. All original baseline states remain untouched. Next elaborate per-class retention under the existing SPEC authority; remaining conditional policies, feedback, symbols, workspace/integrations, registry/RSS and every other unchecked requirement remain required. The overall goal remains active.

## Native log retention increment

- [x] Validate master/log lifetime precedence, inheritance and maintenance interval without activating cleanup. Files: internal/config/config.go and focused owning tests. Verify ignored-invalid input RED, explicit overrides, default/minimum/maximum and invalid selected values.
  - 2026-10-17: Seven real deployment-loader RED cases returned nil for previously ignored invalid master/log/interval inputs. Config now resolves master GOODSENTRY_RETENTION_DAYS, GLITCHTIP_RETENTION_DAYS, RETENTION_DAYS and90, with log-specific GoodSentry/GlitchTip precedence then inheritance. Selected days1..3650 and interval1..3600/default60 pass; invalid lower-priority values do not defeat explicit settings. Eight focused inheritance/default/boundary cases, full14-package unit suite, vet/format and Linux Go1.27.2 config race1.035s pass. Native Windows race requires unavailable CGO; that environment limitation is not a product failure. Evidence is under ignored otlp-acceptance-20261017/retention-config-{red,full}.log. This prerequisite activates no cleanup and does not close log or per-class retention.
- [x] Add022 receipt/reference indexes and the bounded transactional log/orphan-batch cleanup owner. Preserve all prior rows and receipts. Verify real PostgreSQL boundary/cohort/SDK hashes/OTLP sibling/cancellation/failure/two-pool/reopen behavior before integration.
- [x] Integrate the in-process retention lifecycle with startup/backlog/idle/cancellation, disabled-ingestion behavior and truthful committed-removal diagnostics. Verify actual lifecycle and replica effects, not private ordering.
- [x] Verify the final new copied image with official SDK/OTLP owned fixtures, physical expiry, authenticated list/detail/trace results, separate backup restoration and every protected original. Run full integration/race/vet, exact public source and matching CI; document actual operating guarantees before closing only log retention. Other required retention classes and cold archival remain open.

- 2026-10-17: Retention provider implementation now has migration022 receipt index and Store.CleanupLogs, without startup activation. Five real PostgreSQL owner cases pass3.116s, plus a prior focused Linux race3.452s; the last test addition also proves501 committed orphan batches drain250/250/1. Tests prove receipt age versus old/future telemetry time,601-log250/250/101 progress, exact retained SDK retry hash and selected OTLP sibling bytes, final-parent removal, idempotence, SKIP LOCKED, a real second-pool maintenance gate, post-log-deletion trigger failure rollback, real cancellation behind the batch gate and022 prior-row/receipt preservation/reopen. Initial RED is explicit missing-method compilation, not a durability claim. Full14-package native PostgreSQL suite passes (server47.759s/store131.997s), integration-tag vet/format pass. Full Linux race is currently running. No accepted image/database or original baseline is upgraded by this provider-only work. Next finish its full race/publication before implementing the lifecycle and isolated-image expiry/backup restoration.

- 2026-10-17: Provider1d619c3 is public with exact305-path verification. Full native integration passes; initial local full race loses a PostgreSQL connection through host.docker.internal before a preexisting issue test opens its fixture, then the same full suite through direct172.17.0.2 passes (server43.282s/store119.232s). Its remote CI39081886948/job117540354051 instead exposes an existing artifact-snapshot test sampling flaw:25ms live chunks expire before the readers' fixed1000-read budget sees any complete batch (complete0/missing1000). Primary reads the full owner and real two-stage snapshot provider, preserves that actual CI failure and repairs only test synchronization. All60 real expiry/reupload cycles, immutable bytes, complete/missing requirements and atomic missing-order assertions remain. Each phase now waits for an actual reader result and keeps live chunks renewed until observed; readers run through the writer lifecycle rather than exhausting a fixed budget. Native result6235 complete/3144 missing passes; two Linux race runs pass with11576/4924 and12484/4994. No product artifact behavior or clock is modified, no failing assertion is removed and no acceptance database is reset. Evidence: artifact-race-synchronized{,-linux}.log and retained CI logs. Worker activation/image acceptance remains separate.

- 2026-10-17: Retention lifecycle now starts inside the single application, including when log ingestion/uptime is disabled, and uses cancellable bounded passes with committed counts. One real PostgreSQL worker owner passes2.212s and Linux race1.486s, proving601 startup rows drain250/250/101 despite an idle-hour configuration, prompt idempotent stop, two actual replica pools, idle catch-up, an observed real trigger failure then recovery, and no post-stop removal. Error messages contain the owning operation without driver SQL/telemetry; progress counts follow commit. Full final14-package integration passes (server45.371s/store155.696s), full Linux race passes (server45.459s/store140.420s), integration-tag vet/format pass. The artifact-snapshot synchronization correction52cfe4a independently passes remote CI39082271807. Earlier79adfaa closure-doc CI failure and1d619c3 provider CI failure remain historical and do not replace the successful537e863 OTLP/config/corrected-source checks.
- 2026-10-17: Actual candidate imagef8c6445eee4020cec7778964cfe8367a29bcab7298dd25d57b7e4dbec0072076 upgrades a fresh logical-backup/restored copy of the4209-row/527-event/28-batch accepted OTLP baseline to022. It never updates/restarts that original. Official Python exporters in own project83 send24 real logs/8HTTP200 across protobuf/JSON identity/gzip; actual NodeSDK11.7.0 project84 sends error plus eight-byte attachment and typed log. Only those owned fixture receipts are aged91 days. Worker physically removes25 logs, retains all8 canonical batches while eight selected siblings remain readable200, then reclaims each batch after its last sibling expires; deleted detail404 and authenticated lists agree. A pre-final-expiry pg_dump is restored into a DIFFERENT recovery database and its complete row/byte snapshot is exact. A second fresh actual SDK capture in project85 proves cleanup remains active with ingestion disabled. App-only restart preserves every4233 current row/529 events/28 baseline batches, all22 migration receipts and five exact served assets; all4209 baseline row hashes remain present. Protected original image/process/configuration/mounts/rows and uploads remain exact. Existing baseline uploads are empty and the copied volume remains empty.
- 2026-10-17: Acceptance-driver failures remain distinct: PowerShell inline quoting prevented one command before execution, a closed psycopg ConnectionInfo was read after actual restore had completed, and an imported helper executed its CLI dispatch. Fixes resume verification of the same restored/current databases without reseeding, re-expiring or repeating restore. Product source is unchanged by those driver repairs. Actual lifecycle/expiry/recovery evidence is under ignored test-results/retention-acceptance-20261017/{expiry,restoration,final-runtime}.json, full-{go,race}.log and .tmp/retention-acceptance scripts/backups. Final publication/exact-tree/CI remains pending; the log-retention and all-class parent tasks stay open until that passes.

- 2026-10-17: Native log retention is accepted at7cca1c4cdbfead3e7727e7713133a699c972928c with exact307-path public identity. Matching CI39082687686/job117542749805 succeeds for formatting/vet, full PostgreSQL integration/race and image build; primary inspects its actual completed steps/log and retains ci-worker-success.log. Public runtime/README files are fetched as plaintext and byte-match the commit. Final image f8c6445 is independently verified as static Linux Go1.27.2 ELF SHA4d4f3ab41850871a011494afd1ad5f98204dbbfe45694b77a424101d48bf3e87, UID65532, exact MIT and150 CA roots. Docker metadata size31,095,666 bytes is not RSS. Normal enabled candidate is running again after the disabled-ingestion proof; only the owned candidate/disabled containers were stopped or restarted. Original accepted OTLP state and every4209 original row remain exact. Expired project83 trace-filtered logs are empty; the log-only/no-span trace route correctly returns404, not a fabricated empty forest. Exact backup restoration and current4233-row/529-event/28-batch restart pass. Logs have a complete accepted retention consumer; other retention classes, cold archival, remaining issue policies/feedback, native symbols, workspace/integrations, registry/RSS and every other unchecked goal requirement remain open. No overall parity or goal completion is claimed.

## Transaction and standalone-span retention

- [x] Validate transaction lifetime priority/inheritance and1..3650 range without activating deletion. Files: internal/config/config.go and owning configuration tests. Verify ignored-invalid-input RED, defaults, aliases/explicit precedence and minimum/maximum.
  - 2026-10-17: Three real ignored-input RED cases return nil before implementation for zero/fractional/over-limit transaction lifetimes. The loader now resolves explicit GoodSentry then GlitchTip transaction override then master, preserving independent log values. The existing retention table owns11 default/inheritance/override/class-isolation/minimum/maximum cases. Config GREEN0.606s, all14 main-module unit packages, vet/format and Linux Go1.27.2 config race1.033s pass. Evidence: ignored transaction-retention-20261017/{config-red,config-full-unit,config-race}.log. Official public installation HTML is saved with SHAe8d5ce77a23e12bf1ad95e6b680e255afdf774fa946c64034ef1361838b8a3e1. Web search authentication was unavailable; primary fetched only that official documentation directly and read no upstream server source. No transaction/span cleanup or runtime upgrade follows from this prerequisite.
- [x] Freeze complete legacy/span deletion, dependency, receipt-clock and concurrency contracts from current source evidence before implementing cleanup. Primary authors SPEC first; factual explore neither decides design nor writes artifacts.
  - 2026-10-17: Primary verified receipt-before-project capture order, exact receipt recheck for duplicate children, signed-key sorted bulk gates, opaque envelope items without a parent FK, cascading attachments/projections and row-locked legacy projector. SPEC now fixes250/250 strict receipt-age cohorts, project gates before event row locks, exact dependent ownership, independent error/log/release preservation, rollback,023 indexes, lifecycle and isolated-image/recovery acceptance. Standalone opaque orphans are explicitly reserved for file retention; no unsupported sweep is claimed.
- [x] Implement the specified bounded atomic transaction/span provider and migration023 with real PostgreSQL receipt/ownership/concurrency/rollback/reopen acceptance.
  - 2026-10-17: Primary implemented CleanupTransactions with250 legacy and250 span limits, strict receipt cutoff, maintenance try-lock, frozen legacy cohort, signed-key project gates before row locks, dependent opaque-item deletion and FK cascades. Seven owning tests pass natively4.523s and Linux Go1.27.2 race3.694s. They cover future producer timestamps on expired receipts, old producer timestamps on retained receipts, independent errors/issues/logs/canonical OTLP/releases, retained detail/duplicate identity, fresh post-expiry receipt,601/601 bounded progress, declared late span failure, busy/row-locked maintainers,023 exact prior rows/checksums/reopen, observed duplicate-child ordering on both sides of cleanup, active projector skip and cancellation after earlier removal. Evidence: ignored transaction-retention-20261017/{provider,provider-race,order}.log. The test worker returned no usable patch and was closed; primary completed and reviewed its bounded tests. No production test seam or upstream server source was used.
- [x] Activate the in-process transaction/span lifecycle and verify startup/backlog/idle/retry/shutdown plus full integration/race/vet.
  - 2026-10-17: StartTransactionRetention runs unconditionally and shares the accepted log timer/cancellation loop. The existing lifecycle owner now covers both real providers,601-row backlogs, idle catch-up, two replicas, declared failure recovery and idempotent shutdown. Initial full native/race runs exposed the test's two-commit seed, allowing a valid cleanup before its late-failure span existed; primary fixed only fixture publication to one real transaction and preserved the failure logs. Ten focused repeats pass12.335s. Full14-package native integration/vet GREEN (jobs10.221s/server51.255s/store209.810s) and Linux Go1.27.2 race GREEN (jobs9.517s/server49.361s/store203.322s) pass. Review inverted the receipt cutoff, observed owning cleanup-count RED, restored exact source bytes and verified GREEN. Evidence: ignored worker-repeated.log, full-{go,race}{,-green}.log and mutation-{red,green}.log. No test assertion was weakened or product deletion changed to hide the fixture race.
- [x] Verify isolated-image SDK/API expiry, unchanged independent telemetry, separate backup recovery, restart, exact public source and matching CI before claiming transaction/span retention. Full per-class retention and every other unchecked product requirement remain required.
  - 2026-10-17: Actual image45db3db7 on18100 passes Node11.7.0/four envelopes, legacy binary/opaque/projection expiry, receipt-only aging, native aggregate/detail/trace/raw-node reads, missing-parent survival, disabled logs/uptime cleanup, preservation of independent error attachment/log/release/opaque SDK session, exact4262-row recovery into a separate DB, and exact4257-row restart before the final login (4258 final). All4235 protected source rows and original runtime state remain exact; two later prior-acceptance sessions explain the4233-to4235 checkpoint increase. The static nonroot31,112,047-byte image has150 CA certificates and exact license; memory target remains unmeasured. Evidence: ignored first-expiry.json, final-expiry.json, recovery.json, baseline-audit.json, final-runtime.json and SDK/envelope captures. Primary corrected harness expectations on the same fixtures without reseeding, resetting or modifying originals. README documents transaction lifetime and surviving trace correlation. Await exact public tree and successful matching CI before checking this item.
  - 2026-10-17 closure: Functional commit eea9788c236fe8a738cd2da835ae5279a23a4ca8 is exact across all311 public Git blobs; all six Pages files are HTTP200 and source-exact. Matching CI39084784196/job117548962451 succeeds for format/vet, full PostgreSQL race (jobs6.654s/server43.339s/store94.962s) and actual image build. Primary inspected the actual94488-character job log, saved as ignored ci-success.log with public-tree/text/status and evidence hashes. Ten original runtime state/config/mount checkpoints and all older row protections pass again, including the existing independently explained three staging expirations and immutable4010-row reference. Preserve the new accepted4258-row/530-event/28-batch/23-migration18100 runtime, stopped disabled candidate, independent recovery DB and logical backup. Close only this class; full retention and all other unchecked goal capabilities remain open.

## Error-event retention

- [x] Validate event lifetime priority/inheritance and1..3650 range without activating deletion. Extend the current config owner with defaults, aliases, winning-value validation, class isolation and limits; prove actual ignored-input RED.
  - 2026-10-17: Three actual RED cases return nil before implementation for zero, fractional and excessive selected event lifetimes. Config.EventRetentionDays now resolves explicit GoodSentry then GlitchTip event override then master, independently of logs/transactions. The existing owner, renamed TestTelemetryRetentionConfiguration to reflect its actual scope, covers13 default/inheritance/override/isolation/minimum/maximum cases. All14 unit packages and vet pass (config0.785s); Linux Go1.27.2 config race1.036s passes. Evidence: ignored error-retention-20261017/config-{red,unit,race}.log and the existing official installation reference capture. No cleanup/lifecycle/README capability claim follows from configuration alone. Closing transaction documentation f92f613 now has successful CI39085124713/job117549951256.
- [x] Freeze complete receipt/dependent/group-summary/empty-origin and concurrency contracts from current owners before storage/lifecycle implementation. Preserve monotonic receipt progress and independent history/telemetry.
  - 2026-10-17: Bounded explore reports counter/topology/FK/read owners; primary verifies015 receipt-progress intent, capture updates, current recount, descending row order, project-gate ordering, mute calculation, origins and native IssueDetail200 with empty payload. SPEC fixes250 frozen receipt IDs, sorted exclusive project gates, at most250 descending current-owner locks before SKIP LOCKED event pins, atomic dependent removal/recount and unchanged monotonic progress/history/policies. Empty roots remain readable/triageable and gain a truthful UI state; no event is fabricated. Primary verified that native renderIssue currently tolerates an empty payload but labels its blank metadata as an event. New acceptance DB is an exact logical-backup copy of4258 rows/530 events/28 batches/23 receipts; no new app or cleanup is running on it yet. Full provider/lifecycle/image/API/browser/recovery gates remain required.
- [x] Implement bounded atomic error-event expiry/recount with real receipt/group/progress/concurrency/rollback/migration checks.
  - 2026-10-17: CleanupEvents/024 pass11 owning PostgreSQL tests natively7.758s and Linux Go1.27.2 race6.775s. Actual no-removal RED precedes implementation. A601-event regression catches repeated affected owners multiplying retained count to87750 instead of351; deduplicating owners at the retention caller repairs the cause, preserving the strict count assertion. Tests protect producer-versus-receipt age,250 bounded passes, exact frozen cutoff from the actual cleanup transaction's PG xact_start, unowned message receipts, mixed/empty/merged summaries, comments/mutes/assignment/resolution/routes/history, fresh/duplicate and detached-origin reuse, observed capture-before/after and stale duplicate ordering, in-flight merge owner reread, post-expiry triage, busy/locked maintainers, late recount error/cancellation rollback and024/reopen. Fixture timestamps use exact PostgreSQL microseconds after the initial submicrosecond comparison failure; no production precision policy changed. Evidence: ignored provider-red.log, provider-shared-owner-red.log, provider-owner-green.log, provider-{complete,complete-race}.log and prior attempts. No protected baseline or upstream server source was used.
- [x] Activate the error lifecycle and verify all three retention providers through the existing real worker owner, full integration/race/vet and native empty-issue browser acceptance.
  - 2026-10-17: StartEventRetention runs unconditionally with the validated event lifetime and shared timer/cancellation owner. The existing real worker test covers all three classes,601-row startup drain, idle catch-up, two replicas, declared failure retry and idempotent stop (3.814s). All14-package native integration/vet pass (jobs12.236s/server62.481s/store217.076s); Linux Go1.27.2 race passes (jobs13.758s/server62.898s/store200.588s). After the scoped CSS fix, server/config unit checks0.979s/0.386s and integration vet pass. The cutoff inversion fails the owning receipt-count assertion, exact source restoration returns GREEN. Primary reviewed the bounded worker's renderIssue diff and its existing event-list contract; actual Edge retained/empty/recovered views, policy/comment mutations and safe literal hostile title pass320/768/1440. Final3802aaa4 verifies visible data and hidden/open/cancel confirmation at every width. Evidence: ignored workers.log, full-{go,race}.log, final-unit.log, mutation-{red,green}.log and browser phase reports/screenshots. No new API flag or test-only seam was added.
- [x] Preserve the existing issue deletion confirmation visibility while rendering empty issue history.
  - 2026-10-17: Actual screenshots reveal the existing form-stack display grid overriding the section's hidden attribute. The real browser visibility assertion fails true-versus-false on initial a9e50db3. A single scoped selector on the existing confirmation restores hidden/open/cancel behavior, with generated embedded CSS from the pinned build stage. Fresh image3802aaa4 on the same database passes that regression and retained-payload rendering at320/768/1440. Preserve the initial image/container as gs-event-retention-initial-20261017; no reset/reseed or protected-original upgrade occurs. This restores the approved deletion control, changes no controller or global hidden behavior, and preserves all4301 pre-style rows with only the verification login adding a session. Evidence: ignored visibility-{red,green}.log, browser/visibility-*.json/screenshots and before-style-upgrade-rows.json. Final empty-state expiry/restart acceptance remains pending.
- [x] Verify actual isolated-image SDK/API expiry, preserved surviving issue/trace data, separate backup recovery, restart, exact public source and successful matching CI. General retention and all other unchecked goal requirements remain open.
  - 2026-10-17: Actual final3802aaa4 on18103 passes four owned receipt/attachment expirations over two stages, real Node11.7.0 seed/same-ID recovery/retained duplicate, preserved route/history/mute progress, corrected mixed/empty issue counts, compatible selected/raw404 versus readable empty issue200, disabled logs/uptime cleanup and independent two-node trace/log preservation. A separate4290-row pre-expiry backup restores exactly. Phase2 compares every row, allowing only the selected receipts/dependents and affected count fields to change; final browser's explicit comment/triage actions are separate. Exact restart preserves4305 rows/530 events/28 canonical batches/24 migration receipts/38 tables; all4258 baseline rows and older runtime states stay exact. Static Go1.27.2 Linux amd64 image is UID65532:65532,31,131,334 bytes,150 CA certificates and exact daisyUI license, with five source-exact served assets; RSS remains unmeasured. Evidence: ignored first-expiry.json, final-expiry.json, sdk-recovery.json, recovery.json, final-runtime.json, accepted-rows/runtime.json and actual SDK/envelope/browser files. The default-sandbox Docker inspection denial interrupted only a read after the SDK recovery succeeded; primary resumed that same proof with approved access and did not replay/reseed. README documents event retention and readable empty history. Exact public source and successful matching CI still gate closure.
  - 2026-10-17 closure: Functional8884b9d883946a9007767bf07a35e9807e1da182 matches all316 public Git blobs and all six live Pages files. Matching CI39087725482/job117557705536 completes successfully for format/vet, PostgreSQL race (jobs7.741s/server40.758s/store90.845s) and actual image build. Primary inspected the full96899-character job log with no workflow errors, preserved as ignored ci-success.log plus public-tree/text/status and evidence hashes. Earlier row/state protections pass again, including all4258 source rows, the4235/4209 checkpoints, the explained three staging expirations and immutable4010-row reference. Preserve accepted gs-event-retention-verify-20261017 on18103/database goodsentry_event_retention_20261017 (4305 rows/530 events/28 batches/24 migrations), its stopped initial/disabled peers, independent recovery DB and logical backups. Close only error-event retention; all-class retention and every other unchecked product requirement stay open.

## Uptime check retention

- [x] Freeze completion clock, child ownership, cached-state/lease/alert preservation and missing-result presentation contracts before implementation.
  - 2026-10-17: Primary verifies the read-only explore report against004, monitorNow's actual DB clock after parent locking, completeMonitorCheck/Heartbeat, history pagination and independent check-ID delivery snapshots. completed_at is the durable server completion stamp; no created_at exists and none is invented. SPEC fixes250 child-only deletes, no parent locks/updates, preserved lease fences/status thresholds/outbox,025 index-only migration and native/browser recovery gates. The two4305-row baseline monitors are verified paused; no original is changed. Official installation reference is freshly fetched with SHAe8d5ce77a23e12bf1ad95e6b680e255afdf774fa946c64034ef1361838b8a3e1 and its uptime alias/master inheritance saved under ignored uptime-retention-20261017/public-install.{html,json}. Web-tool authentication remains unavailable, so primary fetched only the official documentation directly. Closing error documentation d79da03 CI39088112899/job117558865058 succeeds.
- [x] Validate uptime lifetime priority/inheritance and1..3650 range without activating deletion; extend the existing configuration owner and prove ignored-input RED.
  - 2026-10-17: Three actual ignored-input RED cases return nil before implementation for selected zero/fractional/over-limit uptime values. Config.UptimeRetentionDays now resolves explicit GoodSentry, then GlitchTip uptime alias, then master, independently of the other classes. The existing retention owner covers15 default/inheritance/override/isolation/minimum/maximum cases. All14 unit packages and vet pass (config0.849s); Linux Go1.27.2 config race1.039s passes. Evidence: ignored uptime-retention-20261017/config-{red,unit,race}.log. No worker, cleanup or README capability claim follows from configuration alone.
- [x] Implement025 and bounded child-only cleanup with real completion-age, monitor/lease/alert preservation, concurrency, rollback/cancellation and migration/reopen checks.
  - 2026-10-17: CleanupMonitorChecks/025 pass eight owning PostgreSQL tests natively5.115s and Linux Go1.27.2 race4.338s. Actual no-removal RED precedes implementation; a reversed age predicate is independently caught (one fresh removal versus two expired), then exact source restoration passes. Coverage protects completion rather than start age, strict frozen boundary from the real cleanup transaction,601 rows across250/250/101 bounded passes, preserved parent/status/threshold/schedule/active lease and alert snapshots, a waiting heartbeat plus old-lease fencing, no parent lock, row/maintainer contention, late failure/cancellation rollback and index-only025/reopen with exact old receipts. The cutoff test explicitly reads back its retained row after cleanup; no race-prone snapshot can hide its deletion. Evidence: ignored uptime-retention-20261017/provider-{red,complete,complete-race}.log, mutation-{red,restored-green}.log and earlier attempts. No worker or original acceptance runtime is changed by this increment.
- [x] Activate the fourth shared retention lifecycle and truthful missing-result UI; verify real lifecycle plus full integration/race/vet and browser states.
  - 2026-10-17: StartUptimeRetention runs unconditionally through the existing lifecycle. Extending its actual PostgreSQL matrix to four classes produces the intended no-drain RED, then proves601-row startup draining, two replicas, idle catch-up, declared SQL retry and prompt repeated stop. Full14 packages pass native integration (jobs9.893s/server46.197s/store195.737s), vet and Linux race (jobs15.335s/server57.219s/store171.878s). Initial actual image600eceb0 completes eight HTTP/TCP/heartbeat checks with threshold2 and four sent transition snapshots on a new4305-row source-exact copy. Seven owned expired completions disappear while the recent completion with an old start survives; all cached API state, alerts and independent rows remain exact. Actual Edge RED shows Not checked yet for expired history; the narrow latest-result fallback and status notice pass on fresh17918bfd image at320/768/1440, including never-checked, filtered far-page latest, capability controls, literal hostile name and hidden/open/cancel deletion controls. Primary inspected mobile/desktop screenshots. Same database/volume and every previous row survive the image replacement; initial container is preserved stopped. A verifier incorrectly decoded existing row hashes as JSON after expiry; primary resumed comparison against the saved aged cohort, accounted for the one RED-browser session and did not re-age/reseed. Separate4331-row pre-expiry backup restores exactly. Evidence: ignored integration/race/vet/final-unit logs, lifecycle-red.log, actual-probes.json, first-expiry.json, ui-upgrade.json, recovery.json, initial/final build and browser RED/GREEN files. Fresh-result/disabled cleanup/restart/public-CI acceptance remains pending.
- [x] Verify actual isolated-image probes/heartbeats, expiry, cached-state/transition preservation, fresh-result recovery, disabled-feature cleanup, independent backup restore/restart, exact public source and successful matching CI. File/release retention, archival and all other unchecked goal requirements stay open.
  - 2026-10-17: Final17918bfd image on18106 verifies five fresh completions after seven prior expirations, continuing both probes from cached Down and heartbeat Up/no-change then Down/Up; all four original delivery/attempt snapshots survive and eight transitions are sent in total. Disabled uptime/logs then remove the six remaining owned checks, with every other row exact, including monitors/leases/schedules/rules/deliveries/attempts. Thirteen owned physical checks expire across both stages. Final Edge recovered/empty states pass320/768/1440; four monitors remain readable (three cached Up and one never checked). Exact restart preserves4342 rows/530 events/28 canonical batches/25 migration receipts/38 tables; separate4331-row pre-expiry recovery and all4305 source rows remain exact. The static Go1.27.2 Linux image is UID65532:65532 and31,136,561 bytes, with150 CA certificates, exact license and five source-exact served assets; memory remains unmeasured. Primary corrected two verifier expectations on the same data: the existing heartbeat reason is heartbeat_received and a disabled POST receives the existing405 fallback while management reads404. Failed logs/screenshots and explicit resumed evidence are preserved; no product behavior was changed to satisfy those expectations. Preserve accepted18106, stopped initial/disabled containers, probe fixture, separate recovery DB and logical backups. README now describes completion lifetime and retained state. Exact public source and successful matching CI still gate closure.
  - 2026-10-17 closure: Functional ef0d962e86c3b945884cf9ac8e3122ce371be409 matches all321 public Git blobs and all six live Pages files. Matching CI39090223884/job117565204685 succeeds for formatting/vet, PostgreSQL race (jobs7.548s/server38.399s/store71.782s) and actual image build. Primary inspected the99784-character job log with zero workflow error markers and saved ignored ci-success.log, public-tree/text/status and92 hashed evidence/backup files. An initial Pages DNS lookup failure was transient; a later normal lookup and all six content checks succeed without deployment changes. Six prior audit owners pass again: all4305/4258/4235 checkpoints, the4209-row OTLP acceptance, the separately explained three staging expirations with immutable4010-row reference and all ten original runtime states. Preserve accepted gs-uptime-retention-verify-20261017 on18106/database goodsentry_uptime_retention_20261017 (4342 rows/530 events/28 batches/25 migrations), stopped initial/disabled peers, probe fixture and separate recovery/backups. Close only uptime-history retention; file/release retention, cold archival and every other unchecked product requirement remain required.

## Completed-file and release retention

- [x] Specify the file/master inheritance and independent release365 configuration prerequisite without activating cleanup, using the captured official installation reference.
- [x] Validate both typed lifetimes and aliases through the existing owning tests, actual ignored-input RED, full unit/vet and Linux config race.
  - 2026-10-17: Six actual ignored-input RED cases return nil before implementation for selected zero/fractional/over-limit file/release values. Config.FileRetentionDays inherits the validated master after explicit/legacy aliases; Config.ReleaseRetentionDays resolves its independent explicit/legacy/default365 chain. The existing owner now covers20 default/inheritance/override/empty/isolation/minimum/maximum cases, including ignored invalid lower-priority inputs. All14 unit packages and vet pass (config0.642s), and Linux Go1.27.2 config race1.040s passes. Evidence: ignored file-retention-20261017/config-{red,unit,vet,race}.log. No cleanup, migration, worker or README capability claim follows from this prerequisite. The explorer's factual report is preserved under ignored uptime-retention-20261017/file-release-explore-report.txt; primary verification remains required.
- [x] Verify the explorer's consequential storage/lock facts and unresolved capture/release consumers, then specify the completed-file provider/lifecycle/acceptance contracts before implementation. Release provider elaboration remains required separately.
  - 2026-10-17: Primary verifies003/010/014, native map write/read, bundle write, DeleteRelease/releaseIdentity/observeRelease, CaptureItems/standalone items, Ingest/mapEvent and signed issue-project gates. The report's claim that write releaseTransaction uses repeatable-read is incorrect: writes use default read-committed; reads are repeatable-read. Native map upload also holds authorized membership/project SHARE before release observation, and bundle already takes empty-release artifact gates. created_at exists on all three file tables; defaults use transaction now(), so026 will change future defaults only to actual insertion time. SourceMaps already owns one repeatable-read budget/bytes snapshot; persisted issue release references are deliberately FK-free snapshots. SPEC fixes250 candidates per file class, frozen IDs, issue-project then artifact gates, no release/parent row locks, empty-release native writer serialization, retained event/history state, unconditional lifecycle and new-copy recovery/browser/public-CI gates. No cleanup has shipped from these decisions yet.
- [x] Implement and verify completed-file retention, including source maps and independent opaque orphans, without weakening accepted upload, capture and recovery contracts.
  - [x] Implement026 and the bounded CleanupFiles provider, including empty-release native upload serialization, with real state/clock/ordering/snapshot/rollback/migration ownership checks.
    - 2026-10-17: Eleven file owners plus seven existing map/bundle/release writer tests pass natively13.248s and Linux Go1.27.2 race13.270s; integration-tag vet passes. The actual no-op RED removes0/0/0 instead of2/1/2. Another real RED catches a debug-only upload acknowledging its expired old generation; moving its existing artifact gate outside release observation repairs that race. Tests cover first-receipt retries/conflicts, retained selector coalescing/bytes, fresh map/child/orphan generations with unchanged parents,601 receipts per class across250/250/101 passes, capture/orphan/native/multi-project-bundle ordering on both sides of cleanup, insertion clocks after actual gate waits, exact frozen cutoff, replacement hash IDs, busy/row-locked maintainers, later statement failure/deferred commit failure/cancellation with prior removals,026/default-only historical preservation/reopen and live map snapshot survival. A read-committed mutation loses actual preflight bytes and fails its owner; exact source restoration returns GREEN. One observer initially missed a newly queued PID because PostgreSQL cached pg_stat_activity in its transaction; clearing only that diagnostic snapshot per poll fixes the harness, preserving the original failure log and all product assertions. The test worker failed before edits with provider402/insufficient funds; primary completed and reviewed its bounded tests, with no reset or production seam. Official PostgreSQL view semantics were fetched directly after the web tool's persistent401, supporting the fixture-only updatable view; actual SQL deletion/read and mutation prove the consumer. Evidence: ignored file-retention-20261017/provider-state-red.log, debug-upload-red.log, snapshot-mutation-red.log, provider-complete.log, provider-complete-race.log, provider-vet.log and prior attempts. No periodic file cleanup is active yet.
  - [x] Activate the fifth shared retention lifecycle, extend the real lifecycle matrix and pass full integration/race/vet before actual-image acceptance.
    - 2026-10-17: StartFileRetention is wired unconditionally in the one application process. The shared real lifecycle owner expands to five classes and proves startup601-per-class backlog, idle catch-up, two replicas, late third-table SQL retry/atomicity and repeated prompt cancellation. The intentional no-drain callback RED precedes implementation; its three-table backlog is committed atomically. All14 packages pass fresh native PostgreSQL integration (jobs11.192s/server50.471s/store234.873s), integration-tag vet and Linux Go1.27.2 race (jobs13.486s/server57.431s/store210.088s). Actual image0426c8b8 starts on18114/new volume and upgrades only the new goodsentry_file_retention_20261017 copy: all4342 source rows survive, plus026 for4343 current rows/530 events/28 canonical batches/38 tables. Static ELF, UID65532:65532,150 CA certificates, exact daisyUI license and five source-exact served assets pass; image31,159,697 bytes is not RSS. Protected source and eleven original runtime states stay exact. Pinned official sentry-cli3.8.0 Windows x86_64 is downloaded with exact published SHA2562257cf6805a616f5c3ee291a549ebbba021190048b646adc006beb4e8cdef7fd and its actual version verified, ready for SDK/CLI acceptance. Evidence: ignored lifecycle-red.log, integration/race/vet/build logs, startup-runtime/rows.json, cli-client.json and baseline/protected snapshots. No owned SDK cohort, aging or backup-recovery proof has run on this copy yet; do not treat startup as completed file retention.
  - [x] Verify actual SDK/CLI/source-map/attachment/opaque expiry, preserved historical frames and independent state, same-identity recovery, disabled cleanup, separate backup restore/restart, native browser states, exact public source and successful matching CI.
    - 2026-10-17: Actual CLI3.8.0 injection succeeds, but its prefix URL is rejected locally before any HTTP request. On the same database/volume/image, a domain-only root candidate preserves all4354 pre-switch rows; the prefix peer is stopped and preserved. Existing projects/token/native map and injected DebugID are reused, and CLI root upload succeeds. Real Node11.7.0 sends five200 envelopes with a mapped original line7, preserved Windows minified/raw location, one SDK attachment, three actual opaque items, two spans and one log. Evidence: ignored setup/root/client logs, setup-root.json, sdk-seed.log, initial-sdk-inspection.json and private SDK/envelope/owned files. Primary discovers no attachment consumer at all in current Store/server/UI, correcting SPEC's false assumption about an existing404 path. No fixture is reseeded or aged. Native attachment access below becomes an explicit prerequisite; compatible management attachment parity remains required separately.
  - [x] Deliver native event attachment metadata/download access and UI under the corrected SPEC contract before closing file retention.
    - [x] Implement snapshot-authorized Store reads and027 index with real containment/current-member/bytes/paging/expiry/reopen owners.
      - 2026-10-17: Native Store reader's actual missing-feature RED denies a real member's existing three files. Three owning tests plus existing compatible-event owners pass natively6.793s and Linux Go1.27.2 race6.197s. They cover exact binary/empty bytes, metadata-only reads and bounded long names/types,25+1 paging without payloads, read-side row immutability, exact SDK/UUID/uppercase references, wrong event/group/project/file and outsider denial, current membership revocation, merged alias read then detached-event rejection after unmerge, retained-parent empty/selected404 after actual file cleanup, same-hash fresh children and027 index-only historical row/checksum/reopen preservation. Reuse of compatibleEventTransaction keeps membership/topology/data in one snapshot; no raw event JSON or unscoped second fetch is needed. This provider adds no HTTP route or UI claim yet. Evidence: ignored attachment-store-{red,green,scope-green,race}.log. The actual SDK cohort remains unaged on its same root candidate.
    - [x] Add session HTTP metadata/binary routes and real credential/query/header/HEAD/hostile-byte checks.
      - 2026-10-17: Actual missing-route HTTP RED precedes native session routes. The owning real PostgreSQL/HTTP test passes natively2.685s and Linux Go1.27.2 race1.613s, covering exact binary/empty bytes, safe bounded Unicode/control/path filenames, forced download MIME/disposition/length/no-store/nosniff, HEAD without a body, strict queries/references, missing files, SDK-key-only and bearer-only denial after positive ingest/compatible-API controls, member access then current-membership revocation, and actual expiry with a readable retained parent. Existing native cookie authentication intentionally ignores Authorization; an initial test incorrectly expected the compatible bearer precedence and was corrected without changing auth. Evidence: ignored attachment-http-{red,complete,complete-race}.log and preserved earlier attempts. UI and actual-image upgrade remain pending.
    - [x] Add native issue attachment loading/retry/paging/download UI and verify actual candidate upgrade/browser bytes/expiry at320/768/1440, retaining the existing SDK cohort and all original states.
      - 2026-10-17: Shared binary requests and the native Event attachments panel pass actual Edge pagination25/3, exact8-byte/empty/hostile downloads, owner/member access, safe literal HTML, offline retry and keyboard disclosure at320/768/1024/1440. Visual inspection found a2100-character name dominating the table; a real height RED precedes the compact native disclosure, then GREEN verifies expansion/collapse and primary inspects mobile/desktop screenshots. Same-cohort SDK duplicate adds27 files without changing its parent; its additional opaque session explains the fourth item. The initial browser expected the pre-ingest path ../ instead of actual stored basename ..; primary corrects only that verifier and preserves its failure. Full14-package native integration (server52.987s/store187.143s), vet and Linux race (server47.461s/store163.478s) pass before actual-image acceptance. Initial027 upgrade preserves4398 rows plus one migration; UI-only upgrade preserves all4399 rows on the same database/volume, with both old peers stopped and preserved. Current9f30b1ca is UID65532:65532,31,201,169 bytes with150 CA certificates, exact license and five exact assets; RSS remains unmeasured. One subsequent member-browser login is a separately intended session row. Evidence: ignored attachment-full-{integration,race,vet}.log, attachment-{upgrade,compact-upgrade}.json, SDK UI/preflight and browser attempts/captures. Actual expiry UI, recovery/restart, public source and matching CI still gate closure; no file receipt has been aged.
- 2026-10-17 closure: Completed-file retention and its native attachment prerequisite are accepted at functional5abbd1398b3e96b6ac288c123c483136e6a14fa0: all334 public blobs and six live Pages files match exactly. CI39095358406/job117580551398 succeeds for formatting/vet, full PostgreSQL race (jobs8.696s/server41.111s/store101.012s) and image build; primary inspects the actual100917-character log. Actual Node11.7.0/CLI3.8.0 expire45 owned file receipts over two stages (three map generations,31 attachments,11 opaque items), preserving every other row exactly in each stage. Same-ID retry recreates bytes without parent progress; the same injected build reuploads as a new map and restores later mapping while mapped/unmapped historical events stay exact. Both features disabled still clean; independent two-node trace and retained comments/assignment/mute survive. Browser proves owner/member binary and empty downloads, real offline/loading retry, no requests for a no-event issue, stale selected404, late/first empty pages, empty maps and surviving trace at320/768/1024/1440. The separate4406-row pre-expiry restore and final4374-row/533-event/28-batch/27-migration/38-table restart stay exact. All4342 protected source rows, eleven original runtime checkpoints and six earlier audit owners pass. Current static Go1.27.2 nonroot9f30b1ca image is31,201,169 bytes with150 CA certificates, exact license and five served assets; RSS remains unmeasured. Four earlier owned peers remain stopped. The backup verifier's Windows encoding error is repaired only in its writer and resumed without repeating comment/triage; another verifier initially rejected SDK-supplied minified source context, then compares the actual raw frame exactly instead. Both failures are preserved, with no reseed/reset/replayed capture or product change. Evidence: ignored first-expiry.json, sdk-recovery.json, final-file-expiry.json, file-recovery.json, final-runtime.json, all-prior-baseline-audits.json, functional-ci-success.log, public-tree/text/status, accepted rows/runtime and SDK/CLI/browser artifacts. README now documents only verified native attachment/file behavior and CLI root restriction. Close only this file class; compatible attachment management, release retention, archival and every other unchecked goal requirement remain required.
- [x] Implement and verify release retention with preserved historical issue state and surviving telemetry, full actual-image recovery and exact public CI. Native symbol/debug files, archival and every other unchecked product requirement remain required.
  - [x] Verify primary storage/writer/policy/reader contracts and specify first-identity receipt age, independent files, shared scope, gates,250-parent cascade budget and complete acceptance before code.
    - 2026-10-17: Primary verifies010/014 ownership and immutable snapshot fields, releaseIdentity/create/update/delete/deploy observation and sorted multi-version spans. Existing nextIssueRelease excludes the baseline version. Native release detail and commits/deploys use existing missing404/list omission behavior; UI handles failed detail via its shared page error. SPEC defines a new shared identity gate before INSERT to avoid pre-wait receipt age, try gates/frozen IDs/strict fixed-duration cutoff, no parent/file locks and unchanged manual deletion. Scope is metadata only; files and repository catalog remain independently owned. Test-audit and temporal modeling precede implementation; factual worker402 occurs before edits and is closed, with primary continuing focused reads. Evidence: ignored release-retention-20261017/primary-facts.txt and explore-unavailable.txt. No provider or lifecycle is implemented yet.
  - [x] Implement028, CleanupReleases and the shared identity gate with real state/batch/clock/frozen/snapshot/busy/rollback/commit/cancellation/migration owners and a proven no-gate timing RED.
    - 2026-10-17: Eight new owning tests and existing release/file/resolution consumers pass natively46.176s and Linux Go1.27.2 race44.582s; integration-tag vet passes. Real no-op RED returns0 instead of2; no-writer-gate RED stamps a replacement identity before cleanup finishes, then moving the actual gate before INSERT produces GREEN. A read-committed mutation loses metadata from the in-flight reader and fails its owner; exact source restoration returns GREEN. Tests cover metadata-only cascades with retained maps/bytes/telemetry/alerts/selected and pending resolution snapshots, open/archived/shared identities with later association/fresh traffic without lifetime refresh, preserved old commit/user release clocks, duplicate no-op versus fresh generations,601-parent250/250/101 progress, frozen cutoff and IDs under America/New_York, strict boundary/newly eligible/replaced identities, actual busy duplicate writer plus independent progress, row-locked and replica skips, second-parent statement/deferred-commit/cancel rollback with prior deletion evidence and lock release, and028 historical receipt/reopen preservation. Initial fixtures incorrectly supplied a next_release version, recreated a different candidate before checking baseline-only recovery, combined a parameterized SQL command batch, and used LOCK TABLE outside a transaction; primary corrected only those owning fixtures with all failed attempts retained. Official public install documentation is fetched directly after the web tool401 and confirms release365/file inheritance; no server implementation is read. Evidence: ignored provider-state-owned-red.log, writer-clock-no-gate-red.log, snapshot-read-committed-red.log, provider-{complete,siblings,siblings-race,vet}.log and public-install.json/html. No release maintenance lifecycle or actual-image acceptance runs yet.
  - [x] Activate the sixth real shared lifecycle and pass full native PostgreSQL integration/vet and Linux race.
    - 2026-10-17: Actual no-drain callback RED leaves the601-release backlog; the real provider then passes startup draining, idle catch-up, two replicas, declared SQL retry and repeated prompt stop in the existing six-class lifecycle matrix. StartReleaseRetention is unconditional in the one application process with static failed-operation logs and committed removedReleases diagnostics. All14 packages pass fresh native PostgreSQL integration (jobs13.204s/server59.410s/store250.985s), integration vet and Linux Go1.27.2 race (jobs17.916s/server61.783s/store241.860s). Evidence: ignored lifecycle-{red,green}.log, full-{integration,vet,race}.log and build.log. Actual-image SDK/CLI/expiry/recovery acceptance remains open.
  - [x] Verify a new exact logical copy/image with actual CLI/SDK/native/compatible/browser expiry, same-version recovery, retained files and resolution/history state, disabled cleanup, independent backup restore/restart, exact public source and successful matching CI.
    - 2026-10-17: Fresh imageb5b4c3a6 starts on18118/new upload volume and goodsentry_release_retention_20261017, an exact4374-row logical copy of accepted files18114. All source rows survive plus028 for4375 rows/533 events/28 batches/28 migrations/38 tables. Source preflight confirms no pre-existing365-day expired releases. Copy-only file lifetime730 exceeds release365 to verify independent files later; no source configuration changes. Static Linux amd64 Go1.27.2, UID65532:65532,31,214,134 bytes,150 CA certificates, exact daisyUI license and five exact assets pass; RSS remains unmeasured. All4374 source rows and sixteen original runtime states remain exact. Primary initially dispatched start before restore completion; Docker125 created no container, then verified terminal restore and missing handle before starting that same copied DB without re-preparing. Evidence: ignored source-preflight/prepare/start/start-resumed.log, startup-runtime/rows.json and baseline/protected snapshots. No owned SDK/CLI cohort or aged release receipt exists yet; startup is not completed release acceptance.
    - 2026-10-18 closure: Release retention is accepted at functional73f4c3342aa97ac0cae765c80c6d7639e362cfa0:340 public Git blobs and six live Pages files match exactly. Matching CI39097651020/job117587310008 succeeds for formatting/vet, full PostgreSQL race (jobs8.940s/server42.162s/store112.255s) and image build; primary inspects its actual101897-character log. CLI3.8.0 creates/shared-associates/finalizes/injects/uploads and reads recreated identity; Node11.7.0 sends mapped error/attachment/profile/session, independent two-node trace/log and pending/muted issue cohorts. Two real stages expire eight owned identities, ten associations, one commit, one deploy and two deploy-project rows; every other row is exact across each stage. Retained SDK retry preserves parent/file receipts without recreating metadata, native existing-map upload creates a fresh release identity with unchanged file bytes/age, later captures still map without bundle reupload, same-baseline recreation does not bind itself, genuinely new next release binds then regresses on that release's error while both activity snapshots retain old baseline, and mute progress continues to99. Final expiry runs with logs/uptime disabled; project release list is empty while maps/files/events/history remain. Actual Edge checks missing selected metadata, surviving/empty lists, saved selected policy, pending/regressed history, mute progress, exact binary download, maps and trace at320/768/1024/1440. Separate4442-row pre-expiry restore and final4447-row/540-event/28-batch/28-migration/38-table restart remain exact. Static Linux Go1.27.2 nonrootb5b4c3a6 is31,214,134 bytes,150 CA certificates, exact license and five served assets; RSS remains unmeasured. All prior accepted data rows and the immutable4010-row reference pass independent audits. An external Docker Desktop shutdown interrupted the session: original process states changed and the engine normalized HostConfig.Dns null to[]; do not claim pre-interruption process equality. Original checkpoints are preserved, all images/configs/mounts/other host fields and all4374 source rows remain exact, old applications stay stopped, existing PostgreSQL/reference containers recover in place, and sixteen new post-interruption checkpoints are exact. No volume/database/cohort is reset or reseeded. Fixture failures (CLI debugId camelCase, UI text expectations, incorrect resolved expectation after target-release capture, and a browser deliberately overlapping final restart) are preserved and corrected/resumed using the same durable artifacts. Evidence: ignored first-expiry.json, identity-recovery.json, final-expiry.json, recovery.json, final-runtime.json, all-prior-rows-audit.json, external-engine*.json, post-engine*.json, functional-ci-success.log, public-tree/text/status, evidence-index.json, accepted rows/runtime/backups and actual SDK/CLI/browser captures. README documents verified release semantics; cold archival, native symbols, compatible attachment management and every other unchecked full-goal requirement remain open.


## Current continuous-main delivery

- [x] Make the empty-volume Compose quickstart work with `docker compose up` and no `.env`: open mode generates a 32-byte random `SECRET_KEY` in the persistent uploads volume, required mode rejects a missing explicit key, and restart reuses the same key and implicit owner. Verify with the config owner and a disposable Compose `up`/restart; document required-mode setup before exposure.
  - 2026-10-23: `go test ./internal/config` passes for generated-key reuse, `0600` permissions, and explicit-key enforcement in required mode. An isolated Compose project with no `.env` and empty volumes starts the application; `/api/v1/setup` reports `required:false`, `/api/v1/me` works without a cookie and returns one owner/organization. After app restart, the response is byte-identical and PostgreSQL still has one matching owner and organization (`1:1`); the secret SHA-256 is unchanged. A one-off required-mode start without an explicit key exits with the expected error. Temporary containers, volumes and image were removed; Docker build cache returned to `0 B`.
- [x] Deliver compatible event attachment list/retrieve/download with stable numeric API IDs, SHA-1, attachment types, filters, scoped credentials and preserved native identities.
  - Metadata review identified unbounded SQL strings. Real PostgreSQL `TestCompatibleAttachmentsBoundPublicMetadataAndPreserveOriginals` fails before repair with name=2100, MIME=312, type=150, disclosure=false. The corrected projection caps 2048/256/128 Unicode characters and discloses truncation. Owner acceptance passes after repair: store 1.957s, HTTP server 1.377s, event 0.005s. Final full PostgreSQL race and actual-image SDK/download/retry/restart acceptance pass. Evidence: test-results/goal-attachment-metadata-red.log, goal-owning-green.log and goal-release/runtime-*.json.
- [x] Deliver actual browser CSP reporting through legacy and Reporting API wire formats, existing issue inspection and public project-key URLs.
  - Primary/advisor verified the CSP3 reporting IDL permits null in six modern fields. Six pure regression cases reproduced the rejection. The owner now preserves each standard nullable field and its original JSON null, and pure CSP checks pass. Non-nullable fields remain strict. Evidence: test-results/goal-csp-null-red.log and goal-csp-null-green.log. Final PostgreSQL race and prefixed actual-image HTTP capture/reads/restart pass.
  - Fractional/exponent Reporting API age cases reproduce rejection by the former integer-only parser. Whole/fraction binary64 conversion now preserves integral milliseconds and accepts those forms, with overflow controls. The first overflow fixture rounded below the binary64 limit; the corrected negative control uses 9223372036854.778. Failed attempts are retained in goal-csp-age-first-green.log, followed by goal-csp-age-green.log. Full PostgreSQL race and final vet pass.
- [x] Simplify README with complete linked compatibility/configuration guides, preserving factual gaps.
  - README is 93 lines; ten linked guides preserve all 13 former command/example blocks. Primary checks all 53 local destinations and balanced fences; independent review checks 59 destinations/anchors without findings. Evidence: test-results/goal-doc-links.json.
- [x] Forward optional Compose .env settings and preserve configurable external/default internal ports.
  - Actual Compose configuration resolves external port 18097/internal 8000, prefixed legacy domain, disabled legacy feature flags and optional retention/OTLP/body limits. Commit 90fe0f6 preserves the one-application deployment.
- [x] Publish verified amd64/arm64 application images after exact-commit CI, with anonymous pull and startup proof.
  - Previous worker logs in /tmp were unavailable; test-results/goal-release/release-audit-recovery.json preserves that limit. Primary now builds exact95fa983 source into an OCI archive and verifies both Go1.27.2 static ELF architectures, UID65532:65532, writable owned data directory,150 CA certificates and exact daisyUI license. The amd64 image runs on isolated18138/database goodsentry_goal_delivery_20261018; official Go0.50.0 proves binary/empty attachments plus the existing errors/transactions/traces/typed-log owner. CSP legacy/modern-null/fractional-age captures and malformed-batch refusal pass. Same-ID retry and same-app restart preserve all retained fields/bytes. Initial retry comparison wrongly expected unchanged project navigation after its own CSP captures; original failure/baseline are preserved, the one owned neighbor is accounted for, and all other fields compare exact. Evidence: goal-release/{build.log,oci-inspection.json,runtime-*.json,retry-fixture-reconciliation.json,go-sdk-full.log}. Remote publication/anonymous pull were pending at that checkpoint and pass in the public acceptance below.
- [x] Publish coherent increments directly on main through gh and verify matching CI/Pages/image state.
  - gh 2.102.0 downloaded from official release and SHA-256 verified. Browser device authorization completed as jerolan. Automatic permission review initially timed out twice; after recovery, actual gh identity/repository/run checks succeed. Final full PostgreSQL race passes (jobs20.888s/server51.149s/store189.146s) and integration-tag vet passes. No credentials are committed. Evidence: test-results/goal-full-race.log. Publication was pending at this checkpoint and passes below.

  - Public acceptance: main5dd302f has396 exact public blobs and successful CI39203971428 (jobs17.256s/server49.101s/store125.043s; actual144507-character log inspected). Container workflow39204296181 publishes amd64/arm64 with provenance/SBOM. Anonymous Docker pull and independent registry requests verify revision/main/latest index448aae4f130d793676da94dcd604ad001813bc942a1a553307e9bb462d43e1b0 and both source revisions/users. Published amd64 binary is byte-exact to local acceptance. Public-image SDK retry, reads/downloads and its own restart preserve all owned fields. Six live Pages files and eight served app assets match. Old local candidate remains stopped. Evidence: test-results/goal-release/{ci-summary.json,image-success.log,registry-verification.json,anonymous-pull.log,published-binary-verification.json,runtime-public*.json,pages-verification.json,served-assets.json}.
- [x] Use the verified registry image by default in Compose, document source builds/updates and redeploy the existing one-page landing.

  - Local quickstart proof: a fresh isolated compose+.env-only directory with no Dockerfile starts public main without credentials or source compilation on18140; official Go0.50.0 verifies errors, transactions/trace ancestry and typed logs. Config checks cover image override, internal8000/external18140, domain prefix and optional retention/body/OTLP settings. Independent diff review corrects the revision-tag immutability wording; only image digests guarantee artifact identity. Commit/push and updated Pages are pending after automatic review reports exhausted workspace credits. User requests local work for now. Evidence: goal-release/quickstart-{up.log,sdk.log,config.json}.

## SSL certificate monitors

- [x] Retrieve public SSL enum/common schemas and specify explicit GoodSentry TLS behavior before code.
  - Official public OpenAPI/documentation captured with URL/time/hash under test-results/goal-ssl-contract; no upstream application source read. Fresh advisor review identifies raw TCP close, original hostname verification, shared deadline, existing failure reasons and total-probe latency as required details. Primary checks the existing nettarget/prober/migration/API/dialog owners and the real SMTP CA-subprocess precedent. No SSL runtime implementation or equivalence is accepted yet.
- [x] Extend the existing pure configuration owner and prove SSL origin admission RED/GREEN.
  - Six valid origins fail on the former unsupported-type behavior; current owner accepts seven origins including original case/leading-zero port spelling, and rejects credentials/query/fragment/non-root/encoded targets, malformed hosts/ports and expected body. Pure checks pass; no sockets are used. Evidence: goal-ssl-contract/config-{red,green,local}.log.
- [x] Implement the real TLS probe and CA-subprocess contracts, preserving private-address policy and cancellation.
  - Local probe uses the pinned dialer, original hostname, system roots/TLS1.2, whole-probe context and raw TCP close. CA-subprocess tests cover DNS/IP/SNI, trusted/untrusted/hostname/expired/future certificates, shared-server TLS1.2 rotation, old protocol refusal, no HTTP bytes, private blocking, timeout and cancellation. 2026-10-20: `go test -race ./internal/uptime -run '^TestProbeSSL'` passes10.179s under an allowed loopback environment; all eight certificate/protocol cases and timeout/cancellation are green. This supersedes the historical socket-denial attempt.
- [x] Expand033 and verify storage/API/scheduler wiring with preserved old rows and shared state/alerts/history.
  - Additive033 and migration/reopen/checksum/null-status provider owner pass against isolated PostgreSQL. The HTTP fixture creates/reads/edits/pauses SSL through the real application, and the jobs suite records an actual untrusted TLS result plus reopened history. Full server/jobs integration/race passes. Evidence: goal-provider-webhooks/ssl-store.log and full-integration-race.log.
- [x] Responsive native SSL creation/edit/pause/history, full race/vet, public image/restart and current browser acceptance pass.
  - Local dialog offers SSL certificate, HTTPS-origin input and clear whole-check guidance; HTTP-specific fields retain valid roundtrip values. SSL TLS failures have a readable label. JS syntax, full integration/race and responsive Chromium list/detail/edit at320/768/1440 pass, including paused status and hidden HTTP body controls. New SSL image/restart, external CA target and publication remain pending. Existing README/landing still list SSL as unfinished. Commit/publication remain blocked by Git permissions. Keep all original runtime/data checkpoints and the verified public5dd302f image intact. Evidence: goal-browser-people-ssl/{report.json,PNGs}.

## Account credential lifecycle

- [x] Inspect existing issuance, sessions, account routes, mail transport and test owners; specify the shared generation and authenticated password-change contract.
  - Primary confirms exactly two production issuers: CreateSession called by bootstrap/login and CreateAPIToken called by the native session route. Current middleware admits cookies independently from Authorization; compatible bearer precedence is separate. Existing alert MIME/outbox requires rule/project context and does not establish account email. SPEC.md now separates credential-generation, account-password, account-mail, account-recovery and membership-invitations without closing any required parent.
  - Independent review identifies a stale-snapshot revocation race under Repeatable Read; primary verifies the public PostgreSQL18 isolation/row-lock contracts and requires explicit Read Committed. The existing issue assignment user SHARE lock has a different project advisory resource from token issuance, so the apparent lock-order inversion supplies no concrete cycle in the inspected paths. Review also corrects commit-result uncertainty: local pgx5.11.0 closes the transaction after a Commit error, so deferred rollback cannot prove the server did not commit. The contract requires new sign-in verification and no automatic retry. These are design findings, not runtime proofs.
- [x] Execute the prepared real PostgreSQL proofs for035 and generation-aware issuance, including both issuer/change orderings, stale login/session refusal, all-user credential boundaries and rollback.
  - 2026-10-20: the complete prepared store family passes in5.544s under race on isolated PostgreSQL, including035 preservation/reopen, stale/zero generation refusal, Repeatable Read waiter rechecks, password-change revocation, concurrent winners and overflow rollback.
- [x] Session/CSRF-protected password route and responsive Account form pass browser/image/restart/public verification.
  - 2026-10-19 local implementation: native `POST /api/v1/account/password`, atomic scalar password replacement/version increment/all-session deletion/cross-organization personal-token revocation, native CSRF/session transport, Account view and sidebar entry are prepared. The UI covers current/new/confirm passwords, pending, mismatch,400,401,503/unknown-result and success-to-sign-in states without persisting passwords.
  - Prepared PostgreSQL owners cover only-owner revocation/all sessions and tokens across organizations, preserved prior revoked timestamps, unrelated member credentials and project SDK keys, stale generation, two concurrent changes and bigint-overflow rollback. 2026-10-20: these owners and the real HTTP owner pass against isolated PostgreSQL; the latter covers missing CSRF, wrong current password, multibyte and strict body rejection, cookie expiry, old-password rejection, new-password login and fresh-session access. Evidence: goal-provider-webhooks/{store-account-family.log,server-http.log}.
  - The earlier checkpoint wrote no account production code, schema migration or account tests. The current local increment supersedes that state for credential generation, authenticated password mutation and password recovery; invitations and membership management are now implemented and pass their prepared PostgreSQL/HTTP owners. SMTP sinks, browser, copied-image/restart and publication remain pending; commits/publication remain blocked by Git permissions and are not retried against the user's local-work preference.
  - Historical 2026-10-19 socket denial is superseded on 2026-10-20: the SSL TLS suite now passes. All10 draft source hashes still match local-source-status.json; HEAD remains5dd302f and the index is unchanged. Evidence: goal-ssl-contract/tls-blocker-recheck.log and local-source-status.json remain provenance, not current status.

## Slack incoming webhooks

- [x] Specify independent Slack destination, wire-message, acknowledgment, rate-limit scheduling and preservation contracts from official public documentation.
  - The previous ordering incorrectly treated pending SSL verification as a dependency of all local work. Slack has no such dependency; its own runtime gates remain mandatory. Additive034 extends only the durable recipient enum; the account-generation reservation moved to035 and is implemented separately. Existing generic webhooks and email keep their behavior.
- [x] Implement the production-consumed message encoder and configuration/retry contracts with meaningful local RED/GREEN.
- [x] Wire Slack acknowledgment, rate-limit retry floors,034 preservation and the native recipient choice.
  - 2026-10-20: the production sender routes Slack through the real webhook transport; the 200/`ok` acknowledgment, bounded response, 429 minimum and durable retry hint pass the full local race suite. Additive034 preservation/reopen and API roundtrip pass against isolated PostgreSQL. The native dialog exposes Slack and explains that the URL is secret.
- [ ] Verify an authorized real Slack delivery receipt before provider closure.
  - No Slack destination is currently supplied or authorized for a real send. Responsive Chromium passes for all seven choices, payload delivery is covered by real loopback receiver tests, and exact-source CI/image plus copied-image/restart acceptance are recorded below. Only the external Slack receipt remains unverified.

- 2026-10-19 local Slack increment: the existing configuration owner and new production-consumed webhook codec/outcome owner reproduce real missing-contract REDs, then pass GREEN. Frequency/down/recovery/test messages preserve safe plain text, Unicode limits, accessible fallback plus prefix-aware link, body acknowledgment semantics and generic JSON/email behavior. A separate retry-floor RED proves the former900-second ceiling discards larger receiver minimums; its repaired policy preserves2h and representable durations. Evidence: goal-slack-contract/{pure-red.log,pure-green-final.log,retry-floor-red.log}.
- Fresh code review identifies an admitted public prefix with spaces producing a broken Slack link. Primary independently verifies the loader and Slack contract, adds a focused RED, canonicalizes URL escaping and passes the full pure owner including Unicode prefixes. The earlier accessibility review similarly adds the public URL to screen-reader fallback before implementation. Evidence: goal-slack-contract/prefix-red.log.
- Final current-source local verification passes the selected pure/configuration/MIME suite with Linux race1.180s, all14 integration-tag packages compile with no tests selected, integration-tag vet, app build, alert JavaScript syntax and diff checks. The pure selector explicitly excludes the newly prepared TestSlackWebhookDelivery network owner. Compilation is not real PostgreSQL/HTTP/TLS/browser proof. Evidence: goal-slack-contract/{pure-race-final.log,all-compile-final.log,vet-final.log}.
- Slack dispatch, exact acknowledgments and Retry-After outcomes are wired to the existing sender and fenced completion; additive034 and the native recipient choice/help are prepared. Primary inspects the new122-line migration/durable-retry PostgreSQL owner and real HTTP POST/deadline owner, adding a parent deadline to keep the latter bounded under regression. Both only compile; real network/database assertions, queue/API/browser/image/restart/provider/publication acceptance remain unexecuted. No accepted database is upgraded and no Slack message is sent. The10 SSL draft input hashes remain unchanged. A worker credit error loses its earlier compilation session, so primary supersedes it with the actual all-package compile result.

## Discord, Teams, Google Chat, Feishu, ntfy, Zulip and Mattermost webhooks

- [x] Specify the six explicit recipient types, provider text limits, mention defenses, acknowledgments and additive038 preservation from public provider contracts and GlitchTip's public OpenAPI.
- [x] Implement production-consumed message encoders, dispatch, bounded acknowledgments, destination validation, migration038 and native recipient choices.
- [x] Prove serialization, escaping, Unicode bounds, invalid contexts, unchanged Slack/generic contracts, actual POST bytes/headers and Feishu/Zulip acknowledgment bodies with pure and loopback HTTP race tests.
- [x] Prove038 prior-row/receipt preservation, reopen and durable admission of every provider type against real PostgreSQL; prove the HTTP rule/test roundtrip and seven-recipient identity through the existing application fixture.
- [x] Prove the seven provider choices, labels/help, internal dialog scrolling and real rule roundtrip in responsive Chromium at320/768/1440 with no page/request/console errors.
- [x] Specify GlitchTip-compatible Mattermost incoming webhook behavior from the official provider contract. Mattermost uses bounded `application/json` with Markdown-safe text and a generated resource link, suppresses mention-like telemetry, and requires HTTP 200 plus `ok` acknowledgment; no real destination is authorized.
- [x] Add the explicit Mattermost recipient type and additive migration `058` without changing prior alert rows, deliveries, or migration receipts.
- [x] Verify exact serialization, contexts, Markdown/mention escaping, Unicode and URL limits, actual loopback POST bytes/headers, acknowledgments, oversized bodies, transport errors, and unchanged existing provider behavior under race detection.
- [x] Verify migration preservation/reopen and durable Mattermost intent admission with real PostgreSQL; round-trip rule creation/testing through the existing HTTP fixture.
- [x] Expose the Mattermost destination choice/help and verify the alert dialog roundtrip at 320/768/1440 without page, request, or console errors.
- [x] Verify exact-source full integration/race CI and publish/inspect the amd64/arm64 image with `gh`.
- [x] Keep the Teams resource URL visible in Workflows MessageCards whose action buttons are not rendered.
  - Contract: include the canonical URL after the shared summary in the plain-text card section, truncating only the summary to stay within3000 Unicode characters; preserve the `OpenUri` action for destinations that render it.
  - Verify: extend `TestProviderMessages` to assert the visible URL and keep it through summary truncation; run the provider race tests, vet, app build and exact-source CI. No external destination is used by these checks.
  - Source: [Microsoft Incoming Webhooks and Workflows](https://learn.microsoft.com/en-us/microsoftteams/platform/webhooks-and-connectors/how-to/add-incoming-webhook).
  - Local evidence (2026-10-23): the extended owner failed before the fix because the MessageCard section contained only the summary. The bounded Teams section now preserves the canonical URL through long-summary truncation. `go test -race ./internal/notify -count=1`, `go vet -tags=integration ./...`, the trim-path app build and `git diff --check` pass. The package race suite used permitted loopback listeners; no external destination was contacted.
  - Public evidence: commit `1927126fd7038dfa812ab956a42410b29104e26b` passes exact-source CI [40057513012](https://github.com/jerolan/good-sentry/actions/runs/40057513012), including formatting, vet, the full PostgreSQL integration/race suite and image build. Image workflow [40057965618](https://github.com/jerolan/good-sentry/actions/runs/40057965618) publishes `main`, `latest` and the full-SHA tag for linux/amd64 and linux/arm64 at OCI index `sha256:fb7b899eec4bca2f9fb15d91d82cb2ef964d229e4f1c5a74c3943ca00c53200d`.
- [ ] Obtain authorized real delivery receipts from representative destinations before advertising all formats as available.
  - 2026-10-20 local evidence: `go test -race ./internal/notify` passes6.235s, including real loopback POSTs for Slack, Discord, Teams, Google Chat, Feishu, ntfy and Zulip. Isolated PostgreSQL passes034/038 together and the complete prepared account/people/invitation family; actual HTTP passes password recovery, invitations, people/teams, account/password and the seven-provider alert roundtrip. No external provider endpoint is configured. Real receipts and the final image remain pending.
  - Browser evidence: `tests/compat/provider_alerts_browser.mjs` creates a real rule, verifies all seven recipient types and reload roundtrip, and captures seven screenshots at320/768/1440 plus the scrolled 320px dialog bottom. Report has zero page errors, failed requests or console errors. Evidence: goal-browser-providers/report.json and PNGs.
  - 2026-10-22 Mattermost local evidence: full `go test -race ./internal/notify`, integration-tag compilation, `go vet -tags=integration ./...`, trim-path app build, and the Mattermost PostgreSQL migration plus server alert roundtrip pass. A fresh Compose image is ready; after app restart, `/api/v1/setup` remains `required:false` and cookie-free `/api/v1/me` returns the single persisted owner and organization. Chromium verifies all eight destinations and rule reload at 320/768/1440 with zero page, request, or console errors. Evidence: `test-results/acceptance-mattermost-20261022/report.json` and screenshots. Exact-source CI/image and real provider receipts remain pending.
  - 2026-10-24 published-image acceptance: commit `d8ea4a7b65c05e04c4f0fa236d9fc473433360eb` passes CI [40058471632](https://github.com/jerolan/good-sentry/actions/runs/40058471632) and image publication [40058930410](https://github.com/jerolan/good-sentry/actions/runs/40058930410), digest `sha256:985d62815788bc7fd35aa30fd1f28e5d6497c1d6856b3c50e2380089b35adaf2` for linux/amd64 and linux/arm64. Compose used that exact digest, an empty project with no `.env`, and fresh PostgreSQL/uploads volumes. Readiness and dashboard returned 200, `/api/v1/setup` returned `required:false`, and cookie-free `/api/v1/me` returned the single implicit owner and organization. Restarting only the app preserved a byte-identical `/api/v1/me` response and IDs; PostgreSQL counts remained one user, organization, membership and team. Temporary containers, network, volumes, image and Compose files were removed; the two pre-existing `sentrygo_*` volumes remain untouched and Docker has zero containers and zero build cache.

## Generation-bound credential issuance

- [x] Add035 and carry a nonpublic authentication generation from bootstrap/login/session admission into mandatory session/token issuance.
  - 2026-10-19 local implementation: additive035, hidden generation loading, mandatory-generation CreateSession/CreateAPIToken, explicit Read Committed, user SHARE pinning and401 stale-session mapping are wired through every production and fixture caller. The authenticated password route is implemented separately below; no reset/email flow was added.
  - Unit JSON contract and Linux race pass; all14 integration-tag packages compile normally and with race instrumentation, integration-tag vet passes and the app builds. Evidence: goal-auth-generation/{unit-race.log,all-compile.log,all-race-compile.log,vet.log}.
- [x] Verify existing-data preservation and both stale/blocked issuance paths with real PostgreSQL and actual native HTTP callers.
  - Real tests are prepared for035 preservation/reopen, stale/zero generation atomic refusal, fresh recovery, Repeatable Read default waiting for both issuers and an actual stale admitted HTTP session receiving401 without a token. They compile but did not execute: DATABASE_URL is unset and no local PostgreSQL socket exists. Full non-integration testing is separately stopped by socket: operation not permitted in listener tests. Keep this verification child open.
- [x] Complete full regression, copied-image/restart and public-source acceptance before closing the prerequisite.
  - This prerequisite does not include email recovery, invitations or membership management; those remain required. Authenticated password mutation is prepared in the Account lifecycle child. The reviewed positive-generation and explicit Read Committed/user-lock contract is unchanged. Pending SSL/Slack runtime gates do not prevent this independent local preparation.

## Current-source verification on 2026-10-20

- [x] Reproduce the debug-file assembly contract failure and return501 until real assembly is available. Existing HTTP/PostgreSQL acceptance fails with200 before the repair and passes with race detection afterward.
- [x] Repair and verify quota admission for oversized untrusted span counts. The existing real HTTP owner now reproduces an exhausted-key bypass with415 instead of429 before the fix.
- [x] Complete current-source integration/race, copied-image/browser/restart and matching-source publication before closing any prepared capability. Existing installations remain untouched; verification uses the separate goodsentry-verification-20261020 PostgreSQL.

- 2026-10-20 verification: complete current-source PostgreSQL integration/race passes all14 packages after the real debug-file200-to501 repair and quota-bypass415-to429 regression. Store231.895s and final server66.640s; integration-tag vet, formatting and JavaScript syntax pass. The final nonroot image is32,157,558 bytes. An upgrade from public5dd302f preserves all49 original rows across40 tables through migration042, including original receipts and SDK telemetry. New Chromium image workflows pass teams/invitations/SSL detail, organization rename/delete and all seven provider choices at320/768/1440; SMTP/browser recovery proves actual email, reset, replay refusal and request-floor suppression. Go0.50.0 quickstart plus Node11.7.0 and Browser11.7.0 SDK errors/transactions/typed logs pass. This is local evidence; current-source publication, restart and the remaining explicit capability gates stay open. Evidence: test-results/acceptance-20261020 and /tmp/goodsentry-*-20261020.log.

- Invitation regression: the real SMTP/browser owner reproduces a valid 184-byte email being permanently rejected because the worker supplied it as a120-character display name. A generic greeting repairs delivery. The same owner then exposes a320px long-address layout overflow; add wrapping to the invitation details before final browser acceptance. Both RED logs and the stopped earlier image remain preserved.

## Delivery checkpoint after current-source acceptance

- [x] Verify the independent public-image quickstart without a Dockerfile or registry login. Official Go SDK errors, transactions, trace ancestry and typed logs pass. Published quickstart source `3b8b711` has successful CI, Pages and multiarchitecture image workflows.
- [x] Verify account changes without organization membership, confirmation mismatch, cleared secret inputs, rejection of the former password and revocation of a second session in Chromium at 320, 768 and 1440 pixels. The older image fails the new Account consumer before mutation.
- [x] Verify actual SMTP password recovery and invitation acceptance. Recovery rejects replay and suppresses another message inside the request floor. The running invitation worker accepts a valid 184-byte email; its new account joins as member and the consumed link rejects replay. Both the long-email delivery failure and 320px overflow have retained REDs before their repairs.
- [x] Deliver optional TOTP two-factor authentication at `1379f82`: additive053 sealed AES-256-GCM secrets, RFC6238 codes with a single-use step ledger, ten hashed single-use recovery codes and five-minute single-use login challenges with a five-attempt budget. Enrollment, confirmation, regeneration, password-confirmed disable, the cookie-free challenge sign-in and recovery-code sign-in pass real PostgreSQL/HTTP and Chromium at390/1440. WebAuthn second factors and organization-level `require2FA` enforcement remain open.
- [x] Verify a public CA certificate through the actual SSL scheduler, then pause the owned monitor. The completed result is up/ok with no HTTP status. Controlled TLS fixtures separately verify expired, untrusted, wrong-host and rotated certificates.
- [x] Verify upgrade preservation of 49 original rows across 40 tables through migration 042, then exact restart and independent restore of 146 rows across 44 tables. Authenticated SDK event projection also compares exactly before/after app restart. Originals and stopped predecessor containers remain preserved.
- [x] Verify official Go `0.50.0`, Python `2.72.0`, Node `11.7.0` and Browser `11.7.0` errors/transactions/typed logs plus the official protobuf/JSON OTLP exporters. These are real transport and authenticated-read proofs on the isolated image.
- [x] Verify the actual SHA-256-pinned official sentry-cli `3.8.0`. Its organization consumer first exposes `require2fa` versus the required `require2FA` spelling; the existing HTTP owner reproduces the wire failure, the serializer repair passes, and info/organizations list/projects list all complete on the corrected image. The temporary personal credential is revoked after each attempt.
- [x] Complete exact-source full race/vet and publish the local commits on main, then verify matching CI, Pages, both registry architectures and the public image before closing this delivery checkpoint.

Evidence is in `test-results/acceptance-20261020` and the corresponding `/tmp/goodsentry-*-20261020.log` files. The automatic approval credit failure interrupted only the unexecuted container switch; later real Docker/GitHub checks succeeded, and the CLI image was then started and tested. This supersedes the temporary tooling block without erasing its provenance. External provider-channel receipts, native crash processing, broader management/integrations and every other unchecked goal requirement remain open. Full Sentry compatibility and GlitchTip parity are not claimed.

- Final current-source full PostgreSQL race passes all packages after the CLI field repair, with server68.030s and store208.477s. Integration-tag vet, formatting and diff checks pass. Official CLI info, organizations list and projects list all return success on image d194409c; all eight served application assets match their current source bytes. Registry/Pages publication remains the next gate. Evidence: /tmp/goodsentry-cli-final-race-20261020.log and test-results/acceptance-20261020/cli-results.json.


## Public acceptance of the workspace delivery

Functional revision `76a1f1e74263e15fdef2898f9d772945f95eba84` is published directly on main without a new PR. Its 474 public blobs match the local tree. Matching CI `39517908283` passes full PostgreSQL race, vet, formatting and image build; actual logs are inspected and retained, with server 69.352s and store 164.401s. Pages `39517908337` and image publication `39518405340` succeed. All six live landing files match source.

Anonymous pull and registry requests verify amd64 and arm64, UID `65532:65532` and the exact source revision. At this acceptance, revision/main/latest shared index `sha256:2d0f8f2700e69e2e00eee9471716afef4043a137baf0a84b63febd8b7881b1f8`. The public amd64 binary matches the accepted local binary exactly, SHA-256 `5acb02011ef00272bcc696fe8482bc6e5ea42ab434ee9e9498577f8e4e49a04b`; the daisyUI license is exact and 150 CA certificates are present.

An initial public restore comparison overlapped the CLI's own login and included one additional session. Preserve its failed 147-row snapshot and interference report; it is not accepted as a restoration proof. A separate pristine restore preserves all 146 source rows across 44 tables before any client is run. The public image then passes official Node errors/transactions/typed logs and official sentry-cli `3.8.0` info, organizations list and projects list. Its subsequent app-only restart preserves all 161 rows across 44 tables, the selected SDK event projection and eight source-exact served assets.

Evidence: `test-results/acceptance-20261020/{public-source.json,ci-public.json,ci-public.log,pages.json,registry.json,public-pull.log,image-public.log,public-binary.json,public-restore-interference.json,public-restored-isolated.json,public-isolated-node-sdk.log,public-isolated-cli,public-before-restart.json,public-after-restart.json,public-runtime.json}`. Preserve the owned public checkpoint `goodsentry-public-pristine-76a1f1e-20261020` on 18172/database `goodsentry_public_pristine_76a1f1e`, its predecessor copies, backups and all original installations. This closes this delivery checkpoint only. Native crash processing, MCP/OAuth, archival, integrations, external provider receipts and every other unchecked specification requirement remain required.

## MCP transport and workspace discovery prerequisite

- [x] Verify the official SDK version and public transport changes. v1.8.0 is published on 2026-09-04, requires Go 1.25, supports 2026-07-28 and earlier protocols, and is downloaded with the recorded Go module sum. This is SDK/client documentation, not upstream server source.
- [x] Specify request-local Bearer authority, transport/Origin/body admission, typed workspace tools, SQL grant containment and incomplete-feature availability in SPEC.md before implementation.
- [x] Add real HTTP/PostgreSQL acceptance using the official SDK client. Before implementation both requested protocol versions failed initialization with Method Not Allowed. The implemented owner now passes exact 2026-07-28/2025-11-25 negotiation, schemas, defaults, selected/all project grants, numeric organization shadowing, paging, invalid inputs, protocol errors and revocation/membership removal on an already connected client.
- [x] Implement the specified transport and two metadata tools without changing credential grants/defaults or enabling the executable's incomplete MCP switch. Real HTTP also passes cookie/SDK-key separation, expiry, duplicate credentials/Origin, query/method/media admission, localhost protection, configured/chunked/1 MiB limits and generic storage failure. Both protocols cancel a PostgreSQL query blocked by a real table lock after client disconnect. Enforce no-store at response commit because the SDK overwrites its preliminary header.
- [x] Verify owning and native sibling checks, full integration/race/vet, default image and matching public source/CI; keep all prior checkpoints intact.
- [x] All17 named MCP tools and OAuth PKCE/refresh/revocation are implemented and accepted at public8160b3.

MCP prerequisite local verification: `go test -race -tags=integration ./...` passes, including fresh server execution (65.596 s); unchanged packages reuse prior accepted cache results. `go vet -tags=integration ./...`, `go mod verify`, formatting and diff whitespace pass. Evidence: `test-results/acceptance-mcp-20261020/integration-race.log`. Default-image and public matching-source/CI acceptance remain pending. The preceding documentation revision f488dc5 also completed public CI39520008811 successfully. The complete MCP/OAuth parent remains open.

## MCP issue investigation prerequisite

- [x] Specify the five tool inputs/results, inherited scopes, immediate status semantics and one-snapshot event/parent lookup before implementation.
- [x] Add owning PostgreSQL and official-client HTTP proofs. Before implementation the official client failed on unknown get_issue. Now all MCP owners pass (server13.610 s); the snapshot owner passes during 30 native merge/split cycles and 160 paired reads (store2.131 s). Inputs, selected/foreign organization grants, event ambiguity, latest/merged IDs, pagination, environment filters, write-only reads, denied-write state/activity and immediate policy clearing are exercised.
- [x] Implement CompatibleEventLookup and the five tools using native event/issue projections and state transitions. No migration or credential grant change. Explicit output schemas preserve JSON values and optional sibling fields; default SDK inference incorrectly described RawMessage as bytes and required omitted embedded navigation. Native merge correctly takes the latest occurrence title; the snapshot owner protects identity/count association rather than assuming the original root title remains.
- [x] Complete owning/native sibling/full race/vet/default-image/public acceptance without enabling the incomplete parent feature. Accepted only after the project-event scope correction at 8df395f; the earlier eddacdb remains superseded.
- [x] All performance, log, alert/monitor tools and OAuth are implemented and publicly accepted.

MCP workspace prerequisite accepted at public `4749e5b985341d62016df4bce793802fcdc5781f`: all 477 published blobs match local source; CI39522360972 passed with fresh server71.504 s and store161.852 s plus the application image build. Container workflow39522899164 passed. Anonymous registry verification confirms amd64/arm64, UID65532:65532, exact revision and shared main/latest index `sha256:3eeee8a2aa0a770238f7b39faf71f47c20f90cf583251d1d6224cebc61d19727`. The anonymously pulled public amd64 executable matches the accepted local executable byte for byte: 20,930,720 bytes, SHA-256 `5b8b082fc25e773f47fcb5b6f43b4c0df7556035a95f7ed4f1906a3d0a7fbfca`.

The default image restores all 146 prior rows across 44 tables before clients, passes official Node11.7.0 errors/transactions/typed logs and CLI3.8.0 discovery, then preserves all 160 rows across 44 tables during an application-only restart. Disabled `/mcp` stays404 and enabled startup explicitly fails before database access. Evidence: `test-results/acceptance-mcp-20261020/{integration-race.log,image-local.log,image-default.json,availability-guard.log,restored-before-start.json,restored-after-start.json,node-sdk.log,cli,before-restart.json,after-restart.json,public-source.json,ci-public.json,ci-public.log,registry.json,public-pull.log,public-binary.json}`. Preserve `goodsentry-mcp-workspace-20261020` on18173 and its independent database, binary/public inspection container and prior checkpoints. This completes only the two-tool prerequisite; MCP remains unavailable to installations until the full parent is complete.

MCP issue increment: owning checks and vet pass; full integration/race is running. Default-image and public-source/CI gates remain pending. Runtime availability still refuses incomplete MCP. No acceptance of the complete capability is implied by these seven prepared tools.

MCP issue local image gates passed for `eddacdb15fee851e467a1491881577f9e780b792`: full integration/race (server97.287 s, store236.915 s, jobs26.589 s), integration-tag vet, module verification, formatting and whitespace. A clean archive of that commit builds the non-root image; default startup restores all 146 prior rows across 44 tables, official Node11.7.0 errors/transactions/typed logs and CLI3.8.0 discovery pass, and app-only restart preserves all 160 rows across 44 tables. Enabled MCP still fails before database access and disabled GET/POST `/mcp` remain404. Evidence: `test-results/acceptance-mcp-issues-20261020/{integration-race.log,image-local.log,image-default.json,availability-guard.log,restored-before-start.json,restored-after-start.json,node-sdk.log,cli,before-restart.json,after-restart.json}`. Preserve `goodsentry-mcp-issues-20261020` on18174 with database `goodsentry_mcp_issues_20261020` and all preceding checkpoints. Public matching-source/CI/image gates remain pending; the complete MCP/OAuth parent remains required.

Primary review found an existing-scope boundary requiring correction before acceptance: the new generic event lookup admitted transaction receipts to event-only credentials, whereas the native API requires project:read for project events without an issue. Preserve the failed acceptance decision: eddacdb is not the accepted issue increment, and its runtime MCP gate still prevents exposure. SPEC now requires an explicit lookup filter, default exclusion, and additional project:read for unassociated events, applied before metadata/ambiguity/payload reads. Reproduce the scope failure with the official client before correction and repeat changed-source gates.

Scope correction proof: the official client first reproduced an event-only token reading an unassociated transaction receipt. The corrected owning server/store race checks pass (13.862 s / 2.563 s). The SQL metadata lookup now defaults to issue-associated receipts; explicit project:read is required to include others. A real duplicate-ID test proves excluded transaction receipts cannot create ambiguity, an explicit project selector cannot rescue them, and combined authority permits the nullable parent. Repeat full/image/public acceptance on the corrected revision before accepting the issue increment.

Corrected local gates pass at `8df395f0afdb1298e9cdb2b7426159e7182917f2`: complete integration/race (fresh server92.821 s and store230.395 s), integration-tag vet, formatting and whitespace. A clean archive builds the non-root corrected image. Its independent database restores all 146 source rows across 44 tables, official Node11.7.0 and CLI3.8.0 consumers pass, and restart preserves all 160 rows across 44 tables. Default MCP remains404 and enabled startup fails before DB access. Preserve `goodsentry-mcp-issues-scoped-20261020` on18175/database `goodsentry_mcp_issues_scoped_20261020`. Evidence files use `corrected-*` and `integration-race-corrected.log` under `test-results/acceptance-mcp-issues-20261020`. The superseded eddacdb public CI39524212609 also passed, but does not accept the scope correction or complete MCP. Accept only matching corrected public-source/CI/image evidence next.

MCP issue prerequisite accepted at corrected public `8df395f0afdb1298e9cdb2b7426159e7182917f2`. All 481 published blobs match the accepted source. CI39525321380 passes with fresh server 92.110 s, store 174.728 s and the image build; its complete 147,174-byte log is inspected. Image workflow39525789491 passes. Anonymous registry checks confirm amd64/arm64, UID `65532:65532`, exact source labels and shared main/latest index `sha256:7510cf1a3c9a06f20877091a29e427cee8aa8142b159387af673b31831c53d82`. The public amd64 executable exactly matches the accepted local executable: 21,016,736 bytes, SHA-256 `1ac97aea7ac261c7317614f8a37a948affdfc8a0fd8db501025028c700e8a8d1`. An actual public-image startup with enabled MCP and an unreachable database refuses MCP before attempting that connection.

Public evidence: `test-results/acceptance-mcp-issues-20261020/{corrected-public-source.json,corrected-ci-public.json,corrected-ci-public.log,corrected-image-public.json,corrected-registry.json,corrected-public-pull.log,corrected-public-binary.json,corrected-public-availability-guard.log}`. Preserve the public inspection container, corrected binaries, independent 18175 checkpoint and all superseded evidence. Seven internal tools are implemented and verified; the six performance tools, two log tools, alert/monitor tools, OAuth and complete enabled-feature acceptance remain required. Keep runtime MCP activation closed and the full product goal active.

## MCP log investigation prerequisite

- [x] Specify explicit opt-in log authority, selected-handler availability, native query/projection reuse and configured capacity before implementation.
- [x] Reproduce the missing opt-in path: native HTTP first returned400 for log:read, then exposed PostgreSQL23514 until additive043 preserved and extended the scope CHECK. The corrected official-client owner now passes real issuance, typed Sentry/OTLP reads, isolated pages, filters and lowered capacity.
- [x] Implement ScopedLogs/ScopedLog, selected-handler log:read issuance and both tools. Fixed organization/current membership/project intersections are in payload SQL; no existing grants/defaults are rewritten. Unknown scopes remain constrained in PostgreSQL, and the executable gate/public token form remain unchanged.
- [x] Pass real Sentry/OTLP/filter/paging/isolation/capacity owners, native siblings and full race/vet/default-image/public/recovery gates. Accepted at public d6d75cb after an actual old-image token/version42 upgrade preserved all prior rows, credential scopes and selected-project access.
- [x] Token scope/log consent UI, OAuth consent pages and enabled MCP availability are integrated and accepted.

Log prerequisite RED evidence: the native API first rejected log:read with400. After selected-handler admission, actual PostgreSQL rejected it with api_tokens_scopes_check/23514 and the API correctly returned generic503. SPEC now owns additive migration043 preserving existing rows/known-value controls. This corrects the prior no-migration assumption; do not drop scope validation or treat HTTP-only admission as acceptance.

MCP log owning checks pass: complete MCP HTTP/client owners17.663 s before the added foreign/default-mode cases; focused final log owner5.395 s, and native Sentry/OTLP detail/filter/capacity siblings5.137 s. Vet, module verification, formatting and whitespace pass. Final full race/integration is running; image/version42-backup upgrade/constraint/public gates remain unexecuted. No complete MCP activation or parent parity is claimed.

MCP log local gates pass at `d6d75cbc6d1d7947ce7878448e5e84c6258dc7ed`: full integration/race (server 98.911 s, store 224.872 s, jobs 22.278 s), vet, module verification and formatting. The default non-root image passes actual Node11.7.0 errors/transactions/typed logs and CLI3.8.0 discovery. Preserve the first 146-row backup-copy result, but reject its initial negative constraint canary: there were zero token rows, so UPDATE0 demonstrated no CHECK behavior. Its failure report and logs are retained.

The accepted local upgrade proof uses a separate version42 database and the actual public 8df395f image to issue one active token with all eight prior scopes and one selected project. Stop that owned old app before the baseline snapshot. Upgrading to043 through the final image preserves all 149 original rows across 44 tables, the active token's exact scopes and selected-project API projection; unknown and empty scope updates both fail the CHECK and roll back. After real official Node/CLI consumers, app-only restart preserves all 164 rows across 44 tables. Enabled MCP still refuses before DB access; disabled GET/POST /mcp remain404. Evidence: `test-results/acceptance-mcp-logs-20261020/{integration-race.log,image-local.log,availability-guard.log,initial-constraint-canary-rejection.json,pre043-seeded.json,post043-seeded-original-rows.json,seeded-upgrade.json,seeded-scope-check-unknown.log,seeded-scope-check-empty.log,upgraded-node-sdk.log,upgraded-cli,upgraded-before-restart.json,upgraded-after-restart.json}`. Preserve the stopped pre043 app18177, upgraded app18178/database `goodsentry_mcp_logs_upgrade_20261020`, first copy18176 and all previous checkpoints. Matching public-source/CI/image gates remain pending; nine internal tools do not complete MCP/OAuth or the product goal.

MCP log prerequisite accepted at public `d6d75cbc6d1d7947ce7878448e5e84c6258dc7ed`: all 485 published blobs match accepted source. CI39528941186 passes with fresh server 82.176 s and store 141.559 s plus the image build; its inspected public log is 146,704 bytes. Image workflow39529296611 passes. Anonymous registry checks confirm amd64/arm64, UID `65532:65532`, exact source labels, and shared main/latest index `sha256:5dff7c3cd9dd7ce316e775edcbfa95565f0fdd5984dd086b5361603e8618e43c`. The anonymously pulled public amd64 executable matches the accepted local executable exactly: 21,074,080 bytes, SHA-256 `6f5bfb3f706ea5696c8bad0e46e8e32c847284e93850787f9bcb2ee5ebb2638d`. The actual public image refuses incomplete MCP with an unreachable database before attempting a connection.

Public log evidence: `test-results/acceptance-mcp-logs-20261020/{public-source.json,public-ci.json,public-ci.log,registry.json,public-pull.log,public-binary.json,public-availability-guard.log}`. Preserve public inspection container/binaries, upgraded checkpoint18178, stopped old app18177 and every prior checkpoint. Nine of seventeen tools are now implemented. Remaining performance/alert/monitor tools, OAuth, token/consent UI, complete enabled-feature acceptance and all unrelated product parity requirements stay open; runtime MCP activation remains closed.

## MCP alert and monitor inspection prerequisite

- [x] Specify read-only scope/role, project intersection, page/filter and redaction contracts before implementation.
- [x] Reproduce missing tools with real native alert/monitor fixtures and the official client; discovery failed at the absent list_alerts schema before implementation.
- [x] Implement ScopedAlerts/ScopedMonitors and sanitized MCP summaries. Alert owner/admin, project and monitor membership grants are checked with real SQL; recipients/capability URLs stay absent.
- [x] Pass owning/native/full race/vet/default-image/upgrade/public/recovery gates. Accepted at public 15aceb2; local implementation gates pass at1e2b0a9, with matching source/CI/image/binary evidence below.

MCP alert/monitor local gates pass for `1e2b0a9`: full integration/race (server120.494 s, store255.929 s, jobs28.347 s), vet, module verification, formatting and whitespace. Official-client owners cover schemas/annotations, paging/filtering, selected/all projects, foreign organizations, owner-to-member role demotion, missing scopes, feature omission and membership removal. Alert output contains recipient types but no destinations/deliveries; monitor output omits heartbeatURL/key/leases. Native Sentry consumers remain green.

The clean non-root image restores all 146 prior rows across 44 tables before clients at version43, passes official Node11.7.0 errors/transactions/typed logs and CLI3.8.0 discovery, then app-only restart preserves all 161 rows across 44 tables. Enabled MCP fails before database access; disabled GET/POST `/mcp` remain404. Evidence: `test-results/acceptance-mcp-alerts-monitors-20261020/{integration-race.log,image-local.log,image-default.json,availability-guard.log,restored-before-start.json,restored-after-start.json,node-sdk.log,cli,before-restart.json,after-restart.json}`. Preserve container18179/database `goodsentry_mcp_alerts_monitors_20261020` and all prior checkpoints. Public source/CI/multi-architecture image/exact binary evidence is next; eleven of seventeen tools are prepared, and MCP/OAuth activation remains closed.

## MCP performance investigation prerequisite

- [x] Specify six PostgreSQL-backed tool contracts, selected-project/time semantics, ambiguity and N+1 meaning before implementation.
- [x] Add a real official-client/PostgreSQL owner with selected/excluded/foreign transaction groups, legacy indexed children, another operation and three UTC days; preimplementation discovery failed on the absent list_transaction_groups schema.
- [x] Implement scoped group/span/trend/N+1 providers and all six read tools without a new scope or migration. Legacy and native span branches are authorized in SQL; no DuckDB backend is claimed.
- [x] Pass owning/native/full race/vet/default-image/upgrade/public/binary gates. Accepted at public 8e675cc; all17 tool implementations are now present behind the incomplete MCP gate.

MCP alert/monitor prerequisite accepted at public `15aceb2921e1ceff737cb9f8b6d67b7e6d4277a9`: all 488 published blobs match accepted source. CI39533965274 passes with fresh server111.823 s/store187.016 s and image build; the inspected log is150,460 bytes. Image workflow39534419926 passes. Anonymous registry checks confirm amd64/arm64, UID65532:65532, exact source labels and shared main/latest index `sha256:30cbcb0e879b92c9195ec4266a875c25f29ded10ed508a052c98bd0240213bae`. The public amd64 executable exactly matches the image built from1e2b0a9:21,123,232 bytes, SHA-256 `4756da4b22a6104f2e50d63c5f2e006754a897714f79ec0c998fbf9d0623324b`. The public image refuses enabled incomplete MCP before database access. Evidence: `test-results/acceptance-mcp-alerts-monitors-20261020/{public-source.json,public-ci.json,public-ci.log,registry.json,public-pull.log,public-binary.json,public-availability-guard.log}`. Eleven tools remain implemented; performance public and OAuth/UI gates stay open.

MCP performance local gates pass at `5421aeb`. The official-client owner verifies all six schemas, groups/paging/sample identity, selected/all grants, ambiguity, foreign rejection, legacy child breakdown, cross-operation span groups, five real six-call N+1 findings, three ascending UTC trend buckets, missing scope and membership removal. The first full-suite attempt was invalidated by PostgreSQL `too many clients` after98 existing verification/application connections; all owned temporary apps were stopped without deleting data, reducing idle connections to9. The same source then passed full integration/race: server was freshly passed in the failed run at130.417 s, and rerun store219.325 s; all other packages passed/cached. Vet, module verification and formatting pass. Evidence: `test-results/acceptance-mcp-performance-20261020/{integration-race.log,integration-race-rerun.log,image-local.log,availability-guard.log,restored-before-start.json,restored-after-start.json,node-sdk.log,cli,before-restart.json,after-restart.json}`. The non-root image restores146 prior rows, passes Node11.7.0/CLI3.8.0, and restart preserves161 rows across44 tables. Preserve stopped verification apps/data and active performance container18180/database `goodsentry_mcp_performance_20261020`; public CI/image/binary acceptance is next.

## MCP OAuth authorization prerequisite

- [x] Specify discovery, DCR, PKCE, login/consent, grant intersection, code/refresh rotation, revocation, storage and activation gates from public standards.
- [x] Add real HTTP/PostgreSQL official-client owners. Initial metadata discovery returned404; implementation then passed DCR, login/consent, PKCE, code exchange, MCP calls, refresh rotation, membership removal and native/OAuth revocation.
- [x] Implement additive044, transactional OAuth lifecycle, metadata/registration/authorize/login/token/revoke and linked native-token revocation. Codes/refresh are keyed hashes, access tokens use native `gs_` authentication, and expired secrets have bounded cleanup.
- [x] Integrate log:read consent, themed OAuth pages, token scope UI, MCP feature state, discovery challenge and metadata while preserving defaults. MCP remains opt-in through `GOODSENTRY_ENABLE_MCP=true`.
- [x] Pass matching public source/CI/image/binary gates and mark the parent accepted. Public8160b3 has exact source, successful CI/Pages/image, amd64/arm64 registry checks and an exact binary matching eb6a898.

MCP performance prerequisite accepted at public `8e675cc32e1c4440116d4a8d42799dc071c71143`. All491 published blobs match accepted source. CI39535832874 passes with fresh server125.494 s/store173.939 s and image build; the inspected log is148,060 bytes. Image workflow39536323483 passes. Anonymous registry checks confirm amd64/arm64, UID65532:65532, exact revision and shared main/latest index `sha256:0db8bbbbe1a02d9d06d16b6f2111ef6a6abb3bc8702079ac204c381d4019d33e`. The public amd64 executable exactly matches the image built from5421aeb:21,278,880 bytes, SHA-256 `3a753e1040991d0fe0a7a53a07b88ea794a5b6cb144a03d1f889b7c4696b2fd5`. The public image refuses enabled incomplete MCP before database access. Evidence: `test-results/acceptance-mcp-performance-20261020/{public-source.json,public-ci.json,public-ci.log,registry.json,public-pull.log,public-binary.json,public-availability-guard.log}`. All17 named tools are implemented and individually accepted; OAuth, enabled UI and complete parent/model/browser acceptance remain required.

MCP OAuth local gates pass at `eb6a898`. Full integration/race passes with server138.943 s, store266.340 s and jobs32.523 s; vet/module/format/JS checks pass. The official MCP client uses the image OAuth flow for organization/project/issue/log/performance/alert/monitor reads; authorization code and refresh replay fail, rotation revokes the prior access token, native API revocation and OAuth refresh revocation both take effect next request, and removing membership invalidates current access. Login, scoped consent, logs-disabled metadata, unsafe DCR/PKCE/resource input and default404 paths pass.

The clean non-root image starts with MCP opt-in, publishes discovery metadata and returns a resource-metadata/scopes Bearer challenge. Its full flow passes registration201, consent303, code exchange200, MCP initialize200, refresh rotation, old-access401 and revoked-access401. A separate default image keeps MCP/OAuth404, restores146 prior rows, passes Node11.7.0/CLI3.8.0 and preserves162 rows across47 tables on restart. A real version42 backup upgraded through the old public8e675cc image to43, issued an active eight-scope selected-project token, then upgraded through eb6a898 to44: all43 original data tables/107 non-migration rows and the token/project projection remain exact, with only the expected migration row and three OAuth tables added. Evidence: `test-results/acceptance-mcp-oauth-20261020/{integration-race.log,image-local.log,version42-before-old-start.json,pre044-seeded.json,pre044-private-token.json,post044-without-clients.json,upgrade-preservation.json,upgrade-report.json,image-oauth-flow.json,restored-before-start.json,restored-after-start.json,node-sdk.log,cli,before-restart.json,after-restart.json}`. Preserve pre/post containers18182/18183, default18184, databases and all prior checkpoints. Matching public publication is the remaining MCP parent gate.

MCP parent accepted at public `8160b300100443b4edbc0ffd60b00f68c75d41fa`: all496 published blobs match; CI39538713048 passes with fresh server131.323 s/store180.706 s and image build; Pages39538713139 and image39539169221 pass. The live landing reflects MCP/OAuth. Anonymous registry confirms amd64/arm64, UID65532:65532 and shared main/latest index `sha256:a8753ec706dafad193ca6b059217b482df8c6c4ed370fb2c3f15e17d07004998`. Public amd64 binary matches eb6a898 exactly:21,872,800 bytes, SHA-256 `17b1c882884ac52a5c0615691e16dc6ebf027899d7f1d66a6a85728c05eff01f`. The actual public image starts with MCP enabled, publishes metadata and challenges `/mcp` with401. Evidence: `test-results/acceptance-mcp-oauth-20261020/{public-source.json,public-ci.json,public-ci.log,pages.json,pages.html,registry.json,public-pull.log,public-binary.json,image-oauth-flow.json,upgrade-report.json,upgrade-preservation.json}`. Preserve public runtime18189, OAuth upgrade/default containers, databases and all prior checkpoints.

Current public8160b3 image browser/SMTP acceptance: isolated SMTP recovery resets the password, rejects replay and suppresses a second request inside the request floor; invitation delivery creates a signed-in member and rejects replay; People creates/renames/deletes teams and manages members; SSL monitor UI is responsive at320/768/1440 with no overflow. App-only restarts preserve56/47 account rows and54/47 people/SSL rows exactly. Preserve isolated account apps/databases18186–18188 and `/tmp/goodsentry-account-smtp-20261020`. This closes account, membership, invitation and SSL UI/restart children only; social OAuth/OIDC, external provider receipts, GitLab/Grafana, crash symbolication, cold archival and the other unchecked product gates remain open.

Current public `8160b3` validation extends beyond MCP/account workflows: official sentry-go0.50.0, sentry-sdk2.72.0, @sentry/node11.7.0 and Browser11.7.0 each persist an exception, transaction and typed log with trace correlation. A second independent run is saved under `test-results/acceptance-sdk-current-20261020/{go.log,python.log,node.log,browser.log,...}`; app-only restart preserves144 rows across47 tables. A concurrent four-SDK repeat sampled the application process16 times during traffic and observed VmRSS/VmHWM `25,550,848` bytes (24.367 MiB); all four exited0. Evidence: `memory-workload.json`, `*-memory2.log`.

The same public binary passes `browser.mjs` onboarding after an exact selector correction (scope event ID to Issue summary) and classification of the expected navigation-aborted logout POST: first-owner/workspace creation, project/DSN/copy, accessible names, real hostile telemetry kept as text, search/environment persistence, resolve/comments, transaction metrics/filter, trace/log pagination/type attributes, multipart source maps and mapped stack, sign-out/login, and no overflow at320/768/1440. Restart preserves100 rows/47 tables. Evidence: `test-results/acceptance-browser-current-20261020/{onboarding3,before-restart.json,after-restart.json,restart.log}`. Preserve containers/databases18190–18193 and test fixtures.

Cross-cutting public8160b3 verification: official sentry-go0.50.0, sentry-sdk2.72.0, @sentry/node11.7.0 and Browser11.7.0 pass independent error/transaction/typed-log runs and app restart; the public Chromium workflow passes onboarding, DSN, accessibility, issue workflow, search, transactions, traces, logs, source maps and320/768/1440 responsive checks. The concurrent four-SDK workload sampled application VmRSS/VmHWM at25,550,848 bytes (24.367 MiB), PostgreSQL excluded. Current-source 5-second fuzz runs complete115,172 envelope executions and384,062 source-map-boundary executions withPASS. Full PostgreSQL race, vet, image and recovery evidence are current. Documentation now includes Compose quickstart, image upgrade, backup/restore, SDK and endpoint matrix. Evidence: `test-results/acceptance-sdk-current-20261020`, `acceptance-browser-current-20261020`, `acceptance-account-current-20261020`, `acceptance-final-current-20261020` and `acceptance-mcp-oauth-20261020`.

Native Minidump AMD64 local gates pass at `4f660fa`: full integration/race (native1.025 s, server122.769 s, store233.841 s, jobs26.536 s), integration-tag vet and module checks. The clean non-root image accepts an authenticated multipart dump, resolves `CrashFunction` at `C:\src\crash.cc:42`, retains the two-frame stack/module debug metadata and release/environment, keeps only the uploaded `.sym` row, and does not store raw minidump bytes. App-only restart preserves167 rows across47 tables. Evidence: `test-results/acceptance-native-minidump-20261020/{integration-race.log,image-local.log,image-native-flow.json,restored-before-start.json,restored-after-start.json,before-restart.json,after-restart.json}`. Preserve container18196/database and the synthetic fixture under `internal/server/testdata`. x86 and official DIF upload were subsequently implemented; ARM64, CFI/DWARF, PDB/dSYM/SymCache and complete native formats remain open, so the parent native task stays unchecked.

Chunked DIF upload is locally accepted at `2a565ae`. The actual SHA-256-pinned sentry-cli3.8.0 completes capability negotiation, initial `not_found`, gzip chunk upload, SHA-1 verification, second assembly and `state:ok` with the stored debug-file DTO. Full integration/race passes with fresh server108.421 s (unchanged store/native packages correctly cached); the clean image repeats a new-symbol upload (`Uploaded 1 missing debug information file`, processing complete) and app restart preserves174 rows across47 tables. Evidence: `test-results/acceptance-dif-20261020/{cli-image-success.log,integration-race.log,image-local.log,before-restart.json,after-restart.json}` and `acceptance-native-minidump-20261020/cli-dif-success.log`. Raw client logs are local only; the stored token file is mode0600 and no credential is committed. DIF archive/PDB/dSYM/SymCache consumption remains open.

DIF regression acceptance also confirms artifact bundles remain intact with `debug_files` advertised: sentry-cli3.8.0 `sourcemaps upload` reports `Upload type: artifact bundle`, uploads 2 files and completes processing. Evidence: `test-results/acceptance-dif-20261020/cli-sourcemap-regression.log`.

Native x86/DIF final-image local acceptance at `8978f70`: context-size selection parses synthetic AMD64 and x86 crashes, follows 32/64-bit frame-pointer chains and symbolicates Breakpad source lines; full integration/race passes (native1.012 s, server113.287 s, unchanged store cached), vet and formatting pass. The final clean image runs sentry-cli3.8.0 chunk negotiation/gzip upload/assembly for a fresh symbol and source-map artifact-bundle upload still completes, preserving both protocols. Image minidump ingestion resolves `CrashFunction` at line42, raw dumps remain unstored, and restart preserves185 rows across47 tables. Evidence: `test-results/acceptance-native-x86-20261020/{cli-image-success.log,image-minidump.json,integration-race.log,image-local.log,before-restart.json,after-restart.json}` plus `acceptance-dif-20261020/cli-sourcemap-regression.log`. ARM64, CFI/DWARF, PDB/dSYM/SymCache and archive extraction remain open; parent native task stays unchecked.

Native/DIF public acceptance at `84258933b3c246d9981bd9f2f4c093961b704056`: all503 published blobs match accepted source. CI39545752568 passes fresh native1.022 s, server131.702 s and store183.247 s plus image build; image workflow39546205589 passes. Anonymous registry confirms amd64/arm64, UID65532:65532, exact revision and shared main/latest index `sha256:db1b4bf45c2cc34b79080edde3061cbf2c298e467927f38c9f34cf90ac18a0ff`. The public amd64 executable exactly matches the final local image:21,946,528 bytes, SHA-256 `d39ac35c76af8b776ffcd333aaee057565535733fc9367179b7d9f735bbbf2e5`. Evidence: `test-results/acceptance-native-x86-20261020/{public-source.json,public-ci.json,public-ci.log,registry.json,public-pull.log,public-binary.json}`. This accepts the AMD64/x86 Minidump, textual Breakpad and official chunked DIF slices only. ARM64, CFI/DWARF, PDB/dSYM/SymCache and archive extraction remain required, so the parent native task stays open.

Native ARM64 local gates pass at `3682a21`: full integration/race (native1.019 s, server109.854 s, unchanged store cached), vet and formatting. The public context order from Breakpad/Windows for ARM64 frame pointer/link/SP/PC is covered by a synthetic context, while x64/x86 tests remain green. The clean image still completes official sentry-cli3.8.0 chunked DIF upload and app restart preserves187 rows across47 tables. Evidence: `test-results/acceptance-native-arm64-20261020/{integration-race.log,image-local.log,cli-image-success.log,before-restart.json,after-restart.json}`. CFI/DWARF, PDB/dSYM/SymCache, archive extraction and representative real receipts remain open; parent native task stays unchecked.

ARM64 public acceptance at `3e2434c7ebfc9e5b2aed3a54cbffe9c1ff40098c`: all503 published blobs match source. CI39546905029 passes native1.009 s, server121.126 s/store157.234 s and image build; image39547322718 passes. Anonymous registry confirms amd64/arm64, UID65532:65532, exact revision and shared main/latest index `sha256:b66779de197da388dac0cc0c7efdb45ea4abe11213e25eb8fd9e96f224f46d69`. Public amd64 binary matches local exactly:21,946,528 bytes, SHA-256 `6e892153c639e41e18c7f037034a8fbb13d3c705ebcd0214a869b92b9e7cfc01`. Evidence: `test-results/acceptance-native-arm64-20261020/{public-source.json,public-ci.json,public-ci.log,registry.json,public-pull.log,public-binary.json}`. CFI/DWARF, PDB/dSYM/SymCache, DIF archive extraction and representative real native receipts remain open.

Native ELF/DWARF symbolication is public at `e7448518d56098b513cb5bf8f36039d1243b3778`: all509 published blobs match source, CI39550203812 passes fresh server104.434 s and image39550598550 passes. Anonymous registry confirms amd64/arm64, UID65532:65532, exact revision and shared main/latest index `sha256:7837f12e4f58e1e615257173d64b5a8c417eb79ecc9cd2230643c815340badf4`. The public amd64 executable exactly matches the final local image:22,294,688 bytes, SHA-256 `783af65105d9fd1a67ae934b4ad00b7a1b12eea2cd6cd02bd042e12ccc084b72`. A clean non-root image accepts an authenticated ELF upload plus Minidump whose module name differs, resolves `fixture_function` at `elf-dwarf-source.c:2` by Debug ID and persists the matching `debug_id`. Evidence: `test-results/acceptance-native-dwarf-20261020/{integration-race.log,targeted-native-race.log,local-gates.json,image-local.json,image-native-flow.json,public-source.json,public-ci.json,public-ci.log,public-image.json,public-image-ci.log,public-manifest.json,registry.json,public-binary.json}`. This accepts the ELF/DWARF path and preserves Breakpad/DIF; Mach-O parsing still lacks a representative public receipt, while CFI unwinding, PDB/SymCache/dSYM archive consumption and real multi-platform crashes remain open.

Mach-O dSYM companion symbolication is public at `0ae6c9d26c56f306e1b61383614b655753d43311`: all526 published blobs match source, CI39552536661 and image39552991305 pass. Anonymous registry confirms amd64/arm64, UID65532:65532, exact revision and shared main/latest index `sha256:2af1c0e888155ce225798dfb4970025d1cbd873582d79be62fa95b5b405c971d`. The public amd64 executable matches the final local image exactly:22,294,688 bytes, SHA-256 `3efbb43acb8b49413c1399f7c7138bbc72bfe2555dfcf42429e6f8a88705b0af`. A dSYM companion whose zero-byte `__TEXT` segment has a different address from its file-backed `__DWARF` segment now selects the executable base; the clean image resolves `main` at `hello.c:3` through a differently named Minidump module and matching Debug ID. Evidence: `test-results/acceptance-native-macho-20261020/{integration-race.log,targeted-macho-race.log,local-gates.json,image-local.json,image-native-flow.json,public-source.json,public-ci.json,public-ci.log,public-image.json,public-image-ci.log,public-manifest.json,registry.json,public-pull.log,public-binary.json}`. CFI unwinding, PDB/SymCache and dSYM/DIF archive extraction remain open.

Native DWARF CFI unwinding is public at `6dc6bd345ad75f51a4403b9749e746b6319bb914`: all544 published blobs match source, CI39555756589 and image39556285400 pass. Anonymous registry confirms amd64/arm64, UID65532:65532, exact revision and shared main/latest index `sha256:f6b9c88206b9a203864a330855c687e69bba5d32d43603c6b091bc52c3db3680`. The public amd64 executable matches the local image exactly:22,356,128 bytes, SHA-256 `c29cd91a54007e08e6eda82d592902debbd2d4c3ae0243422181548dd7b7b80c`. With RBP zero, the clean image evaluates ELF `.eh_frame` and recovers `crash_frame`, `middle_frame` and `outer_frame` with source lines through an authenticated Minidump; ET_EXEC and PIE fixtures, multiple objects sharing a Debug ID, and truncated CFI fail-closed boundaries pass. Evidence: `test-results/acceptance-native-cfi-20261020/{integration-race.log,targeted-cfi-race.log,local-gates.json,image-local.json,image-native-flow.json,public-source.json,public-ci.json,public-ci.log,public-image.json,public-image-ci.log,public-manifest.json,registry.json,public-pull.log,public-binary.json}`. Uncommon expressions/signal-frame conventions, PDB/SymCache and dSYM/DIF archive extraction remain open.

Bounded ZIP debug archives are public at `efbb401f2a06a5881f0f189a3683836d62dcddc4`: all559 published blobs match source, CI39558091320 and image39558480873 pass. Anonymous registry confirms amd64/arm64, UID65532:65532, exact revision and shared main/latest index `sha256:02df787eb2c471427acc26ae137324a7b14767fceff845a7332542b201946356`. The public amd64 executable matches the local image exactly:22,360,224 bytes, SHA-256 `c727830019c902119c3e9ec8093252e2557c52d9502664b69662a5f246550334`. A bundle containing an ELF object and Mach-O dSYM expands into two parsed objects with independent symbols and CFI; the clean image resolves `main` at `hello.c:3` from the archived dSYM through a differently named module and Debug ID. Evidence: `test-results/acceptance-native-debug-archive-20261020/{integration-race.log,targeted-archive-race.log,local-gates.json,image-local.json,image-native-flow.json,public-source.json,public-ci.json,public-ci.log,public-image.json,public-image-ci.log,public-manifest.json,registry.json,public-pull.log,public-binary.json}`. PDB/SymCache, uncommon CFI expressions and nested/tar archive discovery remain open.

- 2026-10-21 UTC: TOTP two-factor authentication passes local gates at1379f82 and public acceptance at0f5b230. Additive053 stores a sealed AES-256-GCM secret, a confirmation instant, a single-use step ledger and hashed recovery codes; login challenges are single-use, expire after five minutes and die after five rejected codes. RFC6238 vectors, sealing/tamper, enrollment, replay, attempt-budget, expiry, recovery-code and credential-generation cases pass real PostgreSQL and HTTP, and the full integration race, vet, build, formatting and module checks are green. Chromium enrolls, shows ten recovery codes, keeps the enabled state across reload, completes a challenge sign-in and a recovery-code sign-in at390/1440 with no page or console error beyond the expected session probe and favicon request, then disables with the password. The published image `sha256:4c7218591cf5733c28c9a08287aebb6a0cb2e6dc36d6ef54d1d23f1d7c2286fb` repeats the whole workflow as `65532:65532`; CI39687393982 and image39687952771 pass with1249 exact blobs and byte-identical amd64/arm64 binaries. Evidence: `test-results/acceptance-two-factor-20261021`. WebAuthn second factors and organization-level `require2FA` enforcement remain open.

- 2026-10-21 UTC: The compatible retained-log API is public at e91fb9a. Additive054 projects host.name from Sentry attributes and OTLP resources, and GET /api/0/organizations/{org}/logs/ plus /logs/{id}/, /logs/resources/ and /logs/stats/ serve keyset pages with a cursor Link, distinct resource names and hourly zero-filled series. The published image sha256:0f0009dc5dde5c9674f27f22cc1d05d553d97f0b4ef7caccff9404f94676cd37 answered a real Sentry log envelope carrying warn/error levels and service/environment/host attributes: the warn row was published as warning with severityNumber13, filters and resources matched, and admission returned401/400/404 as specified. CI39691342975 and image39691944963 pass with1272 exact blobs and byte-identical amd64/arm64 binaries. Evidence: test-results/acceptance-compatible-logs-20261021. Numeric log identities stand in for GlitchTip UUIDs, and one unrelated one-second project-key rate test flaked once at a window boundary before three focused reruns passed.

## Compatible account tokens and recovery codes

- [x] Probe the running GlitchTip6 reference for exact account-token and recovery-code behavior before implementation.
  - Integer IDs, millisecond `created`, scope order, `body.payload` validation locations, empty-array acceptance, limit0/over100/negative semantics, malformed-JSON400, path422, missing404 and bidirectional cursor behavior are recorded in SPEC.
- [x] Add additive056 with user-scoped sealed tokens and one staged recovery-code set per user.
  - `user_api_tokens` stores the owner, label, 16-bit mask, lookup hash and AES-256-GCM sealed 32-byte secret; `compat_recovery_codes` stores one code-hash array. Both cascade on user deletion. The migration preserves prior users, organizations, projects and migration receipts across two reopens.
- [x] Implement session-only token management, Bearer reuse under native scopes, owner-membership resolution and recovery-code staging.
  - Management routes reject every Authorization header and require CSRF for mutations. Account tokens authenticate compatible reads and writes, answer404 outside the owner's workspaces and never issue credentials. Recovery codes accept a session or Bearer credential, replace an existing staged set, reject a wrong code and clear on success.
- [x] Verify store, HTTP, open mode, full race, vet, build and formatting; save evidence under `test-results/acceptance-account-tokens-20261021`.
  - Focused store2.003s and HTTP2.688s pass. Fresh full race passes server151.731s/store286.028s/uptime11.410s; vet and build pass. Tests cover owner isolation, cascade deletion, migration reopen, bearer scope denial, project:releases writes, pagination both directions, validation messages, recovery replacement and cookie-free open mode.

Public acceptance at `322e2a530c7a7dba0a0d76423cc3a1dbd2c3cedd`: public main matches the pushed commit; CI39704603274 passes full race, vet, formatting and image build in5m24s; image39705130351 publishes both architectures in1m50s. The anonymous public image digest `sha256:e069989675180cc0e88dc5a7ce1b0824f169c03dccdc7e0c59e367f3cd9e9b55` starts with a fresh database in cookie-free open mode, returns an empty token list, creates a64-hex token with integer id and millisecond created, lists it, returns ten recovery codes and consumes one with204. Evidence: `test-results/acceptance-account-tokens-20261021/{report.json,store.log,http.log,full-race.log,vet.log,build.log,public-smoke.json}`. The remaining account route is invitation acceptance; release files and cold archival remain open product gates.

- 2026-10-21 UTC: Re-verified the explicit frictionless-start acceptance on public `241a447` image `sha256:91e3b70b013b2331b185b5b81aafc3f82ebe66c7301922fb526431857be7fbc9`. A fresh Compose volume reports `required:false`, serves `/api/v1/me` without a cookie as `owner@localhost` in `default-organization`, returns the dashboard200 and creates a project201. The database contains exactly one user, organization, team, project and membership; an application-only restart preserves those counts and the project. A separate fresh required-mode stack reports `required:true`, creates the first owner201, serves the session200, logs in200, logs out204, rejects the signed-out session401, rejects anonymous account-token reads401 and serves the session list200. Both stacks and volumes were removed after the run. Evidence: `test-results/acceptance-compose-open-final-20261021/report.json`.

- 2026-10-21 UTC: Compatible invitation acceptance is public at `89af96bb837387aaaef5ad885489c148f2c49f10`. `GET/POST /api/0/accept/{invitation}/{token}/` validates the existing sent, unexpired invitation by identity and token; GET is public and returns the pending organization user, while POST accepts session or Bearer, same-origin and `{acceptInvite:boolean}`, links a matching primary or verified secondary account, creates one membership and consumes the invitation. Replay, wrong account/token, cancellation and expiry answer403; an existing membership answers409. Focused store2.186s and HTTP3.445s pass; the fresh full race passes server152.975s/store285.966s/uptime11.437s. The run exposed and repaired an unrelated midnight-boundary flake in the transaction-trend fixture by keeping its synthetic events within one recent UTC day. CI39709720672 passes in4m35s and image39710159714 in1m59s; public digest `sha256:6e9529f6cef97b24f6424a7829c29df1ad7e5dd29715944483a8c57020e1bdf2`. Evidence: `test-results/acceptance-compat-invitation-20261021/{report.json,focused.log,full-race.log,vet.log,build.log}`. Release-file reads/deletes and cold archival remain open.

- 2026-10-21 UTC: Compatible release-file reads/deletes are public at `6b895b441505c4eedaf4577482a77e5859a527fc`. Organization and project `GET .../releases/{version}/files/`, detail and `DELETE .../files/{file}/` read the existing release-scoped source maps; lists default50, accept limit/per_page1..100 with cursor Links, and return string id, UTC dateCreated, SHA-1, nullable headers, name and size. `project:releases`, organization/selected-project containment and owner/admin session deletes apply. Focused store0.453s and HTTP0.694s pass; the fresh full race passes server150.427s/store289.314s/uptime11.404s. CI39712944574 passes in5m40s and image39713456722 in2m16s; public digest `sha256:5385d1677712344bd2a11b02a9d7aa40114d5581b1616c320ac590b8e18e1d88` lists a real source map with SHA-1 `0cf6a3a5e29fb09ef640db78f38007ea7ca7fde5` and deletes it204. Evidence: `test-results/acceptance-release-files-20261021/{report.json,focused.log,full-race.log,vet.log,build.log}`. Cold archival and the final requirement audit remain open.

- 2026-10-21 UTC: Compatible heartbeat checks are public at `ee5e4351f4788e8009774e2f486bc77de23bd011`. Additive057 adds a stable UUID endpoint identity to every monitor. Compatible monitor JSON exposes `endpointId` and a compatible absolute `heartbeatEndpoint`; public `POST /api/0/organizations/{org}/heartbeat_check/{endpoint}/` drains at most1024 bytes, records one up check through the native enabled-monitor transaction and returns `{isUp:true,reason:null,startCheck}`. Unknown/foreign/disabled endpoints404, queries400 and oversized bodies413; the native key route is unchanged. Focused store0.615s and HTTP1.025s pass; the fresh full race passes server155.031s/store287.132s/uptime11.416s. CI39715679490 passes in5m51s and image39716215604 in2m18s; public digest `sha256:066aeee3e079021b879a3356298ba5853ada9e96ffada7ce5b127920cb99b957` creates a heartbeat monitor201 with endpointId `8241312d-a65e-4596-bc2d-3e508de66f2c`, then records a check200. Evidence: `test-results/acceptance-heartbeat-endpoint-20261021/{report.json,focused.log,full-race.log,vet.log,build.log}`. Cold archival and the final requirement audit remain open.

- 2026-10-21 UTC: The compatible embed error page is public at `bd31cb98964d42d37cbf899cbc1a05c1b5ed94cd`. Public `GET/POST /api/embed/error-page/` binds the DSN public key to the final project identity and returns a self-contained accessible JavaScript widget with configurable strings; URL-encoded name/email/comments submissions commit through the existing legacy user-report receipt/project gates. Invalid/disabled DSNs401, empty/overlong fields400 with an errors object and success200. Configurable strings are embedded as JSON and rendered with textContent. Focused store0.390s and HTTP0.498s pass; the widget passes `node --check`; the fresh full race passes server152.134s/store284.511s/uptime11.413s. CI39718511921 passes in5m42s and image39719042290 in1m56s; public digest `sha256:317407f0070a831743b205397802934458b2c088e77e16275d37953c14b65781` serves a6214-byte widget and stores a real report200. Evidence: `test-results/acceptance-embed-error-page-20261021/{report.json,focused.log,full-race.log,vet.log,build.log}`. Cold archival, import and the final requirement audit remain open.

- Published at `285df09906512c0dd2479f26afdd6f572ec616a1`: additive migration062 admits `manager` and stores invitation team IDs; owner/manager/admin role rules protect owners and prevent admin manager escalation; admin team assignments require existing membership; invitation acceptance atomically applies surviving team IDs; pending responses expose current team slugs; compatible `teamRoles` is strictly parsed, its `role` is a no-op, and active-member updates do not change teams. Team membership remains separate from project/event access. Focused PostgreSQL/HTTP race tests and the non-integration suite pass. The exact-source CI run `40003913880` passes formatting, vet, the complete PostgreSQL integration/race suite and application image build. Image workflow `40004775714` publishes linux/amd64 and linux/arm64 at OCI index `sha256:73cfaa20274ac377137ca41cd3d45e9d2c51d8b533084ce9aaebfdd3a0ca7a5d`; `docker buildx imagetools inspect` confirms both manifests. CI `40002750068` exposed an obsolete migration-037 fixture that dropped `organization_invitations` before its new migration-062 child table; `285df09` drops the child first and the focused PostgreSQL race test passes. On 2026-10-23 UTC, Chromium 156.0.8078.4 verified the manager workflow from source commit 756f0fa against PostgreSQL 18 with GOODSENTRY_AUTH=required: manager login, owner-role protection, member-role changes, invitation options that allow manager but exclude owner, team creation and member assignment all pass. The People view has no horizontal overflow at 320, 768 or 1440 px and no page errors. The disposable app and database, temporary browser script, and task-created Go build cache were removed. Docker ends with 0 containers and 0 B of build cache.

- 2026-10-23 UTC: `TestOpenModeMCPOAuthUsesImplicitOwner` covers cookie-free MCP consent and grant issuance for the implicit owner, the required CSRF and same-origin checks, PKCE exchange, and selected-project tool reads. Exact-source CI [40048844658](https://github.com/jerolan/good-sentry/actions/runs/40048844658) passes formatting, vet, the full PostgreSQL integration/race suite and image build; image workflow [40049346247](https://github.com/jerolan/good-sentry/actions/runs/40049346247) publishes amd64 and arm64.

- [x] Implement additional-user mute via `ignoreUserCount` in compatible API0 and native issue triage.
  - [x] Confirm Sentry's public API meaning: stop after this many additional users, with an optional separate `ignoreUserWindow` field deferred. Use GoodSentry's existing affected-user identity rule; keep anonymous captures out.
  - [x] Fix the privacy and lifecycle contract in `SPEC.md`: organization-scoped HMAC digests use the stable app secret, survive event retention only while the mute is active, preserve progress across merge/unmerge, and define native PATCH/UI behavior.
  - [x] Add strict individual/bulk and native PATCH parsing, typed policy support, and the native additional-user form; zero disables the compatible condition and `ignoreUserWindow` remains rejected.
  - [x] Add additive storage, new-user progress, atomic expiry/activity, issue reads and compatible/native visibility without persisting raw identities.
  - [x] Prove merge/unmerge, deduplication, anonymous events, retention, scope/CSRF and official SDK capture; run full PostgreSQL integration/race/vet, responsive browser and image restart, then verify exact-source CI with `gh`.
  - 2026-10-24 UTC: commits `2ab5a0f` and `99d0017` implement the mute and correct the legacy migration fixture. Exact-source CI [40052915792](https://github.com/jerolan/good-sentry/actions/runs/40052915792) passes formatting, vet, PostgreSQL integration/race and image build; image workflow [40053308722](https://github.com/jerolan/good-sentry/actions/runs/40053308722) publishes amd64 and arm64 at `sha256:db46ab99c17d98804ec439ba4cf80de1a703af708bc0fb6f514c195bc3e8067b`. `@sentry/node` 11.7.0 passes five runtime checks with 13 acknowledged events. Chromium 156.0.8078.4 passes seven UI checks across 320/768/1440px with 27 screenshots. The published image serves the SDK event after replacing the local process against the same PostgreSQL database. Acceptance containers, pulled image tag and temporary caches were removed.

- [x] Add rolling `ignoreWindow` support to compatible and native issue mutes.
  - Contract: accept 0 or 1..10080 whole minutes only with a positive `ignoreCount`; zero/omission retains the existing unbounded count. Count only fresh eligible issue receipts by database receipt instant in the inclusive rolling interval `[now-window, now]`. Expire atomically on threshold; age-out may increase `remaining`. Retry IDs do not count, and the window is not a hard mute duration.
  - Test proof: `TestIssueIgnoreRollingWindowLifecycle` covers database receipt time, duplicate IDs, event-retention independence, unchanged/changed policy progress, aging, bounded cleanup and threshold activity. `TestIssueIgnoreRollingWindowIncludesLowerBound` places a receipt exactly at the inclusive edge and observes the resulting unignore activity; changing the production comparison from `>=` to `>` makes it fail. Parser/HTTP owners cover strict individual, bulk and native inputs. No production clock seam is needed.
  - Acceptance: strict individual/bulk/native field admission, top-level/nested agreement, bounds and count dependency; receipt-time inclusive lower bound, duplicate idempotency, expiration reason/activity and alert ordering, dynamic remaining after age-out, unchanged-policy progress, changed-policy reset, duration/user-count OR behavior, merge/unmerge ownership, event-retention independence and bounded cleanup. The responsive native issue form edits/displays the window without losing drafts or other conditions.
  - Verify: focused parser/Store/HTTP integration tests, migration upgrade/reopen, full PostgreSQL race suite, integration-tag vet, trim-path build, responsive browser acceptance, exact-source CI and published amd64/arm64 image through `gh`.
  - Source: [Sentry Update an Issue](https://docs.sentry.io/api/events/update-an-issue/) defines `ignoreWindow` as minutes measured over `ignoreCount`, with a maximum of seven days; GoodSentry receipt-time/idempotency semantics are specified in `SPEC.md`.
  - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `docs/issues.md`, `internal/store/migrations/065_issue_ignore_count_window.sql`, store/compatibility/native issue owners and their existing tests.
  - Local evidence (2026-10-25 UTC): focused parser/HTTP and Store window tests pass, including the exact-boundary mutation check; `go vet -tags=integration ./...`, `go build -trimpath ./cmd/goodsentry`, JavaScript syntax checks and `git diff --check` pass. Chromium reports 7 UI checks and 27 screenshots across 320/768/1440px. The full PostgreSQL race run passes the server package but the Store package hits the unrelated timing-sensitive `TestDeleteCompatibleIssueWaitsForAtomicCaptureItems`; its isolated race rerun passes. Exact-source GitHub CI and image publication remain pending.
  - Completion (2026-10-25 UTC): the later full PostgreSQL race suite passes; the responsive issue acceptance passes 7 checks and 27 screenshots, including both rolling windows. Exact-source CI [40210762379](https://github.com/jerolan/good-sentry/actions/runs/40210762379) passes formatting, vet, PostgreSQL integration/race and image build. Publication [40211296201](https://github.com/jerolan/good-sentry/actions/runs/40211296201) publishes amd64/arm64 at OCI index `sha256:8d19d15bd8fd35e51df5c7ed5966a46df373e6da6120797bd7251d3282c22603`.

- [x] Add rolling `ignoreUserWindow` support to compatible and native issue mutes.
  - Contract: compatible individual/bulk updates accept top-level or `statusDetails.ignoreUserWindow` from1..10080 minutes only with positive `ignoreUserCount`; zero/omission retains its existing unbounded behavior. Native PATCH accepts `ignore.userWindow` from1..10080 only with positive `ignore.userCount`. Count distinct nonbaseline HMAC identities whose latest eligible PostgreSQL receipt is in the inclusive rolling interval; a repeat refreshes the identity, which can count again after its prior receipt ages out. Baseline identities remain excluded. User remaining may increase as receipts age out; the window is not a hard mute duration.
  - Test proof: a PostgreSQL Store test owns inclusive boundary, same-identity refresh/re-entry, baseline exclusion, threshold activity, no-op/reset, retention independence and bounded maintenance; parser/HTTP owners cover strict field admission, copy agreement, bounds, dependency and native response wiring.
  - Acceptance: one database instant for occurrence/user progress, atomic user-threshold expiry and activity, dynamic userRemaining, OR behavior with duration/count, duplicate retry safety, merge/unmerge ownership, migration upgrade/reopen, cleanup and responsive native edit/display without losing drafts.
  - Verify: focused parser/Store/HTTP integration tests, migration/reopen, full PostgreSQL integration/race/vet, trim-path build, responsive browser acceptance, exact-source CI and amd64/arm64 image publication through `gh`.
  - Source: [Sentry Update an Issue](https://docs.sentry.io/api/events/update-an-issue/) and [Bulk Mutate an Organization's Issues](https://docs.sentry.io/api/events/bulk-mutate-an-organizations-issues/) define the field as minutes over `ignoreUserCount`, capped at seven days; receipt-time and identity semantics are GoodSentry's contract in `SPEC.md`.
  - Local evidence (2026-10-25 UTC): focused parser/HTTP tests, all Store integration tests (`./internal/store`, 282.875s), the migration/lifecycle/policy subset with `-race`, `go vet -tags=integration ./...`, trim-path build, JavaScript syntax checks and `git diff --check` pass. The official `sentry-go` v0.50.0 demo also passed against a fresh local app in `GOODSENTRY_AUTH=disabled` with disposable PostgreSQL: exception, transaction, log, user-window threshold, baseline/repeated/anonymous users and same-policy progress. The first all-package integration run had one concurrent comparison failure in `TestCompatibleIssueUpdateResponseDuringNativeCaptureAndGrouping`; that test passes five isolated repetitions and the complete Store package passes in isolation. The Node-specific and responsive browser scripts remain unrun because their private fixture and Node SDK module were unavailable. Exact-source GitHub CI and amd64/arm64 publication remain pending.
  - 2026-10-25 UTC: Full non-integration `go test ./...` passes when local loopback listeners are allowed for SMTP, webhook, S3 and uptime tests; the first restricted run failed only at socket creation. Integration-tag `go vet ./...` and the trim-path application build also pass. The task-created Go cache and build output were removed. Remote CI and image publication remain pending because `gh` still reports an invalid token.
  - Files: `SPEC.md`, `tasks/plan.md`, `tasks/todo.md`, `docs/issues.md`, new additive migration, store/compatible/native issue owners, UI and their existing tests.
  - Completion (2026-10-25 UTC): the official `@sentry/node` 11.7.0 transport passes 5 checks and acknowledges 13 events, including new-user threshold expiry; Chromium passes 7 checks with 27 screenshots at 320/768/1440px, with no page errors, failed requests or unexpected responses. The full PostgreSQL integration/race suite passes. Exact-source CI [40210762379](https://github.com/jerolan/good-sentry/actions/runs/40210762379) passes; publication [40211296201](https://github.com/jerolan/good-sentry/actions/runs/40211296201) publishes amd64/arm64 at OCI index `sha256:8d19d15bd8fd35e51df5c7ed5966a46df373e6da6120797bd7251d3282c22603`. Local reports: `test-results/ignore-sdk-userwindow-20261025/report.json` and `test-results/ignore-browser-userwindow-20261025/report.json`.

- 2026-10-25 UTC: The previously noted concurrent comparison test passes five isolated race repetitions, and a subsequent full `go test -race -tags=integration ./...` passes against disposable PostgreSQL (server176.328s, store331.492s, uptime11.412s). This also clears the prior full-suite Store timing failure for local verification. The temporary container and Go cache were removed; exact-source GitHub CI and amd64/arm64 publication remain pending because `gh` still reports an invalid token.
- 2026-10-25 UTC: A clean `git archive HEAD` Compose build with no `.env` and no `GOODSENTRY_AUTH` value resolves to `disabled`. On fresh PostgreSQL/uploads volumes, dashboard returns200, setup reports `required:false`, and cookie-free `/api/v1/me` returns the implicit owner and CSRF token. Missing CSRF and foreign origin each return403; same-origin plus CSRF creates a project201. Restart preserves the owner, organization and CSRF token with exactly one user, organization and team. A separate fresh-volume Compose run with `GOODSENTRY_AUTH=required` and a test-only key reports `required:true`, rejects cookie-free `/me`, starts with zero users/orgs, completes setup201 and login200, and serves `/me` with the resulting sessions. Temporary projects, volumes, image, source copy and build cache were removed; Docker has zero containers and zero build cache, and the two pre-existing `sentrygo_*` data volumes remain.

- 2026-10-24 UTC: Commits `a49d806`, `6540feb` and `0828c05` add Sentry Native `event.minidump` envelope processing, compatibility docs and default-open detection in the pinned Go SDK smoke harness. Local PostgreSQL/HTTP envelope checks, the default-open Go SDK smoke and required-auth setup/login smokes pass; the full race suite, integration race suite and integration `vet` pass. Exact-source CI [40144251938](https://github.com/jerolan/good-sentry/actions/runs/40144251938) passes formatting, vet, the full PostgreSQL integration/race suite and application image build. Image workflow [40144689725](https://github.com/jerolan/good-sentry/actions/runs/40144689725) publishes OCI index `sha256:43cf29f2a89bc9ee258e7711fd19229a10a7eee5832fd91c2a8cd39e8ed31015`; registry inspection confirms `linux/amd64` and `linux/arm64` manifests.

## Compatible commit-bound issue resolution

- [x] Add a typed commit resolution snapshot to PostgreSQL issue policies.
  - Match exact repository and commit IDs only through the issue project's linked releases. Select the earliest `(created_at,id)` match, preserve that release snapshot, keep same-policy writes idempotent and use existing release regression behavior.
  - PostgreSQL owner tests cover unknown/cross-project identities, earliest match, commit-list and anchor deletion, same-policy retry, fresh-event regression, and migration067 upgrade/reopen without changing existing resolution rows or migration receipts.
  - Files: migration067, `internal/store/issues_resolution.go`, `internal/store/issues_resolution_capture.go` and their existing store/migration test owners.
- [x] Accept and return `statusDetails.inCommit` from compatible individual issue updates.
  - Require exact `{repository,commit}` string shape, resolved status and no conflicting selector; validate through the Store boundary and return both `inCommit` and the selected `inRelease`.
  - Parser tests cover the exact object, malformed/duplicate/oversized/control values and conflicting selectors. The HTTP owner verifies successful projection, unknown-commit 400, and null clearing.
  - Files: `internal/server/compat_issue_update_parse.go`, `internal/server/compat_issue_update.go`, compatible issue projection and their existing tests.
- [x] Preserve commit-resolution behavior for compatible bulk issue updates.
  - Keep per-issue project lookup and atomic rollback when any selected issue cannot resolve the same commit; return the commit and release selectors in acknowledgements.
  - PostgreSQL tests prove same-project and cross-project per-issue anchors, rollback when one selected project lacks the commit, and the bulk HTTP acknowledgement.
  - Files: `internal/server/compat_issue_bulk.go`, compatible Store update owners and their existing tests.
- [x] Pass full integration/race/vet/build checks, then publish exact-source CI and amd64/arm64 image with `gh`.
  - Local proof: full `go test ./... -count=1`, focused PostgreSQL migration/store and compatible individual/bulk HTTP tests pass with `-race`; `go vet -tags=integration ./...` passes. The full GitHub integration/race, vet, formatting and image-build workflow passed at [CI 40214943039](https://github.com/jerolan/good-sentry/actions/runs/40214943039).
  - The [container workflow 40215362685](https://github.com/jerolan/good-sentry/actions/runs/40215362685) published exact revision `caed0989923740f8d6fcd8f18c509aa76c7b98a9` to `ghcr.io/jerolan/good-sentry:main`, `:latest` and `:sha-caed0989923740f8d6fcd8f18c509aa76c7b98a9` for amd64/arm64; manifest digest `sha256:b2b23423c85a613037c480f676bb32f1ef07dcd569fc1a604a59317f45360e8b`.

- [ ] Enforce organization-level `require2FA`.
  - Contract: migration073 stores the setting; enabling requires confirmed TOTP for the actor; members without TOTP are restricted to identity, logout and enrollment; disabling TOTP is rejected while required.
  - Acceptance: native and Bearer API routes are403 before enrollment and succeed after; OAuth consent and MCP are contained; organization update/DTO, open mode, migration/reopen, full PostgreSQL/race, vet, build and UI syntax pass.
  - Local evidence (2026-10-26): organization update rejects enabling `require2FA` without confirmed TOTP; after enrollment the setting persists and its DTO reports it. A non-enrolled member receives403 on native and Bearer project reads, MCP tool calls and OAuth consent while `/api/v1/me` remains readable; TOTP enrollment restores access and disabling the factor is rejected while required. Open-mode, OIDC, social and MCP cancellation regressions pass. Focused tests pass in2.450s; full `go test ./...`, `go test -race -tags=integration ./...` (server223.764s, store400.212s), integration-tag vet, trim-path build and UI syntax pass.
  - Public gates remain pending.


<a id="closure-evidence-v1"></a>

## Registro del contrato de cierre aprobado

Contrato y resultados: [SPEC.md#closure-contract-v1](../SPEC.md#closure-contract-v1).
Orden: [plan.md#closure-order-v1](plan.md#closure-order-v1).
Integración documental autorizada en esta sesión; implementación y aceptación
completa no iniciadas, sin crear, reactivar ni modificar Goals.

Esta matriz enumera cada bloque normativo/fila y flujo aprobado mediante un ID
inmutable. El enlace de cada fila define todos sus resultados y controles.
Las casillas y comprobantes anteriores permanecen como historia, incluyendo
fallos y reparaciones. No acreditan por sí mismos vigencia sobre la revisión
actual. Ninguna fila se marca vigente por esta integración.

Implementación y evidencia se registran por separado. `Por contrastar` describe
que esta integración no auditó el estado de implementación de ese ID; no afirma
que falte la función. `Pendiente` significa que falta revisar u obtener evidencia
vigente para todos los subresultados. Una muestra parcial no satisface el bloque.
Estados de evidencia permitidos: pendiente, vigente, fallida, invalidada,
bloqueada y aplazada por decisión expresa.

Para cada fila sin comprobantes, la siguiente acción es contrastar su contrato,
dependencias y comprobantes históricos al recibir autorización de ejecución.
La columna Acción lo registra individualmente. Las dependencias particulares se
extraen de su contrato y del orden del plan al seleccionarla; no se presuponen
resueltas. Para excepciones, la acción es conservar límites y controles; no
obtener el recibo aplazado ni pedir versiones/accesos excluidos.

Al agregar un comprobante, registrar bajo el ID fecha, revisión fuente,
imagen/digest cuando aplique, versiones, entorno relevante, procedimiento o
comando reproducible, resultado esperado/observado, subresultados cubiertos y
pendientes, alcance, ruta/enlace sin secretos y recibo externo cuando aplique.
Registrar comparación de revisiones para reutilización, dependencias concretas
y causa de invalidación; conservar el resultado histórico y su reparación.
Un bloqueo registra causa, dependencia, responsable, intentos, condición de
desbloqueo y siguiente acción. Un skip no es éxito. Si no se acota el efecto de
un cambio en comportamiento/esquema/configuración/clientes/imagen/entorno,
la evidencia afectada queda invalidada hasta comprobarlo.

### Punto de continuación

Ejecución autorizada por instrucción del usuario el 2026-10-25. Se revisaron
Git, índice y reclamaciones antes de editar: no había criterio reclamado, la
candidata probada fue `54db6e48bf2e52e9a84efe397e3e5b44c27e4d91` y el único
archivo ajeno era `tests/compat/otlp_python_smoke.py`, sin seguimiento y sin
tocar. Se completaron, en orden del plan, CLOSE-operacion-004 a
CLOSE-operacion-012 (arquitectura, durabilidad, UI real, Compose, configuración,
modos de autenticación, features/correo/uploads y RSS), y CLOSE-identidad-001 a
CLOSE-identidad-007 (salud, sesiones, organizaciones, credenciales, grants,
lecturas compatibles, paginación y vista de API tokens). Después se abrieron
CLOSE-identidad-008 a CLOSE-identidad-013 (generaciones, cambio de contraseña,
correo, recuperación e invitaciones); sus comprobantes están abajo y los
la familia de identidad 001–064 está vigente salvo 025/033, bloqueadas por
publicación/CI; errores 001–005, 007–032, 034–046, 048–068, 069–083, 085–100
y 101–111, 139–162, logs 001–011 y logs 012–027 están vigentes; 033/047/084/112 bloqueadas; 006 espera proveedores de
releases/artefactos/nativo/CLI. Los criterios CLOSE-identidad-085 a -102 están
vigentes; 103 queda bloqueada por publicación con su comprobante local abajo.
CLOSE-identidad-104 a -108 quedan vigentes. Siguiente acción: CLOSE-identidad-109
(siguiente criterio del contrato). No se declara aceptación ni cierre del producto. El avance se
informa por criterios/evidencia, sin porcentaje de casillas; tiempo y
presupuesto de un Goal no completan el producto ni borran este registro.

Al cierre de este bloque, `go vet ./...` y
`go test -race -count=1 -tags=integration ./...` pasan (server 214.246s,
store 395.183s, sin `DATA RACE`); el log está en
`test-results/closure-20261025/full-race-account-final.log`. Hay 71 criterios
vigentes, dos bloqueados por publicación y el resto pendiente.

### Matriz de trazabilidad y estado inicial

| ID y contrato normativo | Unidad de resultado | Implementación | Evidencia | Acción siguiente |
|---|---|---|---|---|
| [CLOSE-operacion-001](../SPEC.md#close-operacion-001) | Objective and acceptance boundary: Build an independently authored Go error tracking and observability server.… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-002](../SPEC.md#close-operacion-002) | Objective and acceptance boundary: GlitchTip's publicly documented functionality is the minimum target, not me… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-003](../SPEC.md#close-operacion-003) | Clean-room method: Use public protocol specifications, product documentation, release notes, published API con… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-004](../SPEC.md#close-operacion-004) | Architecture: Use one Go module, a small executable in cmd/goodsentry, net/http routing and explicit dependenc… | implementada | vigente | Comprobante 2026-10-25 abajo; mantener ante cambios de arquitectura |
| [CLOSE-operacion-005](../SPEC.md#close-operacion-005) | Architecture: Ingest events durably before returning success. A request may normalize synchronously and commit… | implementada | vigente | Comprobante 2026-10-25 abajo; repetir tras cambios de captura o colas |
| [CLOSE-operacion-006](../SPEC.md#close-operacion-006) | Architecture: HTTP, workers and scheduler stop on termination signals and close database connections. Startup… | implementada | vigente | Comprobante 2026-10-25 abajo; repetir tras cambios de arranque o migraciones |
| [CLOSE-operacion-007](../SPEC.md#close-operacion-007) | Architecture: Serve an original, responsive application UI with a compact sidebar, neutral surfaces, a teal ac… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de UI/estilos |
| [CLOSE-operacion-008](../SPEC.md#close-operacion-008) | Deployment and configuration: Default HTTP port is 8000. docker compose up -d pulls the verified public applic… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de imagen/Compose |
| [CLOSE-operacion-009](../SPEC.md#close-operacion-009) | Deployment and configuration: Canonical variables are DATABASE_URL, SECRET_KEY, GOODSENTRY_DOMAIN, PORT, EMAIL… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios del loader |
| [CLOSE-operacion-010](../SPEC.md#close-operacion-010) | Deployment and configuration: GOODSENTRY_AUTH defaults to disabled for frictionless local startup. If SECRET_K… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de auth/sesiones |
| [CLOSE-operacion-011](../SPEC.md#close-operacion-011) | Deployment and configuration: An empty VALKEY_URL selects PostgreSQL-only operation. Optional features must re… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de features/correo |
| [CLOSE-operacion-012](../SPEC.md#close-operacion-012) | Deployment and configuration: Memory target: measure application RSS under an explicit repeatable workload and… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de ingestión |
| [CLOSE-identidad-001](../SPEC.md#close-identidad-001) | Runtime and workspace contracts: Provide /healthz for process health and /readyz for database readiness. Setup… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de sesiones/tokens |
| [CLOSE-identidad-002](../SPEC.md#close-identidad-002) | Runtime and workspace contracts: Organizations contain memberships and teams. Projects belong to an organizati… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de workspace/authz |
| [CLOSE-identidad-003](../SPEC.md#close-identidad-003) | Runtime and workspace contracts: The automation-credential prerequisite belongs to workspace, before compatibl… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de credenciales |
| [CLOSE-identidad-004](../SPEC.md#close-identidad-004) | Runtime and workspace contracts: An omitted or empty project list explicitly grants all current and future org… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de grants |
| [CLOSE-identidad-005](../SPEC.md#close-identidad-005) | Runtime and workspace contracts: Consume these credentials through Sentry-compatible GET /api/0/organizations/… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de rutas compatibles |
| [CLOSE-identidad-006](../SPEC.md#close-identidad-006) | Runtime and workspace contracts: Add an owner/admin-only API tokens view to the existing original Tailwind/dai… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de la vista |
| [CLOSE-identidad-007](../SPEC.md#close-identidad-007) | Runtime and workspace contracts: Malformed percent encoding or other invalid URL query syntax returns400 rathe… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de paginación |
| [CLOSE-identidad-008](../SPEC.md#close-identidad-008) | Account credential generations and authenticated password changes: This workspace increment supplies the share… | implementada | vigente | Comprobante 2026-10-26 abajo; detallados 014–026 pendientes |
| [CLOSE-identidad-009](../SPEC.md#close-identidad-009) | Account credential generations and authenticated password changes: fila 1, / credential-generation / Login/boo… | implementada | vigente | Comprobante 2026-10-26 abajo; detallados 014–019 pendientes |
| [CLOSE-identidad-010](../SPEC.md#close-identidad-010) | Account credential generations and authenticated password changes: fila 2, / account-password / A signed-in us… | implementada | vigente | Comprobante 2026-10-26 abajo; detallados 020–026 pendientes |
| [CLOSE-identidad-011](../SPEC.md#close-identidad-011) | Account credential generations and authenticated password changes: fila 3, / account-mail / Purpose-specific a… | implementada | vigente | Comprobante 2026-10-26 abajo; detallados 027–033 pendientes |
| [CLOSE-identidad-012](../SPEC.md#close-identidad-012) | Account credential generations and authenticated password changes: fila 4, / account-recovery / Email request… | implementada | vigente | Comprobante 2026-10-26 abajo; detallados 027–033 pendientes |
| [CLOSE-identidad-013](../SPEC.md#close-identidad-013) | Account credential generations and authenticated password changes: fila 5, / membership-invitations / Owner/ad… | implementada | vigente | Comprobante 2026-10-26 abajo; detallados 045–049 pendientes |
| [CLOSE-identidad-014](../SPEC.md#close-identidad-014) | Account credential generations and authenticated password changes: Additive migration035 after034 adds users.a… | implementada | vigente | Comprobante 2026-10-26 abajo; mantener ante cambios de 035 |
| [CLOSE-identidad-015](../SPEC.md#close-identidad-015) | Account credential generations and authenticated password changes: Both production issuers must require the ge… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de emisores |
| [CLOSE-identidad-016](../SPEC.md#close-identidad-016) | Account credential generations and authenticated password changes: Session and API-token issuance use an expli… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de locks |
| [CLOSE-identidad-017](../SPEC.md#close-identidad-017) | Account credential generations and authenticated password changes: Password change uses explicit pgx.ReadCommi… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de ChangePassword |
| [CLOSE-identidad-018](../SPEC.md#close-identidad-018) | Account credential generations and authenticated password changes: When issuance acquires its user SHARE lock… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de interleaving |
| [CLOSE-identidad-019](../SPEC.md#close-identidad-019) | Account credential generations and authenticated password changes: The need for a fresh statement snapshot fol… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de aislamiento |
| [CLOSE-identidad-020](../SPEC.md#close-identidad-020) | Account credential generations and authenticated password changes: Register prefix-aware POST /api/v1/account/… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios del handler |
| [CLOSE-identidad-021](../SPEC.md#close-identidad-021) | Account credential generations and authenticated password changes: Load the user's current password hash and g… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios del handler |
| [CLOSE-identidad-022](../SPEC.md#close-identidad-022) | Account credential generations and authenticated password changes: Return204 only after the password transacti… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de cookie |
| [CLOSE-identidad-023](../SPEC.md#close-identidad-023) | Account credential generations and authenticated password changes: Expose an Account link beside the signed-in… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de la vista Account |
| [CLOSE-identidad-024](../SPEC.md#close-identidad-024) | Account credential generations and authenticated password changes: Real PostgreSQL owns035 upgrade preservatio… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de transacciones |
| [CLOSE-identidad-025](../SPEC.md#close-identidad-025) | Account credential generations and authenticated password changes: Real HTTP owns actual current-password veri… | implementada (local) | bloqueada | Publicar imagen/CI de la misma fuente y repetir aceptación pública |
| [CLOSE-identidad-026](../SPEC.md#close-identidad-026) | Account credential generations and authenticated password changes: Deployment must stop every older app proces… | implementada | vigente | Comprobante 2026-10-26 abajo; mantener la guía de no-rolling-upgrade |
| [CLOSE-identidad-027](../SPEC.md#close-identidad-027) | Account email and single-use password recovery: Account mail is purpose-specific and independent of project al… | implementada | vigente | Comprobante 2026-10-26 abajo; mantener ante cambios de 036 |
| [CLOSE-identidad-028](../SPEC.md#close-identidad-028) | Account email and single-use password recovery: register and login screens expose a password-reset request for… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de request |
| [CLOSE-identidad-029](../SPEC.md#close-identidad-029) | Account email and single-use password recovery: A reset token is 32 random bytes as64 lowercase hex digits. St… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de criptografía |
| [CLOSE-identidad-030](../SPEC.md#close-identidad-030) | Account email and single-use password recovery: The one application worker sends a plain-text MIME message thr… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios del worker |
| [CLOSE-identidad-031](../SPEC.md#close-identidad-031) | Account email and single-use password recovery: POST /api/v1/account/password/reset/request is public but requ… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de reset |
| [CLOSE-identidad-032](../SPEC.md#close-identidad-032) | Account email and single-use password recovery: The reset form consumes a resetToken query parameter without p… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios del formulario |
| [CLOSE-identidad-033](../SPEC.md#close-identidad-033) | Account email and single-use password recovery: Pure checks own token parsing, AES-GCM integrity/AAD, generati… | implementada (local) | bloqueada | Publicar y repetir copia/reinicio/CI sobre la fuente; luego cerrar |
| [CLOSE-identidad-034](../SPEC.md#close-identidad-034) | Two-factor authentication: Additive migration 053_user_totp.sql adds nullable users.totp_secret (AES-256-GCM c… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios TOTP |
| [CLOSE-identidad-035](../SPEC.md#close-identidad-035) | Two-factor authentication: POST /api/v1/login keeps its strict body and identical Invalid email or password re… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de challenge |
| [CLOSE-identidad-036](../SPEC.md#close-identidad-036) | Two-factor authentication: GET /api/v1/me/two-factor reports enabled, pending and recoveryCodesRemaining for t… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de endpoints |
| [CLOSE-identidad-037](../SPEC.md#close-identidad-037) | Two-factor authentication: The Account view exposes the two-factor state, the manual-entry secret and otpauth… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de UI 2FA |
| [CLOSE-identidad-038](../SPEC.md#close-identidad-038) | Two-factor authentication: Additive migration 073_organization_require_2fa.sql adds organizations.require_2fa… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de require2FA |
| [CLOSE-identidad-039](../SPEC.md#close-identidad-039) | Two-factor authentication: When any current organization requires two-factor authentication and the account ha… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios del gate |
| [CLOSE-identidad-040](../SPEC.md#close-identidad-040) | Organization people and teams: Organization members and project teams are managed through the native authentic… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de People |
| [CLOSE-identidad-041](../SPEC.md#close-identidad-041) | Organization people and teams: GET /api/v1/organizations/{org}/members returns {data:{id,name,email,role}} ord… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de roles |
| [CLOSE-identidad-042](../SPEC.md#close-identidad-042) | Organization people and teams: GET/POST /api/v1/organizations/{org}/teams lists or creates teams; GET/PATCH/DE… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de equipos |
| [CLOSE-identidad-043](../SPEC.md#close-identidad-043) | Organization people and teams: POST/DELETE /api/v1/organizations/{org}/teams/{team}/members/{member} adds/remo… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de members |
| [CLOSE-identidad-044](../SPEC.md#close-identidad-044) | Organization people and teams: Pure/API checks own canonical IDs and strict bodies. Real PostgreSQL owns owner… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de People |
| [CLOSE-identidad-045](../SPEC.md#close-identidad-045) | Organization invitations: Additive migration037 after036 stores organization email invitations: delivery ID, o… | implementada | vigente | Comprobante 2026-10-26 abajo; mantener 037 |
| [CLOSE-identidad-046](../SPEC.md#close-identidad-046) | Organization invitations: Owners may invite owner, manager, admin or member; managers may invite manager, admi… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de roles |
| [CLOSE-identidad-047](../SPEC.md#close-identidad-047) | Organization invitations: Public request/accept routes are same-origin, contain no bearer authentication and r… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de rutas |
| [CLOSE-identidad-048](../SPEC.md#close-identidad-048) | Compatible invitation acceptance: GET/POST /api/0/accept/{invitation}/{token}/ maps GlitchTip's invitation-acc… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios compatibles |
| [CLOSE-identidad-049](../SPEC.md#close-identidad-049) | Compatible invitation acceptance: Acceptance owners are pure token/MIME tests, real PostgreSQL migration/parti… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de aceptación |
| [CLOSE-identidad-050](../SPEC.md#close-identidad-050) | OpenID Connect sign-in: OIDC is disabled unless GOODSENTRY_OIDC_ISSUER, GOODSENTRY_OIDC_CLIENT_ID and GOODSENT… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios OIDC |
| [CLOSE-identidad-051](../SPEC.md#close-identidad-051) | OpenID Connect sign-in: GET /auth/oidc/start creates random state, nonce and PKCE verifier values, stores an H… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios PKCE |
| [CLOSE-identidad-052](../SPEC.md#close-identidad-052) | OpenID Connect sign-in: Additive045 stores provider, subject, normalized email, user and login instants with u… | implementada | vigente | Comprobante 2026-10-26 abajo; mantener 045 |
| [CLOSE-identidad-053](../SPEC.md#close-identidad-053) | OpenID Connect sign-in: GET /api/v1/auth/config exposes only whether OIDC is enabled and its display name. The… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de config |
| [CLOSE-identidad-054](../SPEC.md#close-identidad-054) | Organization single sign-on applications: Additive migration 055_social_apps.sql stores organization-managed O… | implementada | vigente | Comprobante 2026-10-26 abajo; mantener 055 |
| [CLOSE-identidad-055](../SPEC.md#close-identidad-055) | Organization single sign-on applications: GET, POST /api/0/organizations/{org}/social-apps/ and GET, PUT, DELE… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de rutas |
| [CLOSE-identidad-056](../SPEC.md#close-identidad-056) | Organization single sign-on applications: Each application's callback is {domain}/auth/social/{providerID}/cal… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de callback |
| [CLOSE-identidad-057](../SPEC.md#close-identidad-057) | Organization single sign-on applications: Sign-in through an organization application links the verified subje… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de vínculo |
| [CLOSE-identidad-058](../SPEC.md#close-identidad-058) | Organization single sign-on applications: Organization applications also accept the github and gitlab presets.… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de presets |
| [CLOSE-identidad-059](../SPEC.md#close-identidad-059) | Organization single sign-on applications: The microsoft preset keeps the stored openid_connect provider and de… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de Microsoft |
| [CLOSE-identidad-060](../SPEC.md#close-identidad-060) | Organization single sign-on applications: Successful OAuth2 sign-in writes the same social:{providerID} identi… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de identidad social |
| [CLOSE-identidad-061](../SPEC.md#close-identidad-061) | Compatible user API tokens and recovery codes: Additive migration 056_account_tokens.sql stores user-scoped AP… | implementada | vigente | Comprobante 2026-10-26 abajo; mantener 056 |
| [CLOSE-identidad-062](../SPEC.md#close-identidad-062) | Compatible user API tokens and recovery codes: GET, POST /api/0/api-tokens/ and DELETE /api/0/api-tokens/{id}/… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de rutas |
| [CLOSE-identidad-063](../SPEC.md#close-identidad-063) | Compatible user API tokens and recovery codes: A user token also authenticates other compatible reads and writ… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de bearer |
| [CLOSE-identidad-064](../SPEC.md#close-identidad-064) | Compatible user API tokens and recovery codes: GET, POST /api/0/generate-recovery-codes/ stage single-use acco… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de códigos |
| [CLOSE-errores-001](../SPEC.md#close-errores-001) | Ingestion contract: Support POST /api/{project}/store/ and /api/{project}/envelope/, including deployments und… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de auth/ingest |
| [CLOSE-errores-002](../SPEC.md#close-errores-002) | Ingestion contract: Envelope headers and item headers are one-line UTF-8 JSON. Item length counts bytes and pe… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de framing |
| [CLOSE-errores-003](../SPEC.md#close-errores-003) | Ingestion contract: Accept identity, gzip, zlib-wrapped deflate, Brotli and Zstandard HTTP bodies. Support leg… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de compresión |
| [CLOSE-errores-004](../SPEC.md#close-errores-004) | Ingestion contract: Persist errors/messages with timestamps, exception chains, stack frames, breadcrumbs, tags… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de captura |
| [CLOSE-errores-005](../SPEC.md#close-errores-005) | Issues and artifacts: Default grouping is an independently designed stable hash of exception type and normaliz… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de grouping |
| [CLOSE-errores-006](../SPEC.md#close-errores-006) | Issues and artifacts: Merge groups without losing events or their original grouping keys; subsequent events fo… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-007](../SPEC.md#close-errores-007) | Issue search, historical filters and pages: Complete the existing native session-authenticated GET /api/v1/org… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de búsqueda |
| [CLOSE-errores-008](../SPEC.md#close-errores-008) | Issue search, historical filters and pages: The native query accepts single project, query, status, environmen… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de query |
| [CLOSE-errores-009](../SPEC.md#close-errores-009) | Issue search, historical filters and pages: Search accepts free text plus intersected key:value terms. Bare ke… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de gramática |
| [CLOSE-errores-010](../SPEC.md#close-errores-010) | Issue search, historical filters and pages: Keep domain values explicit with IssueSearch{Text string; Terms Is… | implementada | vigente | Comprobante 2026-10-26 abajo; mantener dominio tipado |
| [CLOSE-errores-011](../SPEC.md#close-errores-011) | Issue search, historical filters and pages: Evaluate explicit environment/release, event-level structured term… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de selección |
| [CLOSE-errores-012](../SPEC.md#close-errores-012) | Issue search, historical filters and pages: Organization membership/selected-project authorization, count and… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de authz |
| [CLOSE-errores-013](../SPEC.md#close-errores-013) | Issue search, historical filters and pages: Extend the existing responsive Issues form with release, All statu… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de filtros UI |
| [CLOSE-errores-014](../SPEC.md#close-errores-014) | Issue search, historical filters and pages: Real Chromium at320/768/1440 must verify combined historical/tag/r… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios del formulario |
| [CLOSE-errores-015](../SPEC.md#close-errores-015) | Compatible issue deletion: Implement complete individual and bounded bulk error-issue deletion, usable through… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de borrado |
| [CLOSE-errores-016](../SPEC.md#close-errores-016) | Compatible issue deletion: GlitchTip declares individual global/organization204 and organization bulk200; Sent… | implementada | vigente | Comprobante 2026-10-26 abajo; mantener status |
| [CLOSE-errores-017](../SPEC.md#close-errores-017) | Compatible issue deletion: Add exact DELETE /api/0/issues/{issue}/, /api/0/organizations/{org}/issues/{issue}/… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de rutas |
| [CLOSE-errores-018](../SPEC.md#close-errores-018) | Compatible issue deletion: Add only event:admin through additive020 migration of api_tokens_scopes_check; it a… | implementada | vigente | Comprobante 2026-10-26 abajo; mantener 020 |
| [CLOSE-errores-019](../SPEC.md#close-errores-019) | Compatible issue deletion: Organization bulk accepts the implemented id/project/environment/query/status/sort/… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de bulk |
| [CLOSE-errores-020](../SPEC.md#close-errores-020) | Compatible issue deletion: Without IDs, organization bulk uses the existing actual same-event occurrence/searc… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de filtros |
| [CLOSE-errores-021](../SPEC.md#close-errores-021) | Compatible issue deletion: Store interfaces are DeleteCompatibleIssue(ctx,access ManagementAccess,id string) e… | implementada | vigente | Comprobante 2026-10-26 abajo; mantener interfaces |
| [CLOSE-errores-022](../SPEC.md#close-errores-022) | Compatible issue deletion: The deletion unit is the complete current group: root plus all direct historical or… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de grupo |
| [CLOSE-errores-023](../SPEC.md#close-errores-023) | Compatible issue deletion: A new matching SDK event after deletion can create a fresh issue through the retain… | implementada | vigente | Comprobante 2026-10-26 abajo; mantener generación |
| [CLOSE-errores-024](../SPEC.md#close-errores-024) | Compatible issue deletion: Remove the post-commit child-storage gap. Define CaptureAttachment{Filename,Content… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de captura |
| [CLOSE-errores-025](../SPEC.md#close-errores-025) | Compatible issue deletion: The HTTP envelope boundary collects only the normalized parent's attachment/opaque… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de envelope |
| [CLOSE-errores-026](../SPEC.md#close-errores-026) | Compatible issue deletion: UI: provide Delete issue in detail and Delete selected in the list, using real comm… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de UI |
| [CLOSE-errores-027](../SPEC.md#close-errores-027) | Permanent issue discard: Implement Sentry-compatible discard:true on the individual compatible issue PUT and a… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de discard |
| [CLOSE-errores-028](../SPEC.md#close-errores-028) | Permanent issue discard: Use a durable project-scoped discarded-signature table keyed by project and exact nor… | implementada | vigente | Comprobante 2026-10-26 abajo; mantener 069 |
| [CLOSE-errores-029](../SPEC.md#close-errores-029) | Permanent issue discard: Every new capture checks its exact project/fingerprint marker under the same shared p… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de captura |
| [CLOSE-errores-030](../SPEC.md#close-errores-030) | Permanent issue discard: For a discarded event inside a mixed envelope, only that parent and its attached chil… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de envelope |
| [CLOSE-errores-031](../SPEC.md#close-errores-031) | Permanent issue discard: Use the public Sentry issue update API(https://docs.sentry.io/api/events/update-an-is… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de API |
| [CLOSE-errores-032](../SPEC.md#close-errores-032) | Permanent issue discard: Owning PostgreSQL checks cover020 checksum/reopen/all old-row preservation, actual fu… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de SQL/tests |
| [CLOSE-errores-033](../SPEC.md#close-errores-033) | Permanent issue discard: Use the current pinned environment and commands: go test -tags=integration ./cmd/...… | implementada (local) | bloqueada | SDK/imagen/CI pública pendientes; publicar y repetir |
| [CLOSE-errores-034](../SPEC.md#close-errores-034) | Compatible hashes and future grouping reset: Complete compatible hash listing and future-only detachment, incl… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de hashes |
| [CLOSE-errores-035](../SPEC.md#close-errores-035) | Compatible hashes and future grouping reset: The observed GlitchTip DELETE202 removes a signature's future rou… | implementada | vigente | Comprobante 2026-10-26 abajo; mantener 202 |
| [CLOSE-errores-036](../SPEC.md#close-errores-036) | Compatible hashes and future grouping reset: Add migration019_issue_hash_routes.sql. Create issue_hash_routes… | implementada | vigente | Comprobante 2026-10-26 abajo; mantener 019 |
| [CLOSE-errores-037](../SPEC.md#close-errores-037) | Compatible hashes and future grouping reset: Ingest retains retry advisory serialization and the existing earl… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de ingesta |
| [CLOSE-errores-038](../SPEC.md#close-errores-038) | Compatible hashes and future grouping reset: Native and compatible merges continue to flatten issue membership… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de merge |
| [CLOSE-errores-039](../SPEC.md#close-errores-039) | Compatible hashes and future grouping reset: Register exact GET and DELETE /api/0/organizations/{org}/issues/{… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de rutas |
| [CLOSE-errores-040](../SPEC.md#close-errores-040) | Compatible hashes and future grouping reset: GET accepts only scalar limit, cursor and full, rejecting duplica… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-041](../SPEC.md#close-errores-041) | Compatible hashes and future grouping reset: latestEvent is the newest retained event by occurrence timestamp… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-042](../SPEC.md#close-errores-042) | Compatible hashes and future grouping reset: DELETE accepts only repeated id parameters,1..1000 supplied UUID… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-043](../SPEC.md#close-errores-043) | Compatible hashes and future grouping reset: A concurrent capture and reset serialize on the project gate. A c… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-044](../SPEC.md#close-errores-044) | Compatible hashes and future grouping reset: Internal contracts: CompatibleIssueHashFilter contains Offset/Lim… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-045](../SPEC.md#close-errores-045) | Compatible hashes and future grouping reset: Add a Future grouping section to native issue detail with real pa… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de UI hashes |
| [CLOSE-errores-046](../SPEC.md#close-errores-046) | Compatible hashes and future grouping reset: Verification must apply test-audit before authorship. PostgreSQL… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-047](../SPEC.md#close-errores-047) | Compatible hashes and future grouping reset: Acceptance requires actual official SDK captures before/after res… | implementada (local) | bloqueada | SDK/imagen/CI pública pendientes; publicar y repetir |
| [CLOSE-errores-048](../SPEC.md#close-errores-048) | Compatible bulk issue changes and merging: Complete PUT /api/0/projects/{org}/{project}/issues/ and PUT /api/0… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-049](../SPEC.md#close-errores-049) | Compatible bulk issue changes and merging: Authorities are the public project bulk contract(https://docs.sentr… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-050](../SPEC.md#close-errores-050) | Compatible bulk issue changes and merging: A separate unmodified GlitchTip6.2.6 image a3d8eb1b36c1e1603d55ab32… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-051](../SPEC.md#close-errores-051) | Compatible bulk issue changes and merging: Both bulk PUTs require the existing event:write capability or an au… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-052](../SPEC.md#close-errores-052) | Compatible bulk issue changes and merging: Accept repeated id/project/environment and scalar query/status/sort… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-053](../SPEC.md#close-errores-053) | Compatible bulk issue changes and merging: Reuse implemented issue orders, default -last_seen, with determinis… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-054](../SPEC.md#close-errores-054) | Compatible bulk issue changes and merging: The strict1MiB JSON object accepts the individual fields status/ass… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-055](../SPEC.md#close-errores-055) | Compatible bulk issue changes and merging: All selected operations commit atomically in one ReadCommitted tran… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-056](../SPEC.md#close-errores-056) | Compatible bulk issue changes and merging: Share the native merge body through applyIssueMerge(ctx,tx,userID,r… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-057](../SPEC.md#close-errores-057) | Compatible bulk issue changes and merging: Internal contracts: CompatibleIssueBulkFilter contains IDs/ProjectR… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-058](../SPEC.md#close-errores-058) | Compatible bulk issue changes and merging: Return200 with a partial acknowledgement object, not an array of is… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-059](../SPEC.md#close-errores-059) | Compatible bulk issue changes and merging: Owning verification: parser tests cover bulk-only aliases, tri-stat… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-060](../SPEC.md#close-errores-060) | Compatible bulk issue changes and merging: Acceptance requires actual official sentry-cli3.8.0 against newly i… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir con el CLI fijado |
| [CLOSE-errores-061](../SPEC.md#close-errores-061) | Compatible issue comments: GET/POST /api/0/issues/{issue}/comments/ and the corresponding organization-prefixe… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios |
| [CLOSE-errores-062](../SPEC.md#close-errores-062) | Compatible issue comments: POST and PUT accept only {"data":{"text":"..."}} and return CommentSchema with nume… | implementada | vigente | Comprobante 2026-10-26 abajo; mantener body estricto |
| [CLOSE-errores-063](../SPEC.md#close-errores-063) | Compatible issue tags: GET /api/0/issues/{issue}/tags/ and the organization-prefixed route require event:read… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios |
| [CLOSE-errores-064](../SPEC.md#close-errores-064) | Compatible issue tags: Each IssueTagSchema includes the exact key, a title-cased display name, distinct-value… | implementada | vigente | Comprobante 2026-10-26 abajo; mantener schema |
| [CLOSE-errores-065](../SPEC.md#close-errores-065) | Compatible user preferences: Additive047 stores a JSONB user-options object and subscribeByDefault preference… | implementada | vigente | Comprobante 2026-10-26 abajo; mantener 047 |
| [CLOSE-errores-066](../SPEC.md#close-errores-066) | Compatible user preferences: Additive050_user_emails.sql adds globally unique secondary addresses without rewr… | implementada | vigente | Comprobante 2026-10-26 abajo; mantener 050 |
| [CLOSE-errores-067](../SPEC.md#close-errores-067) | Compatible user preferences: The emailed link is GET /account/emails/confirm?token=... on the configured domai… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de email |
| [CLOSE-errores-068](../SPEC.md#close-errores-068) | Compatible user preferences: GET/PUT /users/{id}/notifications/ and /notifications/alerts/ expose and update s… | implementada | vigente | Comprobante 2026-10-26 abajo; mantener notificaciones |
| [CLOSE-errores-069](../SPEC.md#close-errores-069) | Compatible individual issue changes: Implement the complete individual state/assignment/native-policy write sl… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-070](../SPEC.md#close-errores-070) | Compatible individual issue changes: Add only event:write with additive018 scope-constraint migration. Preserv… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-071](../SPEC.md#close-errores-071) | Compatible individual issue changes: PUT paths require canonical positive int64 issue IDs, exact trailing slas… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-072](../SPEC.md#close-errores-072) | Compatible individual issue changes: Status accepts unresolved/resolved/ignored plus muted (explicit alias of… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-073](../SPEC.md#close-errores-073) | Compatible individual issue changes: assignedTo omitted means no assignment update; null or empty string expli… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-074](../SPEC.md#close-errores-074) | Compatible individual issue changes: statusDetails accepts only inRelease, inNextRelease, inCommit, ignoreDura… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-075](../SPEC.md#close-errores-075) | Compatible individual issue changes: A nonnull inCommit is accepted only inside statusDetails as an object wit… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-076](../SPEC.md#close-errores-076) | Compatible individual issue changes: The shared resolution input adds mode commit with the exact Commit and Re… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-077](../SPEC.md#close-errores-077) | Compatible individual issue changes: ignoreDuration/ignoreCount/ignoreUserCount/ignoreWindow/ignoreUserWindow… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-078](../SPEC.md#close-errores-078) | Compatible individual issue changes: Store only an organization-scoped HMAC digest for each identity observed… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-079](../SPEC.md#close-errores-079) | Compatible individual issue changes: Compatible individual/bulk updates support ignoreWindow, ignoreUserWindow… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-080](../SPEC.md#close-errores-080) | Compatible individual issue changes: Internal types: CompatibleIssueChange has Status string, Assignment Compa… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-081](../SPEC.md#close-errores-081) | Compatible individual issue changes: Extract the existing native validation and mutation body into validateIss… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-082](../SPEC.md#close-errores-082) | Compatible individual issue changes: Write transactions retain native ReadCommitted and fresh-after-wait behav… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-083](../SPEC.md#close-errores-083) | Compatible individual issue changes: Owning tests: PostgreSQL covers project/organization/current-member gates… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-084](../SPEC.md#close-errores-084) | Compatible individual issue changes: Acceptance requires actual official SDK errors controlled through restric… | implementada (local) | bloqueada | SDK/imagen/CI pública pendientes; publicar y repetir |
| [CLOSE-errores-085](../SPEC.md#close-errores-085) | Compatible issue summaries and real analytics: Deliver organization/project issue lists, global/organization i… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-086](../SPEC.md#close-errores-086) | Compatible issue summaries and real analytics: Register exact GET /api/0/organizations/{org}/issues/, /api/0/p… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-087](../SPEC.md#close-errores-087) | Compatible issue summaries and real analytics: The organization short-ID lookup returns an object with organiz… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-088](../SPEC.md#close-errores-088) | Compatible issue summaries and real analytics: Lists return current roots and at most the requested page size,… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-089](../SPEC.md#close-errores-089) | Compatible issue summaries and real analytics: Support GlitchTip sorts last_seen/first_seen/count/priority wit… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-090](../SPEC.md#close-errores-090) | Compatible issue summaries and real analytics: Occurrence filters are inclusive start/exclusive end, ISO nanos… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-091](../SPEC.md#close-errores-091) | Compatible issue summaries and real analytics: Top-level count/firstSeen/lastSeen preserve the native root's k… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-092](../SPEC.md#close-errores-092) | Compatible issue summaries and real analytics: An identified user uses the first nonblank valid id, email, use… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-093](../SPEC.md#close-errores-093) | Compatible issue summaries and real analytics: Charts contain actual zero-filled integer Unix-seconds,count pa… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-094](../SPEC.md#close-errores-094) | Compatible issue summaries and real analytics: Wire issue summaries use actual string id/count/shortId, normal… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-095](../SPEC.md#close-errores-095) | Compatible issue summaries and real analytics: Use Sentry's typed statusDetails for implemented native policie… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-096](../SPEC.md#close-errores-096) | Compatible issue summaries and real analytics: Issue-stats requires repeated groups canonical positive IDs,1..… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-097](../SPEC.md#close-errores-097) | Compatible issue summaries and real analytics: Internal contracts: IssueAnalytics contains UserCount int64 and… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-098](../SPEC.md#close-errores-098) | Compatible issue summaries and real analytics: Add compatible GET /api/0/issues/{issue}/commits/ and /api/0/or… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-099](../SPEC.md#close-errores-099) | Compatible issue summaries and real analytics: Native issue detail displays Affected users (identified retaine… | implementada | vigente | Comprobante 2026-10-26 abajo; repetir tras cambios de UI |
| [CLOSE-errores-100](../SPEC.md#close-errores-100) | Compatible issue summaries and real analytics: Owning PostgreSQL tests cover grants/current membership/alias a… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-101](../SPEC.md#close-errores-101) | Compatible event inspection: Implement compatible event inspection as the first vertical part of the required… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-102](../SPEC.md#close-errores-102) | Compatible event inspection: Register GET /api/0/projects/{org}/{project}/events/ and .../events/{event}/. Reg… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-103](../SPEC.md#close-errores-103) | Compatible event inspection: Migration017 extends only the current token scope constraint with event:read. Do… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-104](../SPEC.md#close-errores-104) | Compatible event inspection: Use read-only repeatable-read transactions for current membership/project authori… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-105](../SPEC.md#close-errores-105) | Compatible event inspection: List queries accept only limit/per_page, cursor, full, sample, environment, query… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-106](../SPEC.md#close-errores-106) | Compatible event inspection: Time filters use stored occurrence time, with inclusive start and exclusive end.… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-107](../SPEC.md#close-errores-107) | Compatible event inspection: Use canonical cursor0:offset:direction, where offset is a nonnegative decimal no… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-108](../SPEC.md#close-errores-108) | Compatible event inspection: Serialize actual event data with id/eventID as SDK ID strings, projectID as integ… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-109](../SPEC.md#close-errores-109) | Compatible event inspection: The GlitchTip Request/APIEventBreadcrumb schemas are reachable from the event res… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-110](../SPEC.md#close-errores-110) | Compatible event inspection: The JSON view is a legacy compatibility projection, not byte-identical SDK transp… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-111](../SPEC.md#close-errores-111) | Compatible event inspection: Internal contracts: eventview.Record contains ID/GroupID strings, ProjectID int64… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-112](../SPEC.md#close-errores-112) | Compatible event inspection: Require actual official SDK events queried through compatible project and issue U… | implementada (local) | bloqueada | SDK/imagen/CI pública pendientes; publicar y repetir |
| [CLOSE-errores-113](../SPEC.md#close-errores-113) | Native merge, original partitions and unmerge: Complete native merging and separation of error issues through… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-114](../SPEC.md#close-errores-114) | Native merge, original partitions and unmerge: An issue row owns its original project/fingerprint forever. A v… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-115](../SPEC.md#close-errores-115) | Native merge, original partitions and unmerge: Historical group counts and fresh-capture progress are distinct… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-116](../SPEC.md#close-errores-116) | Native merge, original partitions and unmerge: Add a subsequent migration016 for topology constraints and inde… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-117](../SPEC.md#close-errores-117) | Native merge, original partitions and unmerge: Merge/unmerge locks current organization membership FOR SHARE,… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-118](../SPEC.md#close-errores-118) | Native merge, original partitions and unmerge: The surviving root retains status, assignment, resolution/regre… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-119](../SPEC.md#close-errores-119) | Native merge, original partitions and unmerge: Comments and activities remain on their original issue IDs. Rea… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-120](../SPEC.md#close-errores-120) | Native merge, original partitions and unmerge: Native POST /api/v1/organizations/{org}/issues/merge accepts ex… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-121](../SPEC.md#close-errores-121) | Native merge, original partitions and unmerge: Native POST /api/v1/issues/{issue}/unmerge accepts exactly {ori… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-122](../SPEC.md#close-errores-122) | Native merge, original partitions and unmerge: All grouping mutations use the existing session/CSRF controls a… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-123](../SPEC.md#close-errores-123) | Native merge, original partitions and unmerge: GET /api/v1/issues/{issue}/origins returns {data,total,page,pag… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-124](../SPEC.md#close-errores-124) | Native merge, original partitions and unmerge: The store owns MergeIssues(ctx,userID,orgSlug,ids) returning Is… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-125](../SPEC.md#close-errores-125) | Native merge, original partitions and unmerge: The original responsive issue list adds member checkboxes and m… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-126](../SPEC.md#close-errores-126) | Native merge, original partitions and unmerge: Owning PostgreSQL tests prove partition/raw preservation, flatt… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-127](../SPEC.md#close-errores-127) | Native release-anchored and next-release resolution: Complete both native release-resolution modes with the ex… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-128](../SPEC.md#close-errores-128) | Native release-anchored and next-release resolution: Native session/CSRF PATCH issue adds resolution, requirin… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-129](../SPEC.md#close-errores-129) | Native release-anchored and next-release resolution: Omitted resolution preserves an already resolved policy;… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-130](../SPEC.md#close-errores-130) | Native release-anchored and next-release resolution: Implement internal/releaseorder.Compare(a,b string)(int,b… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-131](../SPEC.md#close-errores-131) | Native release-anchored and next-release resolution: For chosen mode, exact version equality stays resolved ev… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-132](../SPEC.md#close-errores-132) | Native release-anchored and next-release resolution: For next mode, eligible binding is a different open proje… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-133](../SPEC.md#close-errores-133) | Native release-anchored and next-release resolution: A bound next version always regresses on exact equality,… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-134](../SPEC.md#close-errores-134) | Native release-anchored and next-release resolution: Additive migration014 owns one issue_resolutions row per… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-135](../SPEC.md#close-errores-135) | Native release-anchored and next-release resolution: Stage the private frequency hook: prepare receipt/context… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-136](../SPEC.md#close-errores-136) | Native release-anchored and next-release resolution: Extend always-on issue maintenance: expired mute claims r… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-137](../SPEC.md#close-errores-137) | Native release-anchored and next-release resolution: Add session GET /api/v1/issues/{issue}/releases?query=&pa… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-138](../SPEC.md#close-errores-138) | Native release-anchored and next-release resolution: Comparison tests own independent grammar/precedence/class… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-139](../SPEC.md#close-errores-139) | Native conditional mute: Deliver timed, additional-event and additional-user mute before release-anchored/next… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-140](../SPEC.md#close-errores-140) | Native conditional mute: Compatible individual and bulk issue updates accept ignoreWindow at the top level or… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-141](../SPEC.md#close-errores-141) | Native conditional mute: Occurrence-window progress uses server receipt instants, never SDK timestamps or loca… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-142](../SPEC.md#close-errores-142) | Native conditional mute: Additive migration065 stores one server receipt instant per qualifying occurrence in… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-143](../SPEC.md#close-errores-143) | Native conditional mute: Extend native session/CSRF-protected issue PATCH with optional ignore. It requires ex… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-144](../SPEC.md#close-errores-144) | Native conditional mute: Issue DTOs add nullable ignore:{minutes,count,userCount,window,userWindow,until,remai… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-145](../SPEC.md#close-errores-145) | Native conditional mute: Additive migration013 creates issue_ignores with one row per issue, optional positive… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-146](../SPEC.md#close-errores-146) | Native conditional mute: Run an always-on in-process issue maintenance worker, independent of uptime/log inges… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-147](../SPEC.md#close-errores-147) | Native conditional mute: Ignored issues, both indefinite and conditional, suppress new frequency-rule deliveri… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-148](../SPEC.md#close-errores-148) | Native conditional mute: The original issue detail adds a labeled mute form for minutes/count/additional users… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-149](../SPEC.md#close-errores-149) | Native conditional mute: Owning PostgreSQL proof covers deadline receipt bounds, same-policy idempotence, assi… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-150](../SPEC.md#close-errores-150) | Native assignment, comments and recorded triage activity: Complete native assignment, comment management and v… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-151](../SPEC.md#close-errores-151) | Native assignment, comments and recorded triage activity: Keep unresolved/resolved/ignored. Extend issue DTOs… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-152](../SPEC.md#close-errores-152) | Native assignment, comments and recorded triage activity: Resolve records database clock_timestamp() after acq… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-153](../SPEC.md#close-errores-153) | Native assignment, comments and recorded triage activity: Add GET issue/assignees (optional query at most200 U… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-154](../SPEC.md#close-errores-154) | Native assignment, comments and recorded triage activity: Comment POST retains201 and trimmed1..10000 UTF-8 by… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-155](../SPEC.md#close-errores-155) | Native assignment, comments and recorded triage activity: Original responsive detail gains searchable/paginate… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-sourcemaps-001](../SPEC.md#close-sourcemaps-001) | Release lifecycle, commit metadata and deploys: This artifacts increment provides actual release entities shar… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-002](../SPEC.md#close-sourcemaps-002) | Release lifecycle, commit metadata and deploys: An exact release version is1..255 Unicode characters excluding… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-003](../SPEC.md#close-sourcemaps-003) | Release lifecycle, commit metadata and deploys: Expose compatible organization GET/POST /api/0/organizations/{… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-004](../SPEC.md#close-sourcemaps-004) | Release lifecycle, commit metadata and deploys: Release JSON returns string id, version, shortVersion (the exa… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-005](../SPEC.md#close-sourcemaps-005) | Release lifecycle, commit metadata and deploys: List releases by dateCreated descending then numeric id descen… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-006](../SPEC.md#close-sourcemaps-006) | Release lifecycle, commit metadata and deploys: Support organization GET/POST .../releases/{version}/commits/,… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-007](../SPEC.md#close-sourcemaps-007) | Release lifecycle, commit metadata and deploys: Persist an organization/name repository identity whenever expl… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-008](../SPEC.md#close-sourcemaps-008) | Release lifecycle, commit metadata and deploys: Organization GET/POST .../releases/{version}/deploys/ records… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-009](../SPEC.md#close-sourcemaps-009) | Release lifecycle, commit metadata and deploys: Fresh accepted SDK error/message/transaction rows and standalo… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-010](../SPEC.md#close-sourcemaps-010) | Release lifecycle, commit metadata and deploys: Add an original responsive Tailwind/daisyUI Releases view, laz… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-011](../SPEC.md#close-sourcemaps-011) | Release lifecycle, commit metadata and deploys: Acceptance: real PostgreSQL owns shared-version atomic create/… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-012](../SPEC.md#close-sourcemaps-012) | Compatible release files: Expose compatible organization and project GET .../releases/{version}/files/, GET ..… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-013](../SPEC.md#close-sourcemaps-013) | Compatible release files: The view is backed by the existing release-scoped source_maps rows, not a duplicate… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-014](../SPEC.md#close-sourcemaps-014) | JavaScript artifacts: The observed sentry-cli 3.8.0 artifact flow negotiates chunk parameters at /api/0/organi… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-015](../SPEC.md#close-sourcemaps-015) | JavaScript artifacts: The modern JavaScript upload increment connects exact GET/POST /api/0/organizations/{org… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-016](../SPEC.md#close-sourcemaps-016) | JavaScript artifacts: Negotiate an absolute upload URL derived from the configured public domain/prefix, hashA… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-017](../SPEC.md#close-sourcemaps-017) | JavaScript artifacts: Persist chunks in PostgreSQL with organization, checksum, bytes, size and expiry; migrat… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-018](../SPEC.md#close-sourcemaps-018) | JavaScript artifacts: Assembly accepts one bounded JSON object with required checksum, nonempty ordered chunks… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-019](../SPEC.md#close-sourcemaps-019) | JavaScript artifacts: Use source_map file URLs as map names/source-resolution bases, never ZIP member paths or… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-020](../SPEC.md#close-sourcemaps-020) | JavaScript artifacts: Materialize maps for all deduplicated authorized projects in one transaction. Enforce AR… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-021](../SPEC.md#close-sourcemaps-021) | JavaScript artifacts: Acceptance owners: real PostgreSQL proves quota/expiry, organization/project isolation,… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-022](../SPEC.md#close-sourcemaps-022) | JavaScript artifacts: The first artifact integration stores validated JavaScript source maps by project, artif… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-023](../SPEC.md#close-sourcemaps-023) | JavaScript artifacts: Projects show a source-map disclosure with metadata list and an owner/admin multipart up… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-024](../SPEC.md#close-sourcemaps-024) | JavaScript artifacts: Integration review reproduced two source-map defects that must be repaired before accept… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-025](../SPEC.md#close-sourcemaps-025) | JavaScript artifacts: Source content follows the selected mapping's source record, including indexed sections… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-001](../SPEC.md#close-nativo-001) | CFI extended dereference: Support DWARF DW_OP_xderef (0x18) and DW_OP_xderef_size (0x95) in CFI expressions. T… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-002](../SPEC.md#close-nativo-002) | CFI extended dereference: Additive042 stores project-scoped debug files with a UUID identity, SHA-1 content id… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-003](../SPEC.md#close-nativo-003) | CFI extended dereference: The compatible POST /api/0/projects/{org}/{project}/reprocessing/ keeps Sentry CLI's… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-004](../SPEC.md#close-nativo-004) | CFI extended dereference: Chunk negotiation advertises debug_files before artifact_bundles, a 64MiB maximum fi… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-005](../SPEC.md#close-nativo-005) | CFI extended dereference: Textual Breakpad symbol files accept the optional m marker on FUNC and PUBLIC record… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-006](../SPEC.md#close-nativo-006) | CFI extended dereference: For ELF and Mach-O DWARF, derive source mappings from each compilation unit's line t… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-007](../SPEC.md#close-nativo-007) | CFI extended dereference: The first native-processing slice accepts POST /api/{project}/minidump/ with Sentry… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-008](../SPEC.md#close-nativo-008) | CFI extended dereference: For each Memory64ListStream range, validate its 64-bit file offset against the dump… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-009](../SPEC.md#close-nativo-009) | CFI extended dereference: When a full 168-byte-or-larger Exception stream contains its ThreadContext location… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-010](../SPEC.md#close-nativo-010) | CFI extended dereference: Legacy Breakpad ARM64 Minidumps use processor architecture 0x8003 and a distinct 796… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-011](../SPEC.md#close-nativo-011) | CFI extended dereference: The standard Sentry envelope route also accepts native SDK crash events containing e… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-012](../SPEC.md#close-nativo-012) | CFI extended dereference: Basic stack reconstruction starts at the crashing AMD64/x86/ARM32/ARM64/RISC-V64 con… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-013](../SPEC.md#close-nativo-013) | CFI extended dereference: Scalar CFI expressions support DW_OP_implicit_value with an unsigned-LEB128 byte len… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-014](../SPEC.md#close-nativo-014) | CFI extended dereference: Decode CFI ULEB128 and SLEB128 operands without truncation: accept only values repre… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-015](../SPEC.md#close-nativo-015) | CFI extended dereference: For CIE P augmentation, consume the personality field according to its supported bas… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-016](../SPEC.md#close-nativo-016) | CFI extended dereference: Add RISC-V64 minidump support for Breakpad architecture 0x8006 and Crashpad Minidump… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-017](../SPEC.md#close-nativo-017) | CFI extended dereference: RISC-V64 ELF CFI accepts little-endian ELF64 EM_RISCV .eh_frame or .debug_frame and… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-018](../SPEC.md#close-nativo-018) | CFI extended dereference: The PDB procedure-symbol slice consumes classic Microsoft PDB/MSF 7 files up to the… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-019](../SPEC.md#close-nativo-019) | CFI extended dereference: For C13 source lines, read each module's declared C13ByteSize after its symbol and C… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-020](../SPEC.md#close-nativo-020) | CFI extended dereference: For classic MSF 7 PDBs whose DBI ArePrivateSymbolsStripped flag is set, scan the DBI… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-021](../SPEC.md#close-nativo-021) | CFI extended dereference: SymCache debug files are accepted in little-endian SYMC versions 7, 8 and 9, using t… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-022](../SPEC.md#close-nativo-022) | CFI extended dereference: For classic MSF 7 PDB inline call sites, read module S_INLINESITE (0x114d) and S_INL… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-023](../SPEC.md#close-nativo-023) | CFI extended dereference: This native slice remains incomplete: Minidump CPU contexts beyond AMD64, x86, ARM32… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-024](../SPEC.md#close-nativo-024) | CFI extended dereference: Classic PDB symbol-scope Parent/End pointers are offsets from the beginning of the m… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-025](../SPEC.md#close-nativo-025) | CFI extended dereference: The official Sentry Native 0.17.4 Crashpad backend sends Minidump requests with sent… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-026](../SPEC.md#close-nativo-026) | CFI extended dereference: The repository's repeatable native SDK demonstration pins official sentry-native 0.1… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-001](../SPEC.md#close-rendimiento-001) | Telemetry, uptime and alerts: Transactions persist trace/span relationships, operation, endpoint, duration, st… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-001](../SPEC.md#close-uptime-001) | Uptime status pages: Additive046 stores organization-owned status pages and a many-to-many selection of same-o… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-002](../SPEC.md#close-uptime-002) | Uptime status pages: GET /status/{slug} and GET /api/v1/public/status-pages/{slug} expose only public pages. T… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-003](../SPEC.md#close-uptime-003) | Compatible uptime monitors: When uptime is enabled, GET/POST /api/0/organizations/{org}/monitors/, GET/PUT/DEL… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-004](../SPEC.md#close-uptime-004) | Compatible uptime monitors: Lists and checks use strict cursor pagination. Checks expose start time, isUp and… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-005](../SPEC.md#close-uptime-005) | Compatible uptime monitors: Additive057 gives every monitor a stable UUID endpoint identity and a unique index… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-alertas-001](../SPEC.md#close-alertas-001) | Compatible project alerts: GET/POST /api/0/projects/{org}/{project}/alerts/ and PUT/DELETE .../alerts/{id}/ ex… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-002](../SPEC.md#close-alertas-002) | Compatible project alerts: MonitorIn-style fields map as follows: timespanMinutes to the rolling window, quant… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-003](../SPEC.md#close-alertas-003) | Compatible project alerts: POST .../alerts/{id}/test/ queues the same native test delivery and returns one rec… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-001](../SPEC.md#close-logs-001) | Native log retention: This is the first retention increment, covering Sentry and OTLP logs. Error events, tran… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-002](../SPEC.md#close-logs-002) | Native log retention: Resolve the master lifetime from GOODSENTRY_RETENTION_DAYS, then GLITCHTIP_RETENTION_DAY… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-003](../SPEC.md#close-logs-003) | Native log retention: Measure log age from the existing logs.created_at receipt timestamp, not the application… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-004](../SPEC.md#close-logs-004) | Native log retention: Add forward-only migration022 with an index on logs(created_at,id) and a partial index o… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-005](../SPEC.md#close-logs-005) | Native log retention: OTLP canonical batches are authoritative shared bytes. Delete a batch only when no log r… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-006](../SPEC.md#close-logs-006) | Native log retention: Run the log-retention loop inside the one application process, on startup and after each… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-007](../SPEC.md#close-logs-007) | Native log retention: Acceptance owners: configuration tests protect master/log precedence, inheritance, exact… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-008](../SPEC.md#close-logs-008) | Compatible retained-log API: Additive migration 054_log_host.sql adds logs.host and a (project_id,timestamp DE… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-009](../SPEC.md#close-logs-009) | Compatible retained-log API: GET /api/0/organizations/{org}/logs/ serves the published GlitchTip log shape fro… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-010](../SPEC.md#close-logs-010) | Compatible retained-log API: Admission is session or Bearer event:read/event:write. Repeated project filters m… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-011](../SPEC.md#close-logs-011) | Compatible retained-log API: Pages are keyset ordered by timestamp DESC,id DESC with a default limit100 and a… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-rendimiento-002](../SPEC.md#close-rendimiento-002) | Transaction and standalone-span retention: Transaction/span retention is the next required class after accepte… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-003](../SPEC.md#close-rendimiento-003) | Transaction and standalone-span retention: The configuration prerequisite adds Config.TransactionRetentionDays… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-004](../SPEC.md#close-rendimiento-004) | Transaction and standalone-span retention: The storage contract is Store.CleanupTransactions(ctx, days), retur… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-005](../SPEC.md#close-rendimiento-005) | Transaction and standalone-span retention: Under those project gates and pinned event rows, delete envelope_it… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-006](../SPEC.md#close-rendimiento-006) | Transaction and standalone-span retention: Delete standalone spans independently, with no trace-wide cascade.… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-007](../SPEC.md#close-rendimiento-007) | Transaction and standalone-span retention: Migration023 adds only receipt-order indexes on events(created_at,i… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-008](../SPEC.md#close-rendimiento-008) | Transaction and standalone-span retention: Acceptance requires real PostgreSQL expiry based on receipt age wit… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-009](../SPEC.md#close-rendimiento-009) | Transaction and standalone-span retention: The transaction/span runtime acceptance on2026-10-17 uses new candi… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-010](../SPEC.md#close-rendimiento-010) | Transaction and standalone-span retention: The same candidate cleans the remaining fixtures with logs and upti… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-011](../SPEC.md#close-rendimiento-011) | Transaction and standalone-span retention: Transaction/span acceptance closes at functional eea9788c236fe8a738… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-013](../SPEC.md#close-operacion-013) | Error-event retention: After accepted logs and transaction/spans, error-event retention remains required. The… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-014](../SPEC.md#close-operacion-014) | Error-event retention: The accepted configuration prerequisite activates no deletion and adds no worker. Its e… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-015](../SPEC.md#close-operacion-015) | Error-event retention: Store.CleanupEvents(ctx, days) returns a committed removal count and error. In one Post… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-016](../SPEC.md#close-operacion-016) | Error-event retention: Delete envelope_items matching each pinned receipt's exact project_id,event_id, then it… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-017](../SPEC.md#close-operacion-017) | Error-event retention: Issue count and its first/last/title/environment/release reflect the surviving stored p… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-018](../SPEC.md#close-operacion-018) | Error-event retention: Preserve existing authorization and snapshot semantics. Native IssueDetail already retu… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-019](../SPEC.md#close-operacion-019) | Error-event retention: The issue UI derives retained-payload presence from the existing detail.events list, wi… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-020](../SPEC.md#close-operacion-020) | Error-event retention: Run StartEventRetention unconditionally through the shared in-process retention lifecyc… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-021](../SPEC.md#close-operacion-021) | Error-event retention: Error-retention runtime acceptance on2026-10-17 uses final3802aaa459a9a93225b9b4a307849… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-022](../SPEC.md#close-operacion-022) | Error-event retention: Real Edge checks retained, empty and restored payload views at320/768/1440 with comment… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-023](../SPEC.md#close-operacion-023) | Error-event retention: Error-event acceptance closes at functional8884b9d883946a9007767bf07a35e9807e1da182:316… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-006](../SPEC.md#close-uptime-006) | Uptime check retention: The next required retention class owns monitor_checks, not monitors. Official installa… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-007](../SPEC.md#close-uptime-007) | Uptime check retention: Use completed_at as the existing durable server completion clock. Both CompleteMonitor… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-008](../SPEC.md#close-uptime-008) | Uptime check retention: Store.CleanupMonitorChecks(ctx, days) returns a committed removed-check count and erro… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-009](../SPEC.md#close-uptime-009) | Uptime check retention: Preserve every monitor configuration and cached status, consecutive_failures, last_che… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-010](../SPEC.md#close-uptime-010) | Uptime check retention: Keep the current MonitorChecks authorization, completed_at/id ordering, transition-onl… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-011](../SPEC.md#close-uptime-011) | Uptime check retention: StartUptimeRetention runs unconditionally inside the one application process through t… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-012](../SPEC.md#close-uptime-012) | Uptime check retention: Actual acceptance uses a new logical-backup copy of the accepted4305-row/530-event/28-… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-024](../SPEC.md#close-operacion-024) | File and release retention prerequisites: Completed-file and release retention remain required after the four… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-025](../SPEC.md#close-operacion-025) | File and release retention prerequisites: First add typed Config.FileRetentionDays and Config.ReleaseRetention… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-026](../SPEC.md#close-operacion-026) | Release metadata retention: Release age is the first server receipt of its organization/version identity, rele… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-027](../SPEC.md#close-operacion-027) | Release metadata retention: Add Store.CleanupReleases(ctx,days)(removed int64,error). One transaction first ta… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-028](../SPEC.md#close-operacion-028) | Release metadata retention: Release writers need the same identity gate before their first INSERT/lookup, in r… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-029](../SPEC.md#close-operacion-029) | Release metadata retention: Deleting release metadata cascades release_projects, release_commits, release_depl… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-030](../SPEC.md#close-operacion-030) | Release metadata retention: Activate StartReleaseRetention unconditionally through the shared in-process lifec… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-031](../SPEC.md#close-operacion-031) | Release metadata retention: Test authoring gate: the Store owner protects actual metadata/cascade removal, str… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-032](../SPEC.md#close-operacion-032) | Release metadata retention: Acceptance requires full native PostgreSQL integration/vet and Linux race, a fresh… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-033](../SPEC.md#close-operacion-033) | Release metadata retention: Extend the existing deployment and telemetry-retention configuration owners, not a… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-034](../SPEC.md#close-operacion-034) | Release metadata retention: Before activating either cleanup class, primary must elaborate its complete owners… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-035](../SPEC.md#close-operacion-035) | Completed-file retention: This increment consumes Config.FileRetentionDays for the currently implemented durab… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-036](../SPEC.md#close-operacion-036) | Completed-file retention: Use each existing created_at as its first durable file receipt; retries of a still-p… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-037](../SPEC.md#close-operacion-037) | Completed-file retention: Store.CleanupFiles(ctx, days) returns committed source-map, attachment and opaque-it… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-038](../SPEC.md#close-operacion-038) | Completed-file retention: Before any source-map row lock/deletion, take the existing per-project/release artif… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-039](../SPEC.md#close-operacion-039) | Completed-file retention: All source-map writers must use the existing artifact gate for their project/release… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-040](../SPEC.md#close-operacion-040) | Completed-file retention: SourceMaps already preflights and reads bytes/aliases in one repeatable-read snapsho… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-041](../SPEC.md#close-operacion-041) | Completed-file retention: Run StartFileRetention unconditionally through the existing in-process retention lif… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-042](../SPEC.md#close-operacion-042) | Completed-file retention: The real PostgreSQL owning tests must cover independent file/event clocks and curren… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-043](../SPEC.md#close-operacion-043) | Completed-file retention: Acceptance requires full native integration/vet and Linux race, a fresh actual appli… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-156](../SPEC.md#close-errores-156) | Native event attachment access: Attachments are already admitted atomically with their parent SDK receipt, but… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-157](../SPEC.md#close-errores-157) | Native event attachment access: Add session-authenticated GET /api/v1/issues/{issue}/events/{event}/attachment… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-158](../SPEC.md#close-errores-158) | Native event attachment access: Expose Store.IssueEventAttachments(ctx,userID,issueID,eventID,page) and Store.… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-159](../SPEC.md#close-errores-159) | Native event attachment access: Metadata includes id, filename, contentType, size and dateCreated, never bytea… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-160](../SPEC.md#close-errores-160) | Native event attachment access: The HTTP boundary adds downloadName to metadata, using the same policy as Cont… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-161](../SPEC.md#close-errores-161) | Native event attachment access: On a retained issue event, add an Event attachments region with loading/retry,… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-162](../SPEC.md#close-errores-162) | Native event attachment access: Test-audit precedes authoring. Real PostgreSQL owns exact binary/empty data, b… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-012](../SPEC.md#close-logs-012) | Native OTLP log reception: This telemetry increment receives logs from standard OpenTelemetry HTTP exporters w… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-013](../SPEC.md#close-logs-013) | Native OTLP log reception: GlitchTip6.2 release notes confirm this endpoint, but the public installation/log g… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-014](../SPEC.md#close-logs-014) | Native OTLP log reception: Authenticate using exactly one nonempty X-GoodSentry-Key header, with the existing… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-015](../SPEC.md#close-logs-015) | Native OTLP log reception: OPTIONS /v1/logs returns204 without database access or ingestion, allows POST/OPTIO… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-016](../SPEC.md#close-logs-016) | Native OTLP log reception: Decode the HTTP entity as one ExportLogs request. Binary zero bytes and JSON {} are… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-017](../SPEC.md#close-logs-017) | Native OTLP log reception: Use the official general-purpose go.opentelemetry.io/proto/otlp v1.11.0 signal mess… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-018](../SPEC.md#close-logs-018) | Native OTLP log reception: MAX_BODY_BYTES bounds both compressed and decompressed bytes and the canonical stor… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-019](../SPEC.md#close-logs-019) | Native OTLP log reception: An admitted batch preserves every record's full AnyValue body; resource, scope and… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-020](../SPEC.md#close-logs-020) | Native OTLP log reception: Index one UTC timestamp per record using nonzero timeUnixNano, otherwise nonzero ob… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-021](../SPEC.md#close-logs-021) | Native OTLP log reception: Only nonzero16-byte trace IDs and nonzero8-byte span IDs contribute canonical lower… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-022](../SPEC.md#close-logs-022) | Native OTLP log reception: Migration021 adds an otlp_log_batches table with numeric id, project ownership, rec… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-023](../SPEC.md#close-logs-023) | Native OTLP log reception: Store.IngestOTLP accepts a completely decoded, bounded batch and writes its batch r… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-024](../SPEC.md#close-logs-024) | Native OTLP log reception: For valid encoding/authentication, commit every record or reject the complete reque… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-025](../SPEC.md#close-logs-025) | Native OTLP log reception: Keep the existing native Logs listing/page/filter contract. SDK entries retain thei… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-026](../SPEC.md#close-logs-026) | Native OTLP log reception: The original Tailwind/daisyUI Projects view gains a per-project OpenTelemetry discl… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-027](../SPEC.md#close-logs-027) | Native OTLP log reception: Acceptance owners: pure internal/otlp consumer tests prove actual binary/JSON equiv… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-028](../SPEC.md#close-logs-028) | Native OTLP log reception: Run the actual official Python SDK1.45.1 with opentelemetry-exporter-otlp-proto-htt… | implementada (local) | bloqueada | Restaurar la copia de 4010 filas/524 eventos o autorizar una línea base nueva y CI público |
| [CLOSE-rendimiento-012](../SPEC.md#close-rendimiento-012) | Sentry spans and native telemetry reads: Current JavaScript SDKs use standalone span v2 envelopes by default.… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-013](../SPEC.md#close-rendimiento-013) | Sentry spans and native telemetry reads: The telemetry UI exposes transaction aggregates and paginated log fil… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-014](../SPEC.md#close-rendimiento-014) | Sentry spans and native telemetry reads: The next telemetry slice adds native start and end query parameters t… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-015](../SPEC.md#close-rendimiento-015) | Sentry spans and native telemetry reads: Add UI presets for last hour, 24 hours, seven days and 30 days, with… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-016](../SPEC.md#close-rendimiento-016) | Sentry spans and native telemetry reads: The next detail increment opens a concrete stored transaction occurre… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-017](../SPEC.md#close-rendimiento-017) | Sentry spans and native telemetry reads: Expose authenticated native GET /api/v1/organizations/{org}/transacti… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-018](../SPEC.md#close-rendimiento-018) | Sentry spans and native telemetry reads: The detail JSON has sample matching the reference above; projectName,… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-019](../SPEC.md#close-rendimiento-019) | Sentry spans and native telemetry reads: For legacy transactions, read the selected event's nested spans array… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-020](../SPEC.md#close-rendimiento-020) | Sentry spans and native telemetry reads: In the existing Transactions view, make the transaction name open its… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-021](../SPEC.md#close-rendimiento-021) | Sentry spans and native telemetry reads: The child-operation statistics increment groups spans by exact operat… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-022](../SPEC.md#close-rendimiento-022) | Sentry spans and native telemetry reads: Expose authenticated native GET /api/v1/organizations/{org}/transacti… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-023](../SPEC.md#close-rendimiento-023) | Sentry spans and native telemetry reads: GlitchTip-compatible GET /api/0/organizations/{org}/span-groups/ and… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-024](../SPEC.md#close-rendimiento-024) | Sentry spans and native telemetry reads: GlitchTip-compatible GET /api/0/organizations/{org}/stats_v2/ serves… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-025](../SPEC.md#close-rendimiento-025) | Sentry spans and native telemetry reads: Compatible transaction-group identity is stored in additive migration… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-026](../SPEC.md#close-rendimiento-026) | Sentry spans and native telemetry reads: Legacy statistics include every nested span record, using the exact G… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-027](../SPEC.md#close-rendimiento-027) | Sentry spans and native telemetry reads: For standalone spans, attribute non-segment children only by an attri… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-028](../SPEC.md#close-rendimiento-028) | Sentry spans and native telemetry reads: In Transactions, add an Operations action for each aggregate, opening… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-029](../SPEC.md#close-rendimiento-029) | Sentry spans and native telemetry reads: The trace increment reconstructs all stored transaction roots, nested… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-030](../SPEC.md#close-rendimiento-030) | Sentry spans and native telemetry reads: Migration008 extends events with normalized legacy root span ID, pare… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-031](../SPEC.md#close-rendimiento-031) | Sentry spans and native telemetry reads: A missing resolved parent uses parentId empty and parentPage0; roots/… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-032](../SPEC.md#close-rendimiento-032) | Sentry spans and native telemetry reads: Build the graph from a streamed narrow index of storage identities, v… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-033](../SPEC.md#close-rendimiento-033) | Sentry spans and native telemetry reads: The main trace JSON contains traceId, projectId filter, nodeCount, pr… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-034](../SPEC.md#close-rendimiento-034) | Sentry spans and native telemetry reads: Related logs from the trace must include identifiers differing only i… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-035](../SPEC.md#close-rendimiento-035) | Sentry spans and native telemetry reads: Add View trace actions beside existing trace-to-logs/copy controls in… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-036](../SPEC.md#close-rendimiento-036) | Sentry spans and native telemetry reads: Acceptance owners: real PostgreSQL protects mixed-source/cross-projec… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-013](../SPEC.md#close-uptime-013) | Sentry spans and native telemetry reads: Uptime monitors support HEAD, GET, POST with expected status, TCP and… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-014](../SPEC.md#close-uptime-014) | Sentry spans and native telemetry reads: The first uptime slice uses native organization-scoped /api/v1/organi… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-015](../SPEC.md#close-uptime-015) | Sentry spans and native telemetry reads: Use UTC PostgreSQL timestamptz instants with microsecond precision fo… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-016](../SPEC.md#close-uptime-016) | Sentry spans and native telemetry reads: Persist monitor configuration/current state and append-only check his… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-017](../SPEC.md#close-uptime-017) | Sentry spans and native telemetry reads: Claim one due enabled monitor with FOR UPDATE SKIP LOCKED, assign a r… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-018](../SPEC.md#close-uptime-018) | Sentry spans and native telemetry reads: An opaque random 128-bit heartbeat capability is exposed as heartbeat… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-019](../SPEC.md#close-uptime-019) | Sentry spans and native telemetry reads: Block private, loopback, link-local, unspecified and multicast destin… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-020](../SPEC.md#close-uptime-020) | Sentry spans and native telemetry reads: The Tailwind UI lists monitor status, target, interval and last check… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-021](../SPEC.md#close-uptime-021) | Sentry spans and native telemetry reads: The SSL monitor increment uses the public SSL enum, independently def… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-022](../SPEC.md#close-uptime-022) | Sentry spans and native telemetry reads: Accept native type: SSL with an absolute https://host:port origin, op… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-023](../SPEC.md#close-uptime-023) | Sentry spans and native telemetry reads: Each SSL check creates a new TCP connection through the existing nett… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-024](../SPEC.md#close-uptime-024) | Sentry spans and native telemetry reads: Share one configured deadline across resolution, connect and handshak… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-025](../SPEC.md#close-uptime-025) | Sentry spans and native telemetry reads: Add SSL to the database kind constraint in additive033 without rewrit… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-uptime-026](../SPEC.md#close-uptime-026) | Sentry spans and native telemetry reads: Extend owning configuration tests for accepted origins and distinct r… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-alertas-004](../SPEC.md#close-alertas-004) | Sentry spans and native telemetry reads: Alert rules use error counts over a rolling window and notify on thre… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-005](../SPEC.md#close-alertas-005) | Sentry spans and native telemetry reads: The first alert workflow uses owner/admin native project-scoped /api/… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-006](../SPEC.md#close-alertas-006) | Sentry spans and native telemetry reads: Frequency means all non-transaction issue events in the project, incl… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-007](../SPEC.md#close-alertas-007) | Sentry spans and native telemetry reads: Add an optional environment string of at most 128 characters to monit… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-008](../SPEC.md#close-alertas-008) | Sentry spans and native telemetry reads: Replacing configuration or deleting a rule cancels its pending/in-fli… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-009](../SPEC.md#close-alertas-009) | Sentry spans and native telemetry reads: Delivery states are pending, processing, sent, failed and canceled. S… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-010](../SPEC.md#close-alertas-010) | Sentry spans and native telemetry reads: Run alert workers in the same process with ALERT_WORKERS default 2, r… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-011](../SPEC.md#close-alertas-011) | Sentry spans and native telemetry reads: List project delivery history at /api/v1/organizations/{org}/projects… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-012](../SPEC.md#close-alertas-012) | Sentry spans and native telemetry reads: On 2026-10-14 the user explicitly authorized automatic delivery to ru… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-013](../SPEC.md#close-alertas-013) | Slack incoming webhooks: Provider-specific alerts are independent of SSL certificate monitors and account reco… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-014](../SPEC.md#close-alertas-014) | Slack incoming webhooks: Add explicit recipient type slack using the same destination admission as webhook: an… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-015](../SPEC.md#close-alertas-015) | Slack incoming webhooks: Additive034_slack_alerts.sql expands only alert_deliveries_recipient_type_check to al… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-016](../SPEC.md#close-alertas-016) | Slack incoming webhooks: Use Slack's incoming webhook JSON contract(https://docs.slack.dev/messaging/sending-m… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-017](../SPEC.md#close-alertas-017) | Slack incoming webhooks: The summary is GoodSentry: rule name, then a Project line. Frequency adds the optiona… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-018](../SPEC.md#close-alertas-018) | Slack incoming webhooks: Keep POST, JSON content type and the two existing GoodSentry identity headers for Sla… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-019](../SPEC.md#close-alertas-019) | Slack incoming webhooks: Slack rate-limit responses(https://docs.slack.dev/apis/web-api/rate-limits/) include… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-020](../SPEC.md#close-alertas-020) | Slack incoming webhooks: Expose Slack incoming webhook as a recipient choice in the existing original Alert di… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-021](../SPEC.md#close-alertas-021) | Slack incoming webhooks: Pure owning checks cover configured Slack acceptance/deduplication/URL rejection, act… | implementada (local) | bloqueada | Recibir en un canal de Slack autorizado y publicar imagen/CI |
| [CLOSE-alertas-022](../SPEC.md#close-alertas-022) | Additional provider webhooks: The remaining incoming-webhook providers are explicit recipient types discord, t… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-023](../SPEC.md#close-alertas-023) | Additional provider webhooks: Additive038_provider_alerts.sql expands only alert_deliveries_recipient_type_che… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-024](../SPEC.md#close-alertas-024) | Additional provider webhooks: All six providers receive the same human-readable summary as Slack: GoodSentry:… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-025](../SPEC.md#close-alertas-025) | Additional provider webhooks: Discord receives JSON with a top-level content of the summary, one newline and t… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-026](../SPEC.md#close-alertas-026) | Additional provider webhooks: Microsoft Teams receives a MessageCard with @type:MessageCard, @context:https://… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-027](../SPEC.md#close-alertas-027) | Additional provider webhooks: Google Chat receives {"text": summary + newline + canonical URL} with HTML-signi… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-028](../SPEC.md#close-alertas-028) | Additional provider webhooks: Feishu receives {"msg_type":"text","content":{"text": summary + newline + canoni… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-029](../SPEC.md#close-alertas-029) | Additional provider webhooks: ntfy receives the summary and canonical URL as a UTF-8 plain-text POST of at mos… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-030](../SPEC.md#close-alertas-030) | Additional provider webhooks: Zulip uses its documented Slack-compatible incoming webhook URL and receives {"t… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-031](../SPEC.md#close-alertas-031) | Additional provider webhooks: The Alert dialog exposes each provider type beside Slack, labels the destination… | implementada (local) | bloqueada | Recibos reales de cada proveedor y publicación/CI |
| [CLOSE-alertas-032](../SPEC.md#close-alertas-032) | Mattermost incoming webhooks: GlitchTip supports Mattermost incoming-webhook alert recipients. GoodSentry adds… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-033](../SPEC.md#close-alertas-033) | Mattermost incoming webhooks: Additive migration 058_mattermost_alerts.sql expands only alert_deliveries_recip… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-034](../SPEC.md#close-alertas-034) | Mattermost incoming webhooks: POST Content-Type: application/json with {"text": summary plus resource link}. T… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-035](../SPEC.md#close-alertas-035) | Mattermost incoming webhooks: Mattermost succeeds only for HTTP 200 with a complete response body that trims t… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-alertas-036](../SPEC.md#close-alertas-036) | Mattermost incoming webhooks: Expose Mattermost in the existing Alert dialog with its own label/help and a rem… | implementada (local) | bloqueada | Recibo real de Mattermost autorizado |
| [CLOSE-identidad-065](../SPEC.md#close-identidad-065) | Compatible discovery surface: Organization JSON uses the public case-sensitive require2FA boolean from the per… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-066](../SPEC.md#close-identidad-066) | Compatible discovery surface: Expose the read-only discovery routes used by Sentry-compatible clients: authent… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-067](../SPEC.md#close-identidad-067) | Compatible discovery surface: Organization responses use the public OpenAPI OrganizationSchema fields with rea… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-068](../SPEC.md#close-identidad-068) | Compatible discovery surface: List routes accept strict limit or per_page from1..100 (not both with different… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-069](../SPEC.md#close-identidad-069) | Compatible discovery surface: These routes are read-only and add no schema, feature worker or external integra… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-070](../SPEC.md#close-identidad-070) | Compatible organization detail and project keys: Additive039_compat_management.sql supplies the remaining read… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-071](../SPEC.md#close-identidad-071) | Compatible organization detail and project keys: GET /api/0/organizations/{org}/ returns the live Organization… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-072](../SPEC.md#close-identidad-072) | Compatible organization detail and project keys: Project-key routes are GET/POST /api/0/projects/{org}/{projec… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-073](../SPEC.md#close-identidad-073) | Compatible organization detail and project keys: Key creation preserves the generated public key only in the D… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-074](../SPEC.md#close-identidad-074) | Compatible organization detail and project keys: The native Projects view offers a project-scoped SDK-key mana… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-075](../SPEC.md#close-identidad-075) | Compatible organization detail and project keys: Real PostgreSQL owns039/041 preservation/reopen, UUID/team-cl… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-076](../SPEC.md#close-identidad-076) | Compatible project update and deletion: PUT /api/0/projects/{org}/{project}/ accepts project name and platform… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-077](../SPEC.md#close-identidad-077) | Compatible project update and deletion: Additive061 stores each project's event_throttle_rate with a default o… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-078](../SPEC.md#close-identidad-078) | Compatible project update and deletion: The native project UI exposes Edit and permanent-delete confirmation t… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-079](../SPEC.md#close-identidad-079) | Compatible project update and deletion: GET/POST /api/0/organizations/{org}/repos/ exposes the existing organi… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-080](../SPEC.md#close-identidad-080) | Compatible project update and deletion: POST /api/0/teams/{org}/{team}/projects/ accepts ProjectIn {name,slug?… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-081](../SPEC.md#close-identidad-081) | Compatible project update and deletion: Additive048_project_teams.sql introduces the authoritative many-to-man… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-082](../SPEC.md#close-identidad-082) | Compatible project update and deletion: POST/DELETE /api/0/projects/{org}/{project}/teams/{team}/ add or remov… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-083](../SPEC.md#close-identidad-083) | Compatible project update and deletion: GlitchTip-specific POST /api/0/organizations/{org}/members/{member}/se… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-084](../SPEC.md#close-identidad-084) | Compatible project update and deletion: Additive049_project_environments.sql creates project-scoped environmen… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-085](../SPEC.md#close-identidad-085) | Compatible project update and deletion: GET /api/0/projects/{org}/{project}/environments/ and its /{name}/ det… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-086](../SPEC.md#close-identidad-086) | Compatible project update and deletion: The read-only people surface is GET /api/0/organizations/{org}/members… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-087](../SPEC.md#close-identidad-087) | Compatible project update and deletion: Organization people use OrganizationUserSchema with the real user id,… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-088](../SPEC.md#close-identidad-088) | Compatible project update and deletion: GET /api/0/organizations/{org}/members/{member}/ returns the same Orga… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-089](../SPEC.md#close-identidad-089) | Compatible project update and deletion: Real PostgreSQL covers organization/member/team visibility, ordered te… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-090](../SPEC.md#close-identidad-090) | Compatible project update and deletion: Additive040 expands the API-token scope constraint with org:write. Exi… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-091](../SPEC.md#close-identidad-091) | Compatible project update and deletion: Additive migration062 extends membership and invitation role constrain… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-092](../SPEC.md#close-identidad-092) | Compatible project update and deletion: Compatible team mutations are POST /api/0/organizations/{org}/teams/ w… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-093](../SPEC.md#close-identidad-093) | Compatible project update and deletion: Real PostgreSQL covers040 token preservation/reopen, org:write issuanc… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-094](../SPEC.md#close-identidad-094) | Compatible project update and deletion: PUT /api/0/organizations/{org}/ accepts only OrganizationInSchema {nam… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-095](../SPEC.md#close-identidad-095) | Compatible project update and deletion: POST /api/0/organizations/ is available only when GOODSENTRY_ENABLE_OR… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-096](../SPEC.md#close-identidad-096) | Compatible project update and deletion: Real PostgreSQL covers rename preservation, invalid names and owner/ma… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-operacion-044](../SPEC.md#close-operacion-044) | Public repository and product site: The user also explicitly requests Tailwind CSS plus daisyUI for the applic… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-045](../SPEC.md#close-operacion-045) | Public repository and product site: Use official daisyUI v5.7.50 standalone daisyui.mjs and daisyui-theme.mjs,… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-046](../SPEC.md#close-operacion-046) | Public repository and product site: The user requests publication of the code to https://github.com/jerolan/go… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-047](../SPEC.md#close-operacion-047) | Public repository and product site: Use an original GoodSentry design informed by the public Sentry welcome pa… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-048](../SPEC.md#close-operacion-048) | Public repository and product site: Publish only site/ through the official Pages artifact/deploy workflow on… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-163](../SPEC.md#close-errores-163) | SDK feedback: Feedback parity requires both legacy user reports and modern captureFeedback events. Implement a… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-164](../SPEC.md#close-errores-164) | Legacy report identity and storage: Receive SDK envelope items of type user_report and POST /api/0/projects/{o… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-165](../SPEC.md#close-errores-165) | Legacy report identity and storage: Migration029 adds only user_reports: numeric ID, project FK with cascade,… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-166](../SPEC.md#close-errores-166) | Legacy report identity and storage: Store.SubmitUserReport(ctx,projectID,input) writes under the existing proj… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-167](../SPEC.md#close-errores-167) | Legacy report identity and storage: An absent error yields a durable pending report, visible only to an author… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-168](../SPEC.md#close-errores-168) | Legacy report identity and storage: Store.CleanupPendingUserReports(ctx) removes at most250 unbound reports wi… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-169](../SPEC.md#close-errores-169) | Legacy report consumers and transport: Read reports through project GET /api/0/projects/{org}/{project}/user-f… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-170](../SPEC.md#close-errores-170) | Legacy report consumers and transport: POST supports a current session with existing CSRF, or an authoritative… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-171](../SPEC.md#close-errores-171) | Legacy report consumers and transport: REST submission elaboration:030 only extends the API-token scope constr… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-172](../SPEC.md#close-errores-172) | Legacy report consumers and transport: POST authentication chooses a supplied Authorization header authoritati… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-173](../SPEC.md#close-errores-173) | Legacy report consumers and transport: Replace actual null/zero placeholders: authorized compatible full event… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-174](../SPEC.md#close-errores-174) | Legacy report consumers and transport: Acceptance owners use real PostgreSQL for receipt/update clocks, revers… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-175](../SPEC.md#close-errores-175) | Embed error page: GET/POST /api/embed/error-page/ is a public capability route authenticated only by a project… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-176](../SPEC.md#close-errores-176) | Native legacy report UI elaboration: Use an original User reports panel in the issue's left column between eve… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-177](../SPEC.md#close-errores-177) | Native legacy report UI elaboration: Add reports.js as a dynamically imported module through the existing fact… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-178](../SPEC.md#close-errores-178) | Native legacy report UI elaboration: Native issue inspection currently shows its latest payload only. A report… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-179](../SPEC.md#close-errores-179) | Native legacy report UI elaboration: Browser acceptance must show more than25 actual SDK reports, a reported e… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-180](../SPEC.md#close-errores-180) | Native legacy report UI elaboration: Explicit report refresh/retry moves focus to the report heading before re… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-181](../SPEC.md#close-errores-181) | Native legacy report UI elaboration: The reusable browser owner is tests/compat/user_reports_browser.mjs. GOOD… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-182](../SPEC.md#close-errores-182) | Modern feedback contract: Deliver the distinct modern captureFeedback consumer after the accepted legacy incre… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-183](../SPEC.md#close-errores-183) | Modern feedback contract: Receive feedback as a first-class retained event and issue category, not an opaque i… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-184](../SPEC.md#close-errores-184) | Modern feedback contract: Normalization accepts type feedback and requires an object contexts.feedback with a… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-185](../SPEC.md#close-errores-185) | Modern feedback contract: Migration031 adds issues.category text NOT NULL DEFAULT error with CHECK error/feedb… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-186](../SPEC.md#close-errores-186) | Modern feedback contract: The same migration adds feedback_events with event_receipt_id as its primary key, pr… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-187](../SPEC.md#close-errores-187) | Modern feedback contract: Associate only to an error/message receipt inside the same project. An existing vali… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-188](../SPEC.md#close-errores-188) | Modern feedback contract: Feedback capture acquires the actual signed receipt advisory keys for its own and an… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-189](../SPEC.md#close-errores-189) | Modern feedback contract: Add Store.BindPendingFeedbackAssociations(ctx)(int64,error) for bounded catch-up. It… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-190](../SPEC.md#close-errores-190) | Modern feedback contract: Ingest creates and counts feedback only in its own category, with first-seen activit… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-191](../SPEC.md#close-errores-191) | Modern feedback contract: Issue.Category is durable and appears as category in native DTOs and issueCategory i… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-192](../SPEC.md#close-errores-192) | Modern feedback contract: Add issue.category as a reserved issue-state search term in both native and compatib… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-193](../SPEC.md#close-errores-193) | Modern feedback contract: Native IssueDetail adds first25 FeedbackItems and FeedbackCount in its authorization… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-194](../SPEC.md#close-errores-194) | Modern feedback contract: The reader API is Store.IssueFeedback(ctx,userID,issueID,page)(FeedbackItem,int64,bo… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-195](../SPEC.md#close-errores-195) | Modern feedback contract: Pure eventview projection recognizes retained Kind feedback before exception/CSP fie… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-196](../SPEC.md#close-errores-196) | Modern feedback contract: The Feedback UI shows the actual message, supplied contact/source text, receipt time… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-197](../SPEC.md#close-errores-197) | Native Feedback component: Add view=feedback beside Issues in the existing navigation/admission/title map. Bot… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-198](../SPEC.md#close-errores-198) | Native Feedback component: In a feedback detail, show the same simple Resolve/Reopen and Ignore indefinitely a… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-199](../SPEC.md#close-errores-199) | Native Feedback component: The new lazy module web/feedback.js exports createFeedbackUI({node,request,button,d… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-200](../SPEC.md#close-errores-200) | Native Feedback component: The module's state key is organization,issue.id, stored in view.feedback, with page… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-201](../SPEC.md#close-errores-201) | Native Feedback component: Use one inspection area, scoped by selected feedback eventID plus mode submission/a… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-202](../SPEC.md#close-errores-202) | Native Feedback component: Every page/inspection/attachment callback checks the live issue, connected panel an… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-203](../SPEC.md#close-errores-203) | Native Feedback component: Actual browser acceptance on the new copied image must cover navigation/filter/cate… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-204](../SPEC.md#close-errores-204) | Native Feedback component: The reusable SDK owner is tests/compat/modern_feedback_sdk.mjs. GOODSENTRY_MODERN_F… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-205](../SPEC.md#close-errores-205) | Native Feedback component: The reusable modern browser owner is tests/compat/modern_feedback_browser.mjs. Its… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-206](../SPEC.md#close-errores-206) | Native Feedback component: Acceptance owners are pure normalization for the public field/default/identity cont… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-lectores-001](../SPEC.md#close-lectores-001) | Integrations and compatibility limits: The official sentry-wizard flow is supported through GET /api/0/wizard/… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-lectores-002](../SPEC.md#close-lectores-002) | Integrations and compatibility limits: Management API routes use /api/0 where compatible with published Sentry… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-lectores-003](../SPEC.md#close-lectores-003) | Integrations and compatibility limits: ENABLE_OPENAPI=true exposes GET /api/openapi.json as an OpenAPI 3.1 doc… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-identidad-097](../SPEC.md#close-identidad-097) | Instance user administration: Extend the opt-in ENABLE_ADMIN surface with account operations inspired by Glitc… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-098](../SPEC.md#close-identidad-098) | Instance user administration: Add users.is_instance_admin boolean NOT NULL DEFAULT false and users.is_active b… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-099](../SPEC.md#close-identidad-099) | Instance user administration: When ENABLE_ADMIN=true, expose GET /api/v1/admin/users and PATCH /api/v1/admin/u… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-100](../SPEC.md#close-identidad-100) | Instance user administration: Every list and update rechecks that the current account is active and an instanc… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-101](../SPEC.md#close-identidad-101) | Instance user administration: Once an instance administrator exists, serialized updates and the existing self-… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-102](../SPEC.md#close-identidad-102) | Instance user administration: The first-owner/admin bootstrap must be serialized with user creation. The first… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-103](../SPEC.md#close-identidad-103) | Instance user administration: Acceptance uses real PostgreSQL migration from063 and reopen, store concurrency… | implementada (local) | bloqueada | Autorizar CI público y publicación; luego repetir la actualización con imágenes en Compose |
| [CLOSE-operacion-049](../SPEC.md#close-operacion-049) | Instance user administration: Optional MCP uses Streamable HTTP with authenticated scoped access to issues, lo… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-050](../SPEC.md#close-operacion-050) | Instance user administration: GOODSENTRY_ENABLE_ARCHIVE is the canonical switch. Preserve GOODSENTRY_ENABLE_DU… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-051](../SPEC.md#close-operacion-051) | Instance user administration: Every span read that can include cold data runs on one Repeatable Read, Read Wri… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-052](../SPEC.md#close-operacion-052) | Instance user administration: Route all current span readers through this transaction-owned relation: direct p… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-053](../SPEC.md#close-operacion-053) | Instance user administration: The additive catalog migration marks pre-index manifests incomplete. When archiv… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-054](../SPEC.md#close-operacion-054) | Instance user administration: Readers lock selected ready manifests FOR SHARE before downloading their objects… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-055](../SPEC.md#close-operacion-055) | Instance user administration: GOODSENTRY_ARCHIVE_BACKEND is local by default and may be s3. GOODSENTRY_ARCHIVE… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-056](../SPEC.md#close-operacion-056) | Instance user administration: GOODSENTRY_ENABLE_DUCKDB remains off by default in Compose and retains its docum… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-057](../SPEC.md#close-operacion-057) | Instance user administration: Generic OpenID Connect sign-in and organization-managed OIDC applications are im… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-001](../SPEC.md#close-mcp-001) | Instance user administration: GlitchTip's public MCP guide at https://glitchtip.com/documentation/mcp/ defines… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-002](../SPEC.md#close-mcp-002) | MCP transport and workspace discovery: Use the official github.com/modelcontextprotocol/go-sdk v1.8.0, whose p… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-003](../SPEC.md#close-mcp-003) | MCP transport and workspace discovery: MCP is optional and disabled by default. Disabled configurations return… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-004](../SPEC.md#close-mcp-004) | MCP transport and workspace discovery: Register the exact prefix-aware /mcp endpoint when constructing an MCP-… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-005](../SPEC.md#close-mcp-005) | MCP transport and workspace discovery: Validate Origin on every request before reading its body. Absence suppo… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-006](../SPEC.md#close-mcp-006) | MCP transport and workspace discovery: Only one syntactically valid native Bearer credential authorizes an MCP… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-007](../SPEC.md#close-mcp-007) | MCP transport and workspace discovery: fila 1, / list_organizations / Optional query, page and limit / data of… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-008](../SPEC.md#close-mcp-008) | MCP transport and workspace discovery: fila 2, / list_projects / Required organization ID or slug; optional qu… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-009](../SPEC.md#close-mcp-009) | MCP transport and workspace discovery: Schemas reject unknown fields, null/wrong types and noninteger paginati… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-010](../SPEC.md#close-mcp-010) | MCP transport and workspace discovery: Reuse existing compatible organization and management-project providers… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-011](../SPEC.md#close-mcp-011) | MCP transport and workspace discovery: Acceptance owns observable protocol behavior through a real HTTP listen… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-012](../SPEC.md#close-mcp-012) | MCP issue investigation: Expose the five issue tools listed in the public GlitchTip MCP guide through the same… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-013](../SPEC.md#close-mcp-013) | MCP issue investigation: Read tools accept either event:read or event:write, matching the existing issue API.… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-014](../SPEC.md#close-mcp-014) | MCP issue investigation: fila 1, / list_issues / Required organization ID/slug; optional project ID/slug, quer… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-015](../SPEC.md#close-mcp-015) | MCP issue investigation: fila 2, / get_issue / Required issue_id / Existing compatible issue detail / | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-016](../SPEC.md#close-mcp-016) | MCP issue investigation: fila 3, / get_latest_event / Required issue_id / Existing full compatible event proje… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-017](../SPEC.md#close-mcp-017) | MCP issue investigation: fila 4, / get_event / Required event_id; optional project ID/slug / Full compatible e… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-018](../SPEC.md#close-mcp-018) | MCP issue investigation: fila 5, / update_issue / Required issue_id and status (resolved, unresolved, ignored)… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-019](../SPEC.md#close-mcp-019) | MCP issue investigation: Schemas reject unknown fields, null and wrong types. Pagination retains workspace def… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-020](../SPEC.md#close-mcp-020) | MCP issue investigation: Issue IDs use canonical positive decimal int64 strings. Event IDs accept the existing… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-021](../SPEC.md#close-mcp-021) | MCP issue investigation: Event ID is unique per project, not per organization. Add one Store operation, Compat… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-022](../SPEC.md#close-mcp-022) | MCP issue investigation: update_issue applies the native individual update with its scoped access and audit/ac… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-023](../SPEC.md#close-mcp-023) | MCP issue investigation: Acceptance uses the official MCP client against HTTP and disposable PostgreSQL. Disco… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-024](../SPEC.md#close-mcp-024) | MCP log investigation: Expose list_logs and get_log from the public GlitchTip MCP guide on the internal transp… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-025](../SPEC.md#close-mcp-025) | MCP log investigation: Both tools require a new, explicit log:read scope. Existing org/project/event scopes mu… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-026](../SPEC.md#close-mcp-026) | MCP log investigation: fila 1, / list_logs / Required organization ID/slug; optional project ID/slug, query, l… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-027](../SPEC.md#close-mcp-027) | MCP log investigation: fila 2, / get_log / Required canonical positive int64 log_id / Existing Store LogDetail… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-028](../SPEC.md#close-mcp-028) | MCP log investigation: Reject unknown properties, null and wrong types. Retain workspace pagination defaults/b… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-029](../SPEC.md#close-mcp-029) | MCP log investigation: Add ScopedLogs and ScopedLog using the existing ManagementAccess shape with a required… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-030](../SPEC.md#close-mcp-030) | MCP log investigation: Share the existing native list/detail query and projection kernels. Preserve the native… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-031](../SPEC.md#close-mcp-031) | MCP log investigation: Acceptance uses real HTTP/PostgreSQL and the official MCP client. Create the opt-in sco… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-032](../SPEC.md#close-mcp-032) | MCP alert and monitor inspection: Expose the public GlitchTip tools list_alerts and list_monitors through the… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-033](../SPEC.md#close-mcp-033) | MCP alert and monitor inspection: fila 1, / list_alerts / Required organization ID/slug; optional project ID/s… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-034](../SPEC.md#close-mcp-034) | MCP alert and monitor inspection: fila 2, / list_monitors / Required organization ID/slug; optional page and l… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-035](../SPEC.md#close-mcp-035) | MCP alert and monitor inspection: Reject unknown fields, null/wrong types, invalid page/limit and invalid Unic… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-036](../SPEC.md#close-mcp-036) | MCP alert and monitor inspection: Add ScopedAlerts and ScopedMonitors using ManagementAccess. Both providers r… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-037](../SPEC.md#close-mcp-037) | MCP alert and monitor inspection: list_alerts returns a purpose-built summary: id, projectId, name, kind, enab… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-038](../SPEC.md#close-mcp-038) | MCP alert and monitor inspection: Acceptance uses real HTTP/PostgreSQL and the official MCP client. Discover s… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-039](../SPEC.md#close-mcp-039) | MCP performance investigation: Expose the six public GlitchTip performance tools through the selected MCP hand… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-040](../SPEC.md#close-mcp-040) | MCP performance investigation: fila 1, / list_transaction_groups / Required organization; optional project, st… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-041](../SPEC.md#close-mcp-041) | MCP performance investigation: fila 2, / get_transaction_group / Required organization and transaction; option… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-042](../SPEC.md#close-mcp-042) | MCP performance investigation: fila 3, / list_transaction_spans / Same group inputs plus page / Native grouped… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-043](../SPEC.md#close-mcp-043) | MCP performance investigation: fila 4, / list_span_groups / Required organization; optional project, operation… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-044](../SPEC.md#close-mcp-044) | MCP performance investigation: fila 5, / detect_n_plus_one / Required organization; optional project, start/en… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-045](../SPEC.md#close-mcp-045) | MCP performance investigation: fila 6, / get_transaction_trend / Required organization and transaction; option… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-046](../SPEC.md#close-mcp-046) | MCP performance investigation: Reject unknown/null/wrong inputs. Text references are1..200 valid Unicode chara… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-047](../SPEC.md#close-mcp-047) | MCP performance investigation: Add grant-contained Store providers over the existing event and span projection… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-048](../SPEC.md#close-mcp-048) | MCP performance investigation: Reuse native event/segment span unions, group sorting and latency definitions.… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-049](../SPEC.md#close-mcp-049) | MCP performance investigation: Acceptance creates real transaction events with indexed child spans across sele… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-050](../SPEC.md#close-mcp-050) | MCP OAuth authorization: When MCP is selected, provide a same-origin OAuth2 authorization server for MCP clien… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-051](../SPEC.md#close-mcp-051) | MCP OAuth authorization: Public RFC9728/RFC8414 metadata endpoints are prefix-aware and no-store: /.well-known… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-052](../SPEC.md#close-mcp-052) | MCP OAuth authorization: POST /oauth/register accepts a bounded JSON object: client_name1..100 valid character… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-053](../SPEC.md#close-mcp-053) | MCP OAuth authorization: GET /oauth/authorize validates response_type=code, registered client and exact redire… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-054](../SPEC.md#close-mcp-054) | MCP OAuth authorization: POST /oauth/authorize requires the current session, same-origin form, session CSRF, a… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-055](../SPEC.md#close-mcp-055) | MCP OAuth authorization: POST /oauth/token accepts form encoding. Authorization-code exchange requires exact c… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-056](../SPEC.md#close-mcp-056) | MCP OAuth authorization: Add additive044 storage for OAuth clients, hashed authorization codes and hashed refr… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-057](../SPEC.md#close-mcp-057) | MCP OAuth authorization: MCP Bearer denial changes from bare WWW-Authenticate: Bearer to include canonical res… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-058](../SPEC.md#close-mcp-058) | MCP OAuth authorization: The existing native credential form exposes log:read only when MCP and logs are enabl… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-059](../SPEC.md#close-mcp-059) | MCP OAuth authorization: Acceptance runs real HTTP/PostgreSQL and the official Go MCP client/OAuth handler. It… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-058](../SPEC.md#close-operacion-058) | Commands and source layout: The external JavaScript artifact acceptance uses official sentry-cli3.8.0 and Node… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-059](../SPEC.md#close-operacion-059) | Commands and source layout: The issue-query SDK acceptance consumes a separate mode0600 fixture with the same… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-060](../SPEC.md#close-operacion-060) | Commands and source layout: The browser fixture additionally supplies disposable owner login credentials, the… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-061](../SPEC.md#close-operacion-061) | Commands and source layout: cmd/goodsentry owns composition and lifecycle. internal/config owns environment pa… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-062](../SPEC.md#close-operacion-062) | Commands and source layout: Use gofmt, ordinary Go errors with context, parameterized SQL and explicit context… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-063](../SPEC.md#close-operacion-063) | Verification and delivery: Protect protocol parsing with table cases and fuzzing for binary framing, decompres… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-064](../SPEC.md#close-operacion-064) | Verification and delivery: Exercise pinned official Go, Python, Node and browser Sentry SDKs against the runni… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-065](../SPEC.md#close-operacion-065) | Verification and delivery: Each new test must identify the consumer contract, a credible regression, the gap i… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-066](../SPEC.md#close-operacion-066) | Verification and delivery: Always preserve clean-room provenance, user scope, existing controls and data. Ask… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-207](../SPEC.md#close-errores-207) | Current authorized delivery and compatible attachments (2026-10-18): Implement public Sentry attachment list/r… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-208](../SPEC.md#close-errores-208) | Current authorized delivery and compatible attachments (2026-10-18): Lists cap projected filename to 2048 Unic… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-067](../SPEC.md#close-operacion-067) | Current authorized delivery and compatible attachments (2026-10-18): Simplify the README and move all detailed… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-209](../SPEC.md#close-errores-209) | Browser CSP reporting: Public browser security reports are part of error tracking. Implement both exact /api/{… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-210](../SPEC.md#close-errores-210) | Browser CSP reporting: Validate every report before starting writes. Support csp-violation only; reject unsupp… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-211](../SPEC.md#close-errores-211) | Browser CSP reporting: Acceptance requires pure format/grouping/invalid-batch cases, real prefixed HTTP+Postgr… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-212](../SPEC.md#close-errores-212) | Go 0.50.0: error real por DSN, recuperable en API/UI con contexto, tags, entorno y release. | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-213](../SPEC.md#close-errores-213) | Go 0.50.0: agrupación, reintento sin duplicación, búsqueda/filtros, triage contratado y persistencia tras rein… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-037](../SPEC.md#close-rendimiento-037) | Go 0.50.0: transacciones y spans reales en API/UI con identidad, padre/hijo, tiempos, desglose y navegación de… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-logs-029](../SPEC.md#close-logs-029) | Go 0.50.0: logs reales con valores/tipos, severidad, filtros de tiempo/servicio/entorno/texto/traza, enlace a… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-214](../SPEC.md#close-errores-214) | Python 2.72.0: error real por DSN, recuperable en API/UI con contexto, tags, entorno y release. | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-215](../SPEC.md#close-errores-215) | Python 2.72.0: agrupación, reintento sin duplicación, búsqueda/filtros, triage contratado y persistencia tras… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-038](../SPEC.md#close-rendimiento-038) | Python 2.72.0: transacciones y spans reales en API/UI con identidad, padre/hijo, tiempos, desglose y navegació… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-logs-030](../SPEC.md#close-logs-030) | Python 2.72.0: logs reales con valores/tipos, severidad, filtros de tiempo/servicio/entorno/texto/traza, enlac… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-216](../SPEC.md#close-errores-216) | Node 11.7.0: error real por DSN, recuperable en API/UI con contexto, tags, entorno y release. | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-217](../SPEC.md#close-errores-217) | Node 11.7.0: agrupación, reintento sin duplicación, búsqueda/filtros, triage contratado y persistencia tras re… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-039](../SPEC.md#close-rendimiento-039) | Node 11.7.0: transacciones y spans reales en API/UI con identidad, padre/hijo, tiempos, desglose y navegación… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-logs-031](../SPEC.md#close-logs-031) | Node 11.7.0: logs reales con valores/tipos, severidad, filtros de tiempo/servicio/entorno/texto/traza, enlace… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-errores-218](../SPEC.md#close-errores-218) | Browser 11.7.0: error real por DSN, recuperable en API/UI con contexto, tags, entorno y release. | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-219](../SPEC.md#close-errores-219) | Browser 11.7.0: agrupación, reintento sin duplicación, búsqueda/filtros, triage contratado y persistencia tras… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-040](../SPEC.md#close-rendimiento-040) | Browser 11.7.0: transacciones y spans reales en API/UI con identidad, padre/hijo, tiempos, desglose y navegaci… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-logs-032](../SPEC.md#close-logs-032) | Browser 11.7.0: logs reales con valores/tipos, severidad, filtros de tiempo/servicio/entorno/texto/traza, enla… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-sourcemaps-026](../SPEC.md#close-sourcemaps-026) | Node 11.7.0 y sentry-cli 3.8.0: preparar/subir los artefactos del código minificado probado y capturar error r… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-027](../SPEC.md#close-sourcemaps-027) | Browser 11.7.0 y sentry-cli 3.8.0: preparar/subir los artefactos del código minificado probado y capturar erro… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-041](../SPEC.md#close-rendimiento-041) | Comprobar cantidades y errores contra un conjunto controlado y resultados esperados explícitos, conservando lo… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-042](../SPEC.md#close-rendimiento-042) | Comprobar latencias y percentiles contratados contra un conjunto controlado y resultados esperados explícitos,… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-043](../SPEC.md#close-rendimiento-043) | Comprobar tendencias contra un conjunto controlado y resultados esperados explícitos, conservando los contrato… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-rendimiento-044](../SPEC.md#close-rendimiento-044) | Comprobar detección N+1 contra un conjunto controlado y resultados esperados explícitos, conservando los contr… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-logs-033](../SPEC.md#close-logs-033) | Exportador Python OTLP protobuf/HTTP 1.45.1: logs reales, valores/tipos, severidad, búsqueda y filtros de tiem… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-logs-034](../SPEC.md#close-logs-034) | Exportador Python OTLP JSON/HTTP 0.66b1: logs reales, valores/tipos, severidad, búsqueda y filtros de tiempo/s… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-uptime-027](../SPEC.md#close-uptime-027) | Scheduler real de HEAD: caída, recuperación, umbrales, pausa/reanudación, historial y estado público de monito… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-uptime-028](../SPEC.md#close-uptime-028) | Scheduler real de GET: caída, recuperación, umbrales, pausa/reanudación, historial y estado público de monitor… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-uptime-029](../SPEC.md#close-uptime-029) | Scheduler real de POST: caída, recuperación, umbrales, pausa/reanudación, historial y estado público de monito… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-uptime-030](../SPEC.md#close-uptime-030) | Scheduler real de TCP: caída, recuperación, umbrales, pausa/reanudación, historial y estado público de monitor… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-uptime-031](../SPEC.md#close-uptime-031) | Scheduler real de certificado SSL: caída, recuperación, umbrales, pausa/reanudación, historial y estado públic… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-uptime-032](../SPEC.md#close-uptime-032) | Scheduler real de heartbeat: caída, recuperación, umbrales, pausa/reanudación, historial y estado público de m… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-operacion-068](../SPEC.md#close-operacion-068) | Archivo de errores, backend local: consulta durante retención mediante lectores UI/API aplicables, con filtros… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-069](../SPEC.md#close-operacion-069) | Archivo de errores, backend local: persistencia y consulta tras reinicio. Comprobante propio del tipo y backen… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-070](../SPEC.md#close-operacion-070) | Archivo de errores, backend local: eliminación al vencer el plazo contratado. Comprobante propio del tipo y ba… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-071](../SPEC.md#close-operacion-071) | Archivo de errores, backend compatible S3: consulta durante retención mediante lectores UI/API aplicables, con… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-072](../SPEC.md#close-operacion-072) | Archivo de errores, backend compatible S3: persistencia y consulta tras reinicio. Comprobante propio del tipo… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-073](../SPEC.md#close-operacion-073) | Archivo de errores, backend compatible S3: eliminación al vencer el plazo contratado. Comprobante propio del t… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-074](../SPEC.md#close-operacion-074) | Archivo de logs, backend local: consulta durante retención mediante lectores UI/API aplicables, con filtros y… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-075](../SPEC.md#close-operacion-075) | Archivo de logs, backend local: persistencia y consulta tras reinicio. Comprobante propio del tipo y backend;… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-076](../SPEC.md#close-operacion-076) | Archivo de logs, backend local: eliminación al vencer el plazo contratado. Comprobante propio del tipo y backe… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-077](../SPEC.md#close-operacion-077) | Archivo de logs, backend compatible S3: consulta durante retención mediante lectores UI/API aplicables, con fi… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-078](../SPEC.md#close-operacion-078) | Archivo de logs, backend compatible S3: persistencia y consulta tras reinicio. Comprobante propio del tipo y b… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-079](../SPEC.md#close-operacion-079) | Archivo de logs, backend compatible S3: eliminación al vencer el plazo contratado. Comprobante propio del tipo… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-080](../SPEC.md#close-operacion-080) | Archivo de transacciones, backend local: consulta durante retención mediante lectores UI/API aplicables, con f… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-081](../SPEC.md#close-operacion-081) | Archivo de transacciones, backend local: persistencia y consulta tras reinicio. Comprobante propio del tipo y… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-082](../SPEC.md#close-operacion-082) | Archivo de transacciones, backend local: eliminación al vencer el plazo contratado. Comprobante propio del tip… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-083](../SPEC.md#close-operacion-083) | Archivo de transacciones, backend compatible S3: consulta durante retención mediante lectores UI/API aplicable… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-084](../SPEC.md#close-operacion-084) | Archivo de transacciones, backend compatible S3: persistencia y consulta tras reinicio. Comprobante propio del… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-085](../SPEC.md#close-operacion-085) | Archivo de transacciones, backend compatible S3: eliminación al vencer el plazo contratado. Comprobante propio… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-086](../SPEC.md#close-operacion-086) | Archivo de spans, backend local: consulta durante retención mediante lectores UI/API aplicables, con filtros y… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-087](../SPEC.md#close-operacion-087) | Archivo de spans, backend local: persistencia y consulta tras reinicio. Comprobante propio del tipo y backend;… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-088](../SPEC.md#close-operacion-088) | Archivo de spans, backend local: eliminación al vencer el plazo contratado. Comprobante propio del tipo y back… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-089](../SPEC.md#close-operacion-089) | Archivo de spans, backend compatible S3: consulta durante retención mediante lectores UI/API aplicables, con f… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-090](../SPEC.md#close-operacion-090) | Archivo de spans, backend compatible S3: persistencia y consulta tras reinicio. Comprobante propio del tipo y… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-091](../SPEC.md#close-operacion-091) | Archivo de spans, backend compatible S3: eliminación al vencer el plazo contratado. Comprobante propio del tip… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-alertas-037](../SPEC.md#close-alertas-037) | Regla de frecuencia de errores: destinatarios, entorno, umbral, cooldown, trabajo durable, historial y entrega… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-alertas-038](../SPEC.md#close-alertas-038) | Regla de transición de uptime: destinatarios, entorno, umbral, cooldown, trabajo durable, historial y entregas… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-alertas-039](../SPEC.md#close-alertas-039) | Webhook genérico: alerta recibida por receptor HTTP independiente con JSON correcto, correlación alerta/payloa… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-027](../SPEC.md#close-nativo-027) | Crash real Linux/amd64 con sentry-native 0.17.4 y Crashpad: símbolos del mismo ELF/DWARF, issue con goodsentry… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-nativo-028](../SPEC.md#close-nativo-028) | Conservar regresiones de formatos/arquitecturas soportados y los demás contratos nativos, incluidos CFI, archi… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-identidad-104](../SPEC.md#close-identidad-104) | API y navegador: comprobar modos abierto y autenticado, alta/login/logout, con los resultados y controles de s… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-105](../SPEC.md#close-identidad-105) | API y navegador: comprobar recuperación de contraseña y correo local, con los resultados y controles de sus co… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-106](../SPEC.md#close-identidad-106) | API y navegador: comprobar TOTP, recuperación y require2FA, con los resultados y controles de sus contratos ac… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-107](../SPEC.md#close-identidad-107) | API y navegador: comprobar OIDC y aplicaciones SSO con controles y pruebas locales, con los resultados y contr… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-108](../SPEC.md#close-identidad-108) | API y navegador: comprobar organizaciones, equipos y miembros, con los resultados y controles de sus contratos… | implementada | vigente | Comprobante 2026-10-26 abajo |
| [CLOSE-identidad-109](../SPEC.md#close-identidad-109) | API y navegador: comprobar proyectos y DSNs, con los resultados y controles de sus contratos actuales, sin exc… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-identidad-110](../SPEC.md#close-identidad-110) | API y navegador: comprobar tokens, permisos/aislamiento, expiración y revocación, con los resultados y control… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-092](../SPEC.md#close-operacion-092) | Arranque de imagen documentada con PostgreSQL y volúmenes vacíos, salud/readiness, UI y captura SDK, sin reque… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-093](../SPEC.md#close-operacion-093) | Migraciones y actualización desde una versión publicada fijada al ejecutar, con revisión/digest registrados. | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-094](../SPEC.md#close-operacion-094) | Persistencia de datos, artefactos y configuración tras reinicio. | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-095](../SPEC.md#close-operacion-095) | Backup y restauración en entorno independiente, conservando datos, artefactos y configuración necesarios. | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-096](../SPEC.md#close-operacion-096) | Disponibilidad real de funciones opcionales y verificaciones de imagen, concurrencia y recuperación existentes… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-028](../SPEC.md#close-sourcemaps-028) | Crear y finalizar una release con sentry-cli 3.8.0. No requiere conexión automática con repositorios. | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-029](../SPEC.md#close-sourcemaps-029) | Asociar metadatos locales de commits y registrar un deploy con sentry-cli 3.8.0. No requiere conexión automáti… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-030](../SPEC.md#close-sourcemaps-030) | Consultar en API/UI la misma versión, proyectos, archivos y eventos asociados. No requiere conexión automática… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-031](../SPEC.md#close-sourcemaps-031) | Subida, listado, acceso y eliminación de artefactos sin borrar datos ajenos, con permisos, reintentos y persis… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-220](../SPEC.md#close-errores-220) | Conservar y comprobar los flujos de adjuntos, incluidos almacenamiento, listados, acceso/descarga y controles… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-221](../SPEC.md#close-errores-221) | Conservar y comprobar los flujos de feedback legado, transporte, asociación, reportes y UI contratados. | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-errores-222](../SPEC.md#close-errores-222) | Conservar y comprobar los flujos de feedback moderno, categoría, asociación, retención, lectura e interfaz con… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-097](../SPEC.md#close-operacion-097) | Navegador real a 320px: flujos aprobados, teclado, formularios, estados de carga/vacío/error/permisos, filtros… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-098](../SPEC.md#close-operacion-098) | Navegador real a 768px: flujos aprobados, teclado, formularios, estados de carga/vacío/error/permisos, filtros… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-099](../SPEC.md#close-operacion-099) | Navegador real a 1440px: flujos aprobados, teclado, formularios, estados de carga/vacío/error/permisos, filtro… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-lectores-004](../SPEC.md#close-lectores-004) | Grafana externo 13.0.2 con Sentry datasource 2.2.8: conexión por token con alcance autorizado. Recibo real del… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-lectores-005](../SPEC.md#close-lectores-005) | Grafana externo 13.0.2 con Sentry datasource 2.2.8: consulta de issues. Recibo real del consumidor, revisión/v… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-lectores-006](../SPEC.md#close-lectores-006) | Grafana externo 13.0.2 con Sentry datasource 2.2.8: serie temporal de errores. Recibo real del consumidor, rev… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-lectores-007](../SPEC.md#close-lectores-007) | Grafana externo 13.0.2 con Sentry datasource 2.2.8: revocación del token impide acceso posterior. Recibo real… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-060](../SPEC.md#close-mcp-060) | Cliente oficial Go modelcontextprotocol/go-sdk v1.8.0 contra endpoint real por Streamable HTTP: descubrir e in… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-061](../SPEC.md#close-mcp-061) | Cliente oficial Go modelcontextprotocol/go-sdk v1.8.0 contra endpoint real por Streamable HTTP: descubrir e in… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-062](../SPEC.md#close-mcp-062) | Cliente oficial Go modelcontextprotocol/go-sdk v1.8.0 contra endpoint real por Streamable HTTP: descubrir e in… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-063](../SPEC.md#close-mcp-063) | Cliente oficial Go modelcontextprotocol/go-sdk v1.8.0 contra endpoint real por Streamable HTTP: descubrir e in… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-064](../SPEC.md#close-mcp-064) | Cliente oficial Go modelcontextprotocol/go-sdk v1.8.0 contra endpoint real por Streamable HTTP: descubrir e in… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-065](../SPEC.md#close-mcp-065) | Cliente oficial Go modelcontextprotocol/go-sdk v1.8.0 contra endpoint real por Streamable HTTP: descubrir e in… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-066](../SPEC.md#close-mcp-066) | Cliente oficial Go modelcontextprotocol/go-sdk v1.8.0 contra endpoint real por Streamable HTTP: descubrir e in… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-067](../SPEC.md#close-mcp-067) | Cliente oficial Go modelcontextprotocol/go-sdk v1.8.0 contra endpoint real por Streamable HTTP: descubrir e in… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-068](../SPEC.md#close-mcp-068) | Cliente oficial Go modelcontextprotocol/go-sdk v1.8.0 contra endpoint real por Streamable HTTP: descubrir e in… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-069](../SPEC.md#close-mcp-069) | Cliente oficial Go modelcontextprotocol/go-sdk v1.8.0 contra endpoint real por Streamable HTTP: descubrir e in… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-070](../SPEC.md#close-mcp-070) | Cliente oficial Go modelcontextprotocol/go-sdk v1.8.0 contra endpoint real por Streamable HTTP: descubrir e in… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-071](../SPEC.md#close-mcp-071) | Cliente oficial Go modelcontextprotocol/go-sdk v1.8.0 contra endpoint real por Streamable HTTP: descubrir e in… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-072](../SPEC.md#close-mcp-072) | Cliente oficial Go modelcontextprotocol/go-sdk v1.8.0 contra endpoint real por Streamable HTTP: descubrir e in… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-073](../SPEC.md#close-mcp-073) | Cliente oficial Go modelcontextprotocol/go-sdk v1.8.0 contra endpoint real por Streamable HTTP: descubrir e in… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-074](../SPEC.md#close-mcp-074) | Cliente oficial Go modelcontextprotocol/go-sdk v1.8.0 contra endpoint real por Streamable HTTP: descubrir e in… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-075](../SPEC.md#close-mcp-075) | Cliente oficial Go modelcontextprotocol/go-sdk v1.8.0 contra endpoint real por Streamable HTTP: descubrir e in… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-076](../SPEC.md#close-mcp-076) | Cliente oficial Go modelcontextprotocol/go-sdk v1.8.0 contra endpoint real por Streamable HTTP: descubrir e in… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-mcp-077](../SPEC.md#close-mcp-077) | Cliente oficial Go v1.8.0: autorización y ciclo OAuth/token completos del contrato vigente, incluyendo scopes,… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-operacion-100](../SPEC.md#close-operacion-100) | Auditoría por requisito sobre revisión candidata identificada e imagen vinculada, con todos los checks, invali… | por contrastar | pendiente | Contrastar contrato, dependencias y comprobantes |
| [CLOSE-sourcemaps-032](../SPEC.md#close-sourcemaps-032) | Enriquecimiento automático con fuentes Java; no exige SDK/JDK, bundles ni aceptación Java básica por inferenci… | por contrastar | aplazada por decisión expresa | Conservar límites y controles; ver decisión |
| [CLOSE-sourcemaps-033](../SPEC.md#close-sourcemaps-033) | Conexión automática con repositorios y descubrimiento remoto de commits. Conservar releases, commits locales y… | por contrastar | aplazada por decisión expresa | Conservar límites y controles; ver decisión |
| [CLOSE-identidad-111](../SPEC.md#close-identidad-111) | WebAuthn como segundo factor. Conservar TOTP, recuperación y require2FA. | por contrastar | aplazada por decisión expresa | Conservar límites y controles; ver decisión |
| [CLOSE-errores-223](../SPEC.md#close-errores-223) | Muestreo global de eventos por organización. Conservar muestreo por proyecto y cuotas por clave. | por contrastar | aplazada por decisión expresa | Conservar límites y controles; ver decisión |
| [CLOSE-lectores-008](../SPEC.md#close-lectores-008) | Comprobante real de GitLab Error Tracking. Conservar soporte y regresiones, sin levantar instancia ni exigir v… | por contrastar | aplazada por decisión expresa | Conservar límites y controles; ver decisión |
| [CLOSE-alertas-040](../SPEC.md#close-alertas-040) | Recibos reales de canales específicos de alertas. Conservar proveedores, controles y pruebas locales; webhook… | por contrastar | aplazada por decisión expresa | Conservar límites y controles; ver decisión |
| [CLOSE-identidad-112](../SPEC.md#close-identidad-112) | Recibos reales del proveedor de correo. Conservar SMTP local, recuperación, invitaciones y controles contratad… | por contrastar | aplazada por decisión expresa | Conservar límites y controles; ver decisión |
| [CLOSE-identidad-113](../SPEC.md#close-identidad-113) | Recibos reales de inicio de sesión externo. Conservar OAuth/OIDC, proveedores sociales, capacidades y controle… | por contrastar | aplazada por decisión expresa | Conservar límites y controles; ver decisión |

### Ejecución 2026-10-25: arquitectura, durabilidad y ciclo de vida

Selección registrada antes de trabajar: candidata
`54db6e48bf2e52e9a84efe397e3e5b44c27e4d91`, sin criterio reclamado por otro
agente y con `tests/compat/otlp_python_smoke.py` ajeno sin seguimiento. Se
eligieron en orden del plan CLOSE-operacion-004, CLOSE-operacion-005 y
CLOSE-operacion-006. Los tres dependen solo de las reglas de arquitectura y
del PostgreSQL local; no dependen de SDKs, navegador, imagen externa ni
proveedores. Esta unidad no ejecutó SDKs, Chromium, Compose, Docker image, S3
ni recibos externos.

Entorno: Go 1.27.2 linux/amd64; PostgreSQL 18 en el contenedor desechable
`goodsentry-closure-20261025` (`127.0.0.1:18085`). Sin secretos de producto.
La revisión probada es `54db6e4`; el commit de este comprobante solo añade el
test acotado descrito abajo y documentación/evidencia, sin cambio de
comportamiento, esquema, configuración, clientes ni imagen.

#### CLOSE-operacion-004: módulo, ejecutable, rutas, dependencias y datos

Procedimiento reproducible: `find . -name go.mod` fuera de cachés y copias de
trabajo; `go build ./...`; `go vet ./...`; `rg -n 'go:embed' internal`;
inspección de `internal/server/web/index.html`; `rg -n 'pgx/v5' go.mod`; lectura
de `internal/store/migrations/001_initial.sql` y del uso de transacciones en
`internal/store`.

Resultado esperado: un módulo de servidor, un ejecutable pequeño, rutas
`net/http`, construcción explícita, UI y migraciones embebidas, `pgx`,
transacciones y esquema relacional con datos estructurados en JSONB.
Observado: el servidor es un único módulo en la raíz y `cmd/goodsentry` es su
ejecutable. `tests/compat` y `examples/sentry-go` son módulos separados de
clientes de prueba/ejemplo, fuera del binario y la imagen, como documenta
`docs/development.md`. `server.go` embebe `web/index.html`, `web/app.css` y
`web/*.js`; `store.go` embebe `migrations/*.sql`. `index.html` carga solo
`app.css` y `app.js` locales, sin CDN ni servicio de build en ejecución. El
módulo usa `github.com/jackc/pgx/v5 v5.11.0`; las operaciones de captura,
credenciales, alertas, monitores y gestión abren transacciones. `001_initial`
define `users`, `organizations`, `memberships`, `projects`, `project_keys`,
`issues` y `events`, con `UNIQUE(project_id,fingerprint)`,
`UNIQUE(project_id,event_id)` y columnas `jsonb` para el evento original.
`go build` y `go vet` pasan. Subresultados pendientes: ninguno del bloque;
los módulos auxiliares no son parte del servidor ni de la imagen.

#### CLOSE-operacion-005: captura durable, idempotencia y trabajo reclamable

Procedimiento reproducible:
`DATABASE_URL=<desechable> go test -race -tags=integration ./internal/store -run '^(TestDurableCaptureAndTenantScope|TestAlertClaimsRetryBackoffAndTokenFencing|TestAlertExpiredClaimsRecoverOrStopAtAttemptBudget|TestErrorAlertsUseReceiptCrossingCooldownAndEnvironment|TestMonitorClaimsFenceUpdatesPauseAndExpiredLeases|TestIssueGroupsConcurrentCapturesTriageAndSplit)$' -v`.
Resultado esperado: captura confirmada solo tras commit, supervivencia a
reinicio, identidad de reintento, agrupación concurrente atómica, alertas en la
misma transacción y recuperación de leases vencidos. Observado: los seis tests
pasaron con `-race` en 5.732s. `TestDurableCaptureAndTenantScope` comprueba ocho
capturas concurrentes del mismo evento con un solo issue y `Count=1`, cierra y
reabre la base y conserva el issue. `TestErrorAlerts...` comprueba el cruce de
recibos y el encolado de alerta desde la captura. Los tests de cola prueban
`FOR UPDATE`, reintento con backoff y recuperación de leases vencidos; el de
monitores prueba fencing y expiración. `Ingest` abre una transacción, bloquea
recibos e issues, inserta el evento y encola las alertas antes de `Commit`.
`001_initial` y las migraciones posteriores aportan las claves únicas de
proyecto/evento y proyecto/fingerprint; el estado de colas vive en PostgreSQL.
`ValidateRuntimeAvailability` rechaza un `VALKEY_URL` no vacío en vez de fingir
una dependencia de Valkey. Subresultados pendientes: ninguno del bloque.

#### CLOSE-operacion-006: parada, migraciones, readiness y rollback

Procedimiento reproducible: compilar `cmd/goodsentry`; arrancar en modo
`required` sobre la base desechable; consultar `/healthz` y `/readyz`; detener
PostgreSQL y consultar `/readyz`; reiniciarlo; enviar `SIGTERM`; consultar
`pg_stat_activity` y `schema_migrations`. Después, arrancar dos procesos
simultáneos sobre una base vacía `goodsentry_closure_lock` en puertos distintos.
Resultado esperado: parada limpia y cierre de conexiones, migraciones hacia
adelante serializadas, readiness ligada a la base y ausencia de rollback
destructivo automático. Observado: `/healthz` 200 y `/readyz` 200 con la base
arriba; `/readyz` 503 con la base detenida y 200 tras recuperarla; `SIGTERM`
terminó con código 0, cero conexiones restantes y 73 migraciones aplicadas. Los
dos arranques concurrentes quedaron listos en 200, con 73 versiones únicas,
checksums válidos y salida 0. `store.migrate` usa
`pg_advisory_xact_lock(73042189)` y `schema_migrations` con checksum; no existe
ninguna migración `down`/`rollback` y el rollback documentado exige binario
compatible o restauración de backup. Subresultados pendientes: ninguno del
bloque.

#### Reparación de la primera corrida completa

La primera corrida completa `-race` falló únicamente
`TestRetentionWorkersDrainRetryAndStop/releases`: el subtest agotó un plazo fijo
de 5s tras 6.44s bajo la carga de toda la suite. En aislamiento el mismo subtest
pasó en 1.16s. La retención borra en lotes acotados de 250; 601 releases exigen
tres pasadas bajo instrumentación de carrera y una base compartida. Se elevó
únicamente el plazo de drenaje del test a 15s, manteniendo una aserción acotada
y sin cambiar código de producción. La corrida completa final sin caché pasó:
`cmd/goodsentry` 2.580s, `jobs` 27.770s, `server` 207.087s, `store` 368.206s,
sin `DATA RACE`. Historial en
`test-results/closure-20261025/full-race-1-failure.log` y
`jobs-isolated.log`; resultado final en `full-race-final.log`.

#### Vigencia, invalidaciones y siguiente acción

`go build ./...`, `go vet ./...`, `go test ./...`, la integración PostgreSQL y
la suite completa `-race -count=1 -tags=integration ./...` pasan sobre la
candidata y el cambio de test. Los comprobantes de 004, 005 y 006 quedan
vigentes. El cambio de test no invalida evidencia de producción; cualquier
cambio futuro de captura, colas, arranque, esquema o migraciones invalidará el
comprobante afectado y exigirá repetirlo. No se ejecutaron SDKs, navegador,
imagen, Compose, S3 ni proveedores externos; esas pruebas siguen pendientes y
no se infieren de esta unidad. Siguiente acción concreta: CLOSE-operacion-007,
verificar la interfaz real en Chromium a 320, 768 y 1440 con sus estados,
controles y permisos, y continuar después con CLOSE-operacion-008.

### Ejecución 2026-10-26: interfaz real y estados de permiso

Selección registrada antes de trabajar: CLOSE-operacion-007, siguiente en el
orden del plan después de 004-006. Candidata `e6ba2de` (el producto es idéntico
a `54db6e4`; el commit anterior solo añadió test y documentación). No había
criterio reclamado por otro agente. La dependencia de navegador se resolvió con
Playwright 1.55.0 instalado en `/tmp` y el Chromium 1187 ya presente en la caché;
ninguno forma parte del repositorio, de la imagen ni del runtime.

Procedimiento reproducible: compilar `cmd/goodsentry`; arrancar con PostgreSQL
18 desechable en modo `required`; ejecutar
`node tests/compat/browser.mjs` con `GOODSENTRY_TEST_URL` y
`GOODSENTRY_TEST_OUTPUT`; después ejecutar
`node tests/compat/member_permission_browser.mjs` con una cuenta miembro real y
la organización del fixture. Revisar además `internal/server/web/styles.css`,
`app.css`, `Dockerfile`, los estados de carga/vacío/error/permiso en `web/*.js`
y la ausencia de sumideros HTML.

Resultado observado: `browser.mjs` pasó sus 13 comprobaciones en una corrida
real, incluyendo onboarding, proyecto/DSN, error ingerido con stack,
breadcrumbs/tags, filtros de búsqueda y entorno tras recarga, triage y
comentarios persistentes, transacciones, traza, logs tipados, paginación,
source maps, captura minificada y login posterior al cierre de sesión. Las 27
capturas cubren 320, 768 y 1440 sin desbordamiento del documento; no hubo
errores de página. El 400 de un mapa inválido y el `ERR_ABORTED` del logout son
las respuestas esperadas por el propio contrato del script. La inspección
visual de `issues-1440.png`, `onboarding-320.png` y
`member-permission-768.png` confirma sidebar compacto, superficies neutras,
acento teal, tablas legibles y formularios accesibles. La cuenta miembro
autenticada no recibió el enlace de API tokens; la navegación directa mostró
«Permission required», sin formulario ni metadatos, a 320/768/1440 y sin
errores de página. `app.css` declara Tailwind 4.3.3 y está embebido; el
`Dockerfile` compila en una etapa de build y copia solo el binario a
`distroless/static-debian12:nonroot`. Los estados de carga, vacío, error y
permiso existen en las vistas; `rg` no encontró `innerHTML`,
`insertAdjacentHTML`, `outerHTML`, `document.write` ni equivalentes, y los
nodos usan `textContent`/`createTextNode`, incluido el título malicioso
`<img src=x onerror=...>` renderizado como texto en la lista de issues.

Evidencia: `test-results/closure-20261025/browser/{report.json,*.png}`,
`member-permission/{report.json,member-permission-*.png}`,
`browser.log` y `member-permission.log`. Subresultados pendientes del bloque:
ninguno. La comprobación de accesibilidad es automatizada por nombres
accesibles y foco, no una auditoría con lector de pantalla; esa profundidad no
la exige el contrato. No se ejecutaron SDKs, imagen, Compose, S3 ni proveedores
externos; pertenecen a otros criterios.

Siguiente acción: CLOSE-operacion-008. Verificar `docker compose config` y el
arranque limpio local; antes de aceptar la imagen pública, comprobar que el
digest publicado está vinculado a una revisión que contenga esta candidata o
registrar la dependencia de publicación como bloqueo del bloque. Después,
CLOSE-operacion-009 (configuración canónica y alias) tiene dependencias locales
resueltas y puede ejecutarse de forma independiente si la publicación de imagen
no está disponible.

### Ejecución 2026-10-26: despliegue, imagen vinculada y Compose

Selección registrada antes de trabajar: CLOSE-operacion-008, siguiente en el
plan. Candidata `145503d`; producto `54db6e4`. No había criterio reclamado. La
imagen pública desplegada es
`ghcr.io/jerolan/good-sentry@sha256:2fabe51e980380f588a041cf47548977f7b8c1051272a1b7d36ee8de3cae64fa`,
etiquetada con `org.opencontainers.image.revision=c5964e5`. La comparación
`git diff --name-only c5964e5 54db6e4` contiene solo `SPEC.md`,
`tasks/plan.md` y `tasks/todo.md`; los commits posteriores solo añaden un plazo
de test, evidencia y `tests/compat/member_permission_browser.mjs`. Ningún
código de runtime, esquema, configuración, UI embebida o cliente cambió. Por la
regla de reutilización con comparación documentada, la imagen es equivalente al
producto de la candidata. Si la auditoría final exige una etiqueta de revisión
exacta, la imagen debe republicarse desde la candidata o esa comparación debe
aceptarse expresamente.

Procedimiento reproducible: `docker compose config` por defecto y con
`GOODSENTRY_IMAGE`; `docker compose pull goodsentry`; tres proyectos Compose
aislados sin `.env` (imagen pública, build local e imagen construida por
Compose) en puertos 18091-18093; `/healthz`, `/api/v1/setup`, `/api/v1/me`,
reinicio de la aplicación, conteos en PostgreSQL, inspección de imagen y
volúmenes. Un cuarto contenedor de la imagen pública usó un `DATABASE_URL`
externo en una red Docker aislada.

Resultado observado: el Compose por defecto publica el puerto 8000, contiene
solo `goodsentry` y `postgres`, monta `pg-data` y `uploads`, espera el
healthcheck de PostgreSQL y no crea web/worker/cron separados. El `pull`
descargó el digest anterior. Un proyecto limpio sin `.env` con la imagen
pública arrancó en el puerto 18092: `/healthz` 200, `/api/v1/setup`
`{"required":false}`, un owner implícito `owner@localhost`, una organización
`default-organization`, conteos 1/1/1, usuario `65532:65532`, y el reinicio
conservó el mismo usuario y CSRF. La ruta documentada
`GOODSENTRY_IMAGE=goodsentry:closure-compose-build PORT=18093 docker compose up --build -d`
compiló y arrancó correctamente: `/healthz` 200, setup `required:false`,
usuario `65532:65532`, dos servicios. La imagen local
`goodsentry:closure-20261026` mide 56,682,768 bytes, usa entrypoint
`/goodsentry`, no contiene shell, incluye 150 certificados en
`/etc/ssl/certs/ca-certificates.crt`, el aviso de daisyUI y `/data`. El
contenedor público con `DATABASE_URL` externo arrancó con `/healthz` 200 y
setup `required:true`. La aplicación no necesita contenedores separados.

Evidencia: `test-results/closure-20261025/compose/`
(`config-default.json`, `config-local.json`, `pull-public.log`,
`up-public.log`, `up-source-build.log`, `image-build.log`, `setup.json`,
`me.json`, `me-after-restart.json`, `summary.json`). Los secretos CSRF y de
base se eliminaron del JSON; los logs no contienen credenciales. Los proyectos
y las imágenes locales desechables se eliminaron después de la prueba.
Subresultados pendientes del bloque: ninguno, con la salvedad documentada de la
etiqueta de revisión de la imagen.

Siguiente acción: CLOSE-operacion-009, comprobar la configuración canónica,
alias y precedencia contra el loader y sus tests; después CLOSE-operacion-010
(modos abierto/autenticado, sesiones y CSRF), cuyas dependencias de PostgreSQL
están disponibles.

### Ejecución 2026-10-26: configuración canónica, alias y precedencia

Selección registrada antes de trabajar: CLOSE-operacion-009, siguiente en el
plan. Candidata `708fc8f`; producto sin cambios desde `54db6e4`. No había
criterio reclamado. La unidad añade únicamente un test de precedencia del
dominio; el loader ya implementaba el contrato.

Procedimiento reproducible:
`go test ./internal/config -run '^(TestDeploymentConfiguration|TestFeatureAliasesAndRuntimeOptions|TestDomainAliasPrecedence|TestOpenModeGeneratesAndReusesPersistentSecret|TestRequiredAuthNeedsExplicitSecret)$' -v`
y, con PostgreSQL desechable,
`go test -tags=integration ./internal/server -run '^(TestOpenAPIIsOptInAndReflectsRegisteredRoutes|TestAdministrationIsOptInAndOrganizationScoped|TestInstanceAdministrationRequiresFeatureAndProtectsOpenOwner)$' -v`.
Revisión de `internal/config/config.go` para las variables canónicas, `alias`,
`firstSet`, `SERVER_ROLE` y los flags; revisión de `server.go` para las rutas
opt-in.

Resultado observado: pasan los casos de `DATABASE_URL`, `SECRET_KEY`,
`GOODSENTRY_DOMAIN`/`GLITCHTIP_DOMAIN`, `PORT`, `EMAIL_URL`,
`DEFAULT_FROM_EMAIL`, `VALKEY_URL` y flags `GOODSENTRY_ENABLE_*`. El alias de
dominio usa `GOODSENTRY_DOMAIN` cuando está definido y `GLITCHTIP_DOMAIN` si no;
el nuevo test comprueba ambas direcciones y la normalización del prefijo. Los
alias de logs, uptime, MCP y DuckDB/archivo usan el nombre GoodSentry primero;
el test existente prueba la precedencia explícita de MCP y de creación de
organizaciones. `SERVER_ROLE=all_in_one` se acepta y un rol distinto se rechaza
con error. `ENABLE_OPENAPI` y `ENABLE_ADMIN` solo registran sus superficies
cuando están activos; los tests opt-in/disabled pasan. La creación de
organizaciones vale `false` por defecto, acepta `ENABLE_ORGANIZATION_CREATION`
y respeta `GOODSENTRY_ENABLE_ORGANIZATION_CREATION=false` sobre el alias.
Subresultados pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/config/loader-tests.log` y
`surfaces-tests.log`; el test nuevo está en `internal/config/config_test.go`.

Siguiente acción: CLOSE-operacion-010 (modo abierto/autenticado, sesiones,
CSRF y aislamiento), con dependencias locales resueltas.

### Ejecución 2026-10-26: modos abierto y autenticado, sesiones y CSRF

Selección registrada antes de trabajar: CLOSE-operacion-010, siguiente en el
plan. Candidata `0581dd2`; producto sin cambios desde `54db6e4`. No había
criterio reclamado. La unidad añade una aserción de conservación del hash de
contraseña al test de idempotencia del owner implícito.

Procedimiento reproducible: los tests de configuración de
`GOODSENTRY_AUTH`/`SECRET_KEY`; `TestEnsureDefaultOwnerIsIdempotent` con
PostgreSQL real; los tests de autenticación abierta, invitaciones, MCP OAuth,
tokens de cuenta y 2FA sin sesión; `open_auth_browser.mjs` contra una
instalación vacía en modo abierto; reinicio del proceso para comparar el hash
del secreto y el owner; y la corrida `browser.mjs` en modo `required` de
CLOSE-operacion-007, que cubre setup, login, logout y login posterior.

Resultado observado: `GOODSENTRY_AUTH` vale `disabled` por defecto. Con
`SECRET_KEY` ausente se generó un secreto de 64 caracteres hexadecimales en
`UPLOAD_DIR/.goodsentry-secret`, con permisos `0600`, y su SHA-256 no cambió
tras reiniciar. El modo `required` sin secreto falla; con un secreto explícito
de 32 o más caracteres arranca. `EnsureDefaultOwner` usa el advisory lock
`73042190` y conserva en el segundo arranque el mismo usuario, nombre, hash de
contraseña, organización, membership owner y equipo por defecto. En modo
abierto, Chromium entró sin cookie de sesión, `/api/v1/setup` devolvió
`required:false` y `/api/v1/me` el owner implícito con CSRF estable; una
mutación sin CSRF y otra con origen ajeno devolvieron 403, y la mutación con el
CSRF estable devolvió 201. `POST /api/v1/setup` y `POST /api/v1/login`
devolvieron 409 en modo abierto. Tras reiniciar, el owner siguió siendo
`owner@localhost` con `default-organization` y el mismo secreto. Una invitación
para otro correo devolvió 409 y no creó usuario; la del correo del owner se
aceptó y la identidad sin cookie no cambió. El cliente MCP OAuth usa el owner
implícito. En modo `required`, `/api/v1/me` sin sesión devuelve 401, no se crea
owner implícito y la corrida real de onboarding/login/logout/login pasó. La
documentación de `docs/configuration.md` exige el mismo `SECRET_KEY` en todas
las réplicas y describe ambos modos. Subresultados pendientes del bloque:
ninguno.

Evidencia: `test-results/closure-20261025/open-auth/`
(`tests.log`, `app.log`, `report.json`, `restart.json` y capturas a 1440/390),
más la evidencia de auth del criterio 007. Los CSRF y secretos no se
guardaron.

Siguiente acción: CLOSE-operacion-011 (Valkey vacío, disponibilidad real de
features, correo local/SMTP, uploads y rechazo de secretos de ejemplo), con
dependencias locales resueltas.

### Ejecución 2026-10-26: features, correo, uploads y secretos

Selección registrada antes de trabajar: CLOSE-operacion-011, siguiente en el
plan. Candidata `9f6edcd`; producto sin cambios desde `54db6e4`. No había
criterio reclamado. La unidad añade un caso de secreto placeholder largo y
corrige una afirmación documental sobre el contenido del volumen de uploads.

Procedimiento reproducible:
`go test ./internal/config -run '^(TestDeploymentConfiguration|TestFeatureAliasesAndRuntimeOptions|TestOpenModeGeneratesAndReusesPersistentSecret)$' -v`
y
`go test ./internal/notify -run '^(TestValidateMail|TestConsoleMailMIMEAndConcurrentWrites|TestSMTPDeliveryAndResponses|TestSMTPRequiredTLSAndCredentialProtection)$' -v`;
revisión de `ValidateRuntimeAvailability`, `loadSecret`, `parseMail`, el mapa
`features` de `/api/v1/me`, `UPLOAD_DIR`, Compose y la documentación local.

Resultado observado: `VALKEY_URL` vacío deja el servicio en PostgreSQL-only y
un valor no vacío se rechaza en vez de fingir soporte. `/api/v1/me` reporta
las features reales (`logs`, `uptime`, `alerts`, `alertDelivery`, `mcp`,
`openapi`, `admin`, `oidc`, recuperación de contraseña y modo requerido), no
una lista estática. `EMAIL_URL` acepta `consolemail://`, `smtp`, `smtp+starttls`
y `smtps`; los tests usan receptores reales de loopback, comprueban TLS
obligatorio y que las credenciales no aparecen en errores, comandos ni logs.
`UPLOAD_DIR` es configurable y Compose monta el volumen `uploads` en `/data`;
ahí persisten el archivo Parquet local y el secreto generado en modo abierto.
Los attachments y source maps se guardan en PostgreSQL; `docs/getting-started.md`
afirmaba lo contrario y se corrigió. Un `SECRET_KEY` largo que contiene
`change_me` se rechaza como placeholder, igual que uno corto. Los defaults de
desarrollo local están separados en `docs/development.md` y
`docs/configuration.md`. Subresultados pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/operations/tests.log` y el diff de
`docs/getting-started.md`.

Siguiente acción: CLOSE-operacion-012 (RSS bajo una carga explícita y
publicación del resultado), con dependencias locales resueltas.

### Ejecución 2026-10-26: RSS bajo carga explícita

Selección registrada antes de trabajar: CLOSE-operacion-012, siguiente en el
plan. Candidata `de8130d`; producto sin cambios desde `54db6e4`. No había
criterio reclamado. La unidad añade `tests/compat/rss_workload.py`, un generador
reproducible que usa solo el límite público de envelopes, y publica la medición.

Procedimiento reproducible: arrancar el binario de la candidata en modo abierto
con PostgreSQL desechable; crear un proyecto por la API; ejecutar
`GOODSENTRY_LOAD_DSN=<dsn> python3 tests/compat/rss_workload.py`; leer
`VmRSS`/`VmHWM` de `/proc/<pid>/status` antes y después. El workload envía
1,600 errores, 4,000 logs tipados y 100 transacciones con cinco spans cada una,
desde 16 workers concurrentes: 2,100 peticiones.

Resultado observado: 2,100/2,100 peticiones aceptadas con estado 200, cero
fallos, 12.85s y 163.4 peticiones/s. VmRSS pasó de 29,444 KB (28.75 MiB) a
35,300 KB (34.47 MiB); VmHWM fue 35,300 KB (34.47 MiB). La memoria de
PostgreSQL queda excluida. La base retuvo 1,700 eventos (1,600 errores y 100
transacciones), 4,000 logs y 1,600 issues; la proyección de spans es
asíncrona y no forma parte de la afirmación de memoria. El resultado se publicó
en `docs/development.md`, que mantiene la distinción entre esta carga de
ingestión y el objetivo operativo de 256-512 MB. Subresultados pendientes del
bloque: ninguno.

Evidencia: `test-results/closure-20261025/rss/`
(`summary.json`, `workload.json`, `app.log`, `workload-sha256.txt`).

Siguiente acción: CLOSE-identidad-001 (health/readiness, setup, sesiones,
hash de contraseña, cookies, CSRF, bearer tokens y claves SDK), con
dependencias locales resueltas.

### Ejecución 2026-10-26: salud, cuentas, sesiones y credenciales

Selección registrada antes de trabajar: CLOSE-identidad-001, siguiente en el
orden del plan. Candidata `7a95724`; producto sin cambios desde `54db6e4`. No
había criterio reclamado. La unidad no cambia producto; contrasta los
comprobantes existentes y ejecuta los owners de credenciales y recuperación.

Procedimiento reproducible: reutilizar `/healthz` y `/readyz` del criterio 006;
el setup/login/logout real de `browser.mjs` en modo `required` del criterio 007;
`TestPasswordResetEnqueueDeliveryConsumptionAndRevocation`,
`TestAPITokenAdministrationAndAtomicProjectBoundary`,
`TestAPITokenHTTPContract` y `TestReleaseHTTPContract` con `-race` y PostgreSQL
desechable; revisión de `auth.go`, `credentials.go`, el middleware de sesión,
los tokens de proyecto y el esquema.

Resultado observado: `/healthz` responde 200 con el proceso vivo y `/readyz`
200 con PostgreSQL, 503 cuando la base se detiene y 200 al recuperarse. El
setup crea el primer owner y la organización sin contraseña compartida; las
contraseñas de setup, login, invitación, recuperación y cambio usan bcrypt
`DefaultCost`. Una sesión es un valor aleatorio de 32 bytes en hexadecimal,
se guarda como HMAC-SHA256 con `SECRET_KEY`, expira a los siete días y usa
cookie `HttpOnly`, `SameSite=Lax` y `Secure` cuando el dominio es HTTPS. Las
mutaciones de navegador exigen mismo origen y `X-CSRF-Token`. Los bearer de
gestión se guardan como HMAC-SHA256, tienen prefijo identificable, expiración,
revocación y alcance por organización/proyecto; un bearer no puede leer ni
emitir credenciales nativas y un token de gestión no sirve como clave SDK. Las
claves SDK solo autentican ingestión y no conceden lecturas ni mutaciones de
gestión. La recuperación de contraseña encola, consume una vez y revoca las
sesiones existentes. Subresultados pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/identity/tests.log`, más los
comprobantes de 006 (salud/readiness), 007 (setup/login/logout), 010 (CSRF) y
las capturas del login requerido. No se guardaron contraseñas, sesiones ni
CSRF.

Siguiente acción: CLOSE-identidad-002 (memberships, equipos, proyectos,
autorización por alcance, bootstrap cerrado e IDs opacos), con dependencias
locales resueltas.

### Ejecución 2026-10-26: organizaciones, equipos y alcance

Selección registrada antes de trabajar: CLOSE-identidad-002, siguiente en el
plan. Candidata `a4961ca`; producto sin cambios desde `54db6e4`. No había
criterio reclamado. La unidad no cambia producto; ejecuta los owners de
memberships, equipos, proyectos y aislamiento.

Procedimiento reproducible con PostgreSQL desechable y `-race`:
`TestPeopleMembershipsTeamsAndLastOwner`,
`TestConcurrentOwnerDemotionsRetainOneOwner`,
`TestDurableCaptureAndTenantScope`,
`TestCompatibleProjectTeamAssignmentMultipleRelations` y
`TestPeopleAndTeamHTTPContract`; revisión del esquema `organizations`,
`memberships`, `teams`, `project_teams`, `projects` y de las consultas que
unen la identidad actual con la organización/proyecto.

Resultado observado: las organizaciones contienen memberships y equipos; los
proyectos pertenecen a una organización y aceptan múltiples equipos con
attach/detach idempotente y 404 para equipos ausentes o fuera de alcance. Una
identidad accede a otra organización solo mediante su membership; el dueño de
una organización no obtiene acceso a un proyecto ajeno y la captura/lectura
cruzada devuelve 404. `Bootstrap` acepta un solo primer owner y las llamadas
posteriores devuelven conflicto, incluso concurrentes; el setup HTTP posterior
al primer owner también falla. Las bajas concurrentes de owners conservan al
menos uno. Owners/admins gestionan memberships, equipos, proyectos,
credenciales y alertas, mientras un member conserva el flujo de issues; los
tests HTTP cubren los 403 correspondientes. Los IDs de usuario, organización,
equipo y proyecto se exponen como cadenas opacas, con `id::text` en las
proyecciones y sin filtrar claves internas. Subresultados pendientes del
bloque: ninguno.

Evidencia: `test-results/closure-20261025/identity/tenancy.log` y el esquema
descrito en el comprobante 004.

Siguiente acción: CLOSE-identidad-003 (credenciales de automatización nativas,
alcance por organización/proyecto, HMAC y ciclo de revocación), con
dependencias locales resueltas.

### Ejecución 2026-10-26: credenciales de automatización

Selección registrada antes de trabajar: CLOSE-identidad-003, siguiente en el
plan. Candidata `d93b926`; producto sin cambios desde `54db6e4`. No había
criterio reclamado. La unidad no cambia producto; ejecuta los owners de
credenciales nativas y su consumo compatible.

Procedimiento reproducible con PostgreSQL desechable y `-race`:
`TestAPITokenHTTPContract`,
`TestAPITokenAdministrationAndAtomicProjectBoundary`,
`TestAPITokenPersistsDigestAndGrantAcrossReopen` y `TestReleaseHTTPContract`;
revisión de `internal/server/compat_api_tokens.go`,
`internal/store/api_tokens.go` y las rutas nativas.

Resultado observado: solo una sesión con CSRF crea, lista o revoca; un bearer
no puede emitir ni consumir la API nativa. El token tiene la forma
`gs_` más 64 hexadecimales, se devuelve una sola vez con prefijo de 11
caracteres, se guarda como HMAC-SHA256 y conserva el grant tras reabrir. Los
metadatos incluyen id, nombre, prefijo, scopes, `allProjects`, proyectos,
creador, fechas y revocación; la lista pagina 25 por página y no expone hash
ni token. Los defaults son `org:read` y `project:read`, con 90 días; se
aceptan `projectIds` y `project:releases`, y se rechazan scopes vacíos,
duplicados o desconocidos, proyectos ausentes/ajenos y cuerpos malformados.
La revocación devuelve 204, es idempotente y un token revocado/expirado deja
de autorizar; un proyecto ajeno devuelve 404. El consumo compatible lista y
detalla solo proyectos autorizados sin DSN ni clave SDK, y `project:releases`
queda limitado a sus consumidores implementados. Subresultados pendientes del
bloque: ninguno.

Evidencia: `test-results/closure-20261025/identity/{tests.log,tokens.log}`.

Siguiente acción: CLOSE-identidad-004 (semántica all-projects versus lista,
revocación por pérdida de membership, revalidación y no filtración), con
dependencias locales resueltas.

### Ejecución 2026-10-26: grants, invalidación y revalidación

Selección registrada antes de trabajar: CLOSE-identidad-004, siguiente en el
plan. Candidata `c8781f2`; producto sin cambios desde `54db6e4`. No había
criterio reclamado. La unidad ejecuta los owners de invalidez y grants.

Procedimiento reproducible con PostgreSQL desechable y `-race`:
`TestAPITokenInvalidationUsesCurrentDatabaseState` (expirado, revocado y
creador eliminado), `TestAPITokenAllAndRestrictedProjectGrants` y
`TestAPITokenMetadataHistoryPaging`; revisión de `AuthenticateAPIToken` y
`APITokens` para la revalidación de membership y proyectos.

Resultado observado: una lista vacía/omitida concede todos los proyectos
actuales y futuros y se persiste como `allProjects`; una lista no vacía solo
concede los proyectos existentes seleccionados, rechaza ausentes/ajenos de
forma atómica y no se convierte en all-projects al desaparecer el último
proyecto. La eliminación del membership del creador elimina el credential y
una reincorporación no lo revive; reducir el rol conserva el grant de lectura
ordinario pero bloquea la gestión. El proveedor vuelve a comprobar membership
y alcance de proyecto en cada autenticación. Expiración y revocación impiden
las peticiones posteriores; no abortan una ya autorizada. La historia conserva
activos, expirados y revocados en páginas de 25, los nombres duplicados se
distinguen por ID y las respuestas/logs no exponen hash ni token original.
Subresultados pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/identity/grants.log`.

Siguiente acción: CLOSE-identidad-005 (rutas compatibles de proyectos,
precedencia de bearer, cursores y Links), con dependencias locales resueltas.

### Ejecución 2026-10-26: proyectos compatibles y paginación

Selección registrada antes de trabajar: CLOSE-identidad-005, siguiente en el
plan. Candidata `0d8f73c`; producto sin cambios desde `54db6e4`. No había
criterio reclamado. La evidencia proviene de `TestAPITokenHTTPContract` con
`-race` y PostgreSQL desechable, ya ejecutado en esta sesión.

Resultado observado: `GET /api/0/organizations/{org}/projects/` exige
`org:read` y `GET /api/0/projects/{org}/{project}/` exige `project:read`; las
sesiones de navegador pueden leer las mismas rutas. Solo esas rutas
registradas aceptan bearer, un bearer inválido no cae a la cookie y las rutas
no registradas devuelven 404. Organización y proyecto aceptan slug exacto o ID
numérico canónico; otro tenant o un proyecto fuera de alcance devuelve 404 y
un scope ausente 403. La lista devuelve `id`, `name`, `slug`, `platform` y
`dateCreated`, sin DSN ni claves. `query`, `limit`/`per_page` (1..100,
coincidentes si ambos), cursor base64url canónico y Link
previous/next conservan origen, prefijo, query y tamaño; los cursores
malformados o fuera del máximo devuelven 400. Subresultados pendientes del
bloque: ninguno.

Evidencia: `test-results/closure-20261025/identity/tests.log`, que contiene
la corrida de `TestAPITokenHTTPContract` (1.71s) y `TestReleaseHTTPContract`.

Siguiente acción: CLOSE-identidad-006 (vista nativa de API tokens, creación,
revelado único, revocación y permisos), con dependencias locales resueltas.

### Ejecución 2026-10-26: consultas malformadas y cursor máximo

Selección registrada antes de trabajar: CLOSE-identidad-007, siguiente en el
plan. Candidata `24e73c5`; producto sin cambios desde `54db6e4`. No había
criterio reclamado. La evidencia proviene de `TestAPITokenHTTPContract` con
`-race`, ya ejecutado en esta sesión.

Resultado observado: `limit`, `per_page`, `cursor` y `query` con percent
encoding inválido devuelven 400 en vez de caer a un valor omitido; también son
400 los cursores base64url no canónicos, `offset` negativo/fuera de rango y
los parámetros repetidos o contradictorios. En `offset:100000000` la página
sigue siendo válida, el Link `next` discapacitado conserva el cursor máximo
canónico y reporta `results=false`; seguirlo devuelve una página vacía válida
sin desbordar el offset. Subresultados pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/identity/tests.log`.

Siguiente acción: CLOSE-identidad-006 (vista nativa de API tokens) continúa
pendiente de la corrida de navegador con fixture owner/member; después,
CLOSE-identidad-008 y la tabla de generaciones de credenciales.

### Ejecución 2026-10-26: vista nativa de API tokens

Selección registrada antes de trabajar: CLOSE-identidad-006, siguiente en el
plan. Candidata `2eee5e8`; producto sin cambios desde `54db6e4`. No había
criterio reclamado. La unidad ejecuta el contrato completo de navegador con un
owner y un member reales; no cambia producción.

Procedimiento reproducible: app en modo `required` sobre PostgreSQL desechable;
owner creado por `/api/v1/setup`; member insertado como fixture; ejecutar
`node tests/compat/browser-api-tokens.mjs` con `GOODSENTRY_TEST_EMAIL/PASSWORD`
y `COMPAT_MEMBER_EMAIL/PASSWORD` y Playwright 1.55.0.

Resultado observado: el owner ve la navegación y la tabla; las páginas de 25
metadatos, la recarga, las etiquetas `all current and future projects`, el
estado activo y un nombre con HTML malicioso se renderizan como texto. Un 400
conserva nombre, vida, scopes y proyecto seleccionado. La creación con
`project:read` y proyecto restringido revela el token una sola vez, el botón
Copy lo escribe en el portapapeles y el valor consume un GET compatible real.
El secreto desaparece al salir/volver y tras descartar/refrescar; la revocación
se ve como `Revoked` y el token deja de autorizar. Un member real no recibe el
enlace, la tabla, el formulario ni los metadatos, y la navegación directa
muestra «Permission required»; su GET directo devuelve 403. Las capturas
cubren 320/768/1440 sin desbordamiento. Los ajustes son solo del arnés:
permisos de portapapeles, espera de la notificación de copia y tolerancia al
`ERR_ABORTED` del navegador tras el 204 de revocación, que el servidor ya
confirmó. Subresultados pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/token-ui/`
(`summary.json`, `run.log`, `app.log` y 21 capturas).

Siguiente acción: CLOSE-identidad-008 (tabla de generaciones de credenciales y
cambios autenticados de contraseña), con dependencias locales resueltas.

### Ejecución 2026-10-26: generaciones y cambio de contraseña

Selección registrada antes de trabajar: CLOSE-identidad-008, -009 y -010, en
orden del plan. Candidata `758d644`; producto sin cambios desde `54db6e4`. No
había criterio reclamado. La unidad ejecuta los owners de generación y el flujo
real de contraseña para owner y member.

Procedimiento reproducible con PostgreSQL 18 desechable y `-race`:
`TestAuthGenerationMigration035PreservesCredentialsAndReopens`,
`TestCredentialIssuanceRejectsStaleGenerationAtomically`,
`TestPasswordChangeRevokesEveryOwnedCredentialAtomically`,
`TestConcurrentPasswordChangesHaveOneWinner`,
`TestPasswordChangeGenerationOverflowRollsBackCredentials` y
`TestCredentialIssuanceWaitsAndRechecksUnderRepeatableReadDefault`; después,
`node tests/compat/account_browser.mjs` con una cuenta owner y una member
reales, cada una con otra sesión abierta.

Resultado observado: 035 se aplica y reabre preservando hashes, IDs, sesiones,
tokens, grants, memberships y recibos. La emisión de sesión y de token nativo
exige la generación observada; una generación obsoleta o ausente no inserta
credencial. El cambio de contraseña usa Read Committed explícito, bloquea al
usuario `FOR UPDATE`, reemplaza el hash, incrementa la generación una vez,
elimina todas sus sesiones y revoca sus tokens personales en todas las
organizaciones, incluyendo expirados no revocados, sin tocar los ya revocados
ni credenciales/claves de otros usuarios. Dos cambios concurrentes tienen un
solo ganador; el desbordamiento de bigint revierte todo. El pool con default
Repeatable Read se anula explícitamente y ambos tipos de emisión revalidan tras
la espera. En Chromium, owner y member cambian su propia contraseña sin correo:
la confirmación distinta conserva el formulario, el 204 limpia el estado y
muestra el login, la otra sesión devuelve 401, la contraseña anterior 401 y la
nueva 200; las capturas cubren 320/768/1440 sin desbordamiento. La ruta no
tiene puerta por rol y el fixture admin del owner store demuestra que sus
credenciales ajenas se conservan. Recuperación e invitaciones son handlers
separados y permanecen en 011–013.

Los resultados de fila 008, 009 y 010 quedan vigentes. Los bloques detallados
014–026 siguen pendientes y se ejecutarán como criterios propios; esta unidad
no los declara cerrados.

Evidencia: `test-results/closure-20261025/account/`
(`summary.json`, `generations.log`) y las capturas/reportes de
`account-owner/` y `account-member/`. No se guardaron contraseñas ni hashes.

Siguiente acción: CLOSE-identidad-011 (correo de cuenta purpose-specific,
outbox y entrega SMTP real), con dependencias locales resueltas.

### Ejecución 2026-10-26: correo de cuenta y recuperación

Selección registrada antes de trabajar: CLOSE-identidad-011 y -012, en orden
del plan. Candidata `34bbd91`; producto sin cambios desde `54db6e4`. No había
criterio reclamado. La unidad ejecuta MIME, SMTP, outbox, reintentos y el
contrato HTTP de recuperación.

Procedimiento reproducible: `go test -race ./internal/notify` para MIME,
consolemail y SMTP; `go test -race -tags=integration ./internal/store` para
036, enqueue/consumo/revocación y retry/expiry/lease; y
`TestPasswordRecoveryHTTPContract` con PostgreSQL desechable.

Resultado observado: el correo de cuenta es independiente del de alertas,
rechaza entradas inseguras y entrega por consolemail y SMTP real de loopback con
resultados de reintento y protección de credenciales/TLS. 036 preserva filas y
reabre. La solicitud devuelve 202 tanto para correo desconocido como conocido,
sin crear fila para el desconocido; el piso de 60s conserva una sola entrega.
El outbox pasa a `sent`, el enlace contiene un token de 64 hexadecimales y la
redención es de un solo uso: 204, sesión anterior 401, contraseña anterior 401,
nueva 200 y replay 400. La recuperación no revela la existencia de la cuenta.
Los bloques detallados 027–033 siguen pendientes y se ejecutarán como criterios
propios. La excepción de recibos reales del proveedor de correo
(CLOSE-identidad-112) se conserva: se prueba SMTP local, no un proveedor externo.

Evidencia: `test-results/closure-20261025/account-mail/`
(`summary.json`, `tests.log`).

Siguiente acción: CLOSE-identidad-013 (invitaciones de membership, roles,
último owner y aceptación de identidades nuevas/existentes), con dependencias
locales resueltas.

### Ejecución 2026-10-26: invitaciones y roles

Selección registrada antes de trabajar: CLOSE-identidad-013, siguiente en el
plan. Candidata `1957658`; producto sin cambios desde `54db6e4`. No había
criterio reclamado. La unidad ejecuta migraciones, roles, entrega, aceptación,
replay, cancelación y equipos de invitación.

Procedimiento reproducible con PostgreSQL 18 desechable y `-race`:
`TestInvitationMigration037PreservesPriorRowsAndReopens`,
`TestManagerInvitationTeamsMigrationPreservesStateAndReopens`,
`TestInvitationDeliveryAcceptanceReplayAndCancellation`,
`TestCompatibleInvitationAcceptsExistingAndVerifiedSecondaryAccounts`,
`TestInvitationTeamAssignmentsFollowTeamIDsThroughAcceptance`,
`TestOrganizationInvitationHTTPContract`,
`TestCompatibleInvitationHTTPAcceptance` y
`TestCompatibleOrganizationInvitationMutationHTTP`.

Resultado observado: 037 y la migración de equipos de manager preservan filas
y reabren. El contrato HTTP cubre owner/admin, reglas de rol, último owner,
equipos y mutaciones; la entrega/aceptación/cancelación y el replay se
comportan según el contrato. Una identidad existente o un correo secundario
verificado acepta sin crear una cuenta duplicada, y las asignaciones de equipo
siguen los IDs a través de la aceptación. Los recibos reales de proveedor de
correo quedan aplazados por la excepción 112; se usan los transportes locales.
Los bloques detallados 045–049 siguen pendientes y no se declaran cerrados.

Evidencia: `test-results/closure-20261025/invitations/`
(`summary.json`, `tests.log`).

Siguiente acción: CLOSE-identidad-014 (migración 035, campo de generación,
preservación y exclusión de JSON), iniciando los bloques detallados de
credenciales.

### Ejecución 2026-10-26: migración 035 y generación interna

Selección registrada antes de trabajar: CLOSE-identidad-014, siguiente en el
plan. Candidata `c2bea15`; producto sin cambios desde `54db6e4`. La evidencia
proviene de la corrida `generations.log` ya registrada.

Resultado observado: `035_auth_generation.sql` añade
`users.auth_generation bigint NOT NULL DEFAULT 1 CHECK (auth_generation > 0)`
sin reescribir credenciales. `TestAuthGenerationMigration035PreservesCredentialsAndReopens`
comprueba la preservación de hashes, IDs, sesiones, tokens, grants,
memberships y recibos, y la reapertura. Bootstrap, `UserByEmail`, `UserByID` y
la admisión de sesión leen `auth_generation`; `User.AuthGeneration` lleva
`json:"-"` junto a `PasswordHash`, de modo que no aparece en JSON. Una
generación cero o ausente no autoriza emisión; los tests de emisión obsoleta
lo demuestran y las tablas de sesión/token no añaden una columna de generación.
Subresultados pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/account/generations.log` y el
esquema `internal/store/migrations/035_auth_generation.sql`.

Siguiente acción: CLOSE-identidad-015 (los dos emisores exigen la generación
observada y no hay overload por user ID), con dependencias locales resueltas.

### Ejecución 2026-10-26: emisores con generación observada

Selección registrada antes de trabajar: CLOSE-identidad-015, siguiente en el
plan. Candidata `46c1f85`; producto sin cambios desde `54db6e4`. La evidencia
proviene de `generations.log` y de la revisión de los call sites.

Resultado observado: `CreateSession(userID, authGeneration, ...)` y
`CreateAPIToken(userID, authGeneration, ...)` exigen la generación; no existe
un overload que emita solo con el ID. Login usa el mismo `User` cuyo hash pasó
bcrypt y pasa su `AuthGeneration`; bootstrap usa la generación devuelta por el
insert; la creación nativa de tokens usa la generación admitida por el
middleware (`current(r).User.AuthGeneration`) sin refrescarla. La recuperación
usa `EnqueuePasswordReset(userID, authGeneration, ...)`. Los tests de emisión
obsoleta y de espera bajo Repeatable Read demuestran que una generación
cambiada no inserta credencial. Subresultados pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/account/generations.log`;
`internal/store/credentials.go`, `internal/store/api_tokens.go`,
`internal/server/auth.go` y `internal/server/api_tokens.go`.

Siguiente acción: CLOSE-identidad-016 (transacción Read Committed, locks
user/membership/proyectos y error dedicado), con dependencias locales
resueltas.

### Ejecución 2026-10-26: transacciones y locks de emisión

Selección registrada antes de trabajar: CLOSE-identidad-016, siguiente en el
plan. Candidata `535d2cc`; producto sin cambios desde `54db6e4`. La evidencia
proviene de `generations.log` y de la inspección de los emisores.

Resultado observado: ambos emisores abren `pgx.ReadCommitted` explícito y
`pinUserGeneration` bloquea la fila del usuario `FOR SHARE`, exige una
generación positiva e igual a la observada y mantiene el lock hasta el commit.
La creación de token sigue con el lock de membership de `tokenAdministrator`,
luego bloquea los proyectos seleccionados ordenados `FOR SHARE` y finalmente
inserta token/grants en el orden usuario, membership, proyectos, credencial.
Una generación ausente o cambiada devuelve el error dedicado
`ErrCredentialsChanged` y no inserta fila. La emisión conserva scopes, rol,
expiración, secreto único y CSRF. Los tests cubren sesión y token bajo el pool
con default Repeatable Read y el límite atómico de proyectos. Subresultados
pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/account/generations.log`;
`internal/store/credentials.go` y `internal/store/api_tokens.go`.

Siguiente acción: CLOSE-identidad-017 (cambio de contraseña: reemplazo,
incremento único, borrado de sesiones y revocación global), con dependencias
locales resueltas.

### Ejecución 2026-10-26: reemplazo y revocación de contraseña

Selección registrada antes de trabajar: CLOSE-identidad-017, siguiente en el
plan. Candidata `dc550b7`; producto sin cambios desde `54db6e4`. La evidencia
proviene de `generations.log` y de la inspección de `ChangePassword`.

Resultado observado: el cambio abre `pgx.ReadCommitted` explícito, bloquea la
fila del usuario `FOR UPDATE` y compara la generación observada. En la misma
transacción reemplaza el hash, incrementa la generación una sola vez, elimina
todas las sesiones, revoca con un único `clock_timestamp()` los tokens
`api_tokens` no revocados del usuario en todas las organizaciones (incluidos
los expirados no revocados) y elimina los tokens personales y resets pendientes.
Los `revoked_at` previos y las filas históricas de `api_tokens` se conservan;
las claves SDK/proyecto, memberships, telemetría y credenciales de otros
usuarios no cambian. El test comprueba sesiones y tokens propios revocados,
otra organización, tokens ya revocados intactos, miembro ajeno intacto,
proyectos/DSN sin cambios y rechazo de una generación obsoleta. El desbordamiento
bigint revierte todo; el commit incierto no se reintenta automáticamente.
Subresultados pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/account/generations.log`;
`internal/store/credentials.go`.

Siguiente acción: CLOSE-identidad-018 (ordenamientos emisor/cambio, dos
cambios, admisión obsoleta y nueva generación), con dependencias locales
resueltas.

### Ejecución 2026-10-26: interleavings emisor/cambio

Selección registrada antes de trabajar: CLOSE-identidad-018, siguiente en el
plan. Candidata `904a89c`; producto sin cambios desde `54db6e4`. La evidencia
proviene de `generations.log` y del flujo real de contraseña.

Resultado observado: cuando la emisión adquiere primero el SHARE, el cambio
espera y luego invalida la credencial confirmada; cuando el cambio gana, la
emisión que esperaba observa la nueva generación y falla sin insertar. El test
de espera cubre sesión y token, incluido el pool con default Repeatable Read.
Dos cambios concurrentes con la misma generación tienen un solo ganador. Una
sesión admitida antes de la revocación conserva el semántica de petición en
vuelo, pero no puede emitir una credencial nueva después del cambio; las
autenticaciones posteriores fallan y el login con la nueva contraseña obtiene
una sesión utilizable (verificado también en el navegador). Subresultados
pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/account/generations.log` y
`account/`.

Siguiente acción: CLOSE-identidad-019 (justificación de Read Committed frente
a Repeatable Read y prueba de interleaving real), con dependencias locales
resueltas.

### Ejecución 2026-10-26: aislamiento y snapshot fresco

Selección registrada antes de trabajar: CLOSE-identidad-019, siguiente en el
plan. Candidata `79d96a2`; producto sin cambios desde `54db6e4`. La evidencia
proviene de `generations.log` y del código de los emisores.

Resultado observado: el test crea un pool cuyo default es Repeatable Read y
demuestra que los emisores lo anulan con `pgx.ReadCommitted`; tras la espera
del lock SHARE, la sentencia siguiente ve la generación confirmada por el
cambio y falla, en vez de conservar un snapshot anterior. El caso cubre sesión
y token. La justificación se apoya en el contrato público de PostgreSQL 18
para Read Committed/Repeatable Read y en los modos de lock de fila; no se usa
un sleep como sincronización ni un hook de producción. Subresultados
pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/account/generations.log`.

Siguiente acción: CLOSE-identidad-020 (ruta nativa de contraseña, cuerpo
estricto y límites en bytes), con dependencias locales resueltas.

### Ejecución 2026-10-26: ruta y cuerpo de contraseña

Selección registrada antes de trabajar: CLOSE-identidad-020, siguiente en el
plan. Candidata `1b3f519`; producto sin cambios desde `54db6e4`. La evidencia
proviene de `TestAccountDSNCaptureAndTriage` con `-race` y PostgreSQL
desechable.

Resultado observado: `POST /api/v1/account/password` se registra bajo el
prefijo y pasa por el middleware nativo de sesión/CSRF/mismo origen; un bearer
o DSN solo no autoriza la ruta y el manejo nativo de cookie no se sustituye por
la precedencia compatible. `bodyJSON` limita a 1 MiB, desactiva campos
desconocidos y exige un único objeto JSON. `currentPassword` se admite de 1 a
72 bytes y `newPassword` de 12 a 72 bytes UTF-8, sin trim; la misma contraseña
es válida y ejecuta la revocación completa. El owner HTTP comprueba 403 sin
CSRF, 400 por contraseña actual incorrecta, 400 por multibyte fuera de límite y
400 por campo desconocido/trailing, y 204 en el camino válido. Subresultados
pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/account/route.log` y
`internal/server/account.go`/`server.go`.

Siguiente acción: CLOSE-identidad-021 (verificación de hash/generación,
errores 400/401/503 y bcrypt fuera de la transacción), con dependencias
locales resueltas.

### Ejecución 2026-10-26: verificación y errores de contraseña

Selección registrada antes de trabajar: CLOSE-identidad-021, siguiente en el
plan. Candidata `aca1ed5`; producto sin cambios desde `54db6e4`. La evidencia
proviene de `route.log`, `generations.log` y la inspección del handler.

Resultado observado: el handler carga hash y generación por el ID admitido;
si la generación difiere devuelve 401 antes de aceptar el hash nuevo. Compara
la contraseña actual con bcrypt y calcula el reemplazo con `DefaultCost` antes
de la transacción de escritura, pasando la generación original al store. Una
entrada malformada o contraseña actual incorrecta devuelve 400 y no cambia
estado; una identidad obsoleta/eliminada devuelve 401 sin cookie; sesión o
CSRF/origen inválidos conservan 401/403; `report` responde 503 genérico sin
filtrar hash, contraseña ni generación. Login y creación nativa traducen
`ErrCredentialsChanged` a 401 genérico, no a un conflicto engañoso. Los tests
de emisión obsoleta y el owner HTTP cubren esos caminos. Subresultados
pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/account/route.log` y
`account/generations.log`.

Siguiente acción: CLOSE-identidad-022 (204 solo tras commit, expiración de
cookie y ausencia de secretos en respuestas), con dependencias locales
resueltas.

### Ejecución 2026-10-26: respuesta y cookie de contraseña

Selección registrada antes de trabajar: CLOSE-identidad-022, siguiente en el
plan. Candidata `00ad525`; producto sin cambios desde `54db6e4`. La evidencia
proviene de `route.log`, el flujo de navegador owner/member y la inspección de
`clearSessionCookie`.

Resultado observado: `ChangePassword` solo retorna 204 después de que el store
confirma el commit; después expira la cookie con el prefijo configurado,
`HttpOnly`, `SameSite=Lax` y `Secure` bajo HTTPS, y el navegador vuelve al
formulario de login sin emitir una credencial de reemplazo. Los campos y el
estado autenticado se limpian; la otra sesión devuelve 401 y la contraseña
nueva permite iniciar sesión. Las respuestas y logs no contienen contraseñas,
hashes ni generaciones; el test HTTP comprueba que la sesión previa ya no
accede y que el login anterior falla. Subresultados pendientes del bloque:
ninguno.

Evidencia: `test-results/closure-20261025/account/route.log` y los reportes
`account-owner/`/`account-member/`.

Siguiente acción: CLOSE-identidad-023 (Account UI, roles sin organización,
estados pending/400/503 y foco), con dependencias locales resueltas.

### Ejecución 2026-10-26: Account UI y acceso sin organización

Selección registrada antes de trabajar: CLOSE-identidad-023, siguiente en el
plan. Candidata `4306be5`; producto sin cambios desde `54db6e4`. La evidencia
proviene de los flujos reales owner/member y del fixture sin organización.

Resultado observado: el enlace Account aparece junto a la identidad y el
logout, usa `view=account` y se renderiza antes de cargar organización o
proyecto; owner, member y una cuenta sin membership acceden al formulario. La
vista muestra nombre/email como texto y campos current/new/confirm con
`autocomplete=current-password`/`new-password`; explica antes de enviar que se
cierran todas las sesiones y se revocan los tokens personales, mientras las
claves SDK siguen funcionando. El submit se deshabilita mientras está pendiente
y se restaura si el formulario sigue montado. La confirmación distinta y el
400 conservan el formulario; el 503/fallo de red muestra que el resultado no se
pudo confirmar y ofrece ir a sign in sin reintentar; el éxito limpia el estado
y muestra el mensaje de login. No se guardan contraseñas en history, storage ni
estado compartido; las capturas cubren 320/768/1024/1440 y no hay errores de página.
Subresultados pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/account-owner/`,
`account-owner-1024/`, `account-member/` y `account-noorg/`.

Siguiente acción: CLOSE-identidad-024 (PostgreSQL real de 035, aislamiento,
bloqueos, dos cambios, rollback y commit diferido), con dependencias locales
resueltas.

### Ejecución 2026-10-26: PostgreSQL real de contraseña

Selección registrada antes de trabajar: CLOSE-identidad-024, siguiente en el
plan. Candidata `082c948`; producto sin cambios desde `54db6e4`. La unidad
añade el owner que faltaba para el fallo en COMMIT diferido y el rollback tras
revocar.

Procedimiento reproducible con PostgreSQL desechable y `-race`: los seis tests
de `generations.log` más el nuevo
`TestPasswordChangeRollbackAfterRevocationAndDeferredCommitFailure`, que instala
un trigger de fallo después de la revocación y otro constraint trigger
diferido al COMMIT.

Resultado observado: 035 preserva y reabre; el aislamiento se fuerza a Read
Committed; las comprobaciones de generación, el borrado de todas las sesiones,
la revocación de tokens en varias organizaciones, los `revoked_at` previos y la
preservación de otro usuario/claves SDK pasan. Las dos ordenaciones emisor/cambio
y dos cambios concurrentes tienen el resultado especificado. El nuevo test
demuestra que un fallo después de borrar sesiones/revocar tokens, y un fallo
diferido en el COMMIT, revierten hash, generación, sesiones y tokens por
completo, sin filas parciales. Una generación nueva puede emitir y consumir
sesión y token. Subresultados pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/account/{generations.log,rollback.log}`.

Siguiente acción: CLOSE-identidad-025 (HTTP real, navegador y copia de imagen
con reinicio) antes de la publicación del incremento.

### Ejecución 2026-10-26: aceptación local de imagen y bloqueo de publicación

Selección registrada antes de trabajar: CLOSE-identidad-025, siguiente en el
plan. Candidata `aaf0e0f`; producto sin cambios desde `54db6e4`. Se construyó
una imagen local desechable y se ejecutó el flujo completo; la publicación/CI
de la misma fuente no está autorizada en esta sesión.

Resultado local: imagen `goodsentry:account-025`,
`sha256:3b39f59382f096e51a1ed8c14adf4aefad872c42a9055688e81838e3f0a1589d`,
usuario 65532:65532, 73 migraciones. Sobre PostgreSQL desechable con owner y
member reales: setup required, proyecto y DSN, envelope 200 antes del cambio;
204 al cambiar la contraseña; `/api/v1/me` del owner 401 y del member 200
después; bearer revocado 401; bearer inválido 401; login anterior 401 y nuevo
200; envelope 200 después del cambio. El reinicio solo de la aplicación
conservó usuarios y digests de hashes idénticos, 3 eventos/3 issues, la sesión
del member y login nuevo 200. Los tests HTTP, navegador owner/member/sin
organización y PostgreSQL race ya están registrados. No se publicó ni se hizo
push; no hay CI de la misma fuente.

Bloqueo: la condición de cierre de 025 exige publicación/CI de la misma fuente.
Causa: los commits siguen locales y no hay autorización de publicación en esta
sesión. Dependencia: push/CI/publicación autorizada o un artefacto público
equivalente. Responsable: propietario del repositorio/proceso de publicación.
Intentos: no se intentó publicar para no ejecutar una acción externa no
autorizada. Desbloqueo: imagen y CI con la fuente de la candidata; repetir la
aceptación y el reinicio contra el digest público. Siguiente acción: continuar
con 026/027 sin declarar 025 cerrada.

Evidencia: `test-results/closure-20261025/identity-025/`
(`summary.json`, `build.log`, `flow.json`, `envelope-after.json`, `image.txt`,
`restart.txt`, `app.log`).

### Ejecución 2026-10-26: despliegue y rollback de contraseña

Selección registrada antes de trabajar: CLOSE-identidad-026, siguiente en el
plan. Candidata `aaf0e0f`; producto sin cambios desde `54db6e4`. Se documenta
el procedimiento y se contrasta con el Compose de un solo servicio.

Resultado observado: `docs/configuration.md` exige detener todo proceso
anterior que pueda emitir credenciales antes de habilitar el cambio de
contraseña; declara que old/new mezclados no son un rolling upgrade soportado
para este incremento, que debe recrearse el único servicio `goodsentry` con la
imagen nueva y que deben conservarse la imagen previa y un backup restaurado
por separado. También prohíbe ejecutar un emisor viejo contra la base después
de un cambio de contraseña. El Compose verificado en 008 tiene únicamente
`goodsentry` y `postgres`; el reinicio app-only de 025 confirmó el ciclo de
vida de un solo servicio. Subresultados pendientes del bloque: ninguno; la
ejecución real en un despliegue externo es responsabilidad del operador.

Evidencia: diff de `docs/configuration.md`, `identity-025/` y
`compose/config-default.json`.

Siguiente acción: CLOSE-identidad-027 (migración 036, outbox de reset y
contratos de token), primer bloque detallado de recuperación.

### Ejecución 2026-10-26: outbox de recuperación 036

Selección registrada antes de trabajar: CLOSE-identidad-027, siguiente en el
plan. Candidata `1f8fdf2`; producto sin cambios desde `54db6e4`. La evidencia
proviene de `account-mail/tests.log` y del esquema 036.

Resultado observado: `account_password_resets` contiene delivery ID, usuario,
destinatario, hash único de 64 hex, ciphertext AES-GCM de 92 bytes, generación
emisora, expiración, consumo, estado (`pending`, `processing`, `sent`, `failed`,
`consumed`, `canceled`), intentos 0..20, próximo intento, timestamps de
creación/actualización/envío, razón acotada y lease token/expiry con check de
estado `processing`. Los índices cubren usuario, pendientes y leases vencidos;
036 preserva filas previas y reabre. El test de retry/expiry/lease prueba el
fencing y el agotamiento. Subresultados pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/account-mail/tests.log` y
`internal/store/migrations/036_account_password_resets.sql`.

Siguiente acción: CLOSE-identidad-028 (solicitud de reset, enumeración, piso
de 60s y reemplazo de resets), con dependencias locales resueltas.

### Ejecución 2026-10-26: solicitud de recuperación

Selección registrada antes de trabajar: CLOSE-identidad-028, siguiente en el
plan. Candidata `b2c502e`; producto sin cambios desde `54db6e4`. La evidencia
proviene de `account-mail/tests.log` y del handler.

Resultado observado: login/registro exponen `Forgot password?` y el formulario
de solicitud; el mensaje es genérico. El handler normaliza a minúsculas/trim,
limita a 254 bytes y devuelve el mismo 202 para cuerpo malformado, campo
desconocido, email inválido, cuenta inexistente o solicitud reciente. Un email
conocido y no limitado resuelve la generación por `UserByEmail`, cifra el token
y encola en una transacción Read Committed; el test comprueba que el
desconocido no crea fila y que el piso de 60s conserva una sola entrega. Una
generación cambiada o un fallo de almacenamiento tampoco revelan la cuenta; el
202 se emite después del commit y el 503 es genérico. No hay destino
suministrado por el cliente. Subresultados pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/account-mail/tests.log` y
`internal/server/recovery.go`/`app.js`.

Siguiente acción: CLOSE-identidad-029 (token de 64 hex, HMAC, AES-GCM/AAD,
expiración de 30 minutos y no filtración), con dependencias locales resueltas.

### Ejecución 2026-10-26: secreto y validez del reset

Selección registrada antes de trabajar: CLOSE-identidad-029, siguiente en el
plan. Candidata `6b44d74`; producto sin cambios desde `54db6e4`. La evidencia
proviene de `account-mail/tests.log` y de `internal/accountmail/secretbox.go`.

Resultado observado: `NewToken` genera 32 bytes aleatorios y los devuelve como
64 hex minúsculas; el store solo guarda el HMAC-SHA256 con `SECRET_KEY`.
`SecretBox` deriva AES-256-GCM de la clave, usa nonce aleatorio y liga el hash
del token como AAD (`GoodSentry password reset v1:`), rechazando ciphertext
truncado, cambiado o sin autenticar. El ciphertext solo vive en el outbox; el
HTTP test comprueba el enlace de 64 hex, el estado `sent` antes de redimir, la
expiración de 30 minutos en store, el consumo único y la generación vigente.
Subresultados pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/account-mail/tests.log`.

Siguiente acción: CLOSE-identidad-030 (worker de correo, MIME y entrega
SMTP/reintentos), con dependencias locales resueltas.

### Ejecución 2026-10-26: worker y MIME de recuperación

Selección registrada antes de trabajar: CLOSE-identidad-030, siguiente en el
plan. Candidata `a2d730c`; producto sin cambios desde `54db6e4`. La evidencia
proviene de `account-mail/tests.log` y de `internal/jobs/account_mail.go`.

Resultado observado: un único worker de la aplicación reclama el outbox con
lease, envía un MIME de texto con enlace `resetToken`, respeta intentos y
reintentos, y registra `sent`/`failed` sin exponer el token en logs. Los tests
de `accountmail` cubren MIME/console y rechazo de entradas inseguras; los de
SMTP cubren entrega real de loopback, respuestas temporales/permanentes, TLS
obligatorio y protección de credenciales; el test de store cubre retry, expiry
y lease viejo; el HTTP confirma que la entrega llega a `sent` antes de redimir.
Subresultados pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/account-mail/tests.log`.

Siguiente acción: CLOSE-identidad-031 (redacción del reset, token único,
generación y revocación), con dependencias locales resueltas.

### Ejecución 2026-10-26: redención del reset

Selección registrada antes de trabajar: CLOSE-identidad-031, siguiente en el
plan. Candidata `495d159`; producto sin cambios desde `54db6e4`. La evidencia
proviene de `account-mail/tests.log` y de `internal/server/recovery.go`.

Resultado observado: request y reset son públicos pero exigen mismo origen y
JSON estricto; un bearer solo no autoriza y no se usa la sesión de otro
usuario. La redención lee el token, bloquea primero al usuario `FOR UPDATE` y
revalida la fila de reset con generación vigente, estado `sent`, no consumido
y no expirado. En una transacción Read Committed reemplaza el hash, incrementa
la generación, elimina todas las sesiones, revoca todos los tokens no revocados
en todas las organizaciones, marca el reset consumido e invalida los demás
resets no consumidos. Token obsoleto/expirado/consumido/ajeno/malformado
devuelve 400 sin escrituras; éxito 204 y cookie expirada. El cambio de
contraseña autenticado también borra resets pendientes. El HTTP test confirma
old session/password 401 y new password 200, con replay 400. Subresultados
pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/account-mail/tests.log`.

Siguiente acción: CLOSE-identidad-032 (formulario de reset, token en URL,
estados y 1024), con dependencias locales resueltas.

### Ejecución 2026-10-26: formulario de reset y aceptación agregada

Selección registrada antes de trabajar: CLOSE-identidad-032 y -033. Candidata
`98f762c`; producto sin cambios desde `54db6e4`. La unidad usa SMTP real de
loopback y Chromium; 033 queda bloqueada por la publicación/CI exigida.

Resultado 032: `account_recovery_smtp_browser.mjs` pasó con un sink SMTP local
real: el enlace `resetToken` se consume sin persistirse en estado/historial,
el formulario pide new/confirm, evita doble envío, limpia el token de la URL
al éxito y muestra el login genérico. El replay del token consumido y el piso
de 60s suprimen un segundo correo. Capturas `recovery-reset-form-*` y
`recovery-success-*` a 320/768/1024/1440 sin errores de página. La variante
1024 de Account también se regeneró con un flujo completo
(`account-owner-1024`).

Resultado 033: pure/MIME/AES-GCM, PostgreSQL (036, floor, generación,
due/expiry/lease, consumo único, revocación y rollback), HTTP (mismo origen,
cuerpos estrictos, enumeración, bearer, reset y cookie) y SMTP/browser locales
están cubiertos por `account-mail/tests.log` y `recovery-ui/`. Falta la
condición expresa de imagen copiada/reinicio, fuente exacta, publicación y CI
de la misma fuente. Como en 025, los commits son locales y no hay autorización
de publicación: 033 queda bloqueada, no cerrada. Desbloqueo: publicar/CI y
repetir copia+reinicio+aceptación sobre el artefacto público.

Evidencia: `test-results/closure-20261025/recovery-ui/`,
`account-owner-1024/` y `account-mail/tests.log`.

Siguiente acción: CLOSE-identidad-034 (migración TOTP 053 y reglas RFC6238),
que es independiente de la publicación.

### Ejecución 2026-10-26: TOTP 053 y RFC6238

Selección registrada antes de trabajar: CLOSE-identidad-034, siguiente en el
plan. Candidata `922c32f`; producto sin cambios desde `54db6e4`. La evidencia
proviene de los cinco tests de store y de `totp.go`/`053_user_totp.sql`.

Resultado observado: 053 añade columnas TOTP nullable, `user_recovery_codes`
y `login_challenges` sin reescribir usuarios. `totpBox` deriva AES-256-GCM de
`SECRET_KEY` con etiqueta fija; el secreto plano solo existe en enrollment y en
memoria durante la comprobación. RFC6238 usa HMAC-SHA1, seis dígitos, paso de
30s, ventana ±1 y comparación en tiempo constante; el store rechaza cualquier
paso menor o igual al último aceptado, por lo que un código observado no se
repite. Enrollment, challenge único, intentos/expiración, recovery codes de un
solo uso y generación se cubren con PostgreSQL real y `-race`. Subresultados
pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/identity/totp.log`.

Siguiente acción: CLOSE-identidad-035 (login con challenge TOTP/recovery,
strict body, cinco fallos y sesión), con dependencias locales resueltas.

### Ejecución 2026-10-26: login con segundo factor

Selección registrada antes de trabajar: CLOSE-identidad-035, siguiente en el
plan. Candidata `0646a1a`; producto sin cambios desde `54db6e4`. La evidencia
proviene de los tests HTTP de enrollment/login y cambio de credenciales.

Resultado observado: `POST /api/v1/login` conserva cuerpo estricto y el mismo
mensaje de credenciales inválidas; con secreto confirmado crea un challenge de
cinco minutos y responde 200 con `twoFactorRequired`, token, métodos
`totp`/`recovery` y sin cookie de sesión. `/api/v1/login/two-factor` exige
JSON estricto, mismo origen y completa un único challenge con TOTP o un
recovery code; el código de recuperación se marca usado en la misma transacción
que crea la sesión. Un código incorrecto incrementa el contador; el quinto
fallo, challenge expirado/consumido, generación cambiada o token desconocido
devuelven 401 sin sesión. El test de cambio de credenciales confirma que un
challenge obsoleto no autoriza. Subresultados pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/identity/totp-login.log`.

Siguiente acción: CLOSE-identidad-036 (GET/POST/confirm/delete de 2FA y
recovery codes), con dependencias locales resueltas.

### Ejecución 2026-10-26: endpoints de estado 2FA

Selección registrada antes de trabajar: CLOSE-identidad-036, siguiente en el
plan. Candidata `8301b2c`; producto sin cambios desde `54db6e4`. La evidencia
proviene del test de estado y del lifecycle de enrollment.

Resultado observado: `GET /api/v1/me/two-factor` reporta `enabled`, `pending` y
`recoveryCodesRemaining`; `POST` inicia enrollment y devuelve secreto base32 y
URI `otpauth://` una vez; `POST /confirm` exige código vigente, confirma y
entrega los recovery codes una sola vez; `DELETE` exige contraseña actual y
limpia secreto, confirmación, último paso y códigos; `POST /recovery-codes`
exige contraseña y reemplaza los códigos. Las mutaciones pasan por sesión,
mismo origen y CSRF, y `writeJSON` fija `Cache-Control: no-store` en las
respuestas privadas. El estado está disponible sin sesión en modo abierto. Los
tests de lifecycle, recovery codes y generación pasan. Subresultados pendientes
del bloque: ninguno.

Evidencia: `test-results/closure-20261025/identity/{totp-state.log,totp.log}`.

Siguiente acción: CLOSE-identidad-037 (UI de 2FA y challenge de login), con
dependencias locales resueltas.

### Ejecución 2026-10-26: UI de segundo factor

Selección registrada antes de trabajar: CLOSE-identidad-037, siguiente en el
plan. Candidata `6ec0873`; producto sin cambios desde `54db6e4`. La evidencia
proviene de un nuevo contrato de navegador con TOTP real.

Resultado observado: `two_factor_browser.mjs` enrola desde Account, muestra el
secreto/otpauth y confirma un código TOTP real; entrega los recovery codes una
sola vez. Tras cerrar sesión, el login solo con contraseña exige el challenge y
un código del siguiente paso abre el dashboard; un recovery code también
completa el challenge. Desactivar desde Account exige la contraseña y vuelve a
password-only. Las capturas de Account (enrollment/on) y del challenge cubren
320/768/1440 sin desbordamiento ni errores de página. Los flujos de password,
recuperación, invitación, OIDC y open mode ya están cubiertos por sus propios
comprobantes. Subresultados pendientes del bloque: ninguno. Los
`requestFailures` del reporte son abortos de navegación esperados al cerrar
sesión; no hay page errors.

Evidencia: `test-results/closure-20261025/two-factor-ui/` y
`tests/compat/two_factor_browser.mjs`.

Siguiente acción: CLOSE-identidad-038 (require2FA de organización y gate de
owner/admin), con dependencias locales resueltas.

### Ejecución 2026-10-26: require2FA de organización y gate

Selección registrada antes de trabajar: CLOSE-identidad-038 y -039, siguientes
en el plan. Candidata `e98d028`; producto sin cambios desde `54db6e4`. La
evidencia proviene del test HTTP/MCP con PostgreSQL.

Resultado observado: 073 añade `organizations.require_2fa` default false sin
reescribir filas. El detalle/update compatible acepta `require2FA`; activarlo
antes de que el owner tenga TOTP confirmado devuelve 409, y tras enrolarlo
devuelve 200. Con la organización exigiendo 2FA, `/api/v1/me` reporta
`twoFactorRequired`, las rutas de proyecto por sesión y por bearer devuelven
403, y MCP rechaza la herramienta con un error de two-factor; tras confirmar
TOTP, la sesión y MCP vuelven a 200. Desactivar el segundo factor con la
organización exigiendo devuelve 409. El gate no expone qué organización lo
disparó. Subresultados pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/identity/require2fa.log`.

Siguiente acción: CLOSE-identidad-040 (People/teams nativos, roles y 404),
con dependencias locales resueltas.

### Ejecución 2026-10-26: People, roles y equipos

Selección registrada antes de trabajar: CLOSE-identidad-040 a -044, en orden
del plan. Candidata `1752c78`; producto sin cambios desde `54db6e4`. La
evidencia proviene de `tenancy.log` (tests de store/HTTP con `-race`).

Resultado observado: las rutas nativas de members y teams exigen autenticación
y CSRF/mismo origen; IDs canónicos positivos, roles/nombres estrictos y
404 para usuarios/equipos ajenos o ausentes. La lista de members ordena por
nombre case-insensitive y luego ID; owners/managers pueden asignar manager,
admins no tocan managers/owners y solo un owner transfiere owner. La fila de
organización se bloquea antes de membership; el último owner no puede
degradarse/eliminarse (409). Eliminar un member borra membership, equipos y
assignments/tokens dependientes sin borrar al usuario ni sus sesiones de otras
organizaciones. Teams crean/listan/leen/renombran/eliminan con slug inmutable,
conflicto 409, default no borrable, mirror de proyecto al equipo menor y
members/equipos ordenados. Add/remove team member devuelve 204 y respeta la
autoridad del rol. Los tests HTTP y PostgreSQL cubren la matriz. Subresultados
pendientes de los bloques: ninguno.

Evidencia: `test-results/closure-20261025/identity/tenancy.log` y
`internal/server/people_test.go`/`internal/store/people_test.go`.

Siguiente acción: CLOSE-identidad-045 (migración 037 de invitaciones y
contratos de entrega), primer bloque detallado de invitaciones.

### Ejecución 2026-10-26: invitaciones detalladas

Selección registrada antes de trabajar: CLOSE-identidad-045 a -049, en orden
del plan. Candidata `ca5f417`; producto sin cambios desde `54db6e4`. La
evidencia proviene de `invitations/tests.log` con PostgreSQL/HTTP `-race`.

Resultado observado: 037 y la migración de equipos de manager preservan filas
y reabren; la invitación guarda delivery ID, organización, correo, rol,
equipos, hash/ciphertext del token, generación, expiración, estado e instantes
de entrega. La matriz de roles permite owner→owner/manager/admin/member y
manager→manager/admin/member, con autoridad y último owner probados; las rutas
públicas de request/accept exigen mismo origen, no aceptan bearer y responden
con el contrato de aceptación. La ruta compatible `/api/0/accept/...` mapea la
aceptación para identidades nuevas y existentes; los tests cubren validación
de token, MIME, equipos por ID, replay/cancelación y aceptación de correo
secundario verificado sin duplicar cuentas. Los owners de aceptación
pure/PG/HTTP están representados en el mismo log. Subresultados pendientes de
los bloques: ninguno.

Evidencia: `test-results/closure-20261025/invitations/tests.log`.

Siguiente acción: CLOSE-identidad-050 (OIDC opcional), siguiente familia.

### Ejecución 2026-10-26: OIDC opcional

Selección registrada antes de trabajar: CLOSE-identidad-050 a -053. Candidata
`4028479`; producto sin cambios desde `54db6e4`. La evidencia proviene de los
tests OIDC de store/server con `-race`.

Resultado observado: OIDC se desactiva si faltan issuer/client/secret; al
activarse, `/auth/oidc/start` genera state/nonce/verifier PKCE, guarda solo el
hash y una cookie HttpOnly, y el callback valida state/nonce/PKCE, correo
verificado, issuer/audience y sesión. 045 almacena provider, subject, correo
normalizado, usuario e instantes con unicidad por provider/subject; el store
vincula una identidad existente o crea una cuenta sin duplicar. `/api/v1/auth/
config` solo expone enabled/display name y no filtra issuer, client ni secret.
El proveedor externo real está aplazado (identidad-113), pero el flujo local
está probado. Subresultados pendientes de los bloques: ninguno.

Evidencia: `test-results/closure-20261025/identity/oidc.log`.

Siguiente acción: CLOSE-identidad-054 (social apps por organización), siguiente
familia.

### Ejecución 2026-10-26: social apps

Selección registrada antes de trabajar: CLOSE-identidad-054 a -060. Candidata
`7b5dd06`; producto sin cambios desde `54db6e4`. La evidencia proviene de los
tests de store/server con `-race` y proveedores locales.

Resultado observado: 055 persiste apps OAuth/OIDC por organización sin romper
las existentes; las rutas compatibles list/create/read/update/delete y el
descubrimiento público funcionan con autoridad y validación. El callback usa
`{domain}/auth/social/{providerID}/callback`, state y PKCE, y solo acepta
perfiles verificados. El vínculo social usa provider+subject y crea/une al
miembro de la organización. Los presets GitHub, GitLab, Gitea y DigitalOcean
crean el mismo miembro; Microsoft conserva `openid_connect` y usa la identidad
de tenant. Los perfiles no verificados se rechazan. Subresultados pendientes de
los bloques: ninguno. El recibo de un proveedor externo real sigue aplazado por
identidad-113; aquí se usan proveedores locales.

Evidencia: `test-results/closure-20261025/identity/social.log`.

Siguiente acción: CLOSE-identidad-061 (tokens de cuenta y recovery codes),
última familia de identidad antes de errores.

### Ejecución 2026-10-26: tokens de cuenta y recovery codes

Selección registrada antes de trabajar: CLOSE-identidad-061 a -064. Candidata
`d0fa71b`; producto sin cambios desde `54db6e4`. La evidencia proviene de los
tests store/server con `-race`.

Resultado observado: 056 añade tokens de usuario y recovery codes preservando
filas previas; `GET/POST /api/0/api-tokens/` y `DELETE /api/0/api-tokens/{id}/`
crean, listan y revocan tokens de cuenta con página/orden y aislamiento por
owner. Un token de usuario autentica otras lecturas compatibles y no concede
la API nativa; la revocación lo invalida. `GET/POST
/api/0/generate-recovery-codes/` entrega códigos de un solo uso y su lifecycle
(consumo, replay y regeneración) pasa. El modo abierto mantiene el owner
implícito para estos endpoints. Subresultados pendientes de los bloques:
ninguno.

Evidencia: `test-results/closure-20261025/identity/account-tokens.log`.

Siguiente acción: CLOSE-errores-001 (ingesta store/envelope) al cerrar la
familia de identidad salvo los bloqueos de publicación 025/033.

### Ejecución 2026-10-26: autenticación, framing y compresión de ingesta

Selección registrada antes de trabajar: CLOSE-errores-001 a -003. Candidata
`9df7f08`; producto sin cambios desde `54db6e4`. La evidencia proviene de los
tests de protocolo y del capture HTTP con PostgreSQL.

Resultado observado: store y envelope se registran bajo el prefijo; la
autenticación v7 acepta header, query y `dsn`, exige coincidencia entre todas
las identidades y el proyecto de la ruta, devuelve 403 sin clave y no acepta un
token de gestión como clave SDK. El framing exige cabeceras de una línea UTF-8,
cuenta bytes, permite binarios/newlines internos, longitud ausente como una
línea, LF final opcional, attachments de longitud cero y tipos desconocidos;
rechaza truncado, longitudes negativas/overflow, cabeceras multilínea/padding,
duplicados y bytes extra. La compresión acepta identity, gzip, deflate
zlib-wrapped, Brotli y Zstandard, más el store legado zlib+base64; aplica
límites comprimidos/descomprimidos y cuenta de items, con 400/413/415/503 en
el handler. Los tests cubren los vectores anteriores y el capture mantiene
items dependientes/independientes; Rate-Limit 429 y CORS/OPTIONS tienen sus
owners propios. Subresultados pendientes de los bloques: ninguno.

Evidencia: `test-results/closure-20261025/ingest.log`.

Siguiente acción: CLOSE-errores-004 (persistencia de contexto, attachments y
autorización de lectura).

### Ejecución 2026-10-26: persistencia de eventos y attachments

Selección registrada antes de trabajar: CLOSE-errores-004. Candidata
`10d39c2`; producto sin cambios desde `54db6e4`. La evidencia proviene de los
owners de captura, bytes, metadata y autorización con PostgreSQL/HTTP.

Resultado observado: el evento conserva el JSON estructurado con timestamp,
cadena de excepciones, frames, breadcrumbs, tags, usuario, request, contexts,
SDK, entorno, release y campos extra; la captura mantiene identidad y
children en una transacción. Los attachments se guardan como bytes PostgreSQL
asociados al receipt/event ID, con SHA-256 generado por el servidor y filename
solo como metadata; el scope de proyecto se revalida y la lectura/download
compatible usa autorización actual. Los tests cubren batches binarios,
duplicados/retry, límites de metadata, checksums legados, paginación,
route/query estricto, prefijo y credenciales. Subresultados pendientes del
bloque: ninguno.

Evidencia: `test-results/closure-20261025/ingest-attachments.log`.

Siguiente acción: CLOSE-errores-005 (agrupación estable, fingerprint, conteos
y regresión).

### Ejecución 2026-10-26: agrupación y regresión

Selección registrada antes de trabajar: CLOSE-errores-005. Candidata
`a78b6b3`; producto sin cambios desde `54db6e4`. La evidencia proviene de los
owners de captura, merge y HTTP con PostgreSQL.

Resultado observado: el hash por defecto combina tipo de excepción y
localización normalizada, con fallback de mensaje; los fingerprints custom y
el marcador `{{ default }}` se respetan, el proyecto forma parte de la clave y
el resultado es determinista. Ocho capturas concurrentes del mismo evento
producen un issue con `Count=1`; eventos posteriores incrementan first/last y
el contador una sola vez. Un evento para un issue resuelto genera regresión
visible y los estados unresolved/resolved/ignored, assignment, comments,
búsqueda por texto/estado/entorno/release/tags, orden y paginación están
cubiertos por los owners de issues. Merge/unmerge conservan particiones e
historial. Subresultados pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/errors-grouping.log`.

Siguiente acción: CLOSE-errores-006 queda a la espera de sus proveedores de
releases/artefactos/nativo/CLI; avanzar en CLOSE-errores-007 (issue search) y
los criterios independientes de la familia mientras tanto.

### Ejecución 2026-10-26: búsqueda de issues y snapshots

Selección registrada antes de trabajar: CLOSE-errores-007 a -012. Candidata
`0834dbb`; producto sin cambios desde `54db6e4`. La evidencia proviene de los
owners de gramática, store y HTTP con `-race`.

Resultado observado: la query nativa acepta un solo proyecto, texto, estado,
entorno, release, orden, página y categoría; rechaza duplicados, valores
inválidos y percent-encoding roto. La gramática soporta texto libre, términos
`key:value` intersectados, listas/OR, negación, wildcard y literales citados;
las claves desconocidas quedan como texto y no se inventan alias. La evaluación
usa la misma ocurrencia histórica para entorno/release/tags, el estado es
independiente del evento elegido y un snapshot consistente sobrevive a triage
concurrente. Membership y proyecto se validan; count/pages se calculan sobre el
snapshot autorizado. `IssueSearch` mantiene valores tipados explícitos. Los
tests cubren límites Unicode, SQL-looking values, malformed grammar y 1M de
página. Subresultados pendientes de los bloques: ninguno.

Evidencia: `test-results/closure-20261025/issue-search.log`.

Siguiente acción: CLOSE-errores-013/014 (formulario y Chromium de filtros
combinados), que requieren una corrida de navegador específica.

### Ejecución 2026-10-26: filtros combinados en Chromium

Selección registrada antes de trabajar: CLOSE-errores-013 y -014. Candidata
`b818e54`; producto sin cambios desde `54db6e4`. La unidad usa un proyecto
sembrado por envelopes reales y Chromium.

Resultado observado: el formulario Issues expone búsqueda, proyecto, estado
(incluido All), entorno, release y orden; aplicar texto+entorno+release+estado
devuelve solo el issue cuya misma ocurrencia coincide y escribe los parámetros
en la URL. Tras recargar, los campos y el resultado se mantienen. Una búsqueda
estructurada `region:eu` intersectada con entorno/release devuelve solo el
evento correspondiente. Las capturas cubren 320/768/1440 sin desbordamiento ni
errores de página. Subresultados pendientes de los bloques: ninguno.

Evidencia: `test-results/closure-20261025/issue-filters/`,
`tests/compat/issue_filters_browser.mjs` y `issue-search.log`.

Siguiente acción: CLOSE-errores-015 (borrado individual/bulk compatible) y las
reglas de discard/hash que siguen.

### Ejecución 2026-10-26: borrado compatible de issues

Selección registrada antes de trabajar: CLOSE-errores-015 a -026. Candidata
`b3005b8`; producto sin cambios desde `54db6e4`. La evidencia proviene de
store/HTTP `-race` y un contrato de navegador nuevo.

Resultado observado: 020 añade solo `event:admin` y preserva scopes/reopen. Las
rutas individuales global/organización devuelven 204 y el bulk de organización
200; aceptan IDs, filtros de proyecto/entorno/query/estado/orden/página,
contienen el conjunto autorizado y deduplican. Sin IDs usan la misma ocurrencia
histórica y el scope seleccionado; un filtro oculto precede al límite. Las
interfaces `DeleteCompatibleIssue(s)` usan `ManagementAccess`, grupos completos
root+particiones, conservan historial ajeno y permiten una nueva generación
tras un evento posterior. La captura de attachments/opaque hijos es atómica y
los locks de proyecto se ordenan por clave firmada; los tests cubren esperas,
reintentos, duplicados y cancelación. La UI muestra Delete issue en detalle y
Delete selected en la lista; Chromium confirma ambos con `Confirm deletion`,
elimina solo los grupos elegidos y deja el resto, a 320/768/1440 sin errores.
Subresultados pendientes de los bloques: ninguno.

Evidencia: `test-results/closure-20261025/errores-015-026.log` y
`issue-delete/`, más `tests/compat/issue_delete_browser.mjs`.

Siguiente acción: CLOSE-errores-027 (discard permanente y tabla de firmas).

### Ejecución 2026-10-26: discard permanente y aceptación de borrado

Selección registrada antes de trabajar: CLOSE-errores-027 a -033. Candidata
`607bf66`; producto sin cambios desde `54db6e4`. La evidencia proviene de
store/HTTP `-race` y del navegador de borrado/discard.

Resultado observado: `PUT discard:true` y la acción nativa Delete and discard
forever eliminan un error actual y devuelven 204; feedback/alias se rechazan
(alias conserva 409 mergedInto), el campo debe ser exclusivo, y event:admin
solo no autoriza triage ordinario. La tabla durable por proyecto/fingerprint se
escribe en la misma transacción de borrado, revalida membership/gate/root y
solo marca fingerprints aún attached; sobrevive reset/merge/restart y cascadea
con el proyecto. Toda captura comprueba el marcador bajo el gate compartido y
un parent marcado no crea event/issue/attachment/outbox ni consume cuota,
mientras logs/spans independientes siguen su curso en un envelope admitido;
un request throttleado se rechaza completo. El navegador confirma que discard
elimina el issue y un nuevo capture con la misma firma no reaparece.

032 queda vigente por los owners PostgreSQL/HTTP/CaptureItems y el navegador
actual. 033 exige además SDK oficial real, imagen copiada/reiniciada, fuente
exacta y CI pública; no hay publicación autorizada, así que queda bloqueada,
no cerrada. El árbol local y la suite `-race`/vet ya pasaron en el checkpoint;
los tests añadidos en esta unidad están incluidos en el log.

Evidencia: `test-results/closure-20261025/errores-027-031.log`,
`errores-015-026.log` e `issue-delete/`.

Siguiente acción: CLOSE-errores-034 (hashes y reset futuro de grouping).

### Ejecución 2026-10-26: hashes compatibles y reset futuro

Selección registrada antes de trabajar: CLOSE-errores-034 a -039. Candidata
`5fed26d`; producto sin cambios desde `54db6e4`. La evidencia proviene de los
owners de migración, concurrencia, autorización y HTTP con `-race`.

Resultado observado: 019 crea `issue_hash_routes` y backfill sin reescribir
issue membership; una fila inconsistente se rechaza atómicamente. La lista
compatible pagina entradas full/summary con cursor, límite y Links, y el reset
elimina solo la ruta futura de la firma, conservando eventos históricos y
permitiendo una nueva generación. Captura/reset concurrentes se ordenan con el
gate advisory y un reset cancelado no pierde la generación. La autorización
compatible, sesión/Bearer, scopes, alias mergedInto y filtros se conservan. Las
rutas exactas GET/DELETE y sus bodies estrictos se prueban con sus owners.
Subresultados pendientes de los bloques: ninguno.

Evidencia: `test-results/closure-20261025/errores-034-039.log`.

Siguiente acción: cerrar 040–044/046; después 045 (Future grouping UI) y 047
(aceptación SDK/imagen/CI, bloqueada por publicación).

### Ejecución 2026-10-26: contratos y consultas de hashes

Selección registrada antes de trabajar: CLOSE-errores-040 a -044 y -046.
Candidata `cba1504`; producto sin cambios desde `54db6e4`. La evidencia
proviene de `errores-034-039.log` y los owners de parse/HTTP/store.

Resultado observado: GET acepta solo `limit`, `cursor` y `full` escalares y
rechaza duplicados/encoding inválido; `latestEvent` es el evento retenido más
reciente por timestamp y conserva sample/empty. DELETE acepta únicamente
`id` repetidos 1..1000, UUID de 32 hex o 36 con guiones, deduplica y exige
body vacío; devuelve 202 sin body tras el commit. Captura/reset se serializan
en el gate del proyecto y un reset cancelado no pierde historia. Los contratos
internos `CompatibleIssueHashFilter`/`CompatibleIssueHash` y los providers
`CompatibleIssueHashes`/`ResetCompatibleIssueHashes` mantienen scoped snapshot,
límites y errores consistentes. Los tests de migración, carreras, autorización
y HTTP cubren 046. Subresultados pendientes de 040–044/046: ninguno.

Evidencia: `test-results/closure-20261025/errores-034-039.log`.

Siguiente acción: CLOSE-errores-045 (sección Future grouping en la UI),
después 047 (aceptación SDK/imagen/CI, bloqueada).

### Ejecución 2026-10-26: Future grouping UI

Selección registrada antes de trabajar: CLOSE-errores-045. Candidata
`9755b35`; producto sin cambios desde `54db6e4`. La evidencia proviene de un
contrato de navegador nuevo con un proyecto sembrado.

Resultado observado: el detalle muestra Future grouping con signatures activas,
sample, estado vacío y selección; el reset envía el ID seleccionado por el
DELETE compatible, recibe 202 sin body y confirma que los eventos existentes
quedan en el grupo histórico y la siguiente captura crea otro issue. La
mensaje de confirmación describe ese comportamiento. Las capturas cubren
320/768/1440 sin desbordamiento ni errores de página. Subresultados pendientes
del bloque: ninguno.

Evidencia: `test-results/closure-20261025/grouping-reset/` y
`tests/compat/issue_grouping_reset_browser.mjs`.

Siguiente acción: CLOSE-errores-048 (bulk PUT de issues), con 047 aún bloqueada
por aceptación pública/SDK.

### Ejecución 2026-10-26: bulk issue changes

Selección registrada antes de trabajar: CLOSE-errores-048 a -059. Candidata
`5097023`; producto sin cambios desde `54db6e4`. La evidencia proviene de los
owners de parseo, store y HTTP con `-race`.

Resultado observado: ambas rutas PUT de proyecto/organización exigen
`event:write` o sesión protegida; la selección repetida de id/proyecto/entorno
y los escalares query/status/sort/página se validan, con containment antes de
límites y dedup de aliases. El objeto JSON de 1 MiB admite status/assignment y
las políticas individuales, incluidos `snoozeDuration:null`/`muted`, rechaza
tipos/duplicados y opera atómicamente en Read Committed. Merge usa el cuerpo
nativo compartido, conserva historia/futuras rutas y devuelve el acknowledgement
parcial 200 con el actor committed. Gate ordering, cancelación, recheck tras
merge nativo y límites de 100/101 se prueban. Subresultados pendientes de los
bloques: ninguno.

Evidencia: `test-results/closure-20261025/errores-048-059.log`.

Siguiente acción: revisar los criterios restantes de la familia de errores
(060+ y los clientes SDK) antes de pasar a alertas.

### Ejecución 2026-10-26: aceptación con sentry-cli 3.8.0

Selección registrada antes de trabajar: CLOSE-errores-060. Candidata
`8b7eb7a`; producto sin cambios desde `54db6e4`. Se usó el binario oficial
`sentry-cli 3.8.0` disponible en `/tmp`, una app local con proyecto real y un
token restringido `event:write`.

Resultado observado: `issues resolve -o default-organization -p cli-project -i 3 -i 2`
salió 0, imprimió `Updated matching issues` y dejó los IDs 2/3 `resolved`.
`issues mute -o default-organization -p cli-project -s unresolved` salió 0,
imprimió `new status: muted` y dejó el ID 1 `ignored`; los ya resueltos no
cambiaron. El CLI emitió un warning de formato de token no estándar (`gs_`),
pero ambas solicitudes se autenticaron y mutaron datos reales. El byte/estado
del contrato y el fallback por status quedan cubiertos; no se infiere éxito
solo por exit code. Subresultados pendientes del bloque: ninguno.

Evidencia: `test-results/closure-20261025/cli-issues/`
(`summary.json`, `resolve.*`, `mute.*`, `statuses.txt`). El token no se guardó.

Siguiente acción: CLOSE-errores-061 (comentarios compatibles) y 063 (tags).

### Ejecución 2026-10-26: comentarios y tags compatibles

Selección registrada antes de trabajar: CLOSE-errores-061 a -064. Candidata
`3ab2056`; producto sin cambios desde `54db6e4`. La evidencia proviene de los
owners HTTP/PostgreSQL con `-race`.

Resultado observado: GET/POST de comments en las rutas global y
organización-prefijada respetan `event:read`/scope y sesión, y POST/PUT exigen
exactamente `{"data":{"text":"..."}}` devolviendo CommentSchema con el actor
numérico y timestamps; el texto se escapa y el listado ordena/pagina. GET de
tags en ambas rutas exige `event:read`; cada IssueTagSchema incluye key exacta,
display name title-cased, valores distintos y total, con agregación estable
sobre eventos retenidos. Subresultados pendientes de los bloques: ninguno.

Evidencia: `test-results/closure-20261025/errores-061-064.log`.

Siguiente acción: CLOSE-errores-065 (preferencias de usuario y emails
secundarios), con sus migraciones.

### Ejecución 2026-10-26: preferencias y emails secundarios

Selección registrada antes de trabajar: CLOSE-errores-065 a -068. Candidata
`7438588`; producto sin cambios desde `54db6e4`. La evidencia proviene de los
owners store/HTTP con `-race`.

Resultado observado: PUT de user persiste `options` JSONB con timezone/clock24h
y `subscribeByDefault`; GET/PUT de notifications y notifications/alerts
devuelven y actualizan el valor persistido. 050 añade direcciones secundarias
globalmente únicas preservando usuarios y reabre. POST/GET/DELETE de emails
exigen sesión/CSRF, validan email estricto, generan un enlace
`/account/emails/confirm?token=...` de 64 hex que verifica y redirige 303,
permiten login con el secundario verificado y protegen el primario. Un usuario
ajeno devuelve 404. Subresultados pendientes de los bloques: ninguno.

Evidencia: `test-results/closure-20261025/errores-065-068.log`.

Siguiente acción: CLOSE-errores-069 (cambios individuales de issue), con su
matriz de status/assignment/políticas.

### Ejecución 2026-10-26: cambios individuales de issue

Selección registrada antes de trabajar: CLOSE-errores-069 a -084. Candidata
`122145d`; producto sin cambios desde `54db6e4`. La evidencia proviene de los
owners de migración, mutación, parseo y HTTP con `-race`.

Resultado observado: 018 añade solo `event:write` preservando scopes; las rutas
PUT canónicas aceptan status unresolved/resolved/ignored/muted, assignment por
actor nativo o null/empty, policies y `inCommit` únicamente dentro de
statusDetails, con cuerpos/tipos/duplicados estrictos. La mutación usa
ReadCommitted, revalida membership/alias/generación y conserva historial,
outbox y colas; un cambio parcial devuelve el detalle compatible real. Los
tests cubren policy intent, assignment, cancelación, carrera con captura y
reopen. 069–083 quedan vigentes; 084 exige errores de SDK oficial, imagen
copiada/reiniciada, fuente exacta y CI pública, que no están autorizados, por
lo que queda bloqueada.

Evidencia: `test-results/closure-20261025/errores-069-083.log`.

Siguiente acción: CLOSE-errores-085 (listas y analytics de issues).

### Ejecución 2026-10-26: listas y analytics de issues

Selección registrada antes de trabajar: CLOSE-errores-085 a -098 y -100.
Candidata `483a651`; producto sin cambios desde `54db6e4`. La evidencia
proviene de los owners store/HTTP con `-race`.

Resultado observado: las listas globales/organización/proyecto devuelven roots
actuales con page size/lookahead, filtros de ocurrencia y orden
last_seen/first_seen/count/priority, short-ID y Links prefijados; los grants,
membership, aliases y límites se revalidan. Count/firstSeen/lastSeen conservan
el root nativo, la identidad de usuario usa el primer campo válido, y las
charts devuelven pares Unix-seconds/count con ceros reales. statusDetails,
issue-stats y commits usan los contratos tipados y límites estrictos; la
proyección wire es compatible sin prometer bytes idénticos. 099 (UI de
Affected users) sigue pendiente; 100 queda cubierto por la matriz PostgreSQL
de grants/membership/alias/capturas/merge/políticas y charts.

Evidencia: `test-results/closure-20261025/errores-085-098.log`.

Siguiente acción: CLOSE-errores-099 (UI Affected users); después 101 (event
inspection).

### Ejecución 2026-10-26: Affected users en detalle

Selección registrada antes de trabajar: CLOSE-errores-099. Candidata
`3b8491a`; producto sin cambios desde `54db6e4`. La evidencia proviene de un
contrato de navegador con eventos reales.

Resultado observado: un issue con tres eventos, dos usuarios identificados
distintos y uno sin usuario muestra `Affected users` = 2 en el detalle, con la
nota `Identified retained users`. Las capturas cubren 320/768/1440 sin
desbordamiento ni errores de página. Subresultados pendientes del bloque:
ninguno.

Evidencia: `test-results/closure-20261025/affected-users/` y
`tests/compat/issue_affected_users_browser.mjs`.

Siguiente acción: CLOSE-errores-101 (compatible event inspection), con sus
rutas y proyección tipada.

### Ejecución 2026-10-26: compatible event inspection

Selección registrada antes de trabajar: CLOSE-errores-101 a -112. Candidata
`2186c0d`; producto sin cambios desde `54db6e4`. La evidencia proviene de los
owners store/HTTP con `-race`.

Resultado observado: 017 añade solo `event:read` y preserva filas/recibos; las
rutas global/proyecto/issue aceptan limit/per_page, cursor, full, sample,
environment, query, time y selectores canónicos, con ventanas inclusivas/exclusivas
y cursor `0:offset:direction`. La lectura usa repeatable-read y revalida
membership/proyecto/alias; el detalle devuelve el evento real con id/eventID
string, projectID entero, breadcrumbs/request y siblings, sin efectos laterales.
Los snapshots sobreviven merge/split, la autoridad de lookup precede ambigüedad
y la proyección legacy no promete bytes idénticos. 101–111 quedan vigentes;
112 exige SDK oficial, imagen copiada/reiniciada, fuente y CI pública, por lo
que queda bloqueada, no cerrada.

Evidencia: `test-results/closure-20261025/errores-101-112.log`.

Siguiente acción: CLOSE-errores-113 (merge/original partitions/unmerge nativo).

### Ejecución 2026-10-26: merge y particiones originales

Selección registrada antes de trabajar: CLOSE-errores-113 a -119. Candidata
`01ffae3`; producto sin cambios desde `54db6e4`. La evidencia proviene de los
owners store/HTTP con `-race`.

Resultado observado: 016 añade constraints/índices de topología y rechaza
self/upward/cross-project/chain/active-alias/ownership inválidos atómicamente.
Merge/unmerge bloquean membership vigente y el gate de proyecto, conservan la
partición y fingerprint originales para siempre, distinguen count histórico de
progreso fresco y mantienen status/assignment/resolution/regression/comments/
activity en sus IDs originales. La raíz conserva su política, las rutas nativas
y origenes paginan/buscan literalmente, y capturas/triage/merge concurrentes se
serializan sin resucitar aliases. Subresultados pendientes de los bloques:
ninguno.

Evidencia: `test-results/closure-20261025/errores-113-119.log`.

Siguiente acción: CLOSE-errores-120 (rutas nativas merge/unmerge y UI de
selección).

### Ejecución 2026-10-26: rutas y UI de merge/unmerge

Selección registrada antes de trabajar: CLOSE-errores-120 a -126. Candidata
`0072d90`; producto sin cambios desde `54db6e4`. La evidencia proviene de los
owners HTTP/store y un contrato de navegador.

Resultado observado: POST merge acepta exactamente `issueIds` y devuelve el
root/isChanged; POST unmerge acepta exactamente `originIds` y devuelve las
particiones separadas; ambas usan sesión/CSRF/membership y rechazan IDs
duplicados/no canónicos/ajenos. GET origins devuelve data/total/page/páginas y
búsqueda; el store expone `MergeIssues`/`UnmergeIssues` con particiones e
historial. La lista nativa ofrece checkboxes y Merge selected; el detalle
ofrece Separate selected y confirmación. Chromium fusiona dos issues, deja el
tercero, separa un grupo original y muestra el outcome, a 320/768/1440 sin
errores. Subresultados pendientes de los bloques: ninguno.

Evidencia: `test-results/closure-20261025/errores-113-119.log` y
`merge-ui/`; contrato `tests/compat/issue_merge_browser.mjs`.

Siguiente acción: CLOSE-errores-127 (resolución release-anchored/next-release).

### Ejecución 2026-10-26: resolución por release

Selección registrada antes de trabajar: CLOSE-errores-127 a -138. Candidata
`979233b`; producto sin cambios desde `54db6e4`. La evidencia proviene de los
owners store/HTTP y de `internal/releaseorder`.

Resultado observado: PATCH nativo añade resolution release/next-release con
status resolved, selectores exactos inmutables y distinción entre campo
omitido y null; la política elegida conserva igualdad exacta y la política
next elige una release abierta posterior, congela la frontera y produce una
sola regresión al capturar. 014 persiste una fila por issue con referencias
inmutables, migraciones/reopen y hooks de frecuencia; la maintenance atiende
mutes vencidos. La ruta de candidates pagina/busca releases con query estricta.
`releaseorder.Compare` cubre gramática semántica, precedencia, clasificación,
Unicode, overflow e incomparables; el wire de resolución devuelve la release
almacenada. Cierres/fallos de metadata no rompen el no-op. Subresultados
pendientes de los bloques: ninguno.

Evidencia: `test-results/closure-20261025/errores-127-135.log` y
`releaseorder.log`.

Siguiente acción: CLOSE-errores-139 (mute condicional y ventanas), siguiente
familia.

### Ejecución 2026-10-26: mute condicional y ventanas

Selección registrada antes de trabajar: CLOSE-errores-139 a -149. Candidata
`3773e35`; el mute nativo, su DTO, migraciones, worker y UI no cambian desde
`6946fe9` (ventanas rodantes de usuario) y `eb3fc07` (ventanas de conteo).
Los comprobantes de navegador y de SDK se generaron el 2026-10-25 10:31 sobre
un árbol que ya contenía ambos commits; desde entonces solo cambian campos
compartidos de validación de otras familias. Los owners `TestIssueIgnore*` de
store y HTTP se reejecutaron hoy sobre `3773e35` con `-race` y
`-tags=integration` contra PostgreSQL desechable.

Resultado observado: PATCH compatible individual y bulk aceptan
`ignoreWindow`/nivel superior o `statusDetails` con copia duplicada
consistente, solo con conteo positivo, rango 1..10080 y acknowledgment que
serializa la ventana solo cuando está configurada (140). El progreso usa
`clock_timestamp()` tras el lock de la fila, un recibo por ocurrencia única,
intervalo inclusivo `[now - ventana, now]`, poda de recibos antiguos, expiración
en la misma transacción y `remaining` recalculado en lectura sin mutar estado
(141). La migración 065 persiste recibos sin evento, usuario ni payload, con
cascada y poda acotada bajo lock; merge/unmerge conservan el progreso solo en la
raíz activa y una política sin cambios lo preserva (142). El PATCH nativo con
sesión y CSRF admite `ignore` con status `ignored` obligatorio, condiciones
positivas int32, `userWindow` 1..10080 dependiente de `userCount`, rechazo
estricto de duplicados/nulos/flotantes/exponentes/booleanos/cadenas/vacíos y
no-op con misma política, deadline, baseline y actividad (143). El DTO expone
`ignore` con minutos, conteos, ventanas, `until`, `remaining` y
`userRemaining`; las condiciones son OR con prioridad duration, count,
user_count; el N-ésimo error o usuario reabre antes de evaluar alertas y los
duplicados no avanzan nada (144). Las migraciones 013/063/066 conservan filas
legacy, digests y actividad histórica, y registran una sola actividad
`unignored` automática nullable con `eventID` solo cuando una captura la causa
(145). El worker de mantenimiento reclama con `FOR UPDATE OF i SKIP LOCKED`,
revalida tras el claim, poda ventanas en lotes acotados, converge con
replicas/capturas/cambios manuales y se cancela y une al apagar (146). La
supresión de entregas por regla de frecuencia se evalúa antes de bloquear reglas
con outbox durable, sin replay del umbral cruzado y con elegibilidad ordinaria
en la captura que desmutea (147). La UI de detalle ofrece el formulario
etiquetado, deadline y restantes, actividad legible, borradores preservados,
errores 400 editables y guardas de navegación (148).

Evidencia: `test-results/closure-20261025/errores-139-149.log`;
`test-results/ignore-browser-userwindow-20261025/report.json` con 27 capturas
320/768/1440 y 7 comprobaciones; `test-results/ignore-sdk-userwindow-20261025/report.json`
con el SDK oficial 11.7.0 y capturas tardías/duplicadas reales.

Límites registrados: el fixture de navegador de esa corrida no incluía la issue
vencida por worker (`durationIssueID` nulo), de modo que el vencimiento por
duración se apoya en los owners de store; y CLOSE-errores-149 mantiene para la
auditoría final la imagen final con upgrade/reinicio, CI remoto, landing
desplegada y la suite completa sobre la revisión aceptada.

### Ejecución 2026-10-26: asignación, comentarios y actividad nativa

Selección registrada antes de trabajar: CLOSE-errores-150 a -155. Candidata
`3773e35` más el ajuste `web: drain empty 204 responses` producido en este
bloque a partir del hallazgo del owner de navegador.

Resultado observado: la asignación nativa usa `issue_assignments` con un solo
destino, membresía/equipo vigentes y `regressed_at`; las mutaciones bloquean
membresía `FOR SHARE` e issue `FOR UPDATE`, validan el destino y no mutan
parcialmente en 404/400, con no-op que conserva tiempos y actividad. Los DTO
exponen `assignedTo`, `resolvedAt` y `regressedAt`, historiales paginados
independientes (assignment/comments/activity) y `canEdit`/`updatedAt` en
comentarios; la moderación autor/owner/admin, el borrado de contenido y la
actividad `comment_added/edited/deleted` sin texto se comprueban en store y
HTTP. La captura tardía real del SDK oficial reabre con instante de recibo, una
sola actividad `regressed` y `eventID`, mientras un duplicado no hace nada.

El owner de navegador ejecuta 29 candidatos reales (25+4), 26 comentarios
(25+1), búsqueda/ asignación/limpieza de usuario y equipo con borrador
intacto, error 400 real por bytes UTF-8, edición con borrador más nuevo que la
respuesta, moderación por rol, regresión por SDK, y la carrera de navegación
con restore por Back. La primera corrida en este árbol falló de forma
reproducible: un DELETE ya respondido con 204 aparecía como
`net::ERR_ABORTED` porque el helper compartido regresaba sin consumir el
cuerpo vacío; consumirlo eliminó el fallo y la corrida quedó en cero peticiones
fallidas.

Evidencia: `test-results/closure-20261025/errores-150-155-store.log`,
`errores-150-155-http.log`, `errores-150-155-browser.log` y
`triage-ui/report.json` (7 comprobaciones, 27 capturas 320/768/1440,
`pageErrors` 0, `requestFailures` 0).

Límites registrados: el fixture se sembró en una organización aislada con 26
invitaciones aceptadas por correo local y un equipo; la imagen final con
upgrade/reinicio, el CI remoto y la suite completa sobre la revisión aceptada
siguen en la auditoría final de CLOSE-errores-155.

### Ejecución 2026-10-26: lectura nativa de adjuntos

Selección registrada antes de trabajar: CLOSE-errores-156 a -162. Candidata
`3b2567d`, sin cambios de producto en este bloque; el único archivo nuevo es
el owner de navegador `tests/compat/attachments_browser.mjs`.

Resultado observado: el consumidor nativo de adjuntos expone metadatos y
descarga sobre los recibos ya admitidos, sin cambiar ingesta, identidad de blob
ni permisos. Las rutas con sesión resuelven membresía vigente, issue canónico y
evento exacto en una instantánea repetible, y la descarga devuelve
`application/octet-stream` con `Content-Disposition: attachment`, `nosniff`,
`Cache-Control: private, no-store` y longitud exacta; HEAD no carga ni escribe
bytes. El store pagina 25+1 con consultas estrictas, acota la proyección a 2048
caracteres de nombre y 256 de tipo con `metadataTruncated`, conserva el valor
completo y el hash, y la migración 027 preserva y reabre los recibos previos.
El panel «Event attachments» cubre carga, reintento real tras un 500, 25 filas
por página con etiqueta y controles, descarga con los bytes exactos, error
accionable ante 404, divulgación por teclado del nombre de 158 caracteres con
resumen de 100, aviso de truncación, página posterior vacía y página uno vacía
tras el borrado físico, evento sin adjuntos, issue sin evento retenido (sin
petición) y una carga retrasada que no reescribe la ruta nueva.

La suite completa con `-race` e `integration` pasa en verde en un entorno
quieto (server 195.897s, store 359.066s, sin `DATA RACE`) y `go vet` pasa. Una
corrida previa bajo contención falló en
`TestCompatibleIssueUpdateResponseDuringNativeCaptureAndGrouping` (3.09s,
«native and compatible callers diverged after overlap»); el mismo test pasa
aislado en 2.30s y en la repetición quieta, y el fallo se conserva como
evidencia histórica. La imagen `goodsentry:closure-20261026`
(`34406cb60d22`) se construyó desde esta fuente y arrancó sobre la copia
lógica aceptada restaurada: 27→73 migraciones y todas las tablas existentes
idénticas salvo el chunk de staging vencido por su TTL de 168 h; un reinicio del
contenedor no cambió ninguna fila.

Evidencia: `test-results/closure-20261025/errores-156-162-go.log`,
`errores-156-162-browser.log`, `attachments-ui/report.json`,
`errors-156-162-full-race-contended.log`, `full-race-final-2.log`,
`vet.log`, `image-build.log` y `image-upgrade.txt`.

Límites registrados: no se ejecutó CI público ni se publicó la imagen porque no
hay autorización de publicación; esa comprobación queda para la auditoría
final junto con la landing desplegada.

### Ejecución 2026-10-26: recepción OTLP de logs

Selección registrada antes de trabajar: CLOSE-logs-012 a -028. Candidata
`1cebf3d`; sin cambios de producto.

Resultado observado: los owners de consumo, store y HTTP pasan con `-race`
sobre PostgreSQL desechable (179 subpruebas en `internal/otlp`,
`internal/store` e `internal/server`, incluidas las de admisión,
presupuestos, migración021, rollback y detalle seleccionado). El owner de
navegador se ejecutó contra dos instancias reales del binario actual sobre una
base sembrada al efecto: receta OTLP por proyecto con endpoint prefijado
exacto, encabezado copiable y distinción con el DSN; fila OTLP con distintivo y
detalle perezoso que conserva cuerpo hostil, servicio, entorno, severidad,
nanosegundos exactos, enteros de 64 bits extremos, bytes, `NaN`, NUL escapado,
claves duplicadas y clave vacía; reintento real ante 503, petición única al
reabrir durante la carga, resultado pendiente que no se adjunta tras navegar,
paginación que conserva el proyecto, filtros de texto/servicio que aíslan el
registro y enlace de traza; atributos SDK intactos con enteros exactos; lectura
por un miembro real; y proyecto con logs deshabilitados sin receta utilizable.
Las 10 comprobaciones y 12 capturas 320/768/1440 pasan sin errores de página ni
peticiones fallidas.

Evidencia: `test-results/closure-20261025/errores-logs-012-027-go.log` y
`otlp-ui/report.json`.

Límites registrados: CLOSE-logs-028 queda bloqueada. Su texto exige construir la
copia aislada desde la base aceptada de 18088 conservando 4010 filas y 524
eventos, y esa instancia (`goodsentry-deletion-grouping-regression-check`)
ya no existe en el host ni se conserva un volcado equivalente; tampoco hay
autorización de publicación para el CI remoto. Desbloqueo: restaurar ese volcado
exacto o aprobar una línea base nueva, y ejecutar CI público.

### Ejecución 2026-10-26: retención de logs y lectura compatible

Selección registrada antes de trabajar: CLOSE-logs-001 a -011. Candidata
`281444f`.

Resultado observado: la configuración resuelve el maestro por
`GOODSENTRY_RETENTION_DAYS`, `GLITCHTIP_RETENTION_DAYS` y
`RETENTION_DAYS`, y los logs por `GOODSENTRY_LOG_RETENTION_DAYS` y
`GLITCHTIP_LOG_RETENTION_DAYS`, con enteros 1..3650 e intervalo 1..3600; los
owners de configuración cubren herencia, límites y valores inválidos. La edad se
mide por `logs.created_at` con `created_at < cutoff` en una transacción, la
cohorte se limita a 250 filas con bloqueo de fila y `SKIP LOCKED`, el lock de
asesoría es por esquema y la cancelación o el fallo revierten la remoción; la
migración 022 conserva y reabre los recibos previos. Un batch OTLP solo se borra
cuando ninguna fila lo referencia y el hermano superviviente conserva sus bytes.
El loop corre en el proceso único con drenaje al arranque, reintento tras fallo y
parada limpia. La lectura compatible sirve la forma publicada con niveles
`warning`, cursor keyset, recursos y estadísticas por hora, y devuelve 404 con
la ingesta deshabilitada.

Aceptación sobre una copia aislada con la imagen `goodsentry:logs-20261026`
(`9d48873696fc`): 6 logs y 2 batches reales (OTLP y SDK), 4 recibos propios
envejecidos a dos días con retención de un día; tras el drenaje quedan 2 logs y
1 batch, el batch totalmente vencido desaparece, el hermano conserva el registro
fresco, el hash del SDK superviviente no cambia, el detalle nativo del id vencido
devuelve 404 y el vigente 200, la lectura compatible devuelve solo los ids 4 y 5,
y el respaldo previo restaurado en otra base conserva las cuatro filas vencidas.

Evidencia: `test-results/closure-20261025/logs-001-011-go.log`,
`log-retention-acceptance.json`, `log-retention-acceptance.mjs` y
`logs-image-build.log`.

Límites registrados: el CI público sigue pendiente para la auditoría final; no se
afirma recuperación automática ni archivado en frío.

### Ejecución 2026-10-26: logs reales con el SDK Go 0.50.0

Selección registrada antes de trabajar: CLOSE-logs-029. Candidata `81a4baf`.

Resultado observado: el SDK oficial v0.50.0 (fijado en `tests/compat/go.mod` y
resuelto desde la caché local) emitió tres logs reales por DSN —info, warn y
error— con atributos tipados (cadena, entero de 64 bits
`9223372036854775807`, booleano y doble), servicio y entorno proyectados, y
correlación real de traza y span. La API nativa devuelve las tres filas
filtrando por texto (`warn` aísla una), servicio, entorno y traza; el detalle
conserva los dígitos exactos del entero, la traza y `source:"sentry"`. En
Chromium la lista no muestra insignia OTLP, la divulgación de atributos conserva
el entero exacto, el enlace abre la vista de traza y el filtro por servicio
selecciona los registros, todo a 320/768/1440 sin desbordes. Tras reiniciar la
aplicación siguen las tres filas y la comprobación de navegador vuelve a pasar.

Evidencia: `test-results/closure-20261025/logs-go-api.json`,
`logs-go-ui/report.json`, `logs-go-ui-restart/report.json` y el driver
`tests/compat/logssdk/main.go`.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: logs reales con el SDK Python 2.72.0

Selección registrada antes de trabajar: CLOSE-logs-030. Candidata `39de036`.

Resultado observado: el SDK oficial 2.72.0, instalado en un entorno virtual
propio, emitió tres logs reales por DSN (info, warn y error) con atributos
explícitos —`service.name`, `component`, el entero exacto
`9223372036854775807`, booleano y doble, además de `reason` y `attempt`—,
entorno `logs-python-acceptance`, release `logs-python@2.72.0` y correlación
real de traza y span. La API nativa filtra por texto (tres filas, `warn` una),
servicio, entorno y traza; el detalle conserva los dígitos exactos del entero y
`source:"sentry"`. En Chromium la lista muestra las tres filas sin insignia
OTLP, la divulgación de atributos conserva el entero exacto, el enlace abre la
vista de traza y el filtro por servicio las selecciona, a 320/768/1440 sin
desbordes. Tras reiniciar la aplicación siguen las tres filas y la comprobación
vuelve a pasar.

Evidencia: `test-results/closure-20261025/logs-python-api.json`,
`logs-python-ui/report.json`, `logs-python-ui-restart/report.json` y el driver
`tests/compat/logs_python.py`.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: logs reales con el SDK Node 11.7.0

Selección registrada antes de trabajar: CLOSE-logs-031. Candidata `357218d`.

Resultado observado: el SDK oficial `@sentry/node` 11.7.0 emitió tres logs
reales por DSN (info, warn y error) dentro de un span, con atributos tipados
—entero, booleano y doble, además de cadenas—, servicio
`logs-node-service`, entorno `logs-node-acceptance`, release
`logs-node@11.7.0` y traza correlacionada. La API nativa filtra por texto (tres
filas, `warn` una), servicio, entorno y traza; el detalle conserva el entero
tipado y `source:"sentry"`. En Chromium la lista muestra las tres filas sin
insignia OTLP, la divulgación de atributos conserva los tipos, el enlace abre la
vista de traza y el filtro por servicio las selecciona a 320/768/1440. Tras
reiniciar la aplicación siguen las tres filas y la comprobación vuelve a pasar.

Evidencia: `test-results/closure-20261025/logs-node-api.json`,
`logs-node-ui/report.json` y `logs-node-ui-restart/report.json`; el driver
vive en `tests/compat/logs_node.mjs`.

Límites registrados: el SDK Node envía el span padre solo como atributo
`sentry.trace.parent_span_id`, de modo que la columna derivada de span queda
vacía aunque la traza sí se correlaciona y el enlace funciona. El CI público
sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: logs reales con el SDK Browser 11.7.0

Selección registrada antes de trabajar: CLOSE-logs-032. Candidata `fe93737`.

Resultado observado: el SDK oficial `@sentry/browser` 11.7.0, empaquetado con
esbuild desde el paquete npm, envió un sobre real con tres registros
`application/vnd.sentry.items.log+json` (info, warn y error) con atributos
tipados —entero, booleano y doble, además de cadenas—, servicio
`logs-browser-service`, entorno `logs-browser-acceptance`, release
`logs-browser@11.7.0` y traza generada por el SDK. La API nativa filtra por
texto (tres filas, `warn` una), servicio, entorno y traza; el detalle conserva
los tipos y `source:"sentry"`. En Chromium la lista muestra las tres filas sin
insignia OTLP, la divulgación de atributos conserva los tipos, el enlace abre la
vista de traza y el filtro por servicio las selecciona a 320/768/1440. Tras
reiniciar la aplicación siguen las tres filas y la comprobación vuelve a pasar.

Evidencia: `test-results/closure-20261025/logs-browser-api.json`,
`logs-browser-ui/report.json`, `logs-browser-ui-restart/report.json` y la
página/driver en `tests/compat/browser_logs/`.

Límites registrados: el `bundle.min.js` del CDN en 11.7.0 no incluye el emisor
de logs —solo envía la sesión—, por lo que la captura usa el paquete npm
empaquetado; y el SDK lleva el span padre solo como atributo, así que la columna
derivada de span queda vacía aunque la traza se correlaciona. El CI público
sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: exportador OTLP protobuf/HTTP 1.45.1

Selección registrada antes de trabajar: CLOSE-logs-033. Candidata `414f78d`.

Resultado observado: el exportador oficial
`opentelemetry-exporter-otlp-proto-http` 1.45.1, con `LoggerProvider`,
`BatchLogRecordProcessor` y un span real, envió tres registros por
`POST /v1/logs` (INFO, WARN y ERROR) con `service.name`,
`deployment.environment.name`, traza y span, y atributos tipados (cadena,
entero, booleano y doble). La API nativa filtra por texto (tres filas, `warn`
una), servicio, entorno y traza; el detalle responde `source:"otlp"` con los
tipos conservados. En Chromium la lista muestra la insignia OTLP, el detalle
perezoso conserva cuerpo, severidad, servicio, entorno, traza, span y atributos
tipados, el enlace abre la traza y el filtro por servicio selecciona los
registros a 320/768/1440. Tras reiniciar la aplicación siguen las tres filas y
el batch canónico.

Evidencia: `test-results/closure-20261025/logs-otlp-exporter-api.json`,
`logs-otlp-exporter-ui/report.json`,
`logs-otlp-exporter-ui-restart/report.json` y el driver
`tests/compat/otlp_logs_exporter.py`.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: exportador OTLP JSON/HTTP 0.66b1

Selección registrada antes de trabajar: CLOSE-logs-034. Candidata `eb35648`.

Resultado observado: el exportador oficial
`opentelemetry-exporter-otlp-json-http` 0.66b1 (`OTLPLogExporter`) envió tres
registros JSON por `POST /v1/logs` (INFO, WARN y ERROR) con
`service.name`, `deployment.environment.name`, traza y span, y atributos
tipados (cadena, entero, booleano y doble). La API nativa filtra por texto (tres
filas, `warn` una), servicio, entorno y traza; el detalle responde
`source:"otlp"` con los tipos conservados. En Chromium la lista muestra la
insignia OTLP, el detalle perezoso conserva cuerpo, severidad, servicio,
entorno, traza, span y atributos tipados, el enlace abre la traza y el filtro
por servicio selecciona los registros a 320/768/1440. Tras reiniciar la
aplicación siguen las tres filas y el batch canónico.

Evidencia: `test-results/closure-20261025/logs-otlp-json-api.json`,
`logs-otlp-json-ui/report.json`, `logs-otlp-json-ui-restart/report.json` y el
driver `tests/compat/otlp_logs_json_exporter.py`.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: scheduler real de uptime por tipo

Selección registrada antes de trabajar: CLOSE-uptime-027 a -032. Candidata
`859c71d`.

Resultado observado: los owners de sonda, scheduler y HTTP pasan con `-race`
(90 subpruebas), incluido el trabajador real con dos pools, historial, parada que
no registra caída y SSL no confiable. La aceptación creó un monitor de cada tipo
contra objetivos locales reales —HTTP con rutas HEAD, GET y POST, un listener
TCP y un servidor TLS firmado por una CA local que el proceso confía— con
intervalo de un segundo. HEAD, GET, POST, TCP y SSL alcanzaron `up`; al inducir
fallo bajaron a `down` y volvieron a `up` al restablecerse. El monitor GET con
umbral dos exigió dos fallos consecutivos antes de marcar caída. El heartbeat
pasó a `down` por intervalo vencido y volvió a `up` con un POST real a su
`heartbeatURL`. La pausa se midió sobre filas: 273 comprobaciones, sin
crecimiento en veinte segundos deshabilitado y 292 tras reanudar. La página de
estado pública respondió 200 con los monitores seleccionados y su estado, y tras
reiniciar el proceso se conservaron monitor, historial, clave de heartbeat y
página pública, y el scheduler siguió comprobando.

Evidencia: `test-results/closure-20261025/uptime-027-032-go.log`,
`uptime-acceptance.json`, `uptime-pause.json` y `uptime-ui/report.json`
(3 comprobaciones, 18 capturas 320/768/1440, sin errores de página ni peticiones
fallidas).

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: alertas compatibles por proyecto

Selección registrada antes de trabajar: CLOSE-alertas-001 a -003. Candidata
`200ac1f`.

Resultado observado: los owners de store, entrega y proveedores pasan con
`-race` (103 subpruebas) y el contrato HTTP se amplió con dos owners nuevos.
Las rutas compatibles listan, crean, actualizan, prueban y eliminan reglas del
proyecto resuelto por slug dentro del grant: la lectura exige `project:read` y
la escritura `project:write` sobre la autoridad nativa del creador; un token de
`event:read` recibe 403 en lectura y un token de `project:write` no puede leer.
La paginación usa cursor estricto con enlace `rel="next"` absoluto, segunda
página sin siguiente y 400 para cursor malformado o clave desconocida. El mapeo
convierte `timespanMinutes`, `quantity`, `environments`, `uptime` y
`alertRecipients` (correo del equipo y proveedores por URL) y rechaza con 400
`tagsToAdd`, URL de correo, proveedor sin URL y tipo desconocido; un PUT que
omite campos conserva ventana, cantidad, entornos y uptime, y cambiar el tipo
devuelve 409. El endpoint de prueba responde una entrada `queued` con
`message:null` por destinatario, sin afirmar entrega externa.

Evidencia: `test-results/closure-20261025/alertas-001-003-go.log` con los tres
owners HTTP y la matriz de store/entrega/proveedores.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: alertas nativas, entregas y reintentos

Selección registrada antes de trabajar: CLOSE-alertas-004 a -010. Candidata
`a593d58`.

Resultado observado: los owners de store, cola, configuración y proveedores
pasan con `-race` (262 subpruebas), incluidas reclamación con token de arriendo,
recuperación de arriendos vencidos, presupuesto de intentos, resultados
permanentes, reemplazo que cancela trabajo pendiente y snapshots de destinos. La
aceptación en una instancia real con receptor HTTP local mostró que un primer
evento no dispara, el segundo cruza el umbral y crea dos entregas (webhook y
correo) enviadas, y un tercer evento no genera nuevas entregas por el umbral y el
cooldown. El webhook recibió `X-GoodSentry-Delivery-ID` y
`X-GoodSentry-Event` y un payload versión 1 con regla, organización, proyecto,
issue, `eventId`, conteo, ventana y URL al issue; el historial guarda el intento
con código 200 y milisegundos.

Con el receptor devolviendo 500, el primer intento quedó en `retry`, el segundo
llegó 15 segundos después y el tercero 30 segundos después, ya con 200 y estado
`delivered`. Un miembro real recibe 403 tanto en reglas como en entregas. Un
monitor con entorno `uptime-alerts` produjo una entrega `uptime_down` al
confirmarse la caída y otra `uptime_recovered` al recuperarse, con estado
anterior y actual y URL a monitores; el estado inicial `up` no generó alerta.
En Chromium, con un receptor local real, pasan seis comprobaciones —módulo,
valores por defecto, validación retenida, filtros por URL, entrega 204 con
intento completado, paginación de 25 entregas, caída y recuperación por entorno,
pausa/edición/borrado y restricciones de miembro— con 24 capturas.

Evidencia: `test-results/closure-20261025/alertas-001-003-go.log`,
`alerts-stage1.json`, `alerts-stage2.json`, `alerts-uptime.json` y
`alerts-ui/report.json`.

Límites registrados: esta corrida usó `consolemail://` para el correo, que el
contrato admite; la prueba con receptor SMTP local corresponde a
CLOSE-alertas-011. El driver de navegador ahora tolera únicamente cancelaciones
por navegación completa que él mismo provoca. El CI público sigue pendiente para
la auditoría final.

### Ejecución 2026-10-26: historial de entregas, prueba y SMTP local

Selección registrada antes de trabajar: CLOSE-alertas-011. Candidata
`1881180`.

Resultado observado: el endpoint nativo de prueba responde 202 con
`{"queued":1}` y crea una entrega durable que el trabajador envía; el historial
por proyecto pagina 25 filas y el detalle devuelve la entrega con sus intentos.
Con `EMAIL_URL=smtp://127.0.0.1:18026` el correo sale por SMTP real a un
receptor local: el intento queda `delivered` con código 250 y el mensaje lleva
`To: alerts-smtp@example.test` y un `Message-ID` que contiene el identificador
estable de la entrega. El navegador ya había cubierto el módulo separado, los
estados de carga/error/vacío, la configuración retenida, los filtros por URL, la
paginación de entregas y la restricción de miembro; los owners de store cubren
reclamación con token de arriendo, recuperación tras caída, presupuesto de
intentos, cooldown concurrente y aislamiento entre organizaciones.

Evidencia: `test-results/closure-20261025/alerts-smtp.json` junto con
`alertas-001-003-go.log`, `alerts-stage1.json`, `alerts-stage2.json`,
`alerts-uptime.json` y `alerts-ui/report.json`.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: capacidad de entrega y composición

Selección registrada antes de trabajar: CLOSE-alertas-012. Candidata
`25da13a`.

Resultado observado: `cmd/goodsentry/main.go` compone el remitente, arranca los
trabajadores de alertas y solo después marca `cfg.AlertDelivery`, que se expone
como `features.alertDelivery`; los `defer` paran primero los trabajadores y
después cierran remitente y base, y la prueba HTTP sin trabajadores conserva la
capacidad en falso. La imagen `goodsentry:alerts-20261026`
(`1fc41eea35cd`), construida desde esta fuente, arrancó con receptores locales
propios: `features.alertDelivery` fue verdadero, una regla de frecuencia creó
dos entregas enviadas, el webhook contestó 204 con el payload y la URL al issue,
y el correo salió por SMTP real con código 250 y un `Message-ID` que contiene
el identificador de la entrega. Al detener el contenedor salió con código 0 y
sin errores ni pánicos en el registro.

Evidencia: `test-results/closure-20261025/alerts-image.json`,
`alerts-image-build.log` y `alerts-image-logs.txt`, junto con el owner de
servidor que fija la capacidad en falso sin trabajadores.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: Slack como proveedor específico

Selección registrada antes de trabajar: CLOSE-alertas-013. Candidata
`51dea92`.

Resultado observado: los owners locales de Slack pasan con `-race`
—reconocimiento de la respuesta, migración034 con pista de reintento durable y
payload del proveedor— y la aceptación contra un receptor local con forma de
Slack confirmó el contrato. Con cuerpo `ok` la entrega queda `delivered` con
código 200 y la carga útil usa los campos de Slack (`text`, `mrkdwn`,
`parse`, `unfurl_links`, `unfurl_media`, `blocks`), con proyecto, issue,
conteo, ventana, instante, identificador de entrega y URL en el texto. Un 200
con cuerpo distinto de `ok` produce fallo permanente `slack_response` sin
reintentos. Un 429 devuelve `retry` y la entrega se reintenta a los 15 y 30
segundos hasta `delivered` cuando el receptor vuelve a aceptar. La documentación
sigue presentando los formatos de proveedor como verificados solo contra
receptores locales, sin prometer canales externos.

Evidencia: `test-results/closure-20261025/alertas-slack.json` y
`alertas-001-003-go.log` (owners de reconocimiento, transporte y migración).

Límites registrados: la red real de Slack, la UI específica de proveedores y la
imagen final siguen siendo compuertas abiertas de esta capacidad; CLOSE-alertas-013
solo cierra lo localmente verificable. El CI público sigue pendiente para la
auditoría final.

### Ejecución 2026-10-26: tipo explícito Slack y migración034

Selección registrada antes de trabajar: CLOSE-alertas-014 y -015. Candidata
`fb3b55e`.

Resultado observado: el tipo `slack` existe como selección explícita, con la
misma admisión de destino que `webhook` —URL absoluta HTTP/HTTPS de hasta 2000
caracteres Unicode, sin credenciales ni fragmento—, conservando el recorte
exterior y los componentes secretos de ruta y consulta. Una regla Slack y una
webhook con el mismo destino permanecen entradas distintas, de modo que el
formato lo decide el tipo elegido y no el nombre del host. Los permisos de
reglas y entregas no cambian, y los motivos guardados son códigos estáticos sin
la URL. La migración `034_slack_alerts.sql` solo amplía la restricción de tipo
de destinatario para admitir `email`, `webhook` y `slack`, y su owner
comprueba que reglas, destinos, hashes, payloads, intentos y estado
preexistentes se conservan.

Evidencia: `test-results/closure-20261025/alertas-014-015-go.log` con los
owners de validación de destinos Slack, entrega por proveedor y migración, y
`alertas-slack.json` para la entrega real con el tipo explícito.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: mensaje y reconocimiento de Slack

Selección registrada antes de trabajar: CLOSE-alertas-016 a -018. Candidata
`157d104`.

Resultado observado: los owners del mensaje de Slack pasan con `-race` (38
subpruebas) y describen el contrato exacto. La carga útil lleva `text`
accesible, `mrkdwn:false`, `parse:"none"`, `unfurl_links:false` y
`unfurl_media:false`, sin `link_names`, `channel` ni `username`, y dos
bloques de sección: el primero con `plain_text` y `emoji:false` para el
resumen, el segundo con `mrkdwn` y `verbatim:true` para un enlace manual
etiquetado «View in GoodSentry». El texto de respaldo concatena el resumen y la
URL completa con ampersands y corchetes convertidos en entidades. El nombre de
regla, proyecto, issue, entorno y monitor permanecen en texto plano; la URL se
canonicaliza primero (espacios y prefijos Unicode) y después se codifican
corchetes y barras verticales. El resumen sigue el formato por tipo —frecuencia
con issue opcional y conteo/ventana, caída y recuperación con su línea de
estado, prueba con «Test notification»— más entorno opcional, instante UTC y
entrega, truncado a 3000 caracteres Unicode con elipsis y ambos textos dentro
del límite 1..3000. Un enlace relativo, con credenciales, fragmento, demasiado
largo o que crece al escapar, y un contexto ausente o no soportado, producen
`invalid_payload` permanente. La respuesta de Slack solo acepta HTTP 200 cuyo
cuerpo, recortado, sea exactamente `ok`; un 2xx distinto es fallo permanente
`slack_response`, un cuerpo mayor de 4096 bytes o inesperado también, y los
fallos de lectura, red o plazo usan la clasificación de reintento existente, sin
revelar el cuerpo. Los webhooks genéricos conservan su semántica por 2xx.

Evidencia: `test-results/closure-20261025/alertas-016-017-go.log` y la captura
real de `alertas-slack.json`, cuyo cuerpo incluye los controles y bloques
descritos.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: límite de tasa de Slack y reintento durable

Selección registrada antes de trabajar: CLOSE-alertas-019. Candidata
`cdfb23d`.

Resultado observado: el owner de reconocimiento parsea `Retry-After` con
espacios exteriores, ceros a la izquierda y segundos hasta el máximo de una
duración de Go, y descarta vacío, negativo, cero con signo, signo positivo,
fracciones, espacios interiores, dígitos no ASCII, fecha HTTP, valores
combinados por coma, repetidos y desbordamientos de entero o duración, sin
producir pista. El owner de store rechaza una pista negativa y una positiva
sobre un resultado que no es de reintento, y programa un 429 con pista de dos
horas como `next_attempt` igual a la finalización más esa pista —sin recortarla
al techo de 900 segundos del backoff por defecto—, la persiste en la misma
transacción con arriendo, y tras reabrir la base el historial es idéntico y un
nuevo claim no puede enviar antes del plazo del proveedor. La primera ejecución
de estos owners falló porque el contenedor desechable ya se había eliminado; se
conserva ese fallo y la repetición sobre una base nueva pasa.

Evidencia: `test-results/closure-20261025/alertas-019-go.log` con los casos de
parseo, el propietario de migración034 con pista durable y el de reclamación con
token y backoff.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: Slack en el diálogo de alertas

Selección registrada antes de trabajar: CLOSE-alertas-020. Candidata
`dc68329`.

Resultado observado: el diálogo original de alertas ofrece «Slack incoming
webhook» junto a los demás destinatarios, con la explicación de que la URL del
canal es obligatoria y contiene un secreto. En Chromium, el propietario crea una
regla Slack, la lista y abre su detalle, donde el destino snapshot
`slack: https://hooks.slack.example/...` queda visible; un miembro real no ve
ni el destino ni el control de creación. Las capturas 320/768/1440 no
desbordan y no hay errores de página. La implementación reutiliza el módulo
`alerts.js` existente, sin servicio, dependencia, flag ni paso de instalación
nuevos, y conserva los flujos de regla, historial, pausa y prueba ya cubiertos.

Evidencia: `test-results/closure-20261025/alerts-slack-ui/report.json`.

Límites registrados: el CI público y la recepción real en un canal de Slack
siguen pendientes y pertenecen a CLOSE-alertas-021.

### Ejecución 2026-10-26: matriz de aceptación de Slack

Selección registrada antes de trabajar: CLOSE-alertas-021. Candidata
`5e8701c`.

Resultado observado: la matriz local pasa con `-race` (91 subpruebas entre
notify, store y server): aceptación de Slack configurada con deduplicación y
rechazo de URL, mensajes serializados de frecuencia, caída, recuperación y
prueba, entrada con menciones y límites Unicode, enlaces con prefijo y escapado,
JSON genérico intacto, parseo de `Retry-After` válido e inválido con piso de
backoff, y consumidores reales de transporte y store. Los owners existentes
cubren el POST real con sus cabeceras, el 200/`ok`, cuerpos malformados,
sobredimensionados o lentos, 429/5xx/redirect/TLS/destino privado y
cancelación; PostgreSQL cubre la preservación y reapertura de 034, los intentos
reales y el reintento durable con pista; el servidor cubre sesión, CSRF,
secreto para miembros y los flujos de prueba; el navegador cubre el diálogo y la
secreta visibilidad.

Evidencia: `test-results/closure-20261025/alertas-021-go.log`,
`alerts-slack-ui/report.json` y `alertas-slack.json`.

Bloqueo registrado: el cierre del proveedor exige una recepción en un canal de
Slack autorizado, la aceptación sobre una imagen desechable con reinicio, la
publicación de la fuente y el CI público, ninguna de las cuales está disponible
ni autorizada en este entorno. Condición de desbloqueo: un canal de Slack
autorizado y la autorización de publicación/CI; mientras tanto los demás
proveedores pueden avanzar por unidad independiente.

### Ejecución 2026-10-26: proveedores adicionales y Discord

Selección registrada antes de trabajar: CLOSE-alertas-022 a -025. Candidata
`5b7e3bb`.

Resultado observado: los seis tipos URL-based (`discord`, `teams`,
`googlechat`, `feishu`, `ntfy`, `zulip`) comparten la admisión de destino,
el recorte, la selección explícita de tipo, la identidad durable, el secreto de
propietario/administrador, el trabajador y la política de red existentes, sin
inferir proveedor por host; Slack y la webhook genérica conservan su formato y
semántica. La migración `038_provider_alerts.sql` solo amplía la restricción al
conjunto completo y su owner conserva reglas, entregas, intentos, relojes y
estados previos. El resumen común —regla, proyecto, issue opcional, conteo y
ventana, transición de monitor, prueba, entorno opcional, instante UTC y
entrega— se serializa por proveedor y rechaza contexto ausente, UTF-8 o NUL
inválidos, URL inválida o demasiado larga y mensajes que no caben, sin persistir
el texto renderizado.

La aceptación con un receptor local confirmó Discord: `content` incluye el
resumen y la URL canonicalizada, `flags:4`, `allowed_mentions` con
`parse`/`users`/`roles` vacíos y `replied_user:false`, sin `channel`,
`username` ni `avatar_url`; un 204 se considera entregado y un 500 reintenta a
los 15 y 30 segundos hasta agotar el presupuesto. Los owners cubren además
mensajes de los otros cinco proveedores, escapado de menciones y HTML, límites
Unicode y JSON genérico intacto.

Evidencia: `test-results/closure-20261025/alertas-022-025-go.log` y
`alertas-discord.json`.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: formatos de Teams, Google Chat, Feishu, ntfy y Zulip

Selección registrada antes de trabajar: CLOSE-alertas-026 a -031. Candidata
`5b7e3bb`.

Resultado observado: los owners de proveedor pasan con `-race` (142 subpruebas
en la corrida compartida). Teams emite un `MessageCard` con `@type`,
`@context`, resumen plano de hasta 150 caracteres, sección `markdown:false` de
hasta 3000 con el resumen, salto y URL canonicalizada, y una acción `OpenUri`
«View in GoodSentry»; el truncado recae solo en el resumen. Google Chat envía
`{"text": resumen + "\n" + URL}` con ampersands y corchetes escapados y hasta
4096 caracteres. Feishu usa `msg_type:text` con contenido acotado a 3000 y
corchetes «defanged», y exige 200 con `code` o `StatusCode` numérico cero,
tratando otro 2xx, cuerpo inválido o código distinto como fallo permanente
`feishu_response`. ntfy envía texto UTF-8 de hasta 4096 bytes con
`Content-Type: text/plain; charset=UTF-8`, `Title` de hasta 200 caracteres y
`Click` con la URL, ignorando el cuerpo de cualquier 2xx. Zulip usa el formato
compatible con Slack, acota a 3000 y neutraliza menciones insertando un carácter
de ancho cero tras `@`, exigiendo 200 con `result:"success"`. Los tests cubren
además escapado de menciones y HTML, límites Unicode, contexto por tipo y JSON
genérico intacto.

Evidencia: `test-results/closure-20261025/alertas-022-025-go.log` (owners de
mensajes, contexto, escapado, reconocimiento y configuración) y
`alertas-discord.json` para la entrega real del proveedor ya cerrado.

Bloqueo registrado para CLOSE-alertas-031: exige Chromium cubriendo todas las
opciones, aceptación sobre imagen desechable con reinicio y recibos reales de
cada proveedor, ninguno disponible ni autorizado aquí. Desbloqueo: destinos
reales autorizados y publicación/CI.

### Ejecución 2026-10-26: Mattermost como proveedor

Selección registrada antes de trabajar: CLOSE-alertas-032 a -036. Candidata
`f859b06`.

Resultado observado: `mattermost` existe como tipo explícito con la misma
admisión de URL, recorte, secreto, visibilidad, política de red, snapshot,
clasificación de reintento y límites de intentos que el resto de webhooks. La
migración `058_mattermost_alerts.sql` solo amplía la restricción de tipo y su
owner conserva reglas, destinos, payloads, intentos, estado y recibos previos.
El POST usa `application/json` con `{"text": …}`, resumen escapado en
Markdown, cada `@` neutralizado con U+200B, un enlace Markdown «View in
GoodSentry» con la URL canónica, límite de 4000 caracteres y truncado solo del
resumen; un enlace que no cabe por sí solo falla como `invalid_payload`.
Mattermost solo se considera entregado con 200 y cuerpo recortado `ok`, con
lectura acotada a 4096 bytes, sin exponer el cuerpo y sin añadir semántica de
`Retry-After`. Los owners de mensaje, contexto, escapado, límites,
reconocimiento y migración pasan con `-race` (79 subpruebas).

Evidencia: `test-results/closure-20261025/alertas-032-036-go.log`.

Bloqueo registrado para CLOSE-alertas-036: la exposición en el diálogo y el
roundtrip en navegador están implementados, pero el criterio exige además un
recibo real de Mattermost, que no está disponible ni autorizado. Desbloqueo: un
destino Mattermost autorizado; la aceptación local de imagen y la publicación
exacta ya figuran en el propio criterio.

### Ejecución 2026-10-26: descubrimiento compatible

Selección registrada antes de trabajar: CLOSE-identidad-065 a -067. Candidata
`22c66bd`.

Resultado observado: el JSON de organización expone la clave exacta
`require2FA` como booleano. La CLI oficial `sentry-cli 3.8.0` ejecutó
`organizations list` contra la instancia con un token real y describió la tabla
—ID, nombre, slug, fecha y «Requires 2FA: false»— con salida 0, lo que demuestra
que el parser del cliente lee esa clave y el esquema. El owner HTTP cubre las
rutas `/api/0/`, `/api/0/users/`, `/api/0/users/{id}/`,
`/api/0/organizations/`, `/api/0/projects/`, `/api/0/instance-license/` y
su enlace de soporte, además de `/api/settings/` y
`/api/settings/{org}/` públicos; aplica `org:read` para organizaciones,
admite `org:read` o `project:read` para proyectos, acepta cualquier alcance
de lectura para raíz, usuarios y licencia, limita resultados a membresías y
grants del token, devuelve 404 para usuarios ajenos y no amplía el conjunto con
parámetros de consulta. Los esquemas usan datos reales —organización, proyecto
con su organización autorizada, usuario con correo como usuario, creación y
`hasPasswordAuth`, raíz con versión 0 y sin metadatos inventados de token—.

Evidencia: `test-results/closure-20261025/identidad-065-067-go.log`,
`identidad-065-cli.txt` y `identidad-065-organizations.json`.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: paginación, filtros y ajustes del descubrimiento

Selección registrada antes de trabajar: CLOSE-identidad-068 y -069. Candidata
`22c66bd`.

Resultado observado: el owner HTTP cubre `limit`/`per_page` de 1 a 100 con
rechazo de valores distintos simultáneos, cursor canónico, filtros
`query`/`owner` y 400 para parámetros desconocidos, repetidos, fuera de rango
o con codificación inválida (`%zz`). Las listas devuelven un `Link` público
prefijado con `rel="next"` y el cursor canónico tanto para organizaciones como
para proyectos. Los objetos de usuario, raíz y licencia se emiten en sus rutas
con barra final; los ajustes públicos informan solo nombre real de instancia,
versión, entorno, estado deshabilitado de facturación, social, registro y
creación de organizaciones, y las features activas, mientras la respuesta por
organización mantiene la lista social vacía para no enumerar slugs, y el enlace
de soporte apunta al gestor de incidencias público. Las rutas son de solo
lectura y no añaden esquema, worker ni integración externa.

Evidencia: `test-results/closure-20261025/identidad-065-067-go.log`, el mismo
owner que ejercita paginación, filtros, enlaces y ajustes.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: detalle de organización y claves de proyecto

Selección registrada antes de trabajar: CLOSE-identidad-070 a -072. Candidata
`a9b5372`.

Resultado observado: la migración `039_compat_management.sql` aporta únicamente
los metadatos de gestión restantes —creación de equipos heredada del instante de
su organización, UUID determinista y columnas de límite nulas para claves
existentes, UUID aleatorio para nuevas— y su owner comprueba que las filas y los
recibos previos se conservan y que reabrir no altera valores antiguos. El
detalle de organización devuelve campos vivos, proyectos y equipos reales con su
relación `{id,slug}`, `openMembership:false` y acceso derivado del rol: el
propietario incluye escritura de proyecto, el miembro solo lectura más escritura
de eventos y el manager añade organización, miembros y equipos. Las rutas de
claves aceptan lectura con `project:read` y mutaciones con `project:write` o
rol de propietario/manager/administrador, rechazan IDs desconocidos o de otro
proyecto con 404, filtran por activo/inactivo, paginan con cursor y `Link`
prefijado, y su DTO expone UUID, clave pública, proyecto, etiqueta, estado,
creación y DSN con secreto vacío y URLs públicas de CSP/seguridad. El owner HTTP
cubre crear, renombrar, desactivar —con rechazo real de ingesta 403—, reactivar
—con ingesta 200—, borrar y la paginación.

Evidencia: `test-results/closure-20261025/identidad-070-072-go.log` con los
cinco owners (HTTP de gestión, migración 039, creación en equipo y relaciones
múltiples).

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: claves con límite por ventana

Selección registrada antes de trabajar: CLOSE-identidad-073. Candidata
`a9aa562`.

Resultado observado: crear una clave preserva la clave pública generada solo en
el DTO, sin endpoint de rotación; `PUT` reemplaza etiqueta y límite nulo o
`{window,count}` con rangos 1..604800 y 1..1000000000, acepta `isActive`
opcional y conserva el estado si se omite; `DELETE` elimina exactamente esa
clave. La migración `041_project_key_rate_usage.sql` almacena el uso por clave y
su owner comprueba que las claves y filas previas se conservan y que reabrir no
las altera. La admisión cobra una vez los elementos de evento, transacción,
feedback y log, y los spans por `item_count` válido de 1 a 1000 —los inválidos o
desbordados cuentan uno—, además de cada registro OTLP preparado; ambos insertan
o incrementan la ventana alineada al reloj de la base y, al exceder el conteo,
responden 429 con `Retry-After` y `X-Sentry-Rate-Limits` (OTLP con
`google.rpc.Status` código 8 y exportaciones vacías exitosas). Una clave sin
límite no crea fila de uso, y el proveedor de limpieza borra como máximo 250
ventanas mayores de 14 días por pasada, conservando más de una ventana máxima
previa.

Evidencia: `test-results/closure-20261025/identidad-073-go.log` con los owners
de ingesta, OTLP, limitación por proyecto, admisión y ventana, migración 041 y
limpieza (17 subpruebas en total, sin fallos).

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: gestor de claves y rotación

Selección registrada antes de trabajar: CLOSE-identidad-074 y -075. Candidata
`0e84951`.

Resultado observado: la vista Projects expone un gestor de claves etiquetado.
En Chromium pasan cinco comprobaciones —crear una clave con límite y DSN
copiable, editar límite y estado, confirmar el borrado permanente y paginar en
ambas direcciones con los cursores `Link`— sin errores de página. El owner HTTP
cubre el reparto por rol (miembro solo lectura; propietario, manager y admin con
mutaciones), los alcances Bearer, los cuerpos y consultas estrictos, el DSN con
secreto vacío y los 404 de IDs desconocidos o de otro proyecto; los de store
cubren la preservación y reapertura de 039/041, la reasignación de UUID y reloj
de equipo, el aislamiento entre proyectos, la admisión atómica de cuota, el
reinicio de ventana y la limpieza acotada. La rotación se verificó con ingesta
real: la clave original acepta eventos, una segunda clave se crea y también
ingesta, desactivar la primera devuelve 403, reactivarla vuelve a 200, el
borrado devuelve 204 y su uso pasa a 403 mientras la segunda sigue activa.

Evidencia: `test-results/closure-20261025/project-keys-ui/report.json`,
`identidad-074-075-go.log` y `identidad-keys-rotation.json`.

Límites registrados: la publicación de imagen de fuente exacta sigue siendo una
compuerta de release separada, y el CI público queda pendiente para la auditoría
final.

### Ejecución 2026-10-26: actualización y borrado compatible de proyecto

Selección registrada antes de trabajar: CLOSE-identidad-076. Candidata
`58243a2`.

Resultado observado: `PUT /api/0/projects/{org}/{project}/` acepta nombre y
plataforma con `eventThrottleRate` opcional; los valores fuera de 0..100
devuelven 400, la omisión y el `null` explícito conservan la tasa guardada y el
cero la desactiva. El identificador y el slug permanecen inmutables tras
renombrar. `DELETE` usa la misma autoridad y deja el proyecto inaccesible con
404; su cascada elimina las claves del proyecto (comprobado con conteo en base) y
el resto cuelga de las claves foráneas existentes. La autoridad compartida exige
rol de propietario, manager o administrador en sesión, o un token
`project:write` restringido al proyecto; el owner de creación ya verifica el 403
de un miembro y la denegación de un token de solo lectura.

Evidencia: `test-results/closure-20261025/identidad-076-go.log` con el owner
ampliado de actualización y borrado junto a los de creación y relación de
equipo.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: muestreo de tráfico por proyecto

Selección registrada antes de trabajar: CLOSE-identidad-077. Candidata
`9227c7e`.

Resultado observado: la migración `061_project_event_throttle.sql` añade
`projects.event_throttle_rate` como entero no nulo con valor por defecto 0 y
restricción 0..100; la creación acepta el porcentaje opcional y las respuestas de
lista, detalle y actualización lo exponen. La decisión pura se prueba con
percentiles fijos: 0 nunca rechaza, 1 rechaza con percentil 0 y no con 1, 37
rechaza por debajo de 37, 99 por debajo de 99 y 100 rechaza siempre. En HTTP, el
muestreo se aplica una vez por petición tras resolver clave y proyecto: el sobre
de Sentry, un evento malformado, el minidump y el informe de seguridad/CSP
reciben 429 con `Retry-After: 600` y
`X-Sentry-Rate-Limits: 600:event:project:project_throttled`, y la entrada OTLP
responde lo mismo con `google.rpc.Status` código 8. Las peticiones rechazadas no
escriben eventos ni consumen la cuota por clave independiente, y un proyecto con
tasa 0 nunca rechaza.

Evidencia: `test-results/closure-20261025/identidad-077-go.log` con los owners
de decisión, HTTP y admisión, más el contenido de la migración.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: edición y borrado de proyecto en la UI

Selección registrada antes de trabajar: CLOSE-identidad-078. Candidata
`7f97704`.

Resultado observado: en Chromium, el propietario abre «Edit» en la vista
Projects, cambia el nombre y guarda; la lista actualiza el nombre y aparece el
aviso «Project updated.». Al pulsar «Delete», el diálogo enuncia la cascada
completa —claves DSN, eventos, issues, logs, trazas, releases, adjuntos y
archivos de depuración— y que no se puede deshacer; «Cancel» deja el proyecto
intacto, y «Delete project permanently» elimina solo el proyecto confirmado sin
tocar otro. Un miembro real ve el proyecto sin los botones de edición ni borrado.
Las capturas 320/768/1440 no desbordan. Los owners de PostgreSQL y HTTP de los
criterios previos cubren los valores por defecto de la migración, la persistencia
de la tasa, la conservación de slug y creación, el borrado en cascada y la
conducta estricta de cuerpos, roles y alcances.

Evidencia: `test-results/closure-20261025/project-ui/report.json`.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: repositorios y proyectos de equipo

Selección registrada antes de trabajar: CLOSE-identidad-079 y -080. Candidata
`1ad82c4`.

Resultado observado: `GET/POST /api/0/organizations/{org}/repos/` exige
`org:read` y `org:write`, pagina con límite/cursor estrictos y consulta
acotada. La revisión encontró una desviación: el handler admitía consultas de 200
caracteres donde el contrato fija 50; se corrigió el límite y el mensaje, y el
owner ahora comprueba que 51 caracteres, `limit=0` y un cursor inválido
devuelven 400 y que `limit=1` produce página con enlace. El owner cubre además
el 403 de un miembro, la creación con URL y provider opacos como repositorio
manual activo, el 409 de nombre duplicado y la denegación de un token
`org:read` frente al permiso de `org:write`; el owner de store confirma que un
token restringido no ve repositorios sin commits en sus proyectos, mientras una
sesión con todos los proyectos los ve.

La creación de proyecto en equipo acepta `{name,slug?,platform?,eventThrottleRate?}`:
401 sin sesión, 403 para un miembro, 400 para nombre vacío, de 65 caracteres, con
NUL, tasa fuera de rango, slug no canónico, campo desconocido o JSON con valores
extra, y 404 para un equipo ajeno o inexistente. La creación con slug, plataforma
y tasa 42 devuelve el DTO con su relación de equipo, la clave inicial disponible,
el proyecto legible por las rutas de organización y el 409 correcto al repetir
slug.

Evidencia: `test-results/closure-20261025/identidad-079-080-go.log` y el cambio
en `internal/server/releases.go` con su owner ampliado.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: asociación proyecto-equipo

Selección registrada antes de trabajar: CLOSE-identidad-081 y -082. Candidata
`b854954`.

Resultado observado: la migración `048_project_teams.sql` introduce la
asociación muchos-a-muchos autoritativa, rellena las relaciones heredadas con el
instante de creación del proyecto y conserva filas y recibos previos; los owners
comprueban además que borrar un equipo repara el espejo `projects.team_id` desde
las asociaciones retenidas y que las lecturas y el recuento de proyectos de
equipo usan `project_teams`. La expansión de destinatarios de correo de equipo
recorre todos los equipos asociados: con dos equipos produce dos destinos
distintos y no filtra miembros ajenos.

Las rutas `POST/DELETE /api/0/projects/{org}/{project}/teams/{team}/` exigen
`project:write` o sesión de propietario/manager/administrador: 401 sin sesión,
403 para un miembro, 404 para equipo inexistente y 201 al asociar con la lista
completa de equipos; repetir la asociación es idempotente, una segunda asociación
respeta el orden por slug e id, y el borrado devuelve 200 con las asociaciones
restantes, también idempotente cuando ya no existe. Las listas de proyecto y de
equipo reflejan ambas relaciones y un token de solo lectura no puede mutar.

Evidencia: `test-results/closure-20261025/identidad-081-082-go.log` con los
cinco owners de asociación, migración, reparación del espejo y destinatarios
multi-equipo.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: transferencia de propiedad y entornos de proyecto

Selección registrada antes de trabajar: CLOSE-identidad-083 y -084. Candidata
`873faca`.

Resultado observado: `POST /api/0/organizations/{org}/members/{member}/set_owner/`
exige sesión de propietario/administrador o `org:write` y, en storage, un actor
que sea propietario actual: un administrador recibe 403, al igual que una
petición sin sesión o sin CSRF, un ID malformado devuelve 400, uno ausente 404 y
una consulta 400. La transferencia devuelve 200 con el usuario objetivo como
propietario y deja exactamente un propietario, degradando a los demás. La
revisión detectó que el contrato exige rechazar cuerpos y el handler los
ignoraba; se añadió un rechazo explícito con 400 y se cubrió junto al alias
`me`, que devuelve al propietario actual de forma idempotente.

La migración `049_project_environments.sql` rellena entornos no vacíos desde
eventos, monitores, logs y spans sin reescribir telemetría; los owners cubren la
revisión de visibilidad con `visibility=all|hidden|visible` estricta, el 403 de
un miembro para ocultar, la respuesta `{id:null,name,isHidden}` sin identidad
numérica inventada, el detalle y los nombres de toda la organización limitados a
los proyectos concedidos, y que la visibilidad organizativa es oculta solo si
todas las ocurrencias lo están.

Evidencia: `test-results/closure-20261025/identidad-083-084-go.log` con los
owners de transferencia, entornos, visibilidad y migración 049, más el cambio en
`internal/server/compat_management.go`.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: rutas de entornos por proyecto

Selección registrada antes de trabajar: CLOSE-identidad-085. Candidata
`d887008`.

Resultado observado: la lista y el detalle de entornos por proyecto exigen
`project:read` o sesión de propietario, administrador o miembro dentro del
grant, y la lista aplica `visibility` estricta con paginación y enlaces
prefijados. El `PUT` acepta `{isHidden}` o el formato de GlitchTip
`{name,isHidden}` con nombre igual a la ruta, rechaza renombrar, campos
desconocidos, cuerpos sin `isHidden`, nombres inválidos con controles o barras,
consultas y entornos inexistentes con 400/404 según corresponda, y responde con
`{id:null,name,isHidden}`.

La revisión detectó que el handler admitía también sesiones de manager cuando el
contrato limita la actualización a propietario o administrador (o un token
`project:write`): se ajustó la comprobación y el owner ahora cubre 403 para
miembro y manager, 403 para un token de solo lectura, 200 para un token de
escritura y los rechazos estrictos de cuerpo. Los owners de store cubren la
preservación y reapertura de 049, las uniones en vivo y la agregación de
visibilidad.

Evidencia: `test-results/closure-20261025/identidad-085-go.log` y el cambio en
`internal/server/compat_environments.go` con su owner ampliado.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: personas, equipos y esquemas

Selección registrada antes de trabajar: CLOSE-identidad-086 y -087. Candidata
`8ec6f94`.

Resultado observado: las rutas de lectura de personas —miembros, equipos,
detalle de equipo, sus miembros y proyectos, y los equipos de un proyecto—
exigen membresía actual o grants `org:read`/`project:read` con los mismos
límites que el descubrimiento; un equipo ajeno o inexistente devuelve 404 y las
listas aplican `limit`/`per_page`/`cursor` estrictos con enlaces prefijados
`rel="next"`. Los esquemas usan datos reales: usuario con correo, rol, nombre de
rol, creación, campos anidados, `isOwner` y slugs de equipo ordenados; el equipo
expone creación, estado y recuento de miembros y sus proyectos asignados; la
relación proyecto-equipo devuelve `{id,slug}`; y el detalle de organización
lista los accesos soportados, incluidos los de manager (`org:write`,
`member:write`, `team:write`, `project:write`).

Añadí al owner de personas la comprobación de invitaciones pendientes: el
propietario ve una invitación creada en estado pendiente con `pending:true` y su
correo, mientras una sesión de miembro no ve ninguna entrada pendiente. También
se cubre el rechazo estricto de parámetros y el 404 de equipos ajenos.

Evidencia: `test-results/closure-20261025/identidad-086-087-go.log` con los
owners de personas, gestión de miembros, accesos y store, además del owner
ampliado en `internal/server/compat_people_test.go`.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: detalle y mutaciones de miembro

Selección registrada antes de trabajar: CLOSE-identidad-088. Candidata
`60718ed`.

Resultado observado: el detalle de miembro acepta ID canónico o `me`,
responde 400 para IDs malformados, 404 para ausentes o ajenos, 400 con consultas
y exige membresía actual. Un ID hexadecimal de invitación devuelve la fila
pendiente a propietarios, managers y administradores con `pending:true`,
`user:null` y los slugs de equipo asignados, mientras un miembro recibe 404.
`PUT` acepta solo `{orgRole,teamRoles?}` con roles válidos, ignora `role` en
las entradas de equipo, no cambia asignaciones de un miembro activo, rechaza
cambios de rol sobre invitaciones pendientes y devuelve 400 con campos
desconocidos, `teamRoles` malformados o múltiples valores JSON. Las reglas de
jerarquía se cubren con 403 de miembro, 409 al degradar o borrar al último
propietario, 403 de administrador sobre equipos donde no participa y permiso de
manager sobre equipos ajenos. `DELETE` devuelve 204 con la garantía del último
propietario y, para una invitación, la cancela; las asignaciones de equipo se
añaden y quitan de forma idempotente devolviendo el `TeamProjectSchema`.

Añadí al owner la cobertura de invitación pendiente: detalle para el propietario,
404 para el miembro, 400 al intentar cambiar su rol y 204 al cancelarla.

Evidencia: `test-results/closure-20261025/identidad-088-go.log` con los owners
HTTP de gestión de miembros, detalle de equipo y transferencia de propiedad.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: matriz de personas y equipos

Selección registrada antes de trabajar: CLOSE-identidad-089. Candidata
`79b2759`.

Resultado observado: la matriz del criterio se ejecutó contra PostgreSQL real,
HTTP real y Chromium. En la base desechable, los owners de store cubren la
visibilidad de organización, miembro y equipo, el orden de equipos y de
miembros, los límites de manager y administrador, la privacidad, detalle,
cancelación, asignación de equipos y aceptación de invitaciones pendientes, los
recuentos y asignaciones de proyecto en varios equipos, el backfill y la
reapertura de 048 y el aislamiento de tokens por organización y proyecto. En
HTTP se cubren las rutas con barra final, el detalle, el rol, el borrado y las
mutaciones de equipo del miembro, la creación, el detalle, la cancelación y la
aceptación de invitaciones, `me`, el último propietario, la jerarquía de roles,
el CSRF de sesión, los scopes `org:read`, `org:write` y `project:read`, las
organizaciones ajenas, los equipos desconocidos, la paginación estricta y los
enlaces prefijados. Chromium recorre la vista de propietario a 320/768/1440 con
la invitación pendiente real, crea y renombra «Alpha team», añade y quita al
miembro, cambia su rol a manager con el aviso correspondiente y borra el equipo
tras confirmar, sin desbordamiento de página ni errores de consola.

| Elemento del criterio | Comprobante |
|---|---|
| Visibilidad de organización, miembro y equipo | store `TestCompatibleOrganizationPeopleTeamsAndProjects` |
| Orden de equipos y de miembros | store `TestCompatibleOrganizationPeopleTeamsAndProjects`, `TestCompatibleOrganizationMemberDetailAndTeamMembership` |
| Límites de manager y administrador | store `TestCompatibleOrganizationMemberDetailAndTeamMembership`, `TestCompatibleOrganizationUpdateDeleteAndRoleBoundaries`; HTTP `TestCompatibleOrganizationMemberManagementHTTP` |
| Privacidad y detalle de invitación pendiente | store `TestCompatibleInvitationValidation`, `TestCompatibleInvitationAcceptsExistingAndVerifiedSecondaryAccounts`; HTTP `TestCompatibleOrganizationInvitationMutationHTTP` |
| Cancelación y asignación de equipos de invitación | store `TestInvitationDeliveryAcceptanceReplayAndCancellation`, `TestInvitationTeamAssignmentsFollowTeamIDsThroughAcceptance` |
| Aceptación de invitación con equipos | store `TestInvitationTeamAssignmentsFollowTeamIDsThroughAcceptance`; HTTP `TestCompatibleInvitationHTTPAcceptance` |
| Recuentos y asignaciones de proyecto en varios equipos | store `TestCompatibleProjectTeamAssignmentMultipleRelations`, `TestCompatibleTeamMutationsPreserveProjectRelations` |
| Backfill y reapertura de 048 | store `TestProjectTeamsMigration048PreservesLegacyRelations` |
| Aislamiento de token por organización y proyecto | store `TestCompatibleManagementOrganizationAndProjectKeys`; HTTP `TestCompatibleManagementOrganizationAndProjectKeysHTTP` |
| Rutas con barra final | HTTP `TestCompatiblePeopleTeamsAndProjectRelationsHTTP` |
| Detalle, rol, borrado y equipos del miembro | HTTP `TestCompatibleOrganizationMemberManagementHTTP`, `TestPeopleAndTeamHTTPContract` |
| Creación, detalle y cancelación de invitación | HTTP `TestCompatibleOrganizationInvitationMutationHTTP` |
| `self`/`me` | HTTP `TestCompatibleOrganizationMemberManagementHTTP` |
| Último propietario y jerarquía de roles | HTTP `TestCompatibleOrganizationMemberManagementHTTP`, `TestPeopleAndTeamHTTPContract`, `TestCompatibleSetOrganizationOwnerHTTP` |
| CSRF de sesión | HTTP `TestPeopleAndTeamHTTPContract`, `TestCreateCompatibleOrganizationHTTP` |
| Scopes `org:read`, `org:write` y `project:read` | HTTP `TestCompatibleTeamMutationsHTTP`, `TestCompatibleOrganizationInvitationMutationHTTP`, `TestCompatiblePeopleTeamsAndProjectRelationsHTTP` |
| Organizaciones ajenas y equipos desconocidos | HTTP `TestCompatiblePeopleTeamsAndProjectRelationsHTTP`, `TestCompatibleOrganizationMemberManagementHTTP` |
| Paginación estricta y enlaces prefijados | HTTP `TestCompatiblePeopleTeamsAndProjectRelationsHTTP` |
| Vista de propietario, equipos y rol en Chromium | `people-ui/report.json` y nueve capturas |

Evidencia: `test-results/closure-20261025/identidad-089-go.log` con 32 owners,
31 en la corrida conjunta más el de asignaciones de equipo de invitación, que
quedó fuera del primer patrón por un nombre mal escrito y se ejecutó aparte; y
`test-results/closure-20261025/people-ui/` con `report.json` y las capturas de
320/768/1440 de la vista de personas, del detalle de equipo y del estado tras el
borrado.

Límites registrados: el CI público sigue pendiente para la auditoría final. La
primera corrida de Chromium falló por un selector ambiguo del propio guion y se
repitió con el ámbito del equipo; la base desechable se reinició antes de
sembrar la organización. La tabla de miembros exige desplazamiento horizontal en
320 px, sin desbordar la página.

### Ejecución 2026-10-26: scope org:write y rol vigente

Selección registrada antes de trabajar: CLOSE-identidad-090. Candidata
`dad036a`.

Resultado observado: la migración 040 amplía la restricción de `api_tokens`
con `org:write` y el owner de store comprueba que la reapertura reaplica la
migración dos veces sin alterar los tokens previos, las sesiones, el grant del
token anterior a 040 ni los recibos 001–039; el recibo de 040 sigue
correspondiendo al SHA-256 del archivo. El mismo scope se emite y autentica con
sus proyectos y permite crear, actualizar y borrar organizaciones, invitar y
gestionar miembros y equipos por Bearer. La autoridad se revalida en cada
petición contra la membresía actual: un administrador con token `org:write`
crea un equipo antes de ser degradado a miembro y recibe 403 después; su sesión
sigue el mismo rol y responde 403, y al eliminar la membresía el token deja de
autenticar (401) mientras la sesión responde 404. El formulario nativo de
tokens lista `org:write` con la explicación «org:write invites members and
manages teams.», emite un token con ese scope desde el navegador y la lista
posterior conserva `org:write` sin mostrar el secreto.

Mapa de comprobantes:

| Elemento del criterio | Comprobante |
|---|---|
| Restricción de scope ampliada con `org:write` | store `TestOrgWriteScopeMigration040PreservesTokensAndReopens` |
| Scopes, grants y sesiones previos exactos | store `TestOrgWriteScopeMigration040PreservesTokensAndReopens` (instantáneas de `api_tokens`, `sessions` y grant reautenticado) |
| Recibos 001–039 exactos y recibo 040 con su checksum | store `TestOrgWriteScopeMigration040PreservesTokensAndReopens` |
| Alias Bearer para organizaciones | HTTP `TestCreateCompatibleOrganizationHTTP`, `TestCompatibleOrganizationUpdateDeleteHTTP` |
| Alias Bearer para miembros y equipos | HTTP `TestCompatibleOrganizationInvitationMutationHTTP`, `TestCompatibleTeamMutationsHTTP` |
| Rol actual y autoridad por endpoint revalidados en storage | HTTP `TestOrgWriteScopeUsesCurrentMembershipHTTP`; store `TestAPITokenInvalidationUsesCurrentDatabaseState`, `TestAPITokenAdministrationAndAtomicProjectBoundary` |
| Sesiones con su rol actual, sin scopes de token | HTTP `TestOrgWriteScopeUsesCurrentMembershipHTTP`, `TestPeopleAndTeamHTTPContract` |
| Formulario nativo lista y explica `org:write` | `tokens-ui/report.json` y capturas 320/768/1440 |

Evidencia: `test-results/closure-20261025/identidad-090-go.log` con nueve
owners y `test-results/closure-20261025/tokens-ui/` con `report.json` y seis
capturas del formulario y de la lista de tokens.

Límites registrados: el CI público sigue pendiente para la auditoría final. El
token del navegador se emitió dos veces por una repetición del guion para
encuadrar la fila de la lista; la base es desechable y solo se capturaron
prefijos, nunca secretos.

### Ejecución 2026-10-26: roles con manager y equipos de invitación

Selección registrada antes de trabajar: CLOSE-identidad-091. Candidata
`8ba8ae0`.

Resultado observado: la migración 062 amplía las restricciones de membresía e
invitación con `manager`, crea `organization_invitation_teams` con claves
foráneas en cascada y el owner de store comprueba que la reapertura conserva
usuarios, organización, proyectos, credenciales y recibos 001–061, con el
checksum de 062 intacto. La asignación de equipos de una invitación pendiente se
persiste por ID antes de aceptarla y desaparece al borrar el equipo; la
aceptación añade la membresía de equipo en la misma transacción, y el endpoint
de miembros del equipo devuelve al invitado ya aceptado. La respuesta de
`POST /api/0/organizations/{org}/members/` declara `pending:true`,
`user:null` e `inviteLink:null`, y propietarios, managers y administradores ven
la invitación pendiente en la lista de personas mientras un miembro no la ve.
Las entradas `teamRoles` estrictas, la colisión de invitación pendiente con 409,
la omisión de slugs desconocidos y ajenos, el reenvío deshabilitado con 400 y el
403 de administrador sobre un equipo donde no participa siguen cubiertos por el
owner HTTP.

Mapa de comprobantes:

| Elemento del criterio | Comprobante |
|---|---|
| Restricciones de rol con `manager` en membresías e invitaciones | store `TestManagerInvitationTeamsMigrationPreservesStateAndReopens` |
| Estado previo y recibos 001–061 exactos, checksum de 062 | store `TestManagerInvitationTeamsMigrationPreservesStateAndReopens` |
| Persistencia por ID y cascada al borrar el equipo | store `TestInvitationTeamAssignmentsFollowTeamIDsThroughAcceptance` |
| Aceptación con membresías de equipo en la misma transacción | store `TestInvitationTeamAssignmentsFollowTeamIDsThroughAcceptance`; HTTP `TestCompatibleInvitationHTTPAcceptance` |
| Respuesta con `pending`, `user:null` e `inviteLink:null` | HTTP `TestCompatibleOrganizationInvitationMutationHTTP` |
| Visibilidad para propietario, manager y administrador | HTTP `TestCompatiblePeopleTeamsAndProjectRelationsHTTP` |
| `teamRoles` estricto, 409, 400 de reenvío y 403 de administrador | HTTP `TestCompatibleOrganizationInvitationMutationHTTP` |
| Inspección pública con slugs de equipo y aceptación única | HTTP `TestCompatibleInvitationHTTPAcceptance` |

Evidencia: `test-results/closure-20261025/identidad-091-go.log` con siete
owners y los cambios en
`internal/store/compat_invitations_test.go`,
`internal/server/compat_invitations_test.go`,
`internal/server/compat_management_mutations_test.go` y
`internal/server/compat_people_test.go`.

Límites registrados: el CI público sigue pendiente para la auditoría final.
`gofmt` retiró una línea en blanco sobrante en el owner de personas; no cambia
comportamiento.

### Ejecución 2026-10-26: mutaciones compatibles de equipo

Selección registrada antes de trabajar: CLOSE-identidad-092. Candidata
`726103e`.

Resultado observado: el owner de store comprueba que crear, renombrar y borrar
equipos conserva el identificador, el instante de creación y los enlaces de
proyecto al renombrar, rechaza slugs inválidos y protege el equipo por defecto, y
que el borrado deja el proyecto sin equipo. Las mismas mutaciones por HTTP
responden 201/200/204 con `TeamProjectSchema`, devuelven 409 al repetir slug y
400 con cuerpos malformados o slugs fuera del formato canónico de 50 caracteres,
prohíben la sesión de un miembro y permiten sesiones de propietario,
administrador y manager, además de tokens Bearer con `org:write`. Añadí la
comprobación de que la pertenencia al equipo es solo metadato: un miembro del
equipo que agrupa el proyecto sigue sin acceso a ese proyecto cuando su grant no
lo incluye.

Mapa de comprobantes:

| Elemento del criterio | Comprobante |
|---|---|
| Creación con `{slug}`, respuesta `TeamProjectSchema` | HTTP `TestCompatibleTeamMutationsHTTP`; store `TestCompatibleTeamMutationsPreserveProjectRelations` |
| Slug canónico de hasta 50 caracteres | HTTP `TestCompatibleTeamMutationsHTTP` (400 para mayúsculas, vacío y 51 caracteres) |
| Renombrado conserva id, proyectos y creación | store `TestCompatibleTeamMutationsPreserveProjectRelations` |
| Borrado en cascada con 204 | store `TestCompatibleTeamMutationsPreserveProjectRelations`; HTTP `TestCompatibleTeamMutationsHTTP` |
| Equipo por defecto protegido | store `TestCompatibleTeamMutationsPreserveProjectRelations`; HTTP `TestCompatibleTeamMutationsHTTP` (403) |
| Miembros de sesión prohibidos; owner, manager y admin permitidos | HTTP `TestCompatibleTeamMutationsHTTP`, `TestPeopleAndTeamHTTPContract` |
| Tokens Bearer con `org:write` | HTTP `TestCompatibleTeamMutationsHTTP` |
| Duplicados 409 y cuerpos 400 | HTTP `TestCompatibleTeamMutationsHTTP` |
| La pertenencia no amplía acceso a proyecto o evento | store `TestCompatibleTeamMutationsPreserveProjectRelations` |

Evidencia: `test-results/closure-20261025/identidad-092-go.log` con cinco
owners y el cambio en `internal/store/compat_people_test.go`.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: aceptación conjunta de equipo, token y roles

Selección registrada antes de trabajar: CLOSE-identidad-093. Candidata
`26a888b`.

Resultado observado: la corrida conjunta reúne los owners que sustentan el
criterio. En PostgreSQL real: preservación y reapertura de 040 con recepción de
su checksum, emisión y autenticación de `org:write`, renombrado de equipo con
identificador, instante de creación y proyectos intactos, y protección del
equipo por defecto. En HTTP real: controles de sesión de propietario, manager,
administrador y miembro, diferencia de capacidad entre Bearer `org:read` y
`org:write`, respuesta, privacidad, duplicado y entradas malformadas de
invitaciones, aceptación con asignación de equipos, creación, renombrado y
borrado de equipos con rechazo del equipo por defecto. La gestión de estas
mutaciones en navegador queda fuera de este criterio y se cubrió por separado en
CLOSE-identidad-089 y -090. La base de roles es GlitchTip 6.2.6 estable, con las
tres referencias registradas en el contrato; GoodSentry mantiene la decisión
explícita de transferencia de propiedad e invitaciones de propietario solo para
el propietario, invitaciones de administrador y `org:write` como alias Bearer.
La pertenencia a equipos sigue siendo metadato sin autorización de proyecto ni
de eventos.

Evidencia: `test-results/closure-20261025/identidad-093-go.log` con nueve
owners y los comprobantes individuales de -089 a -092.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: actualización y borrado de organización

Selección registrada antes de trabajar: CLOSE-identidad-094. Candidata
`33d80b1`.

Resultado observado: `PUT /api/0/organizations/{org}/` acepta solo `{name}`,
lo recorta y exige 1..200 caracteres Unicode sin controles: nombre vacío, solo
espacios, controles o 201 ideogramas devuelven 400 igual que un campo
desconocido. El renombrado por administrador, manager o token Bearer con
`org:write` conserva identificador, slug, instante de creación, equipos y
proyectos, mientras un miembro recibe 403 y un token `org:read` también. El
borrado exige el rol de propietario actual: administrador, manager, miembro y un
token `org:write` propiedad de un administrador reciben 403, y el propietario
recibe 204 seguido de 404 al leer la organización. Las cascadas dejan sin filas
los proyectos, equipos y credenciales de la organización.

Mapa de comprobantes:

| Elemento del criterio | Comprobante |
|---|---|
| Solo `{name}`, recorte y 1..200 caracteres sin controles | store `TestCompatibleOrganizationUpdateDeleteAndRoleBoundaries`; HTTP `TestCompatibleOrganizationUpdateDeleteHTTP` |
| Renombrado por propietario, manager, administrador y Bearer | HTTP `TestCompatibleOrganizationUpdateDeleteHTTP` |
| Slug, creación, proyectos y equipos intactos | store `TestCompatibleOrganizationUpdateDeleteAndRoleBoundaries`; HTTP `TestCompatibleOrganizationUpdateDeleteHTTP` |
| Borrado solo para el propietario actual | store `TestCompatibleOrganizationUpdateDeleteAndRoleBoundaries`; HTTP `TestCompatibleOrganizationUpdateDeleteHTTP` |
| 204 y cascadas de proyectos, equipos y credenciales | store `TestCompatibleOrganizationUpdateDeleteAndRoleBoundaries`; HTTP `TestCompatibleOrganizationUpdateDeleteHTTP` |

Evidencia: `test-results/closure-20261025/identidad-094-go.log` con cuatro
owners y los cambios en `internal/store/compat_organization_write_test.go` y
`internal/server/compat_management_mutations_test.go`.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: creación compatible de organización

Selección registrada antes de trabajar: CLOSE-identidad-095. Candidata
`2733df5`.

Resultado observado: `POST /api/0/organizations/` responde 403 con el flag
desactivado antes de leer el cuerpo, también con JSON truncado, y el flag
responde a `GOODSENTRY_ENABLE_ORGANIZATION_CREATION` y a su alias
`ENABLE_ORGANIZATION_CREATION` con la precedencia explícita ya probada en
configuración. Con el flag activo, la creación devuelve 201 con
`OrganizationDetailSchema`, slug canónico, rol de propietario, un solo equipo
por defecto con su membresía y ningún proyecto, y deja intactas las demás
organizaciones, proyectos y equipos. Un propietario o administrador de una
organización existente puede crear; manager y miembro reciben 403; sin sesión,
401; un token `org:read`, 403. El token `org:write` de un administrador crea,
y tras degradar a ese usuario a miembro la misma credencial recibe 403, lo que
demuestra que la membresía de origen se revalida en storage.

Mapa de comprobantes:

| Elemento del criterio | Comprobante |
|---|---|
| Flag y alias con precedencia explícita | config `TestFeatureAliasesAndRuntimeOptions`, `TestExplicitGoodSentryFlagOverridesAlias` |
| 403 antes de leer el cuerpo con el flag apagado | HTTP `TestCreateCompatibleOrganizationHTTP` (cuerpo válido y truncado) |
| 201 con propietario, equipo por defecto y sin proyectos | store `TestCreateCompatibleOrganizationOwnershipTeamAndAuthority`; HTTP `TestCreateCompatibleOrganizationHTTP` |
| Datos existentes intactos | store `TestCreateCompatibleOrganizationOwnershipTeamAndAuthority` (recuentos antes y después) |
| Límites de sesión: propietario y administrador sí; manager y miembro no | store `TestCreateCompatibleOrganizationOwnershipTeamAndAuthority`; HTTP `TestCreateCompatibleOrganizationHTTP` |
| Bearer con `org:write` y membresía de origen revalidada | HTTP `TestCreateCompatibleOrganizationHTTP` |
| Duplicado 409, nombre inválido y cuerpos malformados 400 | store `TestCreateCompatibleOrganizationOwnershipTeamAndAuthority`; HTTP `TestCreateCompatibleOrganizationHTTP` |

Evidencia: `test-results/closure-20261025/identidad-095-go.log` con los
owners de store y HTTP más las pruebas de configuración, y los cambios en
`internal/store/compat_organizations_test.go` y
`internal/server/compat_organizations_test.go`.

Límites registrados: el CI público sigue pendiente para la auditoría final. La
prueba atómica de la transacción se apoya en los recuentos y en el esquema de la
migración; no inyecta un fallo intermedio.

### Ejecución 2026-10-26: aceptación de la vista de organización

Selección registrada antes de trabajar: CLOSE-identidad-096. Candidata
`4f680b6`.

Resultado observado: la corrida de PostgreSQL y HTTP reúne los owners de
renombrado con identificador, slug, creación, equipos y proyectos intactos,
nombres inválidos, límites de propietario, manager, administrador y miembro,
borrado con cascadas y 404 posterior, además de los límites de creación. En
Chromium, con sesión de propietario, la vista nativa «Organization» carga el
nombre real, lo renombra a «Renamed Workspace», el nombre persiste tras recargar
y aparece en la cabecera y el selector; un miembro ve el aviso de que solo el
propietario puede borrar y no tiene el botón; el propietario abre el diálogo de
confirmación y, tras borrar la única organización, la aplicación muestra «No
organization access». Las cuatro capturas de 320/768/1440 no desbordan la página
y no hay errores de consola.

Evidencia: `test-results/closure-20261025/identidad-096-go.log` con cuatro
owners y `test-results/closure-20261025/organization-ui/` con `report.json` y
doce capturas del renombrado, la vista de miembro, la confirmación y el estado
sin organización.

Límites registrados: el CI público sigue pendiente para la auditoría final. El
estado posterior al borrado se capturó con una única organización en una base
desechable.

### Ejecución 2026-10-26: autoridad global separada de la organización

Selección registrada antes de trabajar: CLOSE-identidad-097. Candidata
`01e58bf`.

Resultado observado: el overview de administración conserva su alcance
organizacional y responde 404 con `ENABLE_ADMIN=false`; con el flag activo,
muestra solo las organizaciones donde el actor es propietario o administrador.
La administración de usuarios es una autoridad global distinta: un
administrador de organización ve el overview con 200 pero
`GET /api/v1/admin/users` responde 403 mientras no tenga el bit de instancia,
y un miembro tampoco lo obtiene. Los roles de organización y los scopes de token
Bearer no conceden estas rutas.

Evidencia: `test-results/closure-20261025/identidad-098-go.log` con
`TestAdministrationIsOptInAndOrganizationScoped`,
`TestInstanceUserAdministrationHTTPAuthorizationAndUpdates` y
`TestInstanceAdministrationRequiresFeatureAndProtectsOpenOwner`.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: esquema, bootstrap y CLI de administración

Selección registrada antes de trabajar: CLOSE-identidad-098. Candidata
`01e58bf`.

Resultado observado: la migración 064 añade `is_instance_admin` e
`is_active` sin reescribir cuentas, conserva el owner existente como activo y
sin privilegio global, mantiene sesiones y tokens y deja el recibo con su
checksum; en ese estado heredado sin administradores, la administración de
usuarios responde 403 a todos. El bootstrap de primera cuenta en una base vacía
crea el owner como administrador de instancia, mientras un owner implícito
posterior no se promueve. `goodsentry admin grant --email` promueve una cuenta
existente y activa, no crea cuentas ausentes (404), rechaza cuentas inactivas
(403), es idempotente y habilita la lista al administrador concedido. Un owner
implícito configurado e inactivo devuelve error al arrancar en modo abierto. El
Administration view permite conceder y revocar el privilegio con «Make admin» y
«Remove admin», suspender y reactivar desde el navegador.

Mapa de comprobantes:

| Elemento del criterio | Comprobante |
|---|---|
| Migración 064 aditiva y recibos exactos | store `TestInstanceUserAdminMigration064PreservesExistingAccountsAndCredentials` |
| Estado heredado sin administradores: API no disponible | store `TestInstanceUserAdminMigration064PreservesExistingAccountsAndCredentials` |
| Bootstrap del primer owner y owner implícito posterior no promovido | store `TestInstanceUserAdminBootstrapAndGrant` |
| CLI promueve solo cuentas existentes y activas, idempotente | cmd `TestAdminGrantCommandPromotesExistingActiveAccount`; store `TestInstanceUserAdminBootstrapAndGrant` |
| Arranque rechaza el owner implícito inactivo | store `TestInstanceUserAdminBootstrapAndGrant` y propagación en `cmd/goodsentry/main.go` |
| Conceder y revocar desde la UI | `admin-users-ui/report.json` y capturas 320/768/1440 |

Evidencia: `test-results/closure-20261025/identidad-098-go.log` con los
owners de store, servidor y CLI, más
`test-results/closure-20261025/admin-users-ui/`.

Límites registrados: el CI público sigue pendiente para la auditoría final. El
fallo de arranque se prueba en el owner de store y en la propagación directa del
error en `main`; no se arranca un proceso completo con la cuenta suspendida.

### Ejecución 2026-10-26: API y sección de usuarios de instancia

Selección registrada antes de trabajar: CLOSE-identidad-099. Candidata
`9dcb02b`.

Resultado observado: con `ENABLE_ADMIN=true`, `GET /api/v1/admin/users`
devuelve una página de 25 por defecto con `data`, `total`, `page` y
`perPage`, y cada fila expone solo id, correo, nombre, estado activo, privilegio
de instancia y número de organizaciones; la respuesta no contiene hashes,
generaciones, MFA, credenciales ni estado OAuth. El filtro `query` es una
subcadena sin distinguir mayúsculas sobre correo o nombre, con 100 caracteres
máximo y sin controles, y la paginación acepta `perPage` hasta 100 con páginas
positivas. `PATCH /api/v1/admin/users/{userID}` acepta únicamente booleanos
opcionales `isActive` e `isInstanceAdmin` y rechaza parches vacíos, campos
desconocidos, IDs malformados, usuarios inexistentes y `null` con el error
compartido. La vista embebida añade «Account users» con filtro, paginación,
estado de cuenta, acceso de instancia y acciones; en Chromium el propietario
filtra por correo, concede y revoca el privilegio, suspende y reactiva sin
desbordamiento ni errores de consola.

Mapa de comprobantes:

| Elemento del criterio | Comprobante |
|---|---|
| Lista paginada con la proyección exacta | HTTP `TestInstanceUserAdministrationHTTPAuthorizationAndUpdates` |
| Filtro por subcadena, límite y controles | HTTP `TestInstanceUserAdministrationHTTPAuthorizationAndUpdates` |
| Paginación estricta y páginas fuera de rango | HTTP `TestInstanceUserAdministrationHTTPAuthorizationAndUpdates` |
| PATCH estricto y errores compartidos | HTTP `TestInstanceUserAdministrationHTTPAuthorizationAndUpdates` |
| Sección y mutaciones en la vista | `admin-users-ui/report.json` y capturas 320/768/1440 |

Evidencia: `test-results/closure-20261025/identidad-099-go.log` y
`test-results/closure-20261025/admin-users-ui/`.

Límites registrados: el CI público sigue pendiente para la auditoría final. La
captura de 320 px muestra el encabezado y las primeras secciones; la tabla de
cuentas se revisó en 768 y 1440 y el desbordamiento se comprobó en los tres
anchos.

### Ejecución 2026-10-26: autorización, bloqueo y admisión de instancia

Selección registrada antes de trabajar: CLOSE-identidad-100. Candidata
`f97e55e`.

Resultado observado: cada lista y actualización revalida la cuenta actual contra
storage; un actor suspendido o degradado mientras esperaba el bloqueo compartido
de administración recibe el rechazo al obtener el turno, porque la comprobación
se repite con una instantánea fresca después de `pg_advisory_xact_lock`. Las
mutaciones por sesión exigen CSRF y mismo origen, y `ENABLE_ADMIN=false` deja
las rutas ausentes con 404. En modo abierto el propietario implícito sigue siendo
el actor, conserva CSRF y origen, y no puede desactivarse ni degradarse; un
propietario implícito configurado que esté inactivo hace fallar el arranque en
lugar de reactivarse solo.

Evidencia: `test-results/closure-20261025/identidad-100-go.log` con
`TestInstanceAdminRechecksActorAfterWaitingForSerializationLock`,
`TestInstanceUserAdministrationHTTPAuthorizationAndUpdates` y
`TestInstanceAdministrationRequiresFeatureAndProtectsOpenOwner`.

Límites registrados: el CI público sigue pendiente para la auditoría final. El
fallo de arranque se evidencia en el owner de store y en la propagación directa
del error desde `cmd/goodsentry/main.go`; no se arranca un proceso completo con
la cuenta suspendida.

### Ejecución 2026-10-26: suspensión, últimos administradores y borrado

Selección registrada antes de trabajar: CLOSE-identidad-101. Candidata
`f97e55e`.

Resultado observado: la suspensión incrementa la generación de autenticación,
marca la cuenta inactiva, retira el privilegio de instancia, borra sesiones y
desafíos de acceso y revoca tokens de organización, tokens de cuenta compatibles
y refresh OAuth; la reactivación no revive ninguna credencial ni el privilegio.
Un emisor de credenciales que esperaba detrás de la suspensión observa el estado
inactivo y falla, y la rotación OAuth bloquea la fila del usuario antes del
refresh. Las actualizaciones serializadas rechazan dejar cero administradores
activos, y el borrado de cuenta toma el mismo bloqueo de administración y
devuelve el conflicto de último administrador, también bajo carrera. Las rutas
de creación OIDC e invitaciones respetan el orden de bloqueos de cuenta y
organización.

Mapa de comprobantes:

| Elemento del criterio | Comprobante |
|---|---|
| Último administrador en actualizaciones | store `TestInstanceUserAdminSearchSuspensionAndLastAdmin` |
| Último administrador en borrado de cuenta | store `TestCompatibleUserDeletionRetainsOrganizationOwnerAndCascadesAccount`, `TestConcurrentCompatibleUserDeletionRetainsOneOrganizationOwner` |
| Revocación completa al suspender y no reaparición al reactivar | store `TestInstanceUserAdminSearchSuspensionAndLastAdmin` |
| Emisor esperando detrás de la suspensión | store `TestSuspensionWaitsForCredentialIssuerAndRevokesItsCommit` |
| Rotación OAuth tras el bloqueo de cuenta | store `TestOAuthRefreshRotationLocksUserBeforeRefreshToken` |
| Orden de bloqueos en OIDC e invitaciones | store `TestOIDCAutoCreateSerializesWithFirstOwnerBootstrap`, `TestInvitationSignupWaitsForCreationLockBeforeOrganizationLock`, `TestExistingUserInvitationLocksAccountBeforeOrganization`, `TestNewSignupInvitationDuplicateCheckDoesNotWaitOnLockedAccount` |

Evidencia: `test-results/closure-20261025/identidad-101-go.log` con nueve
owners.

Límites registrados: el CI público sigue pendiente para la auditoría final. La
carrera se prueba con bloqueos reales en PostgreSQL, no con un reloj simulado.

### Ejecución 2026-10-26: serialización del bootstrap y límites de reversión

Selección registrada antes de trabajar: CLOSE-identidad-102. Candidata
`5cacbf9`.

Resultado observado: el bootstrap de primera cuenta y la creación de usuarios
por OIDC o invitación comparten el bloqueo de creación, y los owners comprueban
que la cuenta creada por OIDC espera al bootstrap sin obtener el privilegio, que
la invitación espera al bloqueo de creación antes del de organización y que el
chequeo de duplicados no espera a una cuenta bloqueada. `EnsureDefaultOwner`
es idempotente, repara owner, organización y equipo sin duplicar ni reemplazar
la contraseña, y devuelve error si la cuenta configurada existe inactiva. La
migración 064 no cambia activación ni privilegio: conserva las cuentas, deja al
owner heredado activo y sin administración de instancia, y mantiene su checksum.
La guía de configuración registra la parada de procesos previos a 064, la
reversión no soportada tras suspender una cuenta y la recuperación mediante otro
administrador activo en modo requerido.

Mapa de comprobantes:

| Elemento del criterio | Comprobante |
|---|---|
| Bootstrap serializado con creación de usuario | store `TestOIDCAutoCreateSerializesWithFirstOwnerBootstrap`, `TestInvitationSignupWaitsForCreationLockBeforeOrganizationLock`, `TestExistingUserInvitationLocksAccountBeforeOrganization`, `TestNewSignupInvitationDuplicateCheckDoesNotWaitOnLockedAccount` |
| Solo el primer owner en tabla vacía recibe administración | store `TestInstanceUserAdminBootstrapAndGrant`, `TestOIDCAutoCreateSerializesWithFirstOwnerBootstrap` |
| `EnsureDefaultOwner` idempotente y fallo con cuenta inactiva | store `TestEnsureDefaultOwnerIsIdempotent`, `TestInstanceUserAdminBootstrapAndGrant`; HTTP `TestInstanceAdministrationRequiresFeatureAndProtectsOpenOwner` |
| Migración sin cambios de activación ni privilegio | store `TestInstanceUserAdminMigration064PreservesExistingAccountsAndCredentials` |
| Límites operativos de parada y reversión documentados | `docs/configuration.md` (después de 064 y en modo de autenticación) |

Evidencia: `test-results/closure-20261025/identidad-102-go.log` con ocho
owners y las secciones de configuración citadas.

Límites registrados: el CI público sigue pendiente para la auditoría final. La
parada de procesos previos y la reversión no soportada son restricciones
operativas documentadas; no se simula un binario pre-064.

### Ejecución 2026-10-26: aceptación de administración de instancia

Selección registrada antes de trabajar: CLOSE-identidad-103. Candidata
`87cd9d7`.

Resultado observado: la corrida enfocada reúne migración 064, bootstrap, CLI,
búsqueda y suspensión, último administrador, revalidación del actor tras el
bloqueo, emisor en espera, rotación OAuth, rechazo de identidad OIDC suspendida,
rutas HTTP, modo abierto y concesión desde el CLI. La suite completa
`go test -race -count=1 -tags=integration ./...` pasa con `go vet` y
`go build` previos, sin `DATA RACE` (server 201s, store 386s). La actualización
real desde 063 se ejecutó con el binario previo a 064 y el actual sobre la misma
base: 1 usuario conservado, `is_active` ausente antes y presente después,
migraciones 63→73, cero administradores tras migrar, `admin/users` 403,
`admin grant` promueve al owner existente, la lista responde 200 y un reinicio
posterior conserva el estado. En Chromium, 31 cuentas producen dos páginas: la
primera exige «Previous» deshabilitado, «Next» abre la segunda con la cuenta
restante y la suspensión/reactivación funciona desde esa página, sin
desbordamiento a 320/768/1440.

Evidencia: `test-results/closure-20261025/identidad-103-go.log`,
`full-race-instance-admin.log`, `upgrade-064.log` y
`admin-users-pagination/` con `report.json` y seis capturas.

Bloqueo registrado: el criterio exige `gh` CI e imagen pública y una
actualización Compose con imágenes. No hay autorización de publicación ni una
imagen previa a 064 disponible localmente; el avance local se probó con
binarios sobre PostgreSQL real. Condición de desbloqueo: autorizar CI público y
publicación (o aportar una imagen previa a 064) y repetir la actualización con
las imágenes en Compose. Responsable: instrucción del usuario. Intentos: uno
local con binarios; sin intentos de publicación. Siguiente acción tras
desbloquear: ejecutar CI exacto, publicar y repetir el flujo Compose.

### Ejecución 2026-10-26: modos abierto y autenticado

Selección registrada antes de trabajar: CLOSE-identidad-104. Candidata
`2810041`.

Resultado observado: en modo requerido con base vacía, la página muestra el
formulario de primera configuración, crea el workspace y entra al panel, el
cierre de sesión vuelve al formulario de acceso y el inicio de sesión recupera
el panel; las cuatro pantallas se capturaron a 320/768/1440 sin desbordamiento.
En modo abierto, la aplicación entra directamente al panel con el propietario
implícito, no ofrece control de acceso, expone su token CSRF y acepta una
mutación autenticada con ese token. Los owners HTTP cubren el propietario
implícito y su CSRF, las invitaciones que permanecen en él y los tokens de
cuenta en modo abierto; el owner de store confirma que el propietario por
defecto es idempotente. La guía de configuración mantiene que
`GOODSENTRY_AUTH=required` es necesario antes de exponer el servicio fuera de
una red local de confianza.

Mapa de comprobantes:

| Elemento del criterio | Comprobante |
|---|---|
| Alta de primera cuenta y workspace en modo requerido | `auth-modes/setup-*` |
| Login y logout con sesión recuperada | `auth-modes/{login,dashboard,session}-*` |
| Modo abierto con propietario implícito y sin acceso | `auth-modes/open-dashboard-*`, `open-report.json` |
| CSRF y mutaciones en modo abierto | HTTP `TestOpenAuthenticationUsesImplicitOwnerAndKeepsCSRF`, `TestOpenModeAccountTokens`; navegador `open-report.json` |
| Invitaciones sobre el propietario implícito | HTTP `TestOpenAuthenticationInvitationsStayOnImplicitOwner` |
| Propietario por defecto idempotente | store `TestEnsureDefaultOwnerIsIdempotent` |

Evidencia: `test-results/closure-20261025/identidad-104-go.log` y
`test-results/closure-20261025/auth-modes/` con dos informes y quince capturas.

Límites registrados: el CI público sigue pendiente para la auditoría final.

### Ejecución 2026-10-26: recuperación de contraseña y correo local

Selección registrada antes de trabajar: CLOSE-identidad-105. Candidata
`d624cb0`.

Resultado observado: el formulario de recuperación pide el correo y responde con
el aviso neutro; el correo local llega por la consola con un enlace de un solo
uso, y el navegador completa el cambio de contraseña y entra al panel con la
contraseña nueva. Las tres pantallas (solicitud, formulario de nueva contraseña
y panel posterior) se capturaron a 320/768/1440 sin desbordamiento. El owner HTTP
cubre 202 tanto para correo existente como ausente, la entrega local, el uso
único con rechazo de repetición, la validación de longitud y el origen/CSRF.

Evidencia: `test-results/closure-20261025/identidad-105-go.log` y
`test-results/closure-20261025/password-recovery/` con `report.json` y nueve
capturas.

Límites registrados: el CI público sigue pendiente para la auditoría final. El
correo de consola imprime el cuerpo en quoted-printable sin decodificar; el
guion de aceptación decodifica `=3D` y los saltos suaves antes de abrir el
enlace, igual que un cliente de correo real. No se modificó producto por esto.

### Ejecución 2026-10-26: TOTP y require2FA en navegador

Selección registrada antes de trabajar: CLOSE-identidad-106. Candidata
`5e4f7b7`.

Resultado observado: desde la vista Account, el propietario inicia el alta de
TOTP, recibe un secreto real, confirma con un código calculado a partir de él y
el aviso de habilitación aparece. Tras cerrar sesión, el acceso con contraseña
exige el segundo factor y el mismo autenticador completa el desafío en una
ventana nueva; el panel vuelve con la sesión activa. En la vista Organization,
«Require two-factor authentication for members» se guarda y sigue marcado tras
recargar. Las pantallas de cuenta, desafío, sesión y organización se capturaron
a 320/768/1440 sin desbordamiento ni errores de consola; los códigos de
recuperación no se capturaron. Los owners HTTP cubren el alta y el acceso con
TOTP, el estado disponible sin sesión en modo abierto y la exigencia
organizacional sobre sesiones y tokens Bearer.

Mapa de comprobantes:

| Elemento del criterio | Comprobante |
|---|---|
| Alta de TOTP con secreto y código real | `two-factor/account-*`, `report.json` |
| Acceso con desafío TOTP | `two-factor/challenge-*`, `two-factor/session-*` |
| Persistencia de require2FA | `two-factor/organization-2fa-*` |
| Alta y acceso TOTP por HTTP | HTTP `TestTwoFactorEnrollmentAndLoginHTTP` |
| Estado 2FA sin sesión en modo abierto | HTTP `TestTwoFactorStateAvailableWithoutSessionInOpenMode` |
| require2FA sobre sesiones y Bearer | HTTP `TestOrganizationRequire2FAGatesSessionsAndBearerTokens` |

Evidencia: `test-results/closure-20261025/identidad-106-go.log` y
`test-results/closure-20261025/two-factor/` con `report.json` y doce capturas.

Límites registrados: el CI público sigue pendiente para la auditoría final. El
desafío usa una ventana TOTP posterior al alta porque el servidor rechaza
reutilizar el mismo paso; el guion espera esa ventana.

### Ejecución 2026-10-26: OIDC y aplicaciones SSO

Selección registrada antes de trabajar: CLOSE-identidad-107. Candidata
`f4342f3`.

Resultado observado: los owners locales cubren el flujo OIDC con PKCE, vínculo
de cuenta y sesión, el alta de identidades y la creación automática, las
aplicaciones sociales compatibles por HTTP, el acceso que crea membresía en
GitHub, GitLab, Gitea y DigitalOcean, el rechazo de perfiles no verificados y el
acceso que crea membresía desde una aplicación social. En Chromium, el
propietario añade una aplicación OpenID Connect desde la vista Organization, ve
su callback real, comprueba que el enlace de acceso compartido la ofrece con
«Continue with Local OIDC», la renombra con persistencia y la elimina hasta
dejar la lista vacía; las tres pantallas se capturaron a 320/768/1440 sin
desbordamiento ni errores de consola.

Mapa de comprobantes:

| Elemento del criterio | Comprobante |
|---|---|
| OIDC con PKCE, vínculo y sesión | HTTP `TestOIDCPKCEAccountLinkAndSession` |
| Identidades OIDC, creación y suspensión | store `TestOIDCIdentityLinksAndCreatesAccounts` |
| Aplicaciones sociales compatibles | HTTP `TestCompatibleSocialOAuth2AppsHTTP` |
| Acceso social que crea membresía | HTTP `TestSocialOAuth2SignInCreatesOrganizationMember`, `TestSocialAppSignInCreatesOrganizationMember` |
| Rechazo de perfiles no verificados | HTTP `TestSocialOAuth2RejectsUnverifiedProfiles` |
| Alta, renombrado y borrado en la vista | `sso-ui/report.json` y capturas 320/768/1440 |
| Enlace de acceso compartido | `sso-ui/report.json` (contexto anónimo) |

Evidencia: `test-results/closure-20261025/identidad-107-go.log` y
`test-results/closure-20261025/sso-ui/` con `report.json` y nueve capturas.

Límites registrados: el CI público sigue pendiente para la auditoría final. El
proveedor OIDC y los sociales son locales de prueba; no se acredita un proveedor
público ni un flujo de red externo.

### Ejecución 2026-10-26: organizaciones, equipos y miembros

Selección registrada antes de trabajar: CLOSE-identidad-108. Candidata
`c93b34c`.

Resultado observado: quince owners cubren la visibilidad de organización,
miembro y equipo, el orden y la paginación, los límites de propietario,
manager, administrador y miembro, el detalle y las mutaciones de miembro, las
mutaciones de equipo con el equipo por defecto protegido, la transferencia de
propiedad, el último propietario y su carrera, el renombrado y borrado de
organización con cascadas, y la creación de organización con sus límites. En
Chromium, el propietario ve miembros, invitación pendiente y equipos, crea y
renombra «Alpha team», añade y quita al miembro, cambia su rol a manager y borra
el equipo tras confirmar; las nueve capturas de 320/768/1440 no desbordan la
página ni registran errores de consola.

Mapa de comprobantes:

| Elemento del criterio | Comprobante |
|---|---|
| Visibilidad y esquemas de organización, miembro y equipo | store `TestCompatibleOrganizationPeopleTeamsAndProjects`; HTTP `TestCompatiblePeopleTeamsAndProjectRelationsHTTP` |
| Roles y detalle de miembro | store `TestCompatibleOrganizationMemberDetailAndTeamMembership`; HTTP `TestCompatibleOrganizationMemberManagementHTTP` |
| Mutaciones de equipo y equipo por defecto | store `TestCompatibleTeamMutationsPreserveProjectRelations`; HTTP `TestCompatibleTeamMutationsHTTP` |
| Transferencia de propiedad y último propietario | store `TestSetCompatibleOrganizationOwnerTransfersSingleOwner`, `TestPeopleMembershipsTeamsAndLastOwner`, `TestConcurrentOwnerDemotionsRetainOneOwner`; HTTP `TestCompatibleSetOrganizationOwnerHTTP`, `TestPeopleAndTeamHTTPContract` |
| Organización: renombrado, borrado y creación | store `TestCompatibleOrganizationUpdateDeleteAndRoleBoundaries`, `TestCreateCompatibleOrganizationOwnershipTeamAndAuthority`; HTTP `TestCompatibleOrganizationUpdateDeleteHTTP`, `TestCreateCompatibleOrganizationHTTP` |
| Flujo completo en la vista People | `people108/report.json` y nueve capturas |

Evidencia: `test-results/closure-20261025/identidad-108-go.log` y
`test-results/closure-20261025/people108/`.

Límites registrados: el CI público sigue pendiente para la auditoría final. La
tabla de miembros exige desplazamiento horizontal en 320 px, sin desbordar la
página.

### Comprobante de integración documental

- [x] Integrar el contrato aprobado en los tres artefactos canónicos, con ancla
  `closure-contract-v1`, orden del plan y registro por ID, sin ejecutar producto.
  - Fuente inventariada: `c5964e5d511972f879ee7ca3f9481dbdd1b94bd4`, main,
    con remoto coincidente antes de editar. La fecha de los acuerdos es
    2026-10-25, America/Mexico_City; la revisión de integración es el commit que
    contiene esta entrada, no una candidata de aceptación de producto.
  - Inventario: 598 bloques normativos completos, 22 filas de tablas, 104
    resultados de flujos y ocho excepciones, 732 IDs únicos. Se conservaron
    comandos, controles, procedencia, comprobantes y texto previos íntegros.
  - Verificación documental: IDs/anclas únicos, correspondencia uno a uno con
    el registro, cobertura de bloques fuente y enlaces locales resueltos;
    `git diff --check` sin errores. Comparación exacta contra la instantánea
    previa tras retirar únicamente las anclas nuevas; plan y tareas conservan
    íntegramente sus prefijos anteriores.
  - Concurrencia: índice vacío antes de editar y hash SHA-256 del smoke OTLP
    ajeno sin seguimiento idéntico antes/después. Solo los tres documentos
    pertenecen a este incremento. No se ejecutaron SDKs, tests de producto,
    navegador, aceptación completa ni operaciones sobre Goals. Los estados
    de evidencia de producto siguen pendientes o aplazados por decisión expresa.
