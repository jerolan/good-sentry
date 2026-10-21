import gzip
import hashlib
import io
import json
import tarfile
import urllib.parse
import urllib.request
from pathlib import Path

REPOSITORY = "jerolan/good-sentry"
TAG = "main"
REVISION = "9740b434db868c2641e541e983817339834ccfa5"
EVIDENCE = Path(__file__).resolve().parent
LOCAL_BINARIES = {
    "amd64": Path("/tmp/goodsentry-image-amd64-9740b43/goodsentry"),
    "arm64": Path("/tmp/goodsentry-image-arm64-9740b43/goodsentry"),
}
INDEX_TYPES = ",".join([
    "application/vnd.oci.image.index.v1+json",
    "application/vnd.docker.distribution.manifest.list.v2+json",
])
MANIFEST_TYPES = ",".join([
    "application/vnd.oci.image.manifest.v1+json",
    "application/vnd.docker.distribution.manifest.v2+json",
])


def request(url, token=None, accept=None):
    headers = {"User-Agent": "GoodSentry-acceptance"}
    if token:
        headers["Authorization"] = "Bearer " + token
    if accept:
        headers["Accept"] = accept
    with urllib.request.urlopen(urllib.request.Request(url, headers=headers), timeout=60) as response:
        return response.read(), dict(response.headers)


def sha256(data):
    return hashlib.sha256(data).hexdigest()


token_query = urllib.parse.urlencode({"scope": f"repository:{REPOSITORY}:pull"})
token_bytes, _ = request(f"https://ghcr.io/token?{token_query}")
token = json.loads(token_bytes)["token"]
index_bytes, index_headers = request(
    f"https://ghcr.io/v2/{REPOSITORY}/manifests/{TAG}", token, INDEX_TYPES
)
index = json.loads(index_bytes)
index_digest = index_headers.get("Docker-Content-Digest") or "sha256:" + sha256(index_bytes)
latest_bytes, latest_headers = request(
    f"https://ghcr.io/v2/{REPOSITORY}/manifests/latest", token, INDEX_TYPES
)
latest_digest = latest_headers.get("Docker-Content-Digest") or "sha256:" + sha256(latest_bytes)
(EVIDENCE / "public-manifest.json").write_text(json.dumps(index, indent=2) + "\n")

platforms = []
binaries = {}
for descriptor in index["manifests"]:
    platform = descriptor.get("platform", {})
    architecture = platform.get("architecture")
    if platform.get("os") != "linux" or architecture not in LOCAL_BINARIES:
        continue
    manifest_bytes, _ = request(
        f"https://ghcr.io/v2/{REPOSITORY}/manifests/{descriptor['digest']}",
        token,
        MANIFEST_TYPES,
    )
    manifest = json.loads(manifest_bytes)
    config_bytes, _ = request(
        f"https://ghcr.io/v2/{REPOSITORY}/blobs/{manifest['config']['digest']}", token
    )
    config = json.loads(config_bytes)
    (EVIDENCE / f"public-config-{architecture}.json").write_text(
        json.dumps(config, indent=2) + "\n"
    )
    binary = None
    for layer in manifest["layers"]:
        layer_bytes, _ = request(
            f"https://ghcr.io/v2/{REPOSITORY}/blobs/{layer['digest']}", token
        )
        media_type = layer.get("mediaType", "")
        if media_type.endswith("+gzip") or media_type.endswith(".gzip"):
            layer_bytes = gzip.decompress(layer_bytes)
        with tarfile.open(fileobj=io.BytesIO(layer_bytes), mode="r:") as archive:
            for member in archive:
                if member.isfile() and member.name.lstrip("./") == "goodsentry":
                    binary = archive.extractfile(member).read()
    if binary is None:
        raise RuntimeError(f"no /goodsentry binary in linux/{architecture} image")
    local = LOCAL_BINARIES[architecture].read_bytes()
    binaries[architecture] = {
        "sha256": sha256(binary),
        "bytes": len(binary),
        "localSHA256": sha256(local),
        "localBytes": len(local),
        "exactLocalPublic": binary == local,
    }
    platforms.append({
        "architecture": architecture,
        "digest": descriptor["digest"],
        "user": config.get("config", {}).get("User", ""),
        "revision": config.get("config", {}).get("Labels", {}).get(
            "org.opencontainers.image.revision", ""
        ),
    })

registry = {
    "revision": REVISION,
    "anonymous": True,
    "index": index_digest,
    "architectures": platforms,
    "tags": {TAG: index_digest, "latest": latest_digest},
}
(EVIDENCE / "registry.json").write_text(json.dumps(registry, indent=2) + "\n")
(EVIDENCE / "public-binary.json").write_text(
    json.dumps({"revision": REVISION, **binaries}, indent=2) + "\n"
)
(EVIDENCE / "public-pull.log").write_text(
    f"anonymous ghcr pull {REPOSITORY}:{TAG}\n"
    f"index {index_digest}\nlatest {latest_digest}\n"
    + "".join(
        f"linux/{item['architecture']} {item['digest']} user={item['user']} revision={item['revision']}\n"
        for item in platforms
    )
    + "".join(
        f"{arch} binary: {value['bytes']} bytes sha256={value['sha256']} localExact={value['exactLocalPublic']}\n"
        for arch, value in binaries.items()
    )
)

if len(platforms) != 2 or any(item["user"] != "65532:65532" for item in platforms):
    raise RuntimeError("unexpected platform or runtime user")
if latest_digest != index_digest:
    raise RuntimeError("main and latest tags differ")
if any(item["revision"] != REVISION for item in platforms):
    raise RuntimeError("image revision label mismatch")
if any(not item["exactLocalPublic"] for item in binaries.values()):
    raise RuntimeError("public binary differs from local binary")

print(json.dumps({"index": index_digest, "platforms": platforms, "binaries": binaries}, indent=2))
