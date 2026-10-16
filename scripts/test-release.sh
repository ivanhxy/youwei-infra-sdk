#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
test_dir=$(mktemp -d)
trap 'rm -r "$test_dir"' EXIT
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME=SDK-Test GIT_AUTHOR_EMAIL=sdk-test@example.invalid
export GIT_COMMITTER_NAME=SDK-Test GIT_COMMITTER_EMAIL=sdk-test@example.invalid
export RELEASE_TEST_LOG="$test_dir/commands"
mkdir -p "$test_dir/bin"
cat > "$test_dir/bin/make" <<'STUB'
#!/usr/bin/env bash
echo "make $*" >> "$RELEASE_TEST_LOG"
[[ "${RELEASE_TEST_FAIL:-}" != make ]]
STUB
cat > "$test_dir/bin/go" <<'STUB'
#!/usr/bin/env bash
echo "go $*" >> "$RELEASE_TEST_LOG"
if [[ "$1" == get && "${RELEASE_TEST_FAIL:-}" == download ]]; then exit 1; fi
if [[ "$1" == list ]]; then echo v0.1.0; fi
STUB
chmod +x "$test_dir/bin/"*
export PATH="$test_dir/bin:$PATH"

new_fixture() {
  git init -q --bare "$test_dir/$1-remote"
  git init -q -b main "$test_dir/$1"
  cd "$test_dir/$1"
  mkdir -p scripts
  cp "$script_dir/release.sh" scripts/
  # Exercise the workflow's current approved release branch.
  release_branch=$(sed -n 's/^release_branch=//p' scripts/release.sh)
  [[ "$release_branch" == main ]] || git branch -m "$release_branch"
  printf 'module example.com/sdk\n\ngo 1.22\n' > go.mod
  printf '# Changelog\n\n## v0.1.0 - 2026-10-16\n\nFirst release.\n' > CHANGELOG.md
  git add .
  git commit -qm 'feat(sdk): add fixture'
  git remote add origin "$test_dir/$1-remote"
  git push -q -u origin "$release_branch"
  : > "$RELEASE_TEST_LOG"
}
run_release() { bash scripts/release.sh "$@" > "$test_dir/output" 2>&1; }
assert_no_tag() { [[ -z "$(git ls-remote origin 'refs/tags/*')" ]]; }
expect_failure() {
  if run_release --publish; then echo "Expected release rejection" >&2; exit 1; fi
  [[ "$(cat "$test_dir/output")" == *"$1"* ]] || { cat "$test_dir/output"; exit 1; }
}

new_fixture dirty
printf 'uncommitted' > dirty.txt
expect_failure 'commit all changes'
assert_no_tag

new_fixture wrong-ref
git checkout -qb feat/test
echo feature > feature.txt
git add feature.txt
git commit -qm 'feat: add unmerged feature'
expect_failure 'release checkout must match'
assert_no_tag

new_fixture invalid-version
sed 's/v0.1.0/v0.01.0/' CHANGELOG.md > temp
mv temp CHANGELOG.md
git commit -qam 'docs: set invalid version'
git push -q
expect_failure 'stable v0 or v1 SemVer'
assert_no_tag

new_fixture check-only
run_release --check
assert_no_tag
[[ "$(cat "$RELEASE_TEST_LOG")" == *'go test -race ./...'* ]]

new_fixture test-failure
export RELEASE_TEST_FAIL=make
expect_failure ''
unset RELEASE_TEST_FAIL
assert_no_tag

new_fixture already-published
git tag v0.1.0
git push -q origin refs/tags/v0.1.0
echo change > change.txt
git add change.txt
git commit -qm 'fix: change after release'
git push -q
expect_failure 'already exists at another commit'

new_fixture rollback
git tag v0.2.0
git push -q origin refs/tags/v0.2.0
expect_failure 'must be newer'
assert_no_tag_result=$(git ls-remote origin refs/tags/v0.1.0)
[[ -z "$assert_no_tag_result" ]]

new_fixture publish
run_release --publish
[[ "$(git ls-remote origin 'refs/tags/v0.1.0^{}' | awk '{print $1}')" == "$(git rev-parse HEAD)" ]]
[[ "$(cat "$RELEASE_TEST_LOG")" == *'go get example.com/sdk@v0.1.0'* ]]
run_release --publish

new_fixture retry-download
export RELEASE_TEST_FAIL=download
expect_failure ''
unset RELEASE_TEST_FAIL
[[ -n "$(git ls-remote origin refs/tags/v0.1.0)" ]]
run_release --publish

echo 'Passed: dirty checkout, wrong ref, invalid version, check-only, failed checks, immutable tag, version rollback, publish/retry, remote-install failure/retry'
