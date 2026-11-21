# The mock NAS: ferry's stand-in for a household NAS, for development and
# demonstrations. Build from the repository's root:
#
#   docker build -f hack/mock-nas/Dockerfile -t ferry-mock-nas .
#
# Samba 4.15 from Ubuntu 22.04, the oldest Samba ferry is expected to meet;
# hack/samba covers the newer ones in the tests.
ARG GO_IMAGE=docker.io/library/golang:1.27
ARG UBUNTU_IMAGE=docker.io/library/ubuntu:22.04

# The sample files, generated: nothing in them comes from anywhere.
FROM ${GO_IMAGE} AS seed
WORKDIR /src
COPY go.mod go.sum ./
COPY hack/mock-nas/gen ./hack/mock-nas/gen
RUN go run ./hack/mock-nas/gen -out /seed

FROM ${UBUNTU_IMAGE}
RUN apt-get update -qq \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends samba >/dev/null \
 && rm -rf /var/lib/apt/lists/*
# Two fictional people in one household. The passwords are demo values,
# written in hack/mock-nas/README.md.
RUN groupadd --system family \
 && useradd --no-create-home --shell /usr/sbin/nologin --gid family alex \
 && useradd --no-create-home --shell /usr/sbin/nologin --gid family sam \
 && printf 'alex-nas-demo\nalex-nas-demo\n' | smbpasswd -a -s alex >/dev/null \
 && printf 'sam-nas-demo\nsam-nas-demo\n' | smbpasswd -a -s sam >/dev/null
COPY --from=seed /seed/alex /srv/alex
COPY --from=seed /seed/sam /srv/sam
COPY --from=seed /seed/Family /srv/family
COPY --from=seed /seed/Public /srv/public
COPY --from=seed /seed/Scans /srv/scans
# Unix permissions as a NAS sets them: personal shares private, shared ones
# group-writable with the setgid bit, Public readable by all.
RUN chown -R alex:family /srv/alex && chmod -R u+rwX,go-rwx /srv/alex \
 && chown -R sam:family /srv/sam && chmod -R u+rwX,go-rwx /srv/sam \
 && chown -R root:family /srv/family /srv/scans \
 && find /srv/family /srv/scans -type d -exec chmod 2775 {} + \
 && find /srv/family /srv/scans -type f -exec chmod 0664 {} + \
 && chown -R root:root /srv/public && chmod -R a+rX,go-w /srv/public
COPY hack/mock-nas/smb.conf /etc/samba/smb.conf
COPY hack/mock-nas/entrypoint.sh /entrypoint.sh
EXPOSE 445
ENTRYPOINT ["/entrypoint.sh"]
