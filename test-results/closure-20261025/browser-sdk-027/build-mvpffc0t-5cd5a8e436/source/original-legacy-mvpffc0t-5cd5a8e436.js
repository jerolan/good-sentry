'use strict';
const component = 'browser-artifact-legacy';
function legacyFailure(message) {
  const precedingValue = 'independent browser source context';
  const order = 42;
  // Known line immediately before the real browser exception.
  throw new Error(message);
}
window.__runFailure = legacyFailure;
