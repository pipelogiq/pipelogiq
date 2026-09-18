#!/usr/bin/env python3
"""Install the pinned, checksum-verified scanner into the ignored .tools directory."""
import hashlib
import io
from pathlib import Path
import platform
import sys
import tarfile
import urllib.request

VERSION = "8.30.1"
SHA256 = {
    "darwin_arm64": "b40ab0ae55c505963e365f271a8d3846efbc170aa17f2607f13df610a9aeb6a5",
    "darwin_x64": "dfe101a4db2255fc85120ac7f3d25e4342c3c20cf749f2c20a18081af1952709",
    "linux_arm64": "e4a487ee7ccd7d3a7f7ec08657610aa3606637dab924210b3aee62570fb4b080",
    "linux_x64": "551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb",
}


def install(root):
    system = platform.system().lower()
    machine = platform.machine().lower()
    arch = "arm64" if machine in ("arm64", "aarch64") else "x64" if machine in ("x86_64", "amd64") else machine
    target = f"{system}_{arch}"
    if target not in SHA256:
        raise ValueError("Unsupported scanner platform: " + target)
    tools = root / ".tools" / f"gitleaks-{VERSION}"
    tools.mkdir(parents=True, exist_ok=True)
    name = f"gitleaks_{VERSION}_{target}.tar.gz"
    archive = tools / name
    if archive.exists():
        payload = archive.read_bytes()
    else:
        url = f"https://github.com/gitleaks/gitleaks/releases/download/v{VERSION}/{name}"
        with urllib.request.urlopen(url, timeout=60) as response:
            payload = response.read()
    if hashlib.sha256(payload).hexdigest() != SHA256[target]:
        raise ValueError("Gitleaks archive checksum mismatch")
    if not archive.exists():
        archive.write_bytes(payload)
    with tarfile.open(fileobj=io.BytesIO(payload), mode="r:gz") as tar:
        member = tar.getmember("gitleaks")
        if not member.isfile():
            raise ValueError("Invalid scanner archive member")
        binary = tar.extractfile(member).read()
    path = tools / "gitleaks"
    if not path.exists() or hashlib.sha256(path.read_bytes()).digest() != hashlib.sha256(binary).digest():
        path.write_bytes(binary)
    path.chmod(0o755)
    return path


if __name__ == "__main__":
    try:
        print(install(Path(__file__).resolve().parents[1]))
    except Exception as error:
        print("Scanner installation failed: " + str(error), file=sys.stderr)
        sys.exit(2)
