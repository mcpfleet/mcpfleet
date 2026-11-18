#!/bin/sh
# Check that skills-ref (the reference validator of the Agent Skills spec) and
# "mcpfleet skill validate" give every fixture the verdict its directory names:
# testdata/valid/* must pass, testdata/invalid/* must fail.
#
# Usage: scripts/skills-ref-parity.sh [testdata-dir]
# Needs skills-ref on PATH:
#   pip install "git+https://github.com/agentskills/agentskills@<commit>#subdirectory=skills-ref"
set -eu

testdata=${1:-pkg/skillspec/testdata}

command -v skills-ref >/dev/null 2>&1 || {
	echo "skills-ref not found on PATH" >&2
	exit 2
}

bin=$(mktemp -d)
trap 'rm -rf "$bin"' EXIT
go build -o "$bin/mcpfleet" ./cmd/mcpfleet

failures=0
checked=0
for verdict in valid invalid; do
	want=0
	[ "$verdict" = invalid ] && want=1
	for dir in "$testdata/$verdict"/*/; do
		dir=${dir%/}
		checked=$((checked + 1))

		ref=0
		skills-ref validate "$dir" >/dev/null 2>&1 || ref=$?
		go=0
		"$bin/mcpfleet" skill validate "$dir" >/dev/null 2>&1 || go=$?

		if [ "$ref" -ne "$want" ] || [ "$go" -ne "$want" ]; then
			echo "MISMATCH $dir: want exit $want, skills-ref $ref, mcpfleet $go"
			failures=$((failures + 1))
		fi
	done
done

if [ "$checked" -eq 0 ]; then
	echo "no fixtures under $testdata" >&2
	exit 2
fi
if [ "$failures" -ne 0 ]; then
	echo "$failures of $checked fixtures differ" >&2
	exit 1
fi
echo "all $checked fixtures agree with skills-ref"
