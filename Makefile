GO ?= go
STATICCHECK ?= $(GO) run honnef.co/go/tools/cmd/staticcheck@latest
GOVULNCHECK ?= $(GO) run golang.org/x/vuln/cmd/govulncheck@latest
MODULE := $(shell GOWORK=off $(GO) list -m)

# Nested modules that are tested alongside the root but keep their own
# dependencies out of it. Each requires the root at exactly the version
# the whole repository is released at, and carries a replace pointing at
# the tree; see replaces below, and AGENTS.md for why the two go
# together. mcp brings in agenttool/mcpserver and the MCP SDK, which a
# bundle that does not serve MCP should not carry.
SUBMODULES = mcp

.PHONY: build deps replaces test vet fmt tidy tidy-check lint vuln check \
	extracted interop release-guard release release-commit clean

build:
	$(GO) build ./...
	@for m in $(SUBMODULES); do (cd $$m && $(GO) build ./...) || exit 1; done

# The root module must build from agenttool, agentskill and the standard
# library alone. agentskill brings openresponses and its YAML parser
# with it, which are its dependencies and not this module's choice, so
# they are allowed only as what agentskill requires. The nested modules
# are outside ./..., and the MCP SDK stays in them.
deps:
	@deps=$$($(GO) list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./... \
	  | grep -v '^github.com/ChristopherDavenport/toolbundle' \
	  | grep -v '^github.com/ChristopherDavenport/agenttool' \
	  | grep -v '^github.com/ChristopherDavenport/agentskill$$' \
	  | grep -v '^github.com/ChristopherDavenport/openresponses' \
	  | grep -v '^go.yaml.in/yaml/v3$$' || true); \
	  test -z "$$deps" || { echo "module depends on: $$deps"; exit 1; }

# Every first-party module a nested module requires must also be
# replaced, at a path that exists: the release commit requires a version
# the proxy does not serve until the tag is pushed, and go mod tidy
# ignores go.work, so the replace is what lets it resolve. See AGENTS.md.
replaces:
	@scripts/check-replaces.sh $(SUBMODULES)

test:
	$(GO) test -race ./...
	@for m in $(SUBMODULES); do (cd $$m && $(GO) test -race ./...) || exit 1; done

vet:
	$(GO) vet ./...
	@for m in $(SUBMODULES); do (cd $$m && $(GO) vet ./...) || exit 1; done

tidy:
	$(GO) mod tidy
	@for m in $(SUBMODULES); do (cd $$m && $(GO) mod tidy) || exit 1; done

# Fails when go mod tidy would change any go.mod or go.sum, without
# writing, so a stray dependency shows up in make check and not only in
# CI's diff.
tidy-check:
	$(GO) mod tidy -diff
	@for m in $(SUBMODULES); do (cd $$m && $(GO) mod tidy -diff) || exit 1; done

fmt:
	gofmt -l . && test -z "$$(gofmt -l .)"

lint:
	$(STATICCHECK) ./...
	@for m in $(SUBMODULES); do (cd $$m && $(STATICCHECK) ./...) || exit 1; done

vuln:
	$(GOVULNCHECK) ./...
	@for m in $(SUBMODULES); do (cd $$m && $(GOVULNCHECK) ./...) || exit 1; done

# Everything CI runs, but extracted, which needs the network.
check: fmt tidy-check vet deps replaces lint vuln test

# Builds, vets and tests each nested module the way a consumer gets it:
# extracted to a directory with no parent go.mod, with the in-tree
# replace dropped, so its require of the root is answered by the proxy.
# replaces checks that the require is there and release-guard that it
# names the version being tagged; neither compiles anything against it.
# Between releases it skips a module whose root has moved on since the
# release it requires; see the script. Needs the network, so it is not
# part of check; CI runs it.
extracted:
	@scripts/check-extracted.sh $(SUBMODULES)

# Interoperability with the reference implementation: the skills-ref
# CLI validates the golden skill under testdata/skills, which is what
# export-skill writes. Needs uvx and the network, so it is not part of
# check.
interop:
	SKILLS_INTEROP=1 $(GO) test -race -count=1 -run TestInterop .

# Checks one tag is safe to push, before it is pushed. A pushed tag is
# permanent — the proxy and the checksum database keep the version
# forever — so this is the last point at which a mistake is free:
#   make release-guard TAG=v0.1.0
#   make release-guard TAG=mcp/v0.1.0
release-guard:
	@test -n "$(TAG)" || { echo "usage: make release-guard TAG=<tag>"; exit 1; }
	@scripts/release-guard.sh "$(TAG)"

# Every tag a release writes: the root and one per nested module, all at
# the same version, all from the one commit below.
RELEASE_TAGS = $(VERSION) $(patsubst %,%/$(VERSION),$(SUBMODULES))

# Cut a release:
#
#   make release VERSION=v0.1.0
#
# Every module is released at one version, from one commit, and each
# nested module requires the root at exactly that version, so a consumer
# who takes toolbundle/mcp at vX.Y.Z gets toolbundle vX.Y.Z, the commit
# it was built and tested against. release-commit points the nested
# requires at VERSION, which resolves before it is published because of
# the replaces.
#
# The root is guarded and tagged first, then each nested module, since a
# nested module's guard proves the root tag of its version names this
# commit. Nothing is public until the push on the last line; if a guard
# refuses, undo with git reset --hard HEAD~1 and git tag -d the tags
# written.
#
# --atomic lands every ref in one transaction, so no window exists in
# which one tag is visible without the others, and none in which a
# published go.mod names a version the proxy cannot serve.
release: release-commit
	@scripts/release-guard.sh "$(VERSION)"
	@notes="$$(scripts/release-notes.sh $(VERSION))" || exit 1; \
	 git tag -a $(VERSION) -m "$$notes"
	@notes="$$(scripts/release-notes.sh $(VERSION))" || exit 1; \
	 for m in $(SUBMODULES); do \
	   scripts/release-guard.sh "$$m/$(VERSION)" || exit 1; \
	   git tag -a $$m/$(VERSION) -m "$$notes" || exit 1; \
	 done
	git push origin --atomic HEAD $(RELEASE_TAGS)

# Point every nested module's requires of the root at VERSION, date the
# changelog, check everything, commit. Nothing here is pushed, so a
# failure costs a git reset and no more. TRAILER, when set, is appended
# to the commit message.
#
# go mod tidy is free to move a requirement the go mod edit just set, so
# what landed is read back and asserted before anything is committed.
#
# GitHub creates no events for a push of more than three tags, so the
# release workflow would never run; a fourth module means pushing tags
# one at a time or creating the releases with gh.
#
# The changelog is dated through a temp file rather than sed -i, which is
# a GNU-ism: BSD sed reads the argument after -i as a backup suffix, so
# the GNU spelling fails outright on macOS, where these releases are cut.
# The temp file is removed if sed dies, so a failed run leaves nothing
# untracked behind for the clean-tree gate to trip over next time.
release-commit:
	@test -n "$(VERSION)" || { echo "usage: make release VERSION=vX.Y.Z"; exit 1; }
	@test $(words $(RELEASE_TAGS)) -le 3 || { echo "$(words $(RELEASE_TAGS)) tags would be pushed at once, and GitHub creates no events for a push of more than three; see the Makefile"; exit 1; }
	@test "$(origin SUBMODULES)" = file || { echo "do not override SUBMODULES here: a command-line override propagates into the bump, tidy and check below, so a module would be tagged having checked a subset."; exit 1; }
	@grep -q '^## Unreleased$$' CHANGELOG.md || { echo "CHANGELOG.md has no Unreleased section"; exit 1; }
	@test -z "$$(git status --porcelain)" || { echo "working tree is not clean"; exit 1; }
	@scripts/versions.sh set $(VERSION) $(SUBMODULES)
	sed 's/^## Unreleased$$/## $(VERSION) - '"$$(date +%F)"'/' CHANGELOG.md > CHANGELOG.md.tmp \
	  && mv CHANGELOG.md.tmp CHANGELOG.md \
	  || { rm -f CHANGELOG.md.tmp; exit 1; }
	$(MAKE) tidy
	$(MAKE) check
	@scripts/versions.sh check $(VERSION) $(SUBMODULES)
	git add -A && git commit -q -m "Release $(VERSION)" $(if $(TRAILER),-m "$(TRAILER)")

clean:
	rm -rf .cache
