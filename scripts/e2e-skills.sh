#!/bin/sh
# End-to-end check of skill management across both repos, with the real
# binaries: build mcpfleet-registry and run it on a fresh database, bootstrap
# a token, push the server "github" and the fixture skill gh-notes (which
# requires it) with the mcpfleet CLI, then "apply --skill gh-notes
# claude-code" into an empty HOME and check that
#   - the installed skill is the fixture byte for byte, executable bits included,
#   - skills-ref validates the installed directory,
#   - Claude Code's config (~/.claude.json) holds the server "github".
#
# Usage: scripts/e2e-skills.sh [registry-checkout]   (default ../mcpfleet-registry)
# Needs go, a C compiler (the registry uses SQLite through CGO), curl, jq and
# skills-ref on PATH. E2E_PORT sets the registry port (default 18080).
set -eu

registry_src=${1:-../mcpfleet-registry}
port=${E2E_PORT:-18080}
registry_url="http://127.0.0.1:$port"
skill=gh-notes
fixture=testdata/e2e/skills/$skill

for tool in go curl jq skills-ref; do
	command -v "$tool" >/dev/null 2>&1 || {
		echo "$tool not found on PATH" >&2
		exit 2
	}
done
[ -f "$registry_src/go.mod" ] || {
	echo "no registry checkout at $registry_src" >&2
	exit 2
}

work=$(mktemp -d)
registry_pid=
cleanup() {
	[ -n "$registry_pid" ] && kill "$registry_pid" 2>/dev/null
	rm -rf "$work"
}
trap cleanup EXIT

fail() {
	echo "FAIL: $*" >&2
	if [ -f "$work/registry.log" ]; then
		echo "--- registry log ---" >&2
		cat "$work/registry.log" >&2
	fi
	exit 1
}

echo "==> build mcpfleet and the registry"
go build -o "$work/bin/mcpfleet" ./cmd/mcpfleet
(cd "$registry_src" && CGO_ENABLED=1 go build -o "$work/bin/registry" ./cmd/registry)

echo "==> start the registry on $registry_url"
DATABASE_URL="$work/registry.db" PORT=$port "$work/bin/registry" >"$work/registry.log" 2>&1 &
registry_pid=$!
tries=0
until curl -fsS "$registry_url/healthz" >/dev/null 2>&1; do
	tries=$((tries + 1))
	[ "$tries" -le 50 ] || fail "registry did not answer /healthz"
	kill -0 "$registry_pid" 2>/dev/null || fail "registry exited"
	sleep 0.2
done

echo "==> bootstrap a token"
token=$(curl -fsS -X POST "$registry_url/bootstrap" \
	-H 'Content-Type: application/json' -d '{"name": "e2e"}' | jq -r '.token')
case $token in
mcp_*) ;;
*) fail "bootstrap returned no token" ;;
esac

# The CLI runs with an empty HOME: its token, registry URL, skills state and
# Claude Code's config and skills directory all land there.
home="$work/home"
mkdir -p "$home"
mcpfleet() {
	env -u MCPFLEET_REGISTRY_URL -u CLAUDE_CONFIG_DIR HOME="$home" "$work/bin/mcpfleet" "$@"
}

echo "==> log in, push the server and the skill"
mcpfleet auth login --url "$registry_url" --token "$token"
mcpfleet push testdata/e2e/servers.yaml
mcpfleet skill push "$fixture"
mcpfleet skill list

echo "==> apply --skill $skill claude-code"
mcpfleet apply --skill "$skill" claude-code

installed="$home/.claude/skills/$skill"
[ -d "$installed" ] || fail "no skill at $installed"

echo "==> installed skill is the fixture byte for byte"
diff -r "$fixture" "$installed" || fail "installed skill differs from $fixture"
# diff -r compares content only; compare the executable bit of every file.
(cd "$fixture" && find . -type f) | while read -r f; do
	want=no
	[ -x "$fixture/$f" ] && want=yes
	got=no
	[ -x "$installed/$f" ] && got=yes
	[ "$want" = "$got" ] || fail "$f: executable $got, fixture $want"
done
[ -x "$installed/scripts/hello.sh" ] || fail "scripts/hello.sh is not executable"

echo "==> skills-ref validate"
skills-ref validate "$installed" || fail "skills-ref rejects $installed"

echo "==> Claude Code config holds the required server"
config="$home/.claude.json"
[ -f "$config" ] || fail "no $config"
jq -e '.mcpServers.github.command == "npx"' "$config" >/dev/null ||
	fail "$config has no server github: $(cat "$config")"

echo "==> skills state records the installed hash"
state="$home/.config/mcpfleet/skills-state.json"
[ -f "$state" ] || fail "no $state"
grep -q "$skill" "$state" || fail "$state does not name $skill"

echo "OK: $skill installed into Claude Code with server github"
