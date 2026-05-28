.PHONY: all build test lint vet clean changelog changelog-prepend help

# Go binary path
GO := /usr/local/go/bin/go
GOFMT := /usr/local/go/bin/gofmt

# Build all binaries
BINARIES := bt-agent bt-evaluator bt-langagent bt-dashboard bt-gardener bt-agent-cli
BIN_DIR := bin

all: build

build:
	@mkdir -p $(BIN_DIR)
	@for bin in $(BINARIES); do \
		echo "Building $$bin..."; \
		$(GO) build -o $(BIN_DIR)/$$bin ./cmd/$$bin/; \
	done
	@echo "All binaries built."

test:
	$(GO) test -short -count=1 -race ./...

test-full:
	$(GO) test -count=1 -timeout 600s ./...

lint:
	$(GO) vet ./...

vet: lint

fmt:
	$(GOFMT) -w .

fmt-check:
	@test -z "$$($(GOFMT) -l .)" || (echo "Files need formatting:" && $(GOFMT) -l . && exit 1)

clean:
	rm -rf $(BIN_DIR)/*

# Generate CHANGELOG.md from conventional commits since last tag
changelog:
	@if [ -f CHANGELOG.md ]; then \
		VERSION=$$(git describe --tags --abbrev=0 2>/dev/null || echo ""); \
		if [ -n "$$VERSION" ]; then \
			echo "# Changelog" > CHANGELOG.md.tmp; \
			echo "" >> CHANGELOG.md.tmp; \
			echo "All notable changes to this project will be documented in this file." >> CHANGELOG.md.tmp; \
			echo "" >> CHANGELOG.md.tmp; \
			./scripts/changelog.sh --since "$$VERSION" --next "Unreleased" >> CHANGELOG.md.tmp; \
			echo "" >> CHANGELOG.md.tmp; \
			tail -n +2 CHANGELOG.md >> CHANGELOG.md.tmp; \
			mv CHANGELOG.md.tmp CHANGELOG.md; \
		else \
			./scripts/changelog.sh --all --next "Unreleased" > CHANGELOG.md.tmp; \
			mv CHANGELOG.md.tmp CHANGELOG.md; \
		fi; \
		echo "Updated CHANGELOG.md"; \
	else \
		echo "# Changelog" > CHANGELOG.md; \
		echo "" >> CHANGELOG.md; \
		echo "All notable changes to this project will be documented in this file." >> CHANGELOG.md; \
		echo "" >> CHANGELOG.md; \
		./scripts/changelog.sh --all --next "Unreleased" >> CHANGELOG.md; \
		echo "Created CHANGELOG.md"; \
	fi

# Prepend a new version section for release
changelog-prepend:
	@if [ -z "$(VERSION)" ]; then \
		echo "Usage: make changelog-prepend VERSION=v0.2.0"; \
		exit 1; \
	fi
	@if [ ! -f CHANGELOG.md ]; then \
		$(MAKE) changelog; \
	fi
	@echo "# Changelog" > CHANGELOG.md.tmp; \
	echo "" >> CHANGELOG.md.tmp; \
	echo "All notable changes to this project will be documented in this file." >> CHANGELOG.md.tmp; \
	echo "" >> CHANGELOG.md.tmp; \
	LATEST_TAG=$$(git describe --tags --abbrev=0 2>/dev/null || echo ""); \
	if [ -n "$$LATEST_TAG" ]; then \
		./scripts/changelog.sh --since "$$LATEST_TAG" --next "$(VERSION)" >> CHANGELOG.md.tmp; \
	else \
		./scripts/changelog.sh --all --next "$(VERSION)" >> CHANGELOG.md.tmp; \
	fi; \
	echo "" >> CHANGELOG.md.tmp; \
	tail -n +4 CHANGELOG.md >> CHANGELOG.md.tmp; \
	mv CHANGELOG.md.tmp CHANGELOG.md; \
	echo "Prepended $(VERSION) section to CHANGELOG.md"

help:
	@echo "BT Platform Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  build             Build all binaries (default)"
	@echo "  test              Run fast tests with race detector"
	@echo "  test-full         Run full test suite (includes Ollama)"
	@echo "  lint / vet        Run go vet"
	@echo "  fmt               Format all source files"
	@echo "  fmt-check         Check formatting (CI)"
	@echo "  changelog         Generate/update CHANGELOG.md from git commits"
	@echo "  changelog-prepend Prepend a new version section (VERSION=v0.2.0)"
	@echo "  clean             Remove built binaries"
	@echo "  help              Show this help"
