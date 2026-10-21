import json
import subprocess
import urllib.request
from pathlib import Path

REVISION = "83d02b691557379e5642b2ddcc05d4914c9b24e2"
EVIDENCE = Path(__file__).resolve().parent
URL = f"https://api.github.com/repos/jerolan/good-sentry/git/trees/{REVISION}?recursive=1"
request = urllib.request.Request(URL, headers={"User-Agent": "GoodSentry-acceptance"})
with urllib.request.urlopen(request, timeout=60) as response:
    remote = json.load(response)
if remote.get("sha") != REVISION or remote.get("truncated"):
    raise RuntimeError("unexpected or truncated remote tree")
remote_blobs = {
    item["path"]: item["sha"]
    for item in remote.get("tree", [])
    if item.get("type") == "blob"
}

raw = subprocess.check_output(
    ["git", "ls-tree", "-r", "--full-tree", REVISION], text=True
)
local_blobs = {}
for line in raw.splitlines():
    metadata, path = line.split("\t", 1)
    _, kind, digest = metadata.split()
    if kind == "blob":
        local_blobs[path] = digest

missing = sorted(local_blobs.keys() - remote_blobs.keys())
extra = sorted(remote_blobs.keys() - local_blobs.keys())
different = sorted(
    path for path in local_blobs.keys() & remote_blobs.keys()
    if local_blobs[path] != remote_blobs[path]
)
result = {
    "revision": REVISION,
    "blobs": len(local_blobs),
    "publicBlobs": len(remote_blobs),
    "exact": not missing and not extra and not different,
    "truncated": remote.get("truncated", False),
    "missing": missing,
    "extra": extra,
    "different": different,
}
(EVIDENCE / "public-tree.json").write_text(json.dumps(remote, indent=2) + "\n")
(EVIDENCE / "public-source.json").write_text(json.dumps(result, indent=2) + "\n")
if not result["exact"]:
    raise RuntimeError(json.dumps(result, indent=2))
print(json.dumps(result, indent=2))
