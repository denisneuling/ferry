#!/bin/sh
# The previews' load test (docs/operations.md, *Previews of PDFs*): ferry
# under the chart's limits — a 512 MiB cgroup without swap, GOMEMLIMIT
# 450MiB, 2 CPUs, the default thumbnail budget — against the test Samba
# (make dev-samba), with at once a scanned PDF at 640, a wall of 50 JPEG
# thumbnails, a 256 MB download and, with --heic, two 12.6-megapixel HEIC
# photos (one with a thumbnail item, one without) at 640 and 320. Needs
# systemd-run --user with the memory controller, Python 3 with Pillow (to
# make the pictures) and Go.
#
#   hack/loadtest/run.sh [--heic]
set -eu
HERE=$(cd "$(dirname "$0")" && pwd)
WORK=${WORK:-$(mktemp -d)}
cd "$HERE/../.."
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$WORK/ferry-load" ./cmd/ferry
[ -d "$WORK/wasm" ] || "$WORK/ferry-load" precompile "$WORK/wasm"
python3 "$HERE/pictures.py" "$WORK"
cp "$HERE/load.py" "$WORK/"
HEIC=""
if [ "${1:-}" = --heic ]; then
	FERRY_LOADTEST_DIR="$WORK" go test -count=1 -run TestWriteLoadHEIC ./internal/preview >/dev/null
	HEIC="$WORK/photo-thumbnail.heic $WORK/photo-plain.heic"
fi

systemd-run --user --scope --unit=ferry-load -p MemoryMax=512M -p MemorySwapMax=0 \
  env GOMEMLIMIT=${GOMEMLIMIT:-450MiB} GOMAXPROCS=2 FERRY_SMB_SERVER=127.0.0.1:4445 \
  FERRY_LISTEN=127.0.0.1:8197 FERRY_OPS_LISTEN=127.0.0.1:8198 FERRY_LOG_LEVEL=warn \
  FERRY_WASM_DIR="$WORK/wasm" "$WORK/ferry-load" serve > "$WORK/ferry.log" 2>&1 &
sleep 2
PID=$(pgrep -f "$WORK/ferry-load serve" | head -1)
CG=/sys/fs/cgroup$(cut -d: -f3 "/proc/$PID/cgroup")
echo "ferry $PID, memory.max $(cat "$CG/memory.max")"
python3 "$WORK/load.py" "$PID" "$CG" $HEIC || true
kill "$PID" 2>/dev/null || true
cat "$WORK/ferry.log"
