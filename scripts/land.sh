#!/bin/sh
# make land (🎯T1312): land the working branch on master as one squash commit.
#
# Run after `make gate` attested HEAD's tree. Refuses unless the attestation
# names that tree and the branch already contains origin/master. Then it
# builds one commit with HEAD's tree on top of origin/master, pushes it (the
# pre-push hook checks the attestation again), moves the local master ref,
# and merges the squash back into the branch so the next squash diffs
# cleanly. Each step's own exit status decides; nothing is piped. Doing this
# by hand pushed a zsh-mangled refspec ($SQ:refs reads as the :r modifier)
# while a `| tail` reported the failed push as a success.
#
#   scripts/land.sh MESSAGE_FILE
set -eu
cd "$(git rev-parse --show-toplevel)"

msg=${1:-}
if [ -z "$msg" ] || [ ! -f "$msg" ]; then
    echo "usage: make land MSG=<file with the squash commit message>" >&2
    exit 2
fi

att="$(git rev-parse --git-dir)/gate-attestation"
head_tree=$(git rev-parse 'HEAD^{tree}')
att_tree=$(sed -n 's/^tree=//p' "$att" 2>/dev/null || true)
if [ "$att_tree" != "$head_tree" ]; then
    echo "land: make gate has not attested HEAD's tree ($head_tree); run make gate first" >&2
    exit 1
fi

git fetch -q origin master
base=$(git rev-parse origin/master)
if ! git merge-base --is-ancestor "$base" HEAD; then
    echo "land: origin/master ($base) has commits this branch lacks; merge it, re-run make gate, then land" >&2
    exit 1
fi

if [ "$head_tree" = "$(git rev-parse "$base^{tree}")" ]; then
    echo "land: nothing to land; HEAD's tree is already master's" >&2
    exit 1
fi

squash=$(git commit-tree "$head_tree" -p "$base" -F "$msg")
git push origin "${squash}:refs/heads/master"
git update-ref refs/heads/master "$squash"
git merge -s ours --no-edit -m "merge master (squash $(git rev-parse --short "$squash"))" "$squash"
echo "✓ landed $(git rev-parse --short "$squash") on master"
