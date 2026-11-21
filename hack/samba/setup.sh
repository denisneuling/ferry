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
# A file large enough to measure streaming, made once.
[ -f /srv/alex/big.bin ] || head -c 268435456 /dev/urandom > /srv/alex/big.bin
chown -R alex:family /srv/alex /srv/hidden && chmod 0700 /srv/alex
chown -R sam:family /srv/sam && chmod 0700 /srv/sam
chown -R root:family /srv/family && chmod 2775 /srv/family
chown -R root:root /srv/scans && chmod 0755 /srv/scans
chown -R nobody /srv/public && chmod 0777 /srv/public

mkdir -p /var/lib/ferry-test-samba/bad-password /run/ferry-test-samba/bad-password
smbd -D -s "$CONF/smb-bad-password.conf"
case "$MODE" in
  foreground) exec smbd --foreground --no-process-group --debug-stdout -s "$CONF/smb.conf" ;;
  daemon)     smbd -D -s "$CONF/smb.conf" ;;
  *)          echo "unknown mode $MODE" >&2; exit 2 ;;
esac
