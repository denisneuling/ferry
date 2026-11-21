# A Samba for ferry's local runs and tests: fictional users and shares that
# exercise what ferry has to get right — wrong passwords and unknown users
# Samba would map to guest, a share listed but not open to everyone, a hidden
# share. setup.sh does the work, as it does in CI. Never a model for a real
# installation.
ARG ALPINE_IMAGE=docker.io/library/alpine:3.22
FROM ${ALPINE_IMAGE}
RUN apk add --no-cache samba-server samba-common-tools shadow
COPY smb.conf smb-bad-password.conf setup.sh /opt/ferry-test-samba/
EXPOSE 445 446
ENTRYPOINT ["/opt/ferry-test-samba/setup.sh", "foreground"]
