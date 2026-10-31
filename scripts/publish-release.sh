#!/bin/sh
# Publish a tagged release to the `release` branch: the tag's tree plus the binaries built by
# scripts/build.sh (bin/*/ is gitignored on main). Each release is one commit on top of the previous
# release, so `release` only ever fast-forwards.
#
# Usage: scripts/publish-release.sh vX.Y.Z [REMOTE]
#   REMOTE defaults to origin. Set DRY_RUN=1 to create the commit without pushing.
set -eu
cd "$(dirname "$0")/.."

tag=${1:?usage: publish-release.sh vX.Y.Z [REMOTE]}
remote=${2:-origin}
branch=release
version=${tag#v}

# The tag must name the version the plugin manifests declare.
for manifest in .claude-plugin/plugin.json package.json; do
  declared=$(sed -n 's/^  "version": *"\([^"]*\)".*/\1/p' "$manifest")
  if [ "$declared" != "$version" ]; then
    echo "error: $manifest declares version '$declared', tag is $tag" >&2
    exit 1
  fi
done

tagged=$(git rev-parse "$tag^{commit}")
if [ "$(git rev-parse HEAD)" != "$tagged" ]; then
  echo "error: HEAD is not $tag; check out the tag before building" >&2
  exit 1
fi

for target in darwin-arm64/cnw darwin-amd64/cnw linux-amd64/cnw linux-arm64/cnw windows-amd64/cnw.exe; do
  if [ ! -f "bin/$target" ]; then
    echo "error: bin/$target missing; run scripts/build.sh" >&2
    exit 1
  fi
done

# Build the tree in a scratch index so the working checkout's index is untouched.
GIT_INDEX_FILE=$(mktemp)
export GIT_INDEX_FILE
trap 'rm -f "$GIT_INDEX_FILE"' EXIT
git read-tree "$tagged"
git add -f bin/darwin-arm64/cnw bin/darwin-amd64/cnw bin/linux-amd64/cnw bin/linux-arm64/cnw \
  bin/windows-amd64/cnw.exe
tree=$(git write-tree)

if git fetch --quiet "$remote" "refs/heads/$branch" 2>/dev/null; then
  parent=$(git rev-parse FETCH_HEAD)
  if [ "$(git rev-parse "$parent^{tree}")" = "$tree" ]; then
    echo "$branch already holds $tag"
    exit 0
  fi
  commit=$(git commit-tree "$tree" -p "$parent" -m "release $tag" -m "Source: $tagged")
else
  commit=$(git commit-tree "$tree" -m "release $tag" -m "Source: $tagged")
fi

if [ "${DRY_RUN:-}" = 1 ]; then
  echo "dry run: $branch would move to $commit"
  exit 0
fi
git push "$remote" "$commit:refs/heads/$branch"
echo "published $tag to $branch as $commit"
