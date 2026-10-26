#!/usr/bin/env bash
# Update the Homebrew tap declared in tapper.yaml for an existing GitHub
# release. Run after the release carries its tarballs — tapper reads the
# assets to build the formula, so it must not run before they upload.
set -euo pipefail
TAG="${1:-$(git describe --tags --abbrev=0)}"
command -v tapper >/dev/null || {
	echo "tapper not on PATH — brew install marcelocantos/tap/tapper" >&2
	exit 1
}
exec tapper push --version "$TAG"
