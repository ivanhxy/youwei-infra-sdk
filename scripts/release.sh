#!/usr/bin/env bash
set -euo pipefail
export GIT_TERMINAL_PROMPT=0

cd "$(dirname "$0")/.."
mode="${1:---check}"
[[ "$mode" == --check || "$mode" == --publish ]] || { echo 'Usage: release.sh [--check|--publish]' >&2; exit 1; }
module=$(awk '$1 == "module" {print $2}' go.mod)
release_branch=main
fail() { echo "SDK release: $*" >&2; exit 1; }

[[ -z "$(git status --porcelain)" ]] || fail 'commit all changes before releasing'
version=$(awk '/^## / {print $2; exit}' CHANGELOG.md)
[[ "$version" =~ ^v(0|1)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || fail 'first CHANGELOG heading must contain a stable v0 or v1 SemVer'
tag="$version"
[[ -n "$module" ]] || fail 'missing module path'

git fetch --no-tags origin
git fetch --no-tags origin 'refs/tags/v*:refs/tags/v*'
commit=$(git rev-parse HEAD)
[[ "$commit" == "$(git rev-parse "origin/$release_branch")" ]] || fail "release checkout must match origin/$release_branch"
remote_commit=$(git ls-remote origin "refs/tags/$tag" "refs/tags/$tag^{}" | awk 'END {print $1}')
if [[ -n "$remote_commit" ]]; then
  [[ "$remote_commit" == "$commit" ]] || fail "$tag already exists at another commit; publish a new version"
else
  # Compare decimal strings without depending on GNU sort or integer size.
  latest=$(git tag -l 'v*' --sort=-version:refname | awk '/^v[0-9]+\.[0-9]+\.[0-9]+$/ {print; exit}')
  if [[ -n "$latest" && "$latest" != "$tag" ]]; then
    IFS=. read -r -a wanted <<< "${version#v}"
    previous="${latest#v}"
    IFS=. read -r -a released <<< "$previous"
    newer=false
    for i in 0 1 2; do
      left="${wanted[$i]}"; right="${released[$i]}"
      [[ "$left" == "$right" ]] && continue
      if (( ${#left} > ${#right} )) || { (( ${#left} == ${#right} )) && [[ "$left" > "$right" ]]; }; then newer=true; fi
      break
    done
    [[ "$newer" == true ]] || fail "$version must be newer than $latest"
  fi
  if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
    [[ "$(git rev-parse "$tag^{commit}")" == "$commit" ]] || fail "local $tag points at another commit"
  fi
fi

make test
go test -race ./...
go vet ./...
[[ -z "$(git status --porcelain)" ]] || fail 'checks modified the checkout'
[[ "$(git ls-remote origin "refs/heads/$release_branch" | awk '{print $1}')" == "$commit" ]] || fail "origin/$release_branch advanced; rerun on the latest commit"
if [[ "$mode" == --check ]]; then
  echo "SDK release checks passed: $tag at $commit (no tag pushed)"
  exit 0
fi

if [[ -z "$remote_commit" ]]; then
  notes=$(awk '/^## / {if (section++) exit} section {print}' CHANGELOG.md)
  git rev-parse -q --verify "refs/tags/$tag" >/dev/null || git tag -a "$tag" -m "$notes" "$commit"
  git push origin "refs/tags/$tag"
fi

# A new module/cache proves consumers can install the remote tag.
verification_dir=$(mktemp -d)
trap 'chmod -R u+w "$verification_dir"; rm -r "$verification_dir"' EXIT
export GOPRIVATE= GONOPROXY= GONOSUMDB= GOSUMDB=sum.golang.org
export GOPROXY=https://proxy.golang.org GOMODCACHE="$verification_dir/cache" GOWORK=off
cd "$verification_dir"
go mod init sdk-release-check
printf 'package sdkcheck\nimport infra "%s"\nvar _ = infra.NewClient\n' "$module" > sdk_test.go
go get "$module@$version"
go mod tidy
[[ "$(go list -m -f '{{.Version}}' "$module")" == "$version" ]] || fail 'installed version does not match release'
go test ./...
echo "SDK published and remote installation verified: $tag at $commit"
