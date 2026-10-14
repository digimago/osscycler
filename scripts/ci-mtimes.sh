#!/usr/bin/env bash
# Gives every tracked file a modification time from its content (its git
# blob hash), and every directory a fixed one, so that Go reuses cached
# test results in CI. go test keys a cached result on the size and
# modification time of each file the tests read; a fresh checkout stamps
# every file with the time of the checkout, so tests that read testdata
# would always run again. With this, an unchanged file has the same time
# in every run and a changed one a new time.
set -euo pipefail

git ls-files -s -z | while IFS= read -r -d '' entry; do
	meta=${entry%%$'\t'*}
	path=${entry#*$'\t'}
	read -r _ blob _ <<<"$meta"
	[[ -e $path || -L $path ]] || continue
	touch -h -d "@$((1000000000 + 0x${blob:0:7}))" -- "$path"
done
find . -path ./.git -prune -o -type d -exec touch -d @1000000000 {} +
