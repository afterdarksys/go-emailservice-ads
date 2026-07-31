#!/usr/bin/env bash
set -euo pipefail

root_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
network_name=gomeow-mailnet

docker network inspect "$network_name" >/dev/null 2>&1 || \
  docker network create --subnet 172.30.0.0/24 "$network_name" >/dev/null

docker build -t goemailservice-gomeow:local "$root_dir"
docker rm -f gomeow-emailservice >/dev/null 2>&1 || true
docker run -d --name gomeow-emailservice --network "$network_name" --ip 172.30.0.10 \
  -v "$root_dir/deploy/mailscript/gomeow.media/emailservice.yaml:/opt/goemailservices/config.yaml:ro" \
  goemailservice-gomeow:local >/dev/null

echo "EmailService backend is running as gomeow-emailservice on $network_name."
echo "Start MailScript on that network with upstream gomeow-emailservice:2525."
