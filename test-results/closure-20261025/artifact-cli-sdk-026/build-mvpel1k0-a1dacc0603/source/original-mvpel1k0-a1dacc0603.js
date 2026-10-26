'use strict';
const component = 'artifact-proof';
function originalFailure() {
  const precedingValue = 'independent source context';
  const order = 42;
  // Known line immediately before the real exception.
  throw new Error("GoodSentry artifact SDK mvpel1k0-a1dacc0603");
}
module.exports = originalFailure;
