#!/bin/sh
# Starts the mock NAS: the access-based share enumeration switch, the large
# video (a sparse file of zeros) and the large photo archive (empty files),
# both made here so the image stays small, smbd.
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

# A folder far larger than a listing's batch: 60000 photos by name, empty.
archive="/srv/alex/Fotoarchiv"
if [ ! -d "$archive" ]; then
  mkdir -p "$archive"
  (cd "$archive" && seq -w 1 "${MOCK_NAS_ARCHIVE_SIZE:-60000}" | sed 's/^/IMG_/; s/$/.jpg/' | xargs touch)
  chown -R alex:family "$archive"
fi

echo "mock NAS: Samba $(smbd -V | cut -d' ' -f2), access based share enum = ${MOCK_NAS_ACCESS_BASED_SHARE_ENUM:-no}"
exec smbd --foreground --no-process-group --debug-stdout
