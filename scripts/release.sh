#!/usr/bin/env bash
set -euo pipefail
project_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_root"

: "${VERSION:?Set VERSION to the release tag}"
if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  echo "VERSION must look like v1.2.3 (received $VERSION)" >&2
  exit 1
fi
notes_file="${RUNNER_TEMP:?}/release-notes.md"

case "${1:-}" in
  notes)
    awk -v version="${VERSION#v}" \
      '$1 == "##" && $2 == version { found=1; next } /^## / { found=0 } found' \
      CHANGELOG.md > "$notes_file"
    if ! grep -q '[^[:space:]]' "$notes_file"; then
      echo "CHANGELOG.md has no section for $VERSION" >&2
      exit 1
    fi
    cat "$notes_file"
    ;;
  refresh-notes)
    gh release edit "$VERSION" --notes-file "$notes_file"
    ;;
  tag)
    git config user.name 'github-actions[bot]'
    git config user.email '41898282+github-actions[bot]@users.noreply.github.com'
    # A published release is final; a failed attempt may have left only a tag.
    if git ls-remote --exit-code --tags origin "refs/tags/$VERSION" >/dev/null; then
      if gh release view "$VERSION" >/dev/null 2>&1; then
        echo "$VERSION already has a published release" >&2
        exit 1
      fi
      echo "Tag $VERSION exists without a published release; replacing it with HEAD."
      git push --delete origin "refs/tags/$VERSION"
    fi
    git tag -d "$VERSION" 2>/dev/null || true
    git tag -a "$VERSION" -m "$VERSION"
    git push origin "refs/tags/$VERSION"
    ;;
  publish)
    sha256sum dist/*.whl dist/*.tar.gz > dist/checksums.txt
    gh release create "$VERSION" dist/*.whl dist/*.tar.gz dist/checksums.txt \
      --verify-tag --title "$VERSION" --notes-file "$notes_file"
    ;;
  *)
    echo 'Usage: scripts/release.sh notes|refresh-notes|tag|publish' >&2
    exit 2
    ;;
esac
