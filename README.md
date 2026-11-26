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

> Port 53 is occupied by `systemd-resolved` on most modern Linux systems, so the container's DNS port is mapped to host port `5053`.

```bash
docker run -d \
  --name beacon \
  --restart unless-stopped \
  --log-opt max-size=1m \
  --log-opt max-file=3 \
  -p 127.0.0.1:5053:53/udp \
  -v /var/run/docker.sock:/var/run/docker.sock \
  ghcr.io/harbor-hub/beacon:latest \
    --dns-listen :53 \
    --dns-domain beacon,lh \
    --verbose
```

Outside Docker:

```bash
beacon --dns-listen 127.0.0.1:5053
```

Beacon answers only its domains; other names are forwarded only with `?upstream=` on `--dns-listen`. To also serve containers started with `--dns 172.17.0.1` (the Docker bridge gateway), add a listener there that forwards everything else, without listening on the LAN:

```bash
beacon --dns-listen 127.0.0.1:5053 --dns-listen '172.17.0.1:53?upstream=8.8.8.8'
```

Do not use `upstream=auto` outside Docker: `/etc/resolv.conf` points to `systemd-resolved`, which may send queries back to Beacon.

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

Add one `~<tld>` entry per domain configured in `--dns-domain`.

Replace `<docker-bridge-gateway>` with the IP of your Docker bridge gateway:

```bash
# usually 172.17.0.1
docker network inspect bridge --format '{{ (index .IPAM.Config 0).Gateway }}'
```

Restart `systemd-resolved`:

```sh
sudo systemctl restart systemd-resolved
```

#### Next to another local DNS server

`systemd-resolved` merges all files in `resolved.conf.d` into one server list and sends every routed domain to the current server of that list. If another file there routes other domains to another local server, Beacon's domains may end up there, or the other domains at Beacon. Give Beacon its own link instead, a dummy interface managed by `systemd-networkd`:

```ini
# /etc/systemd/network/10-beacon.netdev
[NetDev]
Name=beacon0
Kind=dummy
```

```ini
# /etc/systemd/network/10-beacon.network
[Match]
Name=beacon0

[Network]
# resolved uses a link for DNS only when it has a global-scope address;
# 192.0.2.53 is TEST-NET-1 (documentation range, never routed).
Address=192.0.2.53/32
DNS=127.0.0.1:5053
Domains=~beacon ~lh
LinkLocalAddressing=no
ConfigureWithoutCarrier=yes
```

Remove `beacon.conf` from `resolved.conf.d`. If you use `DNSStubListenerExtra`, keep it there on its own:

```conf
# /etc/systemd/resolved.conf.d/beacon.conf
[Resolve]
DNSStubListenerExtra=<docker-bridge-gateway>
```

Then:

```bash
sudo systemctl enable --now systemd-networkd   # manages only beacon0; NetworkManager keeps the rest
sudo systemctl restart systemd-resolved
resolvectl status beacon0                      # Current Scopes: DNS, DNS Servers: 127.0.0.1:5053
```

### macOS

#### 1. Start Beacon

> Port 53 can be occupied on macOS, so the container's DNS port is mapped to host port `5053`.

```bash
docker run -d \
  --name beacon \
  --restart unless-stopped \
  --log-opt max-size=1m \
  --log-opt max-file=3 \
  -p 127.0.0.1:5053:53/udp \
  -v /var/run/docker.sock:/var/run/docker.sock \
  ghcr.io/harbor-hub/beacon:latest \
    --dns-listen :53 \
    --dns-domain beacon,lh \
    --verbose
```

#### 2. Configure DNS

Create a resolver file for each domain configured in `--dns-domain`:

```bash
sudo mkdir -p /etc/resolver
sudo tee /etc/resolver/beacon <<EOF
nameserver 127.0.0.1
port 5053
EOF
sudo tee /etc/resolver/lh <<EOF
nameserver 127.0.0.1
port 5053
EOF
```

#### 3. Verify

`dig` and `nslookup` bypass `/etc/resolver`, so they won't use these files. Test with the system resolver instead:

```bash
dscacheutil -q host -a name my-app.beacon
```

Or query Beacon directly:

```bash
dig @127.0.0.1 -p 5053 my-app.beacon
```

#### Container IP routing

Docker Desktop runs containers inside a virtual machine, so container IPs (e.g. `172.17.0.2`) are not reachable from the macOS host by default. Names will resolve, but connections will fail. To route traffic to container IPs, use a tool such as [docker-mac-net-connect](https://github.com/chipmk/docker-mac-net-connect), or a Docker runtime that supports it out of the box, such as [OrbStack](https://orbstack.dev).

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

### Multiple networks

A container on several networks has several IPs. Beacon answers with its IP on the first network in `--network` (default `bridge`) the container is on; if none, with its IP on the first network by name.

Add a shared network when containers not on `bridge` (e.g. Compose services) must be reachable by another container:

```bash
docker network create --subnet 172.30.0.0/24 shared
docker network connect shared beacon   # then recreate Beacon with --network bridge,shared
```

Attach those containers to `shared`; containers on `bridge` keep their `bridge` IP. A container that must reach all of these IPs needs both networks. On `shared`, other containers reach Beacon's API by name, e.g. with `--api-listen :80`: `http://beacon/api/records`.

Beacon reads the IP when a container starts: after changing its networks, restart the container.

### Containers

Containers can use Beacon as their DNS server directly, on any OS. Give Beacon a fixed IP on the [shared network](#multiple-networks) (`--ip` needs a network created with `--subnet`) and forward other names:

```bash
docker network connect --ip 172.30.0.53 shared beacon   # instead of the plain connect above
```

```bash
    --dns-listen ':53?upstream=auto' \
    --network shared,bridge \
```

```bash
docker run --network shared --dns 172.30.0.53 my-app
```

- `upstream=auto` forwards every other name to the nameservers in Beacon's `/etc/resolv.conf`; without `upstream` they get no answer. A listener with an upstream also serves TCP.
- `shared` comes first in `--network`: these containers are on `shared` only, so Beacon must answer with IPs on `shared`. Containers only on `bridge` still get their `bridge` IP.
- The host keeps using the same listener through `-p 127.0.0.1:5053:53/udp`; it sends only Beacon's domains, so nothing is forwarded for it.

### Records API

With `--api-listen`, Beacon serves its records over HTTP, so other tools can use them:

```bash
curl http://127.0.0.1:8080/api/records
```

```json
{"records": [
  {"host": "my-app.beacon", "target": "172.17.0.2", "container": "my-app", "labels": {"proxy.port": "3000"}}
]}
```

Labels are included only when their key starts with the `--record-labels` prefix (e.g. `proxy.`). With `--api-token`, requests to `/api/*` must send `Authorization: Bearer <token>`.

> The API exposes your container map. Do not publish the API port on all interfaces: in Docker, use `--api-listen :8080` without `-p`, or `-p 127.0.0.1:8080:8080`.

## Configuration

All options can be set via flags or environment variables.

| Flag | Variable | Default | Description |
|---|---|---|---|
| `--dns-listen` | `BEACON_DNS_LISTEN` | `:5053` | DNS listen address, `addr[?upstream=auto\|IP[:port]]`, repeatable or comma-separated. `upstream` (repeatable) forwards names outside `--dns-domain`, `auto` to the nameservers in `/etc/resolv.conf`; without it they get no answer. A listener with an upstream also serves TCP. E.g. `:53?upstream=auto` |
| `--dns-domain` | `BEACON_DNS_DOMAIN` | `beacon` | DNS zones to intercept (comma-separated) |
| `--network` | `BEACON_NETWORK` | `bridge` | Docker networks in order of preference (comma-separated) whose container IP is used; otherwise the first network by name |
| `--record-labels` | `BEACON_RECORD_LABELS` | | Include container labels with this prefix in records (empty = none) |
| `--api-listen` | `BEACON_API_LISTEN` | | API listen address, HTTP (empty = disabled) |
| `--api-token` | `BEACON_API_TOKEN` | | Bearer token required for `/api/*` (empty = no auth) |
| `--verbose` | `BEACON_VERBOSE` | `false` | Log DNS queries |
