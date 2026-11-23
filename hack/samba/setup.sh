#!/bin/sh
# Prepares and starts the test Samba: users, groups, sample files and two
# smbd instances. The same script runs in the local container (Dockerfile,
# `make dev-samba`) and in CI, where the test job installs Samba from the
# distribution and calls it directly, so the two cannot drift apart.
#
#   setup.sh foreground   smbd on 445 in the foreground, the second in the background (the container)
#   setup.sh daemon       both in the background (CI)
#
# The two instances share users and shares and differ in one setting:
#   445  map to guest = Bad User       (smb.conf)
#   446  map to guest = Bad Password   (smb-bad-password.conf)
#
# The passwords are test values, known to internal/smb's tests and nothing
# else. Needs root, Samba (smbd, smbpasswd) and useradd/groupadd.
set -eu

MODE="${1:?usage: setup.sh foreground|daemon}"
HERE="$(cd "$(dirname "$0")" && pwd)"
CONF=/etc/ferry-test-samba
mkdir -p "$CONF"
cp "$HERE/smb.conf" "$HERE/smb-bad-password.conf" "$CONF/"

getent group family >/dev/null || groupadd --system family
for u in alex sam; do
  id "$u" >/dev/null 2>&1 || useradd --no-create-home --shell /usr/sbin/nologin --gid family "$u"
done
printf 'alex-test-password\nalex-test-password\n' | smbpasswd -c "$CONF/smb.conf" -a -s alex >/dev/null
printf 'sam-test-password\nsam-test-password\n' | smbpasswd -c "$CONF/smb.conf" -a -s sam >/dev/null

mkdir -p /srv/alex/Photos /srv/family /srv/public /srv/scans /srv/sam /srv/hidden
echo "hello from alex" > /srv/alex/hello.txt
echo "family notes" > /srv/family/notes.txt
echo "scan 1" > /srv/scans/scan-001.txt
echo "sam only" > /srv/sam/private.txt
echo "public" > /srv/public/readme.txt
# Files alex sees in a shared folder but may not read: sam's, mode 0600. A
# cache entry sam's requests made must not reach alex (internal/api).
printf '%s' 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==' | base64 -d > /srv/family/sam-only.png
echo "sam's notes" > /srv/family/sam-only.txt
# A file large enough to measure streaming, made once.
[ -f /srv/alex/big.bin ] || head -c 268435456 /dev/urandom > /srv/alex/big.bin
# A folder of 60000 empty files, made once: listings that are larger than a
# batch, larger than the old bound and larger than a browser wants as DOM.
if [ ! -d /srv/alex/Many ]; then
	mkdir -p /srv/alex/Many
	(cd /srv/alex/Many && seq -w 1 60000 | sed 's/^/photo-/; s/$/.jpg/' | xargs touch)
fi
# Snapshots for the Versions share (smb.conf), made anew each time so that
# their times are the ones the tests expect: Docs/letter.txt in three
# states, stable.txt unchanged, gone.txt deleted since, new.txt new since,
# and Sub a nested dataset with a snapshot of its own.
V=/srv/versions
rm -rf "$V" && mkdir -p "$V/Docs" "$V/Sub/Inner"
snap() { d="$1/.zfs/snapshot/autosnap_$2"; mkdir -p "$d"; echo "$d"; }
echo "v3 now" > "$V/Docs/letter.txt"
echo "stable" > "$V/Docs/stable.txt"
echo "new since" > "$V/Docs/new.txt"
echo "inner now" > "$V/Sub/Inner/inner.txt"
S1=$(snap "$V" 2026-11-23_13:00:09_hourly) && mkdir -p "$S1/Docs" "$S1/Sub"
echo "v1" > "$S1/Docs/letter.txt"
echo "stable" > "$S1/Docs/stable.txt"
echo "gone since" > "$S1/Docs/gone.txt"
S2=$(snap "$V" 2026-11-23_14:00:09_hourly) && mkdir -p "$S2/Docs" "$S2/Sub"
echo "v2 middle" > "$S2/Docs/letter.txt"
echo "stable" > "$S2/Docs/stable.txt"
S3=$(snap "$V/Sub" 2026-11-22_00:00:01_daily) && mkdir -p "$S3/Inner"
echo "inner then" > "$S3/Inner/inner.txt"
touch -d '2026-11-23 12:00:00' "$S1/Docs/letter.txt" "$S1/Docs/gone.txt"
touch -d '2026-11-23 13:30:00' "$S2/Docs/letter.txt"
touch -d '2026-11-21 10:00:00' "$S3/Inner/inner.txt"
touch -d '2026-11-20 09:00:00' "$V/Docs/stable.txt" "$S1/Docs/stable.txt" "$S2/Docs/stable.txt"
touch -d '2026-11-23 12:00:00' "$S1/Docs" "$S2/Docs"
touch -d '2026-11-23 15:00:00' "$V/Docs"
chown -R alex:family "$V" && chmod 0700 "$V"
chown -R alex:family /srv/alex /srv/hidden && chmod 0700 /srv/alex
chown -R sam:family /srv/sam && chmod 0700 /srv/sam
chown -R root:family /srv/family && chmod 2775 /srv/family
chown sam:family /srv/family/sam-only.png /srv/family/sam-only.txt && chmod 0600 /srv/family/sam-only.png /srv/family/sam-only.txt
chown -R root:root /srv/scans && chmod 0755 /srv/scans
chown -R nobody /srv/public && chmod 0777 /srv/public

# A tree to search below (internal/smb's search tests): hits at several
# depths and cases, a name written decomposed as macOS writes it, and a
# folder alex may not open, which a search skips and counts.
S=/srv/alex/Search
rm -rf "$S" && mkdir -p "$S/a/b/c" "$S/Privat" "$S/leer"
echo 1 > "$S/Rechnung 2025.pdf"
echo 2 > "$S/a/rechnung strom.txt"
echo 3 > "$S/a/b/c/RECHNUNG.md"
printf 4 > "$S/a/b/$(printf 'Gru\314\210n Rechnung.txt')"
echo 5 > "$S/a/b/notiz.txt"
echo 6 > "$S/Privat/rechnung geheim.txt"
chown -R alex:family "$S" && chown -R sam:family "$S/Privat" && chmod 0700 "$S/Privat"

mkdir -p /var/lib/ferry-test-samba/bad-password /run/ferry-test-samba/bad-password
smbd -D -s "$CONF/smb-bad-password.conf"
case "$MODE" in
  foreground) exec smbd --foreground --no-process-group --debug-stdout -s "$CONF/smb.conf" ;;
  daemon)     smbd -D -s "$CONF/smb.conf" ;;
  *)          echo "unknown mode $MODE" >&2; exit 2 ;;
esac
