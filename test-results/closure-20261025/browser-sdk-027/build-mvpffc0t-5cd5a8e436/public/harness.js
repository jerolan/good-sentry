var runButton = document.createElement("button");
runButton.id = "run";
runButton.textContent = "Run the minified entry point";
document.body.appendChild(runButton);
window.__captureStage = function (config) {
  globalThis.Sentry.init({
    dsn: config.dsn, release: config.release, dist: config.dist,
    environment: 'browser-artifact', debug: false, sampleRate: 1,
    tracesSampleRate: 0, sendDefaultPii: false, maxBreadcrumbs: 0, enableLogs: false,
  });
  runButton.addEventListener("click", function () {
    try { window.__runFailure(window.__stageMessage); }
    catch (error) { window.__lastOutcome = { eventID: globalThis.Sentry.captureException(error), stack: String(error.stack || "") }; }
  });
};
