# Releases, source maps and attachments

[Documentation](README.md) · [Project home](../README.md)

Use the official Sentry CLI or the native interface for the workflows documented here. See [API tokens](configuration.md#scoped-api-tokens) for credential setup.

## Release automation

In **API tokens**, create a token with `org:read`, `project:read` and the optional `project:releases` permission. Choose the projects it may access. A shared release can be changed only when the token covers every associated project.

The official `sentry-cli 3.8.0` has passed this workflow against the application image, including local Git comparison with the preceding release:

```sh
export SENTRY_URL=http://localhost:8000
export SENTRY_ORG=your-organization
export SENTRY_PROJECT=your-project
export SENTRY_AUTH_TOKEN=your-token
version="$(git rev-parse HEAD)"

sentry-cli releases new "$version"
sentry-cli releases set-commits "$version" --local
sentry-cli releases finalize "$version"
sentry-cli deploys new --release "$version" --env production
sentry-cli deploys list --release "$version"
```

Create, finalize, list, info, archive, restore and delete also pass with the issued credential. Revocation disables subsequent requests. Release deletion removes its metadata and release-specific source maps while preserving captured events and stacks. Repository integration with `--auto` remains pending.

## JavaScript source maps

With the same CLI configuration and `project:releases` permission, inject your built files and upload their artifact bundle:

```sh
sentry-cli sourcemaps inject ./dist
sentry-cli sourcemaps upload --wait ./dist
```

Deploy those injected files so the SDK can attach their Debug IDs to captured exceptions. The official CLI `3.8.0` and Node SDK `11.7.0` have passed this complete workflow, including the original file, line and source context, preserved minified stack, duplicate uploads and project isolation. Uploading the same build under additional release/dist selectors preserves its usable identity.

CLI `3.8.0` requires a server URL at the origin root, such as
`http://localhost:8000/`. It rejects `SENTRY_URL` values containing a path
prefix before making an HTTP request.

Optional `--release` and `--dist` values must match your SDK configuration. An upload with `--release` does not create release metadata; a separate release command or fresh SDK capture does. The release/dist workflow without injection also passes when the artifact URL prefix matches the emitted filename. Incorrect version/distribution selectors remain unmapped.

Chunks persist in PostgreSQL so interrupted uploads can resume after a restart. Temporary staging has a configurable quota and expiry, with automatic cleanup in the Go process. Native debug-file and minidump uploads remain pending.

## Event attachments

Open an issue to list attachments for its displayed retained event. The panel
shows names, types, sizes and upload dates, with 25 files per page. Long names
can be expanded. Downloaded files use a safe filename and an explicit binary
download; their contents are never rendered as a preview.

Organization members can read the files through their current signed-in
session. An SDK DSN or management bearer token does not grant access to the
native download routes.

Sentry-compatible tools can list event attachments at
`GET /api/0/projects/{org}/{project}/events/{event_id}/attachments/`
and retrieve one at `attachments/{attachment_id}/`. Use an API token with
`project:read` or `project:write`, restricted to the relevant project. The
compatible API returns stable numeric IDs, attachment types, sizes, creation
dates and SHA-1 checksums. Native content IDs remain unchanged.

Add `?download` to retrieve the original bytes. Any supplied value, including
`download=false`, requests a download. `HEAD` returns the download headers and
size without a body. `query` filters filenames by case-insensitive substring;
`query=is:screenshot` selects `event.attachment` named `screenshot.png`.
Follow the `Link` header for pagination, optionally setting `per_page` from 1
to 100. Long metadata is truncated in responses, with
`metadataTruncated:true`; stored originals and checksums stay intact.
