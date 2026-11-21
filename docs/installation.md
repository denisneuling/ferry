# Installation

ferry needs one thing: an SMB server to log people in to. It keeps nothing —
no database, no volume, no secret — so installing it is installing a
stateless web server.

## Helm

The chart and the image are public on GitHub's registry:

```sh
helm install ferry oci://ghcr.io/metapatrol/charts/ferry \
  --namespace ferry --create-namespace \
  --set smb.server=nas.example.lan \
  --set smb.serverName="The NAS" \
  --set ingress.enabled=true \
  --set ingress.className=nginx \
  --set 'ingress.hosts[0].host=files.example.com' \
  --set 'ingress.hosts[0].paths[0].path=/'
```

The chart's [README](../charts/ferry/README.md) lists every value. The ones
worth deciding:

- **`smb.server`** — required. Any SMB2/3 server; [samba.md](samba.md) has
  the Samba settings that make the experience better.
- **TLS.** ferry speaks plain HTTP; the Ingress or the gateway in front
  terminates TLS. Without TLS, passwords and the session cookie cross the
  network readable — acceptable on a home LAN at most, and then a
  deliberate choice.
- **`trustedProxies`** — the Ingress controller's addresses. Without them,
  every person behind it shares its address, and so its login rate limit.
- **`smb.shares`** — to offer only some of the server's shares.
- **`limits.maxUpload`** — no limit by default.
- **Memory.** The defaults fit a household ([operations.md](operations.md#memory));
  more people or larger photos mean raising `resources.limits.memory` and
  `goMemLimit` together with `thumbnails.memory`.

One replica, always: sessions live in the process's memory. An upgrade logs
everyone out; nothing else is lost.

## Metrics

Prometheus metrics are on the Service's port `metrics` (8081), path
`/metrics` — [operations.md](operations.md#metrics) lists them. The chart
leaves scraping to the installation: a ServiceMonitor, a PodMonitor or a
static target, as its monitoring wants.

## Without Kubernetes

The image runs anywhere:

```sh
docker run -d --name ferry -p 8080:8080 --read-only --cap-drop ALL \
  -e FERRY_SMB_SERVER=nas.example.lan ghcr.io/metapatrol/ferry:latest
```

and so does the binary of a GitHub release: `FERRY_SMB_SERVER=nas.example.lan
ferry serve`. [configuration.md](configuration.md) has every variable.
