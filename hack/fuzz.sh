#!/bin/sh
# Runs every fuzz target in the module for FUZZTIME each (default 30s), one
# after the other: `go test -fuzz` takes one target at a time. A find fails
# the run, and the input that found it is printed, so it can be committed
# under the package's testdata/fuzz as a seed that keeps the bug fixed.
#
#   sh hack/fuzz.sh            # or: make fuzz FUZZTIME=2m
set -eu
time="${FUZZTIME:-30s}"
status=0
for pkg in $(go list ./...); do
	dir=$(go list -f '{{.Dir}}' "$pkg")
	for target in $(grep -ho '^func Fuzz[A-Za-z0-9_]*' "$dir"/*_test.go 2>/dev/null | cut -c6-); do
		echo "== $pkg $target ($time)"
		before=$(find "$dir/testdata/fuzz/$target" -type f 2>/dev/null | sort)
		if ! go test -run '^$' -fuzz "^$target\$" -fuzztime "$time" "$pkg"; then
			status=1
			for f in $(find "$dir/testdata/fuzz/$target" -type f 2>/dev/null | sort); do
				case "$before" in *"$f"*) continue ;; esac
				echo "-- the input, to commit as ${f#"$PWD/"}:"
				cat "$f"
			done
		fi
	done
done
exit $status
