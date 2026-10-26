'use strict';
const component = 'artifact-proof';
function originalFailure() {
  const precedingValue = 'independent source context';
  const order = 42;
  // Known line immediately before the real exception.
  throw new Error("GoodSentry legacy artifact SDK mvpd9qal-442df1b1f7");
}
module.exports = originalFailure;
