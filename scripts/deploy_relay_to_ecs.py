"""把本机交叉编译的 Linux Relay 二进制放到 Xallor 生产 ECS。

只上传二进制 + systemd 单元。不碰 /opt/xallor、不传 .env、不改网关证书。
仅接受显式指定的可信 HTTPS 反代。调用前必须核对代理的实际源 IP/CIDR，
并用防火墙隔离后端端口；不再提供裸公网明文默认部署。

环境与 Xallor 相同：~/.ssh/xallor_ecs_ed25519 或 XALLOR_ECS_PASSWORD。
"""
from __future__ import annotations

import sys
import argparse
import time
from pathlib import Path
from relay_deploy_config import relay_unit

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")

ROOT = Path(__file__).resolve().parents[1]
BIN = ROOT / "dist" / "xallor-remote-linux-amd64"
REMOTE_TMP = "/opt/tmp-xallor-remote"

REMOTE = r"""
set -euo pipefail
install -d -m 0755 /opt/xallor-remote/bin /var/lib/xallor-remote
if [ -f /opt/xallor-remote/bin/xallor-remote ]; then
  cp -a /opt/xallor-remote/bin/xallor-remote "/opt/xallor-remote/bin/xallor-remote.bak.$(date +%Y%m%d%H%M%S)"
fi
mv /opt/tmp-xallor-remote /opt/xallor-remote/bin/xallor-remote
chmod 0755 /opt/xallor-remote/bin/xallor-remote
file /opt/xallor-remote/bin/xallor-remote
/opt/xallor-remote/bin/xallor-remote --help >/dev/null
if [ -f /etc/systemd/system/xallor-remote-relay.service ]; then
  cp -a /etc/systemd/system/xallor-remote-relay.service "/etc/systemd/system/xallor-remote-relay.service.bak.$(date +%Y%m%d%H%M%S)"
fi
cat >/etc/systemd/system/xallor-remote-relay.service <<'EOF'
__RELAY_UNIT__EOF
systemctl daemon-reload
systemctl enable xallor-remote-relay
systemctl restart xallor-remote-relay
sleep 1
systemctl is-active xallor-remote-relay
ss -lnt | grep 8443 || true
code=$(curl -sS -o /dev/null -w '%{http_code}' \
  -H 'Connection: Upgrade' -H 'Upgrade: websocket' \
  -H 'X-Forwarded-Proto: https' \
  -H 'Sec-WebSocket-Version: 13' -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' \
  http://127.0.0.1:8443/ || echo fail)
echo "ws_upgrade:$code"
echo '=== deploy done ==='
"""

def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--trusted-proxy', action='append', required=True,
                        help='Verified gateway source IP/CIDR; repeat for multiple proxies')
    args = parser.parse_args()
    unit = relay_unit(args.trusted_proxy)
    sys.path.insert(0, str(Path.home() / "Desktop" / "repo" / "xallor" / "scripts"))
    from ecs_ssh import HOST, connect_ecs
    if not BIN.is_file():
        raise SystemExit(f"missing {BIN} — first: GOOS=linux GOARCH=amd64 go build")
    head = BIN.read_bytes()[:4]
    if head != b"\x7fELF":
        raise SystemExit(f"{BIN} is not a Linux ELF binary")
    print(f"[deploy-relay] {BIN} ({BIN.stat().st_size} bytes) -> {HOST}", flush=True)
    client = connect_ecs()
    try:
        sftp = client.open_sftp()
        sftp.put(str(BIN), REMOTE_TMP)
        sftp.close()
        print("[deploy-relay] uploaded, installing…", flush=True)
        _, stdout, stderr = client.exec_command(REMOTE.replace('__RELAY_UNIT__',unit), get_pty=False, timeout=120)
        channel = stdout.channel
        while True:
            if channel.recv_ready():
                sys.stdout.write(channel.recv(4096).decode("utf-8", "replace"))
                sys.stdout.flush()
            if channel.recv_stderr_ready():
                sys.stderr.write(channel.recv_stderr(4096).decode("utf-8", "replace"))
                sys.stderr.flush()
            if channel.exit_status_ready() and not channel.recv_ready() and not channel.recv_stderr_ready():
                break
            time.sleep(0.2)
        while channel.recv_ready():
            sys.stdout.write(channel.recv(4096).decode("utf-8", "replace"))
        while channel.recv_stderr_ready():
            sys.stderr.write(channel.recv_stderr(4096).decode("utf-8", "replace"))
        code = channel.recv_exit_status()
        if code != 0:
            sys.exit(code)
    finally:
        client.close()
    print("[deploy-relay] OK", flush=True)


if __name__ == "__main__":
    main()
