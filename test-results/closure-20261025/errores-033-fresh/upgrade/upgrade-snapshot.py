import json,subprocess,sys,os

def q(sql):return subprocess.check_output(['docker','exec','gsclosure033-postgres-1','psql','-U','closure033','-d','closure033_upgrade','-Atc',sql],text=True)
tables=q("select tablename from pg_tables where schemaname='public' order by 1").splitlines()
out={t:[json.loads(r) for r in q(f'SELECT to_jsonb(t)::text FROM "{t}" t').splitlines() if r] for t in tables}
p=sys.argv[1];open(p,'w').write(json.dumps(out,sort_keys=True));os.chmod(p,0o600);print(json.dumps({'tables':len(tables),'rows':sum(map(len,out.values())),'migrations':len(out['schema_migrations'])}))
