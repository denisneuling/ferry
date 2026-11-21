# syntax=docker/dockerfile:1

# ferry, one static binary on distroless. The interface's build stage is added
# with ui/ (as in websearch's Dockerfile) and compiled into the binary.

# Public base images, so that a build works anywhere. A build that should pull
# through a registry mirror passes its own:
#
#   --build-arg GO_IMAGE=registry.example.com/library/golang:1.27-alpine
ARG GO_IMAGE=docker.io/library/golang:1.27-alpine

FROM ${GO_IMAGE} AS builder

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILT_AT=unknown

WORKDIR /src
COPY go.mod ./
RUN go mod download

COPY . .
# CGO_ENABLED=0: the binary must run on distroless static. go-smb2 is pure Go,
# so nothing needs cgo.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.builtAt=${BUILT_AT}" \
    -o /ferry ./cmd/ferry

# nonroot, no shell, no package manager; the chart adds a read-only root
# filesystem and drops every capability.
FROM gcr.io/distroless/static:nonroot
COPY --from=builder /ferry /ferry
ENTRYPOINT ["/ferry"]
CMD ["version"]
