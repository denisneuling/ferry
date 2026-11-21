#!/bin/sh
# Starts the mock NAS: the access-based share enumeration switch, the large
# video (a sparse file of zeros, made here so the image stays small), smbd.
set -eu

case "${MOCK_NAS_ACCESS_BASED_SHARE_ENUM:-no}" in
  yes|no) ;;
  *) echo "MOCK_NAS_ACCESS_BASED_SHARE_ENUM must be yes or no" >&2; exit 2 ;;
esac
printf '[global]\n  access based share enum = %s\n' "${MOCK_NAS_ACCESS_BASED_SHARE_ENUM:-no}" > /etc/samba/mock-nas-abse.conf

video="/srv/alex/Videos/Urlaub am Meer 🌊 2025.mp4"
if [ ! -e "$video" ]; then
  truncate -s "${MOCK_NAS_VIDEO_SIZE:-1536M}" "$video"
  chown alex:family "$video"
fi

echo "mock NAS: Samba $(smbd -V | cut -d' ' -f2), access based share enum = ${MOCK_NAS_ACCESS_BASED_SHARE_ENUM:-no}"
exec smbd --foreground --no-process-group --debug-stdout
