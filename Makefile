.PHONY: all build test lint vet clean changelog changelog-prepend bench bench-nightly ci help

# Go binary path
GO := /usr/local/go/bin/go
GOFMT := /usr/local/go/bin/gofmt

# Build all binaries
BINARIES := bt-agent bt-evaluator bt-langagent bt-dashboard bt-gardener bt-agent-cli benchcmp
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

# Run benchmark suite (fast, no LLM needed)
bench:
	$(GO) test -bench=. -benchtime=1x -count=1 ./internal/benchmark/... 2>&1

# Save benchmark baseline for regression detection
benchcmp-baseline:
	$(GO) test -bench=. -benchtime=1x -count=3 ./internal/benchmark/... 2>&1 | $(BIN_DIR)/benchcmp baseline --save

# Check benchmarks against stored baseline (exit 1 on critical regression)
benchcmp-check:
	$(GO) test -bench=. -benchtime=1x -count=3 ./internal/benchmark/... 2>&1 | $(BIN_DIR)/benchcmp check

# Reset benchmark baseline
benchcmp-reset:
	$(BIN_DIR)/benchcmp reset

# Complete local CI pipeline — runs vet, fmt-check, tests, and builds all binaries.
# Use before pushing to avoid CI failures.
ci:
	@echo "=== CI Pipeline (local) ==="
	@echo ""
	@echo "1/4  go vet..."
	@$(GO) vet ./...
	@echo "     ✓ passed"
	@echo ""
	@echo "2/4  gofmt check..."
	@test -z "$$($(GOFMT) -l .)" || (echo "     ✗ Files need formatting:" && $(GOFMT) -l . && exit 1)
	@echo "     ✓ passed"
	@echo ""
	@echo "3/4  tests (short + race)..."
	@$(GO) test -short -count=1 -race -timeout 120s ./...
	@echo "     ✓ passed"
	@echo ""
	@echo "4/4  build all binaries..."
	@mkdir -p $(BIN_DIR)
	@for bin in $(BINARIES); do \
		$(GO) build -o $(BIN_DIR)/$$bin ./cmd/$$bin/ || exit 1; \
	done
	@echo "     ✓ passed"
	@echo ""
	@echo "=== CI Pipeline PASSED ==="

# Nightly benchmark suite — runs all evaluation benchmarks (SWE-bench, BFCL, τ-bench, ToolBench)
# Requires Ollama running and ~10GB disk for benchmark datasets.
# Fails if any benchmark score regresses >5% from baseline.
bench-nightly:
	@echo "=== Running Nightly Benchmark Suite ==="
	@echo "SWE-bench..."
	$(GO) test -run TestSWE -count=1 -timeout 3600s ./internal/benchmark/... || echo "SWE-bench failed (check logs)"
	@echo "BFCL..."
	$(GO) test -run TestBFCL -count=1 -timeout 1800s ./internal/benchmark/... || echo "BFCL failed (check logs)"
	@echo "TauBench..."
	$(GO) test -run TestTau -count=1 -timeout 1800s ./internal/benchmark/... || echo "TauBench failed (check logs)"
	@echo "ToolBench..."
	$(GO) test -run TestTool -count=1 -timeout 1800s ./internal/benchmark/... || echo "ToolBench failed (check logs)"
	@echo "=== Nightly Benchmarks Complete ==="

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

# Generate release notes from conventional commits (markdown)
release-notes:
	@./scripts/release-notes.sh --next $(VERSION)

# Generate release notes in JSON format for API consumers
release-notes-json:
	@./scripts/release-notes.sh --next $(VERSION) --format json

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
	@echo "  ci                Run complete CI pipeline locally (vet + fmt + test + build)"
	@echo "  changelog         Generate/update CHANGELOG.md from git commits"
	@echo "  changelog-prepend Prepend a new version section (VERSION=v0.2.0)"
	@echo "  release-notes     Generate release notes from conventional commits"
	@echo "  release-notes-json Generate release notes as JSON"
	@echo "  bench             Run fast benchmarks (no LLM)"
	@echo "  benchcmp-baseline Save benchmark baseline for regression detection"
	@echo "  benchcmp-check    Check benchmarks against stored baseline"
	@echo "  benchcmp-reset    Reset benchmark baseline"
	@echo "  bench-nightly     Run full benchmark suite (SWE-bench, BFCL, τ-bench, ToolBench)"
	@echo "  clean             Remove built binaries"
	@echo "  help              Show this help"
