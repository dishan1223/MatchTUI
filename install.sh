#!/bin/sh
# Installs MatchTUI on Linux and macOS.
#
#   ./install.sh                      build and install to ~/.local/bin
#   sudo ./install.sh                 build and install to /usr/local/bin
#   PREFIX=$HOME/bin ./install.sh     install somewhere else
#   ./install.sh --skip-tests         skip the test run during the build
#
# The install is a single static binary; nothing is written outside the
# install directory except your config file, which the app creates on first
# run in the platform config directory.
#
set -eu

root=$(cd "$(dirname "$0")" && pwd)
cd "$root"

say() { printf '\033[36mmatchtui: %s\033[0m\n' "$1"; }
ok()  { printf '\033[32mmatchtui: %s\033[0m\n' "$1"; }
die() { printf '\033[31mmatchtui: %s\033[0m\n' "$1" >&2; exit 1; }

# --- options -----------------------------------------------------------------

build_args=""
for arg in "$@"; do
    case "$arg" in
        --skip-tests) build_args="--skip-tests" ;;
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

# --- where to install --------------------------------------------------------

# Root installs go to /usr/local/bin, ordinary installs to ~/.local/bin, which
# is on PATH by default on every current distro.
if [ -n "${PREFIX:-}" ]; then
    bindir="$PREFIX/bin"
elif [ "$(id -u)" -eq 0 ]; then
    bindir="/usr/local/bin"
else
    bindir="$HOME/.local/bin"
fi

# --- build -------------------------------------------------------------------

if ! command -v go >/dev/null 2>&1; then
    die "go is not installed. Install it from https://go.dev/dl/ and re-run."
fi

# Work out what this machine is, so the artifact name built by build.sh can be
# found and so we refuse to continue where a bare binary cannot run. This runs
# before any directory is created, so a refusal leaves nothing behind.
case "$(uname -s)" in
    Darwin)               os=macos;  exe=   ;;
    MINGW*|MSYS*|CYGWIN*) os=windows; exe=.exe ;;
    *)                    os=linux;  exe=   ;;
esac
if [ "$os" = "windows" ]; then
    die "this script installs a Unix binary. On Windows use build.ps1, or run 'go install .' from a shell with Go on the PATH."
fi

say "installing to $bindir"
mkdir -p "$bindir"

say "building (this takes about a minute)"
# Reuse the build script so the two never drift, then move the platform binary
# into place under its final name.
sh "$root/build.sh" $build_args

built="dist/matchtui-$os-$(uname -m)$exe"
if [ ! -f "$built" ]; then
    die "expected build output at $built but it is missing"
fi

cp "$built" "$bindir/matchtui"
chmod 755 "$bindir/matchtui"

# --- report ------------------------------------------------------------------

version=$("$bindir/matchtui" -version 2>/dev/null || echo "unknown")
ok "$version installed to $bindir/matchtui"

case ":$PATH:" in
    *":$bindir:"*)
        ok "run it with: matchtui"
        ;;
    *)
        printf '\033[33mmatchtui: %s is not on your PATH\033[0m\n' "$bindir"
        cat <<EOF

Add it by putting this in your ~/.profile or ~/.bashrc:

    export PATH="$bindir:\$PATH"

Or open a new shell and run it by full path:

    $bindir/matchtui
EOF
        ;;
esac

cat <<'EOF'

First run:
    matchtui -provider demo      # offline, no network needed
    matchtui                     # live scores from ESPN, no key required

Config is written on first run to the platform config directory.
Press ? inside the app for the key bindings.
EOF
