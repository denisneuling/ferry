# ferry

ferry — SMB shares in the browser; log in with your Samba account and see exactly what Samba lets you see, with no users, no database and nothing stored

![Version: 0.3.0](https://img.shields.io/badge/Version-0.3.0-informational?style=flat-square) ![AppVersion: 0.3.0](https://img.shields.io/badge/AppVersion-0.3.0-informational?style=flat-square)

ferry logs people in to an SMB server with their own accounts and shows
them, in the browser, exactly what the server lets them see. It keeps
nothing: no users, no database, no volumes, no secrets — sessions live in
the process's memory. An optional cache of thumbnails and details
(`cache.enabled`) is the one exception: a volume and a key the chart makes.

```sh
helm install ferry oci://ghcr.io/metapatrol/charts/ferry --version 0.3.0 \
  --set smb.server=nas.example.lan \
  --set ingress.enabled=true --set 'ingress.hosts[0].host=files.example.com' \
  --set 'ingress.hosts[0].paths[0].path=/' \
  --set 'ingress.tls[0].hosts[0]=files.example.com' \
  --set 'ingress.tls[0].secretName=files-example-com-tls'
```

`smb.server` is required; the chart refuses to render without it. The
image is `ghcr.io/metapatrol/ferry` at the chart's appVersion, linux/amd64.

## What the chart decides

- **One replica.** Sessions are in memory; a second replica would not know
  them. An upgrade logs everyone out and loses nothing else.
- **No privileges.** Non-root, read-only root filesystem, every capability
  dropped, no service-account token, no volume — but the cache's, when it
  is on.
- **The cache, when on.** A ReadWriteOnce claim mounted at
  `/var/cache/ferry`, a Secret with the key that names and encrypts the
  entries (made once, kept across upgrades by `lookup`; or
  `cache.existingSecret`), and the `Recreate` strategy, so that the old pod
  lets go of the volume before the new one starts.
- **Memory.** The defaults — a 512 MiB limit, `GOMEMLIMIT` at 450 MiB, a CPU
  limit — follow [docs/operations.md](https://github.com/metapatrol/ferry/blob/main/docs/operations.md#memory):
  they fit a household with 2 CPUs, a dozen sessions and ten transfers at
  once with the default thumbnail budget. Raise the limit with the budget
  (`thumbnails.memory`), never the budget alone; keep a CPU limit, since Go
  sizes GOMAXPROCS from it and a download's memory grows with GOMAXPROCS.
- **Metrics** are on the Service's port named `metrics`, path `/metrics`.
  How they are scraped is the installation's business: the chart brings no
  ServiceMonitor and no annotations.
- **TLS** belongs to the Ingress or whatever is in front: without it,
  passwords and the session cookie cross the network readable.

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` |  |
| cache.enabled | bool | `false` | Keep thumbnails and details on a volume, encrypted, across restarts and for everyone who may read the file (FERRY_CACHE_DIR). Off, ferry writes nothing anywhere. Needs a PersistentVolumeClaim; the Deployment then replaces its pod rather than rolling, as the volume is ReadWriteOnce. |
| cache.existingSecret | string | `""` | A Secret holding the key that names and encrypts the entries, at least 32 bytes, under `key` (FERRY_CACHE_KEY). Empty: the chart makes one and keeps it across upgrades. A new key makes every entry unreadable, which costs only their making again. |
| cache.maxBytes | string | `"1Gi"` | The most the entries may take (FERRY_CACHE_MAX_BYTES), the least recently used removed beyond it. Keep it below `cache.persistence.size`. |
| cache.persistence.existingClaim | string | `""` | A claim to use instead of making one. |
| cache.persistence.size | string | `"2Gi"` | The volume's size. |
| cache.persistence.storageClass | string | `""` | The StorageClass; empty uses the cluster's default. |
| containerSecurityContext.allowPrivilegeEscalation | bool | `false` |  |
| containerSecurityContext.capabilities.drop[0] | string | `"ALL"` |  |
| containerSecurityContext.readOnlyRootFilesystem | bool | `true` |  |
| cookieSecure | string | `"auto"` | Whether the session cookie is Secure: `auto` (when the request came over HTTPS, as X-Forwarded-Proto says), `true` or `false` (FERRY_COOKIE_SECURE). |
| extraEnv | list | `[]` | Further environment variables for the container. |
| goMemLimit | string | `"450MiB"` | The Go runtime's soft memory limit (GOMEMLIMIT), a little below `resources.limits.memory`, so that the garbage collector works harder before the kernel would kill the pod — and every session with it. Empty leaves it unset. |
| image.pullPolicy | string | `"IfNotPresent"` |  |
| image.repository | string | `"ghcr.io/metapatrol/ferry"` | The image; the default is the public one the GitHub mirror publishes. |
| image.tag | string | `""` | The tag; empty means the chart's appVersion. |
| imagePullSecrets | list | `[]` | Secrets for pulling from a private registry. |
| ingress.annotations | object | `{}` |  |
| ingress.className | string | `""` |  |
| ingress.enabled | bool | `false` | An Ingress for the interface. TLS belongs here or in front: without it, passwords and the session cookie cross the network readable. |
| ingress.hosts | list | `[]` | Hosts, e.g. `[{host: files.example.com, paths: [{path: /, pathType: Prefix}]}]`. |
| ingress.tls | list | `[]` |  |
| limits.maxArchive | int | `10000` | Entries in one ZIP download, folders' contents counted, refused whole beyond it (FERRY_MAX_ARCHIVE). |
| limits.maxDelete | int | `10000` | Entries in one recursive delete, refused whole beyond it (FERRY_MAX_DELETE). |
| limits.maxListing | int | `200000` | Entries in one folder listing (FERRY_MAX_LISTING). A listing is streamed, so this protects the browser, not ferry's memory. |
| limits.maxUpload | string | `"0"` | The largest upload, e.g. `4Gi`; `0` for none (FERRY_MAX_UPLOAD). |
| limits.requestTimeout | string | `"60s"` | Every API request but transfers, and each single SMB call (FERRY_REQUEST_TIMEOUT). |
| limits.stallTimeout | string | `"60s"` | A transfer that moves no bytes for this long ends (FERRY_STALL_TIMEOUT). |
| log.format | string | `"json"` | `json` or `text` (FERRY_LOG_FORMAT). |
| log.level | string | `"info"` | `debug`, `info`, `warn` or `error` (FERRY_LOG_LEVEL). |
| login.ratePerAddress | int | `10` | Login attempts per minute from one client address (FERRY_LOGIN_RATE_ADDRESS). |
| login.ratePerUser | int | `5` | Login attempts per minute for one username (FERRY_LOGIN_RATE_USER). |
| login.timeout | string | `"10s"` | Connecting and the session setup together (FERRY_LOGIN_TIMEOUT). |
| nodeSelector | object | `{}` |  |
| podAnnotations | object | `{}` |  |
| podLabels | object | `{}` |  |
| podSecurityContext.fsGroup | int | `65532` |  |
| podSecurityContext.fsGroupChangePolicy | string | `"OnRootMismatch"` |  |
| podSecurityContext.runAsGroup | int | `65532` |  |
| podSecurityContext.runAsNonRoot | bool | `true` |  |
| podSecurityContext.runAsUser | int | `65532` |  |
| podSecurityContext.seccompProfile.type | string | `"RuntimeDefault"` |  |
| resources | object | `{"limits":{"cpu":"2","memory":"512Mi"},"requests":{"cpu":"50m","memory":"64Mi"}}` | Resources. The memory limit follows docs/operations.md: a household with 2 CPUs, a dozen sessions and ten transfers at once fits in 512 MiB with the default thumbnail budget. Keep a CPU limit: Go sizes GOMAXPROCS from it, and a download's memory grows with GOMAXPROCS. |
| service.metricsPort | int | `8081` | The port of /metrics (and the probes), named `metrics`. How it is scraped is the installation's business. |
| service.port | int | `80` | The port of the interface and the API. |
| service.type | string | `"ClusterIP"` |  |
| session.idleTimeout | string | `"30m"` | A session without a request for this long ends (FERRY_IDLE_TIMEOUT). |
| session.lifetime | string | `"12h"` | A session ends this long after its login, whatever its activity (FERRY_SESSION_LIFETIME). |
| session.maxSessions | int | `200` | Sessions at most, each with `smb.connections` SMB connections (FERRY_MAX_SESSIONS). |
| smb.connections | int | `3` | SMB connections per login, 1 to 4: one for browsing, the others for thumbnails, details and content; `1` puts everything on one (FERRY_SMB_CONNECTIONS). |
| smb.server | required | `""` | The SMB server, `host` or `host:port` (FERRY_SMB_SERVER). |
| smb.serverName | string | `""` | What the login page calls the server; the host of `smb.server` when empty (FERRY_SERVER_NAME). |
| smb.shares | list | `[]` | The shares this ferry offers, compared without case; empty offers all the server lists (FERRY_SHARES). |
| thumbnails.concurrency | int | `2` | Thumbnails decoded at once (FERRY_THUMBNAIL_CONCURRENCY). |
| thumbnails.maxBytes | string | `"64Mi"` | The largest image, or recording's cover, read for a thumbnail (FERRY_THUMBNAIL_MAX_BYTES). |
| thumbnails.maxPixels | int | `50000000` | The most pixels an image may have to be decoded (FERRY_THUMBNAIL_MAX_PIXELS). |
| thumbnails.memory | string | `"320Mi"` | The memory all thumbnails in the making may take, by estimate (FERRY_THUMBNAIL_MEMORY). The largest use of memory in ferry: keep it below `resources.limits.memory`, with room for the rest (docs/operations.md). |
| tolerations | list | `[]` |  |
| tracing.otlpEndpoint | string | `""` | An OTLP/HTTP endpoint for traces (OTEL_EXPORTER_OTLP_ENDPOINT); empty sends none. |
| trustedProxies | list | `[]` | Addresses or CIDRs of the proxies in front of ferry whose X-Forwarded-For it believes (FERRY_TRUSTED_PROXIES). Without them, every client behind the ingress shares its address — and its login rate. |
