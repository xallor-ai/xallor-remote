"""Inspect why /remote hits Hono instead of Relay."""
from __future__ import annotations
import sys
from pathlib import Path
if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
sys.path.insert(0, str(Path.home() / "Desktop" / "repo" / "xallor" / "scripts"))
from ecs_ssh import connect_ecs

REMOTE = r"""
set -euo pipefail
gw=xallor-api_gateway_1
echo '=== full default.conf ==='
docker exec "$gw" cat /etc/nginx/conf.d/default.conf
echo '=== from gateway to relay ==='
docker exec "$gw" wget --header='X-Forwarded-Proto: https' -qO- http://host.docker.internal:8443/health || docker exec "$gw" wget --header='X-Forwarded-Proto: https' -qO- http://172.17.0.1:8443/health || true
echo
echo '=== curl Host api /remote via localhost ==='
curl -sSk -H 'Host: api.xallor.com' https://127.0.0.1/remote/health || true
echo
curl -sSk -H 'Host: api.xallor.com' https://127.0.0.1/xr/health || true
echo
"""
client = connect_ecs()
try:
    _, stdout, stderr = client.exec_command(REMOTE, get_pty=False, timeout=60)
    sys.stdout.write(stdout.read().decode("utf-8", "replace"))
    sys.stderr.write(stderr.read().decode("utf-8", "replace"))
    sys.exit(stdout.channel.recv_exit_status())
finally:
    client.close()
