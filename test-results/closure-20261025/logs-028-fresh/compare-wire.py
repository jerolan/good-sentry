import base64,gzip,json,subprocess,hashlib
from pathlib import Path
from google.protobuf.json_format import ParseDict
from opentelemetry.proto.collector.logs.v1.logs_service_pb2 import ExportLogsServiceRequest
from opentelemetry.proto.logs.v1.logs_pb2 import LogsData
p=Path('/tmp/goodsentry-closure-028/exporters');raw=subprocess.check_output(['docker','exec','gsclosure028-postgres-1','psql','-U','closure028','-d','closure028','-Atc',"select encode(payload,'hex') from otlp_log_batches where project_id=1 order by id"],text=True);stored=[bytes.fromhex(x) for x in raw.splitlines()];assert len(stored)==8

def adapt(value):
 if isinstance(value,list):return [adapt(v) for v in value]
 if isinstance(value,dict):
  return {k:(base64.b64encode(bytes.fromhex(v)).decode() if k in ('traceId','spanId') and v else adapt(v)) for k,v in value.items()}
 return value
checks=[];idx=0
for name in ('protobuf-identity','protobuf-gzip','json-identity','json-gzip'):
 for i in range(2):
  wire=(p/f'{name}-{i}.wire').read_bytes();wire=gzip.decompress(wire) if name.endswith('gzip') else wire
  request=ExportLogsServiceRequest.FromString(wire) if name.startswith('protobuf') else ParseDict(adapt(json.loads(wire)),ExportLogsServiceRequest())
  expected=LogsData(resource_logs=request.resource_logs);actual=LogsData.FromString(stored[idx]);assert actual==expected,(name,i)
  checks.append({'case':name,'replay':i,'records':sum(len(s.log_records) for r in actual.resource_logs for s in r.scope_logs),'storedSHA256':hashlib.sha256(stored[idx]).hexdigest()});idx+=1
report={'passed':True,'projectId':'1','batches':checks,'method':'decode official wire and stored protobuf into LogsData and compare full message; adapt OTLP JSON hex trace/span IDs for protobuf JSON parser'};Path('/tmp/goodsentry-closure-028/wire-comparison.json').write_text(json.dumps(report,indent=2));print(json.dumps({'passed':True,'batches':len(checks),'records':sum(x['records'] for x in checks)}))
