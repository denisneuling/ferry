# A Samba for ferry's tests and local runs: fictional users and shares that
# exercise what ferry has to get right — a wrong password Samba would map to
# guest, a share listed but not open to everyone, a hidden share. Never a
# model for a real installation.
ARG ALPINE_IMAGE=docker.io/library/alpine:3.22
FROM ${ALPINE_IMAGE}
RUN apk add --no-cache samba-server samba-common-tools
COPY smb.conf /etc/samba/smb.conf
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
EXPOSE 445
ENTRYPOINT ["/entrypoint.sh"]
