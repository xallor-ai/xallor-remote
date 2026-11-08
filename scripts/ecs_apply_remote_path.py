"""Apply /remote on live gateway. Reload only — no compose recreate, no .env rewrite."""
from __future__ import annotations

import sys
from pathlib import Path

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")

XALLOR = Path.home() / "Desktop" / "repo" / "xallor"
SRC = XALLOR / "deploy" / "gateway" / "default.conf.template"

sys.path.insert(0, str(XALLOR / "scripts"))
from ecs_ssh import connect_ecs  # noqa: E402

REMOTE = r"""
set -euo pipefail
cd /opt/xallor
ts=$(date +%Y%m%d%H%M%S)
cp -a deploy/gateway/default.conf.template "deploy/gateway/default.conf.template.bak.remote.$ts"
cp -a /opt/tmp-default.conf.template deploy/gateway/default.conf.template

# Live conf already envsubst'd; patch locations without recreate.
gw=xallor-api_gateway_1
docker cp "$gw:/etc/nginx/conf.d/default.conf" /tmp/default.conf.live
cp -a /tmp/default.conf.live "/tmp/default.conf.live.bak.$ts"

python3 - <<'PY'
from pathlib import Path
p = Path('/tmp/default.conf.live')
text = p.read_text(encoding='utf-8')
# Drop temporary /xr blocks if present
import re
text2 = re.sub(
    r'\n\s*# XallorRemote[^\n]*\n\s*location = /xr/health \{.*?\n\s*\}\n\s*location \^~ /xr \{.*?\n\s*\}\n',
    '\n',
    text,
    flags=re.S,
)
if 'location ^~ /remote' not in text2:
    block = '''
    # XallorRemote 中转（宿主机 :8443）
    location = /remote/health {
        proxy_pass http://host.docker.internal:8443/health;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header X-Forwarded-Proto https;
        proxy_set_header X-Real-IP $remote_addr;
    }

    location ^~ /remote {
        proxy_pass http://host.docker.internal:8443/;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header X-Forwarded-Proto https;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }
'''
    needle = '    location / {'
    idx = text2.find(needle)
    if idx < 0:
        raise SystemExit('cannot find location / in default.conf')
    text2 = text2[:idx] + block + '\n' + text2[idx:]
else:
    # ensure /xr gone
    text2 = re.sub(r'\n\s*location = /xr/health \{.*?\n\s*\}\n', '\n', text2, flags=re.S)
    text2 = re.sub(r'\n\s*location \^~ /xr \{.*?\n\s*\}\n', '\n', text2, flags=re.S)
p.write_text(text2, encoding='utf-8')
print('patched default.conf')
PY

docker cp /tmp/default.conf.live "$gw:/etc/nginx/conf.d/default.conf"
# drop leftover remote-acme vhost (unused subdomain)
docker exec "$gw" rm -f /etc/nginx/conf.d/remote-acme.conf || true
docker exec "$gw" nginx -t
docker exec "$gw" nginx -s reload
rm -f /opt/tmp-default.conf.template

echo '=== probes ==='
curl -sSk -o /dev/null -w 'api:%{http_code}\n' https://api.xallor.com/api/health
curl -sSk -w '\nremote:%{http_code}\n' https://api.xallor.com/remote/health
curl -sSk -o /dev/null -w 'xr_old:%{http_code}\n' https://api.xallor.com/xr/health || true
docker exec "$gw" sh -c 'grep -nE "remote|/xr" /etc/nginx/conf.d/default.conf | head'
"""

if not SRC.is_file():
    raise SystemExit(f"missing {SRC}")

client = connect_ecs()
try:
    sftp = client.open_sftp()
    sftp.put(str(SRC), "/opt/tmp-default.conf.template")
    sftp.close()
    _, stdout, stderr = client.exec_command(REMOTE, get_pty=False, timeout=90)
    sys.stdout.write(stdout.read().decode("utf-8", "replace"))
    err = stderr.read().decode("utf-8", "replace")
    if err.strip():
        sys.stderr.write(err)
    sys.exit(stdout.channel.recv_exit_status())
finally:
    client.close()
