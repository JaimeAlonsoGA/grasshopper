# grasshopper — build, install, remove.
#
# PREFIX defaults to ~/.local/bin, which is on PATH on most machines and needs no
# sudo. Override it: make install PREFIX=/usr/local/bin

PREFIX  ?= $(HOME)/.local/bin
OWNER   ?= JaimeAlonsoGA
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  = -s -w -X main.version=$(VERSION)
PLATFORMS = darwin/arm64 darwin/amd64 linux/arm64 linux/amd64
SHA256 := $(shell command -v shasum >/dev/null 2>&1 && echo "shasum -a 256" || echo sha256sum)

.PHONY: help build version-check install uninstall check release clean

help:
	@echo "make install     build, put hop on PATH ($(PREFIX)), register it with your agents"
	@echo "make uninstall   unregister, then remove the binary"
	@echo "make check       gofmt, vet, tests"
	@echo "make release     cross-compiled binaries in dist/"
	@echo ""
	@echo "grasshopper $(VERSION)"

build:
	@go build -ldflags '$(LDFLAGS)' -o hop ./cmd/hop

# A version with -dirty in it is a binary nobody can identify later. Worth one
# line of warning rather than discovering it from hop version a week on.
version-check:
	@case "$(VERSION)" in *dirty*) \
		printf 'Note: building %s — commit and tag first for a clean version.\n' '$(VERSION)';; \
	esac

# Registering is part of installing: a binary on PATH that no agent knows about
# carries nothing, and the gap between those two states is where people conclude
# the tool does not work. Registration goes through each agent's own CLI, so
# grasshopper never edits somebody else's config file by hand.
install: version-check check build
	@mkdir -p '$(PREFIX)'
	@install -m 0755 hop '$(PREFIX)/hop'
	@echo "installed $(PREFIX)/hop ($(VERSION))"
	@command -v claude >/dev/null 2>&1 && { \
		claude mcp remove grasshopper --scope user >/dev/null 2>&1 || true; \
		claude mcp add grasshopper --scope user -- '$(PREFIX)/hop' mcp && \
		echo "registered with claude (user scope)"; } || \
		echo "claude not on PATH — register by hand: claude mcp add grasshopper -- $(PREFIX)/hop mcp"
	@command -v hop >/dev/null 2>&1 || printf '\nNote: %s is not on your PATH.\nAdd it in ~/.zshrc:  export PATH="%s:$$PATH"\n' '$(PREFIX)' '$(PREFIX)'

# Unregistered before removed. In the other order the agent is left calling a
# command that no longer exists, on every startup, forever.
uninstall:
	@command -v claude >/dev/null 2>&1 && claude mcp remove grasshopper --scope user 2>/dev/null || true
	@rm -f '$(PREFIX)/hop'
	@echo "removed $(PREFIX)/hop"

check:
	@test -z "$$(gofmt -l . )" || { echo "gofmt:"; gofmt -l .; exit 1; }
	@go vet ./...
	@go test ./...

release: check
	@rm -rf dist && mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		GOOS=$$os GOARCH=$$arch go build -ldflags '$(LDFLAGS)' -o dist/hop ./cmd/hop || exit 1; \
		tar -czf dist/grasshopper-$(VERSION)-$$os-$$arch.tar.gz -C dist hop; \
		rm dist/hop; \
	done
	@cd dist && $(SHA256) *.tar.gz > checksums.txt
	@ls -l dist

clean:
	@rm -rf hop dist
