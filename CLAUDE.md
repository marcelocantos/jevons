@AGENTS.md

## Homebrew

The formula in `marcelocantos/homebrew-tap` is published by
[tapper](https://github.com/marcelocantos/tapper) from the dev Mac, not by CI.
After a release's assets have uploaded (`.github/workflows/release.yml`), run
`make release-tap TAG=vX.Y.Z` (or `tapper push --version vX.Y.Z`). Config lives in `tapper.yaml`;
the tap token is read from the macOS keychain, so no Actions secret is needed.
Docker must be running.
