# Beacon

Automatic DNS server for Docker containers. Resolves container names to their internal IP addresses without port mapping or editing `/etc/hosts`.

## How it works

Beacon listens to Docker events and automatically registers DNS records when containers start or stop. Each container gets a DNS name based on its `beacon.host` label.

```
my-app.beacon        → 172.17.0.2  (web container)
mysql.my-app.beacon  → 172.17.0.3  (MySQL container)
redis.my-app.beacon  → 172.17.0.4  (Redis container)
```

No port conflicts. No proxy. Direct connection to container IP.

## Installation

### Linux

#### 1. Start Beacon

> Port 53 is occupied by `systemd-resolved` on most modern Linux systems. Beacon uses port `5053` by default to avoid the conflict.

```bash
docker run -d \
  --name beacon \
  --restart unless-stopped \
  --log-opt max-size=1m \
  --log-opt max-file=3 \
  -p 5053:53/udp \
  -v /var/run/docker.sock:/var/run/docker.sock \
  ghcr.io/harbor-hub/beacon:latest \
    --addr 0.0.0.0:53 \
    --domain beacon,lh \
    --verbose
```

When running outside Docker, set upstream DNS explicitly to avoid a forwarding loop with `systemd-resolved`:

```bash
beacon --addr 0.0.0.0:5053 --upstream 8.8.8.8
```

#### 2. Configure DNS (Ubuntu)

Create `systemd-resolved` config file:

```conf
# /etc/systemd/resolved.conf.d/beacon.conf
[Resolve]
DNS=127.0.0.1:5053
# Optional: uncomment to use host DNS inside containers (e.g. docker run --dns <docker-bridge-gateway>)
#DNSStubListenerExtra=<docker-bridge-gateway>
Domains=~beacon ~lh
```

Add one `~<tld>` entry per domain configured in `BEACON_DOMAIN`.

Replace `<docker-bridge-gateway>` with the IP of your Docker bridge gateway:

```bash
# usually 172.17.0.1
docker network inspect bridge --format '{{ (index .IPAM.Config 0).Gateway }}'
```

Restart `systemd-resolved`:

```sh
sudo systemctl restart systemd-resolved
```

### macOS

#### 1. Start Beacon

On macOS, port 53 is typically free, so Docker can bind directly:

```bash
docker run -d \
  --name beacon \
  --restart unless-stopped \
  --log-opt max-size=1m \
  --log-opt max-file=3 \
  -p 53:53/udp \
  -v /var/run/docker.sock:/var/run/docker.sock \
  ghcr.io/harbor-hub/beacon:latest \
    --addr 0.0.0.0:53 \
    --domain beacon,lh \
    --verbose
```

#### 2. Configure DNS

Create a resolver file for each domain configured in `BEACON_DOMAIN`:

```bash
sudo mkdir -p /etc/resolver
sudo tee /etc/resolver/beacon <<EOF
nameserver 127.0.0.1
EOF
sudo tee /etc/resolver/lh <<EOF
nameserver 127.0.0.1
EOF
```

## Usage

Add a `beacon.host` label to your container:

```yaml
services:
  mysql:
    image: mysql
    labels:
      - "beacon.host=mysql.my-app.beacon"

  redis:
    image: redis
    labels:
      - "beacon.host=redis.my-app.beacon"

  web:
    image: nginx
    labels:
      - "beacon.host=my-app.beacon"
```

Connect directly by name:

```
mysql.my-app.beacon:3306      → MySQL
redis.my-app.beacon:6379      → Redis
http://my-app.beacon          → web container port 80
```

No `ports:` mapping needed.

### Multiple hostnames

```yaml
labels:
  - "beacon.host=my-app.beacon,my-app.lh"
```

A comma-separated list registers all names to the same container IP.

### Brace expansion

Brace expansion generates multiple hostnames from a single pattern, keeping labels concise.

**Alternation** — `{a,b,c}` expands to each alternative:

```yaml
# registers: web.my-app.lh, api.my-app.lh, admin.my-app.lh
labels:
  - "beacon.host={web,api,admin}.my-app.lh"
```

**Numeric ranges** — `{N..M}` expands to every integer from N to M inclusive:

```yaml
# registers: node1.lh, node2.lh, …, node16.lh
labels:
  - "beacon.host=node{1..16}.lh"
```

Both forms can be combined in the same label:

```yaml
labels:
  - "beacon.host=my-app.beacon,{foo,bar,baz}.lh,node{1..4}.lh"
```

## Configuration

All options can be set via flags or environment variables.

| Flag | Variable | Default | Description |
|---|---|---|---|
| `--addr` | `BEACON_ADDR` | `0.0.0.0:5053` | DNS listen address |
| `--domain` | `BEACON_DOMAIN` | `beacon` | DNS zones to intercept (comma-separated) |
| `--upstream` | `BEACON_UPSTREAM` | auto | Upstream DNS servers (comma-separated). When unset, read from `/etc/resolv.conf`; falls back to `8.8.8.8, 8.8.4.4` if unavailable. |
| `--verbose` | `BEACON_VERBOSE` | `false` | Log DNS queries |
