#!/bin/sh
# Builds MatchTUI on Linux and macOS.
#
#   ./build.sh               run checks and build for this platform
#   ./build.sh --skip-tests  skip the test run
#   ./build.sh --linux       cross compile the Linux amd64 and arm64 builds
#
set -eu

root=$(cd "$(dirname "$0")" && pwd)
cd "$root"

skip_tests=0
linux_only=0
for arg in "$@"; do
    case "$arg" in
        --skip-tests) skip_tests=1 ;;
        --linux)      linux_only=1 ;;
        -h|--help)
            # Print the leading comment block, stopping at the first non
            # comment, so the range never has to be counted by hand.
            sed -n '2,/^set -eu$/p' "$0" | sed '$d; s/^# \{0,1\}//'
            exit 0
            ;;
        *)
            echo "matchtui: unknown option $arg (try --help)" >&2
            exit 2
            ;;
    esac
done

say() { printf '\033[36mmmatchtui: %s\033[0m\n' "$1"; }
die() { printf '\033[31mmatchtui: %s\033[0m\n' "$1" >&2; exit 1; }

if ! command -v go >/dev/null 2>&1; then
    die "go is not installed. Install it from https://go.dev/dl/"
fi

say "checking formatting and static analysis"
unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
    printf '%s\n' "$unformatted"
    die "these files are not gofmt clean; run gofmt -w ."
fi

go vet ./...

if [ "$skip_tests" -eq 0 ]; then
    say "running tests (use --skip-tests to skip)"
    go test ./...
fi

mkdir -p dist

# -s -w strips the symbol table and DWARF info, which roughly halves the binary.
export CGO_ENABLED=0

if [ "$linux_only" -eq 1 ]; then
    # Arch names follow uname, so a cross built artifact has the same name the
    # native build would have produced on that machine. That keeps install.sh
    # able to find either one.
    for arch in amd64 arm64; do
        case "$arch" in
            amd64) unix_arch=x86_64 ;;
            arm64) unix_arch=aarch64 ;;
        esac
        out="dist/matchtui-linux-$unix_arch"
        say "building $out"
        GOOS=linux GOARCH="$arch" go build -trimpath -ldflags "-s -w" -o "$out" .
        say "built $out ($(du -k "$out" | cut -f1) KB)"
    done
    exit 0
fi

case "$(uname -s)" in
    Darwin)                    os=macos ;;
    MINGW*|MSYS*|CYGWIN*)      os=windows ;;
    *)                         os=linux ;;
esac
case "$os" in
    windows) exe=.exe ;;
    *)       exe=   ;;
esac
out="dist/matchtui-$os-$(uname -m)$exe"
say "building $out"
go build -trimpath -ldflags "-s -w" -o "$out" .
say "built $out ($(du -k "$out" | cut -f1) KB)"

