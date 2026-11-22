# Installation

ferry needs one thing: an SMB server to log people in to. It keeps nothing —
no database, no volume, no secret — so installing it is installing a
stateless web server. The one exception is optional: a cache of thumbnails
and details on a volume (`cache.enabled`, [operations.md](operations.md#cache)),
which makes thumbnails come at once after a restart and for everyone who may
read the file.

## Helm

The chart and the image are public on GitHub's registry. Pin a version —
the [releases](https://github.com/metapatrol/ferry/releases) list them; the
chart's version is the release's, and it runs the image of that release
unless `image.tag` says otherwise:

```sh
helm install ferry oci://ghcr.io/metapatrol/charts/ferry --version 0.1.0 \
  --namespace ferry --create-namespace \
  --set smb.server=nas.example.lan \
  --set smb.serverName="The NAS" \
  --set ingress.enabled=true \
  --set ingress.className=nginx \
  --set 'ingress.hosts[0].host=files.example.com' \
  --set 'ingress.hosts[0].paths[0].path=/' \
  --set 'ingress.tls[0].hosts[0]=files.example.com' \
  --set 'ingress.tls[0].secretName=files-example-com-tls'
```

The TLS secret is the Ingress controller's or cert-manager's business; the
chart only names it.

The chart's [README](../charts/ferry/README.md) lists every value. The ones
worth deciding:

- **`smb.server`** — required; the chart refuses to install without it.
  Any SMB2/3 server, `host` or `host:port`; [samba.md](samba.md) has
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
everyone out; nothing else is lost — the cache, when there is one, survives
it, and losing the cache costs only the time to make its entries again.

## Metrics

Prometheus metrics are on the Service's port `metrics` (8081), path
`/metrics` — [operations.md](operations.md#metrics) lists them. The chart
leaves scraping to the installation: a ServiceMonitor, a PodMonitor or a
static target, as its monitoring wants.

## Without Kubernetes

The image (linux/amd64) runs anywhere:

```sh
docker run -d --name ferry -p 8080:8080 --read-only --cap-drop ALL \
  -e FERRY_SMB_SERVER=nas.example.lan ghcr.io/metapatrol/ferry:0.1.0
```

Tags are the release (`0.1.0`), its minor line (`0.1`) and `latest`.
A GitHub release also carries static binaries for linux/amd64 and
linux/arm64, with `SHA256SUMS`:

```sh
sha256sum -c --ignore-missing SHA256SUMS
chmod +x ferry-linux-arm64
FERRY_SMB_SERVER=nas.example.lan ./ferry-linux-arm64 serve
```

A binary has no compiled code for PDF and HEIC previews, which the image
ships; make it once, where ferry can read it, or PDFs show an icon:

```sh
./ferry-linux-arm64 precompile /var/lib/ferry/wasm
FERRY_WASM_DIR=/var/lib/ferry/wasm FERRY_SMB_SERVER=nas.example.lan ./ferry-linux-arm64 serve
```

ferry then listens on `:8080`, and on `:8081` for the probes and the
metrics. The same rule holds without Kubernetes: TLS in front, from a
reverse proxy. [configuration.md](configuration.md) has every variable.
