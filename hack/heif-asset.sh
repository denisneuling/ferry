#!/bin/sh
# The HEIF module ferry embeds, as release assets (third_party/heif/README.md):
#
#   hack/heif-asset.sh DIR     writes DIR/ferry-heif.wasm and DIR/ferry-heif-LICENSES.md
#   hack/heif-asset.sh notes   prints the release notes' section about it
set -eu
# DIR is resolved before the cd below: it is relative to where this was run.
case "$1" in
notes) ;;
*) out=$(cd "$1" && pwd) ;;
esac
cd "$(dirname "$0")/../third_party/heif"
sha=$(gunzip -c heif.wasm.gz | sha256sum | cut -c1-64)
if [ "$1" = notes ]; then
	cat <<NOTES

### The HEIF module

\`ferry-heif.wasm\` (sha256 \`$sha\`) is libheif 1.18.2 and libde265 1.0.15, under the GNU LGPL 3.0, compiled to WebAssembly; ferry embeds it for HEIC thumbnails. \`ferry-heif-LICENSES.md\` has the licence texts, the sources and the recipe, and how to run ferry with a module of your own (\`FERRY_HEIF_WASM\`).
NOTES
	exit 0
fi
gunzip -c heif.wasm.gz > "$out/ferry-heif.wasm"
{
	cat README.md
	for f in LICENSE.libheif LICENSE.libde265 LICENSE.gen2brain COPYING.LESSER COPYING; do
		printf '\n---\n\n## %s\n\n```\n' "$f"
		cat "$f"
		printf '```\n'
	done
} > "$out/ferry-heif-LICENSES.md"
