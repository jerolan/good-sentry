// Bundle with the pinned browser SDK:
//   npm install --prefix /tmp/gs-browserpkg @sentry/browser@11.7.0 esbuild
//   /tmp/gs-browserpkg/node_modules/.bin/esbuild entry.js --bundle --format=iife --minify --outfile=build/sentry-11.7.0.js
// The CDN bundle.min.js at 11.7.0 does not ship the log sender; the npm build does.
import * as Sentry from '@sentry/browser';

window.Sentry = Sentry;
