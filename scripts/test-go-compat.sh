#!/usr/bin/env sh
set -eu

cd "$(dirname "$0")/.."

go_mod_version="$(awk '$1 == "go" { print $2; exit }' go.mod)"
if [ -z "$go_mod_version" ]; then
	echo "failed to read Go version from go.mod" >&2
	exit 1
fi

case "$go_mod_version" in
*.*.*)
	go_toolchain_version="$go_mod_version"
	;;
*.*)
	go_toolchain_version="${go_mod_version}.0"
	;;
*)
	echo "unsupported Go version in go.mod: ${go_mod_version}" >&2
	exit 1
	;;
esac

GOTOOLCHAIN="go${go_toolchain_version}"

export GOTOOLCHAIN
export GOWORK=off

actual_version="$(go env GOVERSION)"
case "$actual_version" in
go"$go_toolchain_version")
	;;
*)
	echo "expected Go ${go_toolchain_version} toolchain derived from go.mod, got ${actual_version}" >&2
	exit 1
	;;
esac

go test -exec=true ./...