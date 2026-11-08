"""Read-only: remote gateway + relay state on ECS."""
from __future__ import annotations
import sys
from pathlib import Path
if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
sys.path.insert(0, str(Path.home() / "Desktop" / "repo" / "xallor" / "scripts"))
from ecs_ssh import connect_ecs

REMOTE = r"""
set +e
echo '=== relay service ==='
systemctl is-active xallor-remote-relay 2>/dev/null || true
ss -lnt | grep 8443 || true
curl -sS -H 'X-Forwarded-Proto: https' -w '\nlocal_health:%{http_code}\n' http://127.0.0.1:8443/health || true
echo '=== nginx remote/xr locations ==='
docker exec xallor-api_gateway_1 sh -c 'grep -nE "xr|remote" /etc/nginx/conf.d/*.conf 2>/dev/null | head -n 40' || true
echo '=== public probes ==='
curl -sSk -w '\nxr:%{http_code}\n' https://api.xallor.com/xr/health || true
curl -sSk -w '\nremote_path:%{http_code}\n' https://api.xallor.com/remote/health || true
"""
client = connect_ecs()
try:
    _, stdout, stderr = client.exec_command(REMOTE, get_pty=False, timeout=45)
    sys.stdout.write(stdout.read().decode("utf-8", "replace"))
    err = stderr.read().decode("utf-8", "replace")
    if err.strip():
        sys.stderr.write(err)
    sys.exit(stdout.channel.recv_exit_status())
finally:
    client.close()
