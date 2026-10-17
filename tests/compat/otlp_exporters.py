#!/usr/bin/env python3
"""Actual pinned OTLP/HTTP exporters against an isolated GoodSentry image.

Install opentelemetry-sdk==1.45.1, opentelemetry-exporter-otlp-proto-http==1.45.1
and opentelemetry-exporter-otlp-json-http==0.66b1. Set GOODSENTRY_OTLP_FIXTURE
to private JSON with baseURL, ownerEmail, ownerPassword and orgSlug. Output
contains telemetry but never credentials; keep GOODSENTRY_TEST_OUTPUT private.

Test-audit gate: official serialization/real acknowledgments and readable
retained occurrences own this proof. A transport success with dropped data,
JSON convention mismatch or replay collapse must fail. Pure Go and PG tests
cannot exercise official clients. The observer delegates every HTTP request
to the exporter's real transport and returns its unmodified response.
"""
import gzip
import hashlib
import importlib.metadata
import json
import os
from pathlib import Path
import secrets
import time
from urllib.parse import urlsplit

import requests
from opentelemetry._logs import LogRecord, SeverityNumber
from opentelemetry.sdk._logs import ReadableLogRecord
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.util.instrumentation import InstrumentationScope
from opentelemetry.exporter.otlp.proto.http._log_exporter import OTLPLogExporter as ProtoExporter
from opentelemetry.exporter.otlp.json.http._log_exporter import OTLPLogExporter as JSONExporter
from opentelemetry.exporter.otlp.common.http import Compression
from opentelemetry.proto.collector.logs.v1.logs_service_pb2 import ExportLogsServiceRequest, ExportLogsServiceResponse


def main():
    for package, version in [('opentelemetry-sdk', '1.45.1'), ('opentelemetry-exporter-otlp-proto-http', '1.45.1'), ('opentelemetry-exporter-otlp-json-http', '0.66b1')]:
        assert importlib.metadata.version(package) == version
    fixture = json.loads(Path(os.environ['GOODSENTRY_OTLP_FIXTURE']).read_text())
    base = fixture['baseURL'].rstrip('/')
    parsed = urlsplit(base)
    assert parsed.hostname in ('127.0.0.1', 'localhost', '::1') and parsed.port not in (18080, 18081, 18088)
    output = Path(os.environ.get('GOODSENTRY_TEST_OUTPUT', 'test-results/otlp-exporters'))
    output.mkdir(parents=True, exist_ok=True)
    session = requests.Session()
    response = session.post(base + '/api/v1/login', json={'email': fixture['ownerEmail'], 'password': fixture['ownerPassword']}, timeout=15)
    response.raise_for_status()
    csrf = response.json()['csrfToken']
    org = fixture['orgSlug']
    run = secrets.token_hex(6)
    response = session.post(base + f'/api/v1/organizations/{org}/projects', json={'name': 'Official OTLP ' + run, 'platform': 'python'}, headers={'X-CSRF-Token': csrf}, timeout=15)
    assert response.status_code == 201
    project = response.json()
    key = urlsplit(project['dsn']).username
    report = {'passed': False, 'projectId': project['id'], 'run': run, 'exports': []}
    resource = Resource({'service.name': 'official-otlp-' + run, 'deployment.environment.name': 'compat', 'resource.empty': ''}, schema_url='https://example.invalid/resource')
    scope = InstrumentationScope('goodsentry.compat', version='1', schema_url='https://example.invalid/scope')
    now = time.time_ns()
    for encoding, exporter_type in [('protobuf', ProtoExporter), ('json', JSONExporter)]:
        for compressed in (False, True):
            label = encoding + ('-gzip' if compressed else '-identity')
            body = 'Official OTLP ' + run + ' ' + label + ' <script>telemetry</script>'
            record = ReadableLogRecord(LogRecord(timestamp=now, observed_timestamp=now+7, trace_id=int('1234567890abcdef1234567890abcdef',16), span_id=int('1234567890abcdef',16), trace_flags=1, severity_number=SeverityNumber.WARN, severity_text='WARN', body=body, attributes={'exact':9223372036854775807, 'minimum':-9223372036854775808, 'bytes':b'\x00\x01\xff', 'empty':'', 'nested':{'ready':True, 'items':[1,'x']}}), resource, scope)
            empty = ReadableLogRecord(LogRecord(timestamp=now+1, observed_timestamp=now+8, severity_number=SeverityNumber.INFO, body=b'', attributes={'run':run}), resource, scope)
            batch = [record, record, empty]
            exporter = exporter_type(endpoint=base+'/v1/logs', headers={'x-goodsentry-key':key}, compression=Compression.GZIP if compressed else Compression.NONE, timeout=15)
            transport = exporter._client._transport
            original = transport.request
            observed = []
            def request(method, url, **kwargs):
                result = original(method, url, **kwargs)
                headers = {k.lower():v for k,v in (kwargs.get('headers') or {}).items()}
                wire = bytes(kwargs.get('data') or b'')
                index = len(observed)
                (output/f'{label}-{index}.wire').write_bytes(wire)
                response_body = result.content()
                (output/f'{label}-{index}.response').write_bytes(response_body)
                observed.append({'status':result.status_code, 'media':headers.get('content-type'), 'compression':headers.get('content-encoding',''), 'wireSHA256':hashlib.sha256(wire).hexdigest(), 'wireBytes':len(wire)})
                assert result.status_code == 200
                media = 'application/x-protobuf' if encoding == 'protobuf' else 'application/json'
                assert headers.get('content-type') == media
                assert headers.get('content-encoding', '') == ('gzip' if compressed else '')
                assert result.headers().get('content-type', '').split(';')[0] == media
                raw = gzip.decompress(wire) if compressed else wire
                if encoding == 'protobuf':
                    message = ExportLogsServiceRequest.FromString(raw)
                    assert len(message.resource_logs[0].scope_logs[0].log_records) == 3
                    assert ExportLogsServiceResponse.FromString(response_body).partial_success.rejected_log_records == 0
                else:
                    assert len(json.loads(raw)['resourceLogs'][0]['scopeLogs'][0]['logRecords']) == 3
                    assert json.loads(response_body) == {}
                return result
            transport.request = request
            exporter.export(batch); exporter.export(batch); exporter.shutdown()
            assert len(observed) == 2
            response = session.get(base+f'/api/v1/organizations/{org}/logs',params={'project':project['id'],'query':body},timeout=15)
            response.raise_for_status(); rows = response.json()['data']
            assert len(rows) == 4, (label, len(rows))
            for row in rows:
                assert row['source']=='otlp' and row['projectId']==project['id']
                selected=session.get(base+f"/api/v1/organizations/{org}/logs/{row['id']}",timeout=15);selected.raise_for_status()
                data=selected.json()['data']; retained=data['logRecord']
                assert retained['body']['stringValue']==body
                assert retained['timeUnixNano']==str(now) and retained['observedTimeUnixNano']==str(now+7)
                attrs={entry.get('key',''):entry['value'] for entry in retained['attributes']}
                assert attrs['exact']=={'intValue':'9223372036854775807'} and attrs['minimum']=={'intValue':'-9223372036854775808'}
                assert attrs['bytes']=={'bytesValue':'AAH/'} and attrs['empty']=={'stringValue':''}
                assert retained['traceId']=='1234567890abcdef1234567890abcdef' and retained['spanId']=='1234567890abcdef'
                assert data['resourceSchemaUrl']=='https://example.invalid/resource' and data['scopeSchemaUrl']=='https://example.invalid/scope'
            report['exports'].append({'case':label,'observed':observed,'rows':[row['id'] for row in rows]})
    all_rows=session.get(base+f'/api/v1/organizations/{org}/logs',params={'project':project['id']},timeout=15).json()['data']
    assert len(all_rows)==24
    empty_count = 0
    for row in all_rows:
        if 'Official OTLP ' in row['body']:
            continue
        selected=session.get(base+f"/api/v1/organizations/{org}/logs/{row['id']}",timeout=15)
        selected.raise_for_status()
        assert selected.json()['data']['logRecord']['body']=={'bytesValue':''}
        empty_count += 1
    assert empty_count == 8
    report.update(passed=True, logs=24, acknowledgments=8)
    (output/'report.json').write_text(json.dumps(report,indent=2))
    print(json.dumps({'passed':True,'project':project['id'],'logs':24,'acknowledgments':8,'cases':4}))


if __name__ == '__main__':
    main()
