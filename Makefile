.PHONY: test test-race vet generate release-gate tag

# test runs the full suite the way CI does. It must pass before any tag.
test:
	go test ./... -count=1

# test-race runs the same suite under the race detector (slower; matches
# .github/workflows/extended-ci.yml).
test-race:
	go test ./... -race -count=1 -timeout 15m

vet:
	go vet ./...

# generate refreshes the embedded grammar blob (language.bin/language.hash)
# after a grammar.go or gotreesitter change.
generate:
	go generate ./...

# release-gate is the tagging guard: it refuses to let a release proceed on
# red. Run it before every `make tag` (tag depends on it directly, so this
# target mainly exists so CI/release tooling can call the same check).
release-gate: test vet
	@echo "release-gate: tests and vet passed"

# tag creates an annotated git tag only after release-gate passes, so it is
# impossible to tag a red build. Usage: make tag VERSION=v0.6.0
tag: release-gate
	@if [ -z "$(VERSION)" ]; then \
		echo "usage: make tag VERSION=vX.Y.Z" >&2; \
		exit 1; \
	fi
	@case "$(VERSION)" in \
		v[0-9]*.[0-9]*.[0-9]*) ;; \
		*) echo "VERSION must look like vX.Y.Z, got $(VERSION)" >&2; exit 1 ;; \
	esac
	git tag -a "$(VERSION)" -m "$(VERSION)"
	@echo "tagged $(VERSION) — push with: git push origin $(VERSION)"
