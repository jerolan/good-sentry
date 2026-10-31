# CI test selection

`ci-non-native.py` runs the integration and race checks for the closure scope.
The native crash-report checks withdrawn by `CLOSE-nativo-029` are listed in
`ci-test-inventory.json`, along with the native-only packages. The dedicated
native demo workflow is disabled. JavaScript source-map tests and tests of
GoodSentry's own APIs remain in the selection.

The inventory is checked before any test runs. When a package or test is added,
removed or renamed, classify the change and update the inventory. This catches
new top-level tests; changes inside existing tests still require review.
The deployment configuration test runs separately so only its symbol-server
cases are omitted. General attachment and project-throttle tests use generic
attachments and endpoints; their historical native-crash receipts remain in the
private evidence archive.

To run the selection locally, provide a disposable PostgreSQL 18 database in
`DATABASE_URL` and run `python3 scripts/ci-non-native.py`. The selection alone
does not establish the remaining browser, SDK, image or publication gates.
