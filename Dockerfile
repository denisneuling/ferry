# syntax=docker/dockerfile:1

# ferry, one static binary on distroless, with its interface compiled in.

# Public base images, so that a build works anywhere. A build that should pull
# through a registry mirror passes its own:
#
#   --build-arg NODE_IMAGE=registry.example.com/library/node:24-alpine
#   --build-arg GO_IMAGE=registry.example.com/library/golang:1.27-alpine
ARG NODE_IMAGE=docker.io/library/node:24-alpine
ARG GO_IMAGE=docker.io/library/golang:1.27-alpine

# ── The interface ──────────────────────────────────────────────────────────
FROM ${NODE_IMAGE} AS ui-builder
WORKDIR /src/ui
COPY ui/package.json ui/package-lock.json ./
# Every package is public: no token, no build secret.
RUN npm ci --no-audit --no-fund
COPY ui/ ./
# The TypeScript API client is generated from the OpenAPI spec; the build
# writes to ../internal/ui/dist/app, once per language.
COPY api/openapi/ferry-v1.yaml ../api/openapi/ferry-v1.yaml
RUN npm run build

# ── ferry ──────────────────────────────────────────────────────────────────

FROM ${GO_IMAGE} AS builder

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILT_AT=unknown

WORKDIR /src
COPY go.mod go.sum ./
# go.mod replaces go-smb2 with this copy (third_party/go-smb2/PATCHES.md).
COPY third_party/go-smb2/ third_party/go-smb2/
RUN go mod download

COPY . .
COPY --from=ui-builder /src/internal/ui/dist/app/ internal/ui/dist/app/
# CGO_ENABLED=0: the binary must run on distroless static. go-smb2 is pure Go,
# so nothing needs cgo.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.builtAt=${BUILT_AT}" \
    -o /ferry ./cmd/ferry
# The previews' WebAssembly, compiled to machine code here rather than in
# the running container: compiling pdfium takes 1.8 s and 340 MB at its
# peak, more than ferry's memory limit can spare next to its thumbnails.
# The code is read-only in the image; ferry trusts it only while its
# marker matches the build (internal/preview).
RUN /ferry precompile /wasm

# nonroot, no shell, no package manager; the chart adds a read-only root
# filesystem and drops every capability.
FROM gcr.io/distroless/static:nonroot
COPY --from=builder /ferry /ferry
COPY --from=builder /wasm /usr/lib/ferry/wasm
ENTRYPOINT ["/ferry"]
CMD ["serve"]
