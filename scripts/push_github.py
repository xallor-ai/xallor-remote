# Push current HEAD to GitHub via API (git://443 is often blocked).
import base64
import json
import subprocess
import sys

sys.stdout.reconfigure(encoding="utf-8", errors="replace")

REPO = "xallor-ai/xallor-remote"
ROOT = subprocess.check_output(["git", "rev-parse", "--show-toplevel"], text=True).strip()


def gh_api(method: str, path: str, payload=None):
    cmd = ["gh", "api", "-X", method, path]
    if payload is None:
        p = subprocess.run(cmd, capture_output=True, text=True, encoding="utf-8")
    else:
        p = subprocess.run(
            cmd + ["--input", "-"],
            input=json.dumps(payload),
            capture_output=True,
            text=True,
            encoding="utf-8",
        )
    if p.returncode != 0:
        raise SystemExit(p.stderr or p.stdout or "gh api failed")
    return json.loads(p.stdout) if p.stdout.strip() else {}


def main():
    raw = subprocess.check_output(["git", "ls-tree", "-r", "-z", "HEAD"], cwd=ROOT)
    tree = []
    for rec in raw.split(b"\0"):
        if not rec:
            continue
        meta, path = rec.split(b"\t", 1)
        _mode, typ, _sha = meta.split()
        if typ != b"blob":
            continue
        path_s = path.decode("utf-8")
        content = subprocess.check_output(["git", "show", f"HEAD:{path_s}"], cwd=ROOT)
        blob = gh_api(
            "POST",
            f"repos/{REPO}/git/blobs",
            {
                "content": base64.b64encode(content).decode("ascii"),
                "encoding": "base64",
            },
        )
        tree.append({"path": path_s, "mode": _mode.decode(), "type": "blob", "sha": blob["sha"]})
        print("blob", path_s)
    tr = gh_api("POST", f"repos/{REPO}/git/trees", {"tree": tree})
    parent = gh_api("GET", f"repos/{REPO}/git/ref/heads/main")["object"]["sha"]
    msg = subprocess.check_output(["git", "log", "-1", "--format=%B"], cwd=ROOT, text=True).strip()
    commit = gh_api(
        "POST",
        f"repos/{REPO}/git/commits",
        {"message": msg, "tree": tr["sha"], "parents": [parent]},
    )
    gh_api(
        "PATCH",
        f"repos/{REPO}/git/refs/heads/main",
        {"sha": commit["sha"]},
    )
    print("pushed", commit["sha"])


if __name__ == "__main__":
    main()
