"""Put /remote on the :443 server block. Reload only."""
from __future__ import annotations

import sys
from pathlib import Path

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")

sys.path.insert(0, str(Path.home() / "Desktop" / "repo" / "xallor" / "scripts"))
from ecs_ssh import connect_ecs  # noqa: E402

CONF = r'''# 由官方 nginx 镜像 entrypoint 做 envsubst（环境变量见 docker-compose.gateway.yml）
# 公网 HTTPS 在此终止；上游 apps/api 仍为容器内 HTTPS :8788（与 dev-api/certs 或生产证书一致）
#
# :80 仅用于 Let's Encrypt HTTP-01 与跳转 HTTPS。勿删 ACME location，否则无法 webroot 续期。

server {
    listen 80;
    server_name api.xallor.com;

    location ^~ /.well-known/acme-challenge/ {
        root /var/www/certbot;
        default_type "text/plain";
    }

    location / {
        return 301 https://$host$request_uri;
    }
}

server {
    listen 443 ssl;
    http2 on;
    server_name api.xallor.com;

    ssl_certificate     /etc/nginx/certs/fullchain.pem;
    ssl_certificate_key /etc/nginx/certs/privkey.pem;

    # XallorRemote 中转（宿主机 :8443）。与 /api 分流，不另开 HTTP 执行面。
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

    location / {
        proxy_pass https://api:8788;
        proxy_ssl_verify off;
        proxy_http_version 1.1;

        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
    }
}
'''

REMOTE = r"""
set -euo pipefail
gw=xallor-api_gateway_1
docker cp /opt/tmp-default.conf "$gw:/etc/nginx/conf.d/default.conf"
cp -a /opt/tmp-default.conf /opt/xallor/deploy/gateway/default.conf.live.remote
# Keep template in sync for future recreates (envsubst vars restored on host template already)
docker exec "$gw" nginx -t
docker exec "$gw" nginx -s reload
rm -f /opt/tmp-default.conf
echo '=== probes ==='
curl -sSk -w '\napi:%{http_code}\n' https://api.xallor.com/api/health | tail -n 1
curl -sSk -w '\nremote:%{http_code}\n' https://api.xallor.com/remote/health
"""

tmp = Path.home() / "AppData" / "Local" / "Temp" / "xr-default.conf"
tmp.write_text(CONF, encoding="utf-8", newline="\n")

client = connect_ecs()
try:
    sftp = client.open_sftp()
    sftp.put(str(tmp), "/opt/tmp-default.conf")
    sftp.close()
    _, stdout, stderr = client.exec_command(REMOTE, get_pty=False, timeout=60)
    sys.stdout.write(stdout.read().decode("utf-8", "replace"))
    sys.stderr.write(stderr.read().decode("utf-8", "replace"))
    sys.exit(stdout.channel.recv_exit_status())
finally:
    client.close()
