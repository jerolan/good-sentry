# GoodSentry implementation order

Authority: ../SPEC.md. The user approved the capability map and authorized specification, planning and execution on 2026-10-14, then named the product GoodSentry. No further routine phase approvals are required. Changes to scope or consequential design decisions still follow the specification.

1. Establish an executable Go application, validated environment configuration, PostgreSQL migration startup and a reproducible single-image build. Prove readiness against real PostgreSQL.
2. Deliver setup, login, organization/project creation and DSN display through API and UI. Prove isolation, session lifecycle and first-owner bootstrap closure.
3. Implement Sentry authentication and store/envelope framing as a bounded package while event normalization/grouping is built independently. Integrate durable error ingestion, issue list and event detail. Prove real SDK error capture, duplicate retry behavior and restart persistence before expanding telemetry.
4. Add issue filters and triage, comments, assignment and merge/unmerge as independently verified increments. Add releases and JavaScript source maps, then prove mapped production frames and modern CLI compatibility.
5. Deliver transactions/spans and logs from ingestion through aggregates and UI. Verify percentiles from known durations and log/trace correlation. Add native OTLP log ingestion and retention.
6. Deliver monitors and scheduler with durable checks, heartbeat expiry, TCP and transitions. Then add frequency/transition alerts and durable email/webhook delivery with retries.
7. Complete management API parity, native debug-file/minidump symbolication, integrations, optional MCP, OAuth/OIDC and cold archival. Resolve each unknown against public contracts and runtime evidence before claiming support.
8. Run the full SDK/browser/container acceptance matrix, retention/recovery/concurrency checks and measured memory workload. Finish documentation with supported versions, operating limits and reproducible commands. Audit every SPEC.md requirement before marking the goal complete.

Parallel execution is limited to disjoint files with defined contracts. Protocol decoding, pure event normalization and static frontend assets can be delegated after contracts are fixed. The primary owns PostgreSQL integration, auth, routes, consequential decisions, spec changes, diff review and final verification. Workers do not expand scope, delegate or commit shared work.

Every implementation increment leaves Go build/tests passing and gets an intention-focused commit. Tasks track exact evidence. Keep implementation and documentation truthful when later capabilities are still pending. Compile the user-authorized Tailwind CSS into embedded assets and verify the existing browser workflow after that stylesheet change.

Span v2/segment aggregation, native source-map upload-to-mapped-capture, uptime coordination and generic automatic alert delivery are verified prerequisites. Their historical RED/GREEN, SDK, replica/restart and browser evidence remains in tasks/todo.md; parser-only artifact evidence does not establish CLI endpoint support.

Absolute time-window/service filters, stored occurrence detail and aggregate child-operation statistics are verified through PostgreSQL, authenticated HTTP, responsive UI, official Go/Node clients and the restarted image. Cross-project aggregate semantics and immutable sample identity remain intact. Next deliver the specified trace reconstruction/waterfall: upgrade legacy projection to version2 without blocking version1 statistics, then build the snapshot-based narrow graph/forest and paginated metadata/raw/error reads, then authenticated routes and responsive trace navigation. Verify duplicate/missing IDs, cross-project ambiguity, cycles/deep chains, timing and pagination before actual SDK/image upgrade/restart acceptance. Continue the remaining issue/release/CLI, OTLP/retention and SSL/provider work before integrations and the final release audit.

The user's publication increment is verified: reviewed code and README are public at jerolan/good-sentry, and the original static site is live through GitHub Pages Actions. Actual remote CI and deployed-browser checks pass; tasks/todo.md records their evidence. Resume the transaction/span detail and remaining product parity work in the order above. Keep the public status and compatibility claims aligned with verified behavior as later increments land.

Risks to resolve early are malformed/binary envelope handling, event retry/concurrency accounting, per-organization isolation, authenticated browser mutations, modern CLI artifact negotiation, native symbolication and WSL container tooling. Start external-contract research early, while the first vertical slice is built.
