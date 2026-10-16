.PHONY: test release-check release

test:
	go test ./...
	bash scripts/test-release.sh

release-check:
	bash scripts/release.sh --check

release:
	bash scripts/release.sh --publish
