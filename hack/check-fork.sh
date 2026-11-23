#!/bin/sh
# Holds third_party/go-smb2 to what its PATCHES.md says it is: the upstream
# module at the version go.mod requires, with the diff in PATCHES.md applied
# and nothing else. CI runs it; a change to the copy that is not in the diff,
# or a diff that no longer applies, fails it.
#
#   sh hack/check-fork.sh           # check
#   sh hack/check-fork.sh --update  # write the copy's diff into PATCHES.md
set -eu
cd "$(dirname "$0")/.."
FORK=third_party/go-smb2
MOD=github.com/cloudsoda/go-smb2
VERSION=$(go list -m -f '{{.Version}}' "$MOD")
UPSTREAM=$(GOFLAGS=-mod=mod go mod download -json "$MOD@$VERSION" | sed -n 's/^[[:space:]]*"Dir": "\(.*\)",$/\1/p')
[ -d "$UPSTREAM" ] || { echo "cannot fetch $MOD@$VERSION"; exit 1; }
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
mkdir "$WORK/a" "$WORK/b"
cp -R "$UPSTREAM/." "$WORK/a/"
cp -R "$FORK/." "$WORK/b/"
rm -f "$WORK/b/PATCHES.md"
chmod -R u+w "$WORK"

# The diff lives in PATCHES.md, between the two marker lines.
if [ "${1:-}" = --update ]; then
	(cd "$WORK" && git diff --no-index a b | sed 's#^diff --git a/[ab]/\(.*\) b/[ab]/\(.*\)$#diff --git a/\1 b/\2#; s#^--- a/a/#--- a/#; s#^+++ b/b/#+++ b/#') > "$WORK/diff" || true
	awk -v diff="$WORK/diff" '
		/^<!-- diff:begin -->$/ { print; print "```diff"; while ((getline l < diff) > 0) print l; print "```"; skip = 1; next }
		/^<!-- diff:end -->$/ { skip = 0 }
		!skip' "$FORK/PATCHES.md" > "$WORK/PATCHES.md"
	cp "$WORK/PATCHES.md" "$FORK/PATCHES.md"
	echo "PATCHES.md updated"
	exit 0
fi
awk '/^<!-- diff:begin -->$/ { on = 1; next } /^<!-- diff:end -->$/ { on = 0 } on && !/^```/' "$FORK/PATCHES.md" > "$WORK/diff"
(cd "$WORK/a" && git apply --whitespace=nowarn "$WORK/diff")
if ! diff -r "$WORK/a" "$WORK/b" >/dev/null; then
	echo "$FORK differs from $MOD@$VERSION with PATCHES.md applied:"
	diff -r "$WORK/a" "$WORK/b" | head -40
	exit 1
fi
echo "$FORK is $MOD@$VERSION with PATCHES.md applied"
