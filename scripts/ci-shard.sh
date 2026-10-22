#!/usr/bin/env bash
# Runs the race tests of one shard, so that CI can spread the slow world
# packages over several runners (each world build already uses every core
# of one, so running their tests side by side on one runner gains little).
#
#   scripts/ci-shard.sh rest      every package but the sharded ones
#   scripts/ci-shard.sh PKG I N   shard I (from 0) of N of package PKG
#
# A test goes to the shard its name hashes to, so adding a test moves no
# other. Otherwise as make race: the tests of a package run one after the
# other, and RACE_FLAGS is passed on (CI empties it to reuse cached
# results).
set -euo pipefail

sharded='/internal/worlds?$'
flags="-race -timeout 20m ${RACE_FLAGS--count=1}"

if [[ $1 == rest ]]; then
	# shellcheck disable=SC2046,SC2086 # word splitting wanted
	exec go test $flags $(go list ./... | grep -v -E "$sharded")
fi

pkg=$1 i=$2 n=$3
mine= all=0 count=0
# The tests read from the source, as go test -list would compile a test
# binary without the race detector first.
for t in $(sed -n 's/^func \(Test[A-Za-z0-9_]*\)(t \*testing\.T).*/\1/p' "$pkg"/*_test.go | sort -u); do
	all=$((all + 1))
	read -r sum _ < <(printf %s "$t" | cksum)
	if ((sum % n == i)); then
		mine+=${mine:+|}$t
		count=$((count + 1))
	fi
done
echo "shard $i of $n of $pkg: $count of $all tests"
((count)) || exit 0
# shellcheck disable=SC2086
exec go test $flags -run "^($mine)\$" "$pkg"
