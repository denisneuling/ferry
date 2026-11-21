#!/bin/sh
# Users, groups and sample files for the test shares. The passwords are test
# values, known to the tests (internal/smb/smbtest) and nothing else.
set -eu
addgroup -S family 2>/dev/null || true
for u in alex sam; do
  id "$u" >/dev/null 2>&1 || adduser -D -H -s /sbin/nologin -G family "$u"
done
printf 'alex-test-password\nalex-test-password\n' | smbpasswd -a -s alex >/dev/null
printf 'sam-test-password\nsam-test-password\n' | smbpasswd -a -s sam >/dev/null

mkdir -p /srv/alex/Photos /srv/family /srv/public /srv/scans /srv/sam /srv/hidden
echo "hello from alex" > /srv/alex/hello.txt
echo "family notes" > /srv/family/notes.txt
echo "scan 1" > /srv/scans/scan-001.txt
echo "sam only" > /srv/sam/private.txt
echo "public" > /srv/public/readme.txt
# A file large enough to measure streaming, made once per container.
[ -f /srv/alex/big.bin ] || head -c 268435456 /dev/urandom > /srv/alex/big.bin
chown -R alex:family /srv/alex /srv/hidden && chmod 0700 /srv/alex
chown -R sam:family /srv/sam && chmod 0700 /srv/sam
chown -R root:family /srv/family && chmod 2775 /srv/family
chown -R root:root /srv/scans && chmod 0755 /srv/scans
chown -R nobody:nogroup /srv/public 2>/dev/null || chown -R nobody /srv/public
chmod 0777 /srv/public

exec smbd --foreground --no-process-group --debug-stdout
