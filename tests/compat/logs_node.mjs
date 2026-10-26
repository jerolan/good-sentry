import { createRequire } from 'node:module';
// Run against a disposable GoodSentry instance with:
// GOODSENTRY_DSN=<project dsn> GOODSENTRY_SDK_MODULE=<pinned @sentry/node> node tests/compat/logs_node.mjs
const Sentry = createRequire(import.meta.url)(process.env.GOODSENTRY_SDK_MODULE || '@sentry/node');

const dsn = process.env.GOODSENTRY_DSN;
if (!dsn) { console.error('GOODSENTRY_DSN is required'); process.exit(2); }
const run = process.env.GOODSENTRY_RUN || 'run1';
Sentry.init({ dsn, environment: 'logs-node-acceptance', release: 'logs-node@11.7.0', tracesSampleRate: 1, sendDefaultPii: false });
let traceId = '';
Sentry.startSpan({ name: 'logs-node-acceptance', op: 'logs' }, span => {
  traceId = span.spanContext().traceId;
  Sentry.logger.info('Node SDK info log ' + run, { component: 'logs-node', attempt: 7, healthy: false, ratio: 1.5, 'service.name': 'logs-node-service' });
  Sentry.logger.warn('Node SDK warn log ' + run, { reason: 'filter-target', 'service.name': 'logs-node-service' });
  Sentry.logger.error('Node SDK error log ' + run, { attempts: 3, 'service.name': 'logs-node-service' });
});
const flushed = await Sentry.flush(10000);
console.log(JSON.stringify({ traceId, flushed }));

