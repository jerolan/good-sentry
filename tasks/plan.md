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

The pinned Node SDK matrix found a concrete gap in step 5: current JavaScript sends span v2 batches rather than transaction envelopes. Implement the specified atomic span storage and segment aggregation next, integrate the envelope handler, then rerun that unchanged SDK acceptance. The telemetry UI can consume the existing aggregate/log APIs in parallel. Artifact upload parsing may proceed independently after its observed bundle contract is recorded.

After that compatibility check, connect the already verified source-map consumer to scoped durable uploads and error ingestion. Prove mapped frames through the native authenticated API before adding compatible releases and the modern CLI chunk/assembly flow. Keep the parser-only proof distinct from that full workflow.

Native source-map upload and mapped capture are verified. Next execute step 6's independent uptime slice while CLI/release and OTLP acceptance remain open. First implement the bounded network probe and PostgreSQL monitor/check/lease contracts in disjoint files; integrate scheduler lifecycle and authenticated routes in the primary. Then connect the Tailwind UI, enable the compose feature and verify actual probes, replica/restart recovery and browser flows. Alert delivery follows its own durable increment.

Risks to resolve early are malformed/binary envelope handling, event retry/concurrency accounting, per-organization isolation, authenticated browser mutations, modern CLI artifact negotiation, native symbolication and WSL container tooling. Start external-contract research early, while the first vertical slice is built.
