GO ?= go
BINARY := hust-mail-cli
COMMAND := ./cmd/hust-mail-cli
BIN_DIR ?= bin
BIN := $(BIN_DIR)/$(BINARY)
COVERAGE_FILE ?= coverage.out
GOBIN ?= $(shell $(GO) env GOBIN)
TARGETS ?=
ARGS ?= --help

export GOBIN

.DEFAULT_GOAL := build

.PHONY: all build cross-build install uninstall run test test-race vet lint check fmt fmt-check coverage deps clean help

all: check build

build:
	@mkdir -p "$(BIN_DIR)"
	CGO_ENABLED=0 $(GO) build -trimpath -o "$(BIN)" $(COMMAND)

# An empty TARGETS uses the script's complete Linux/macOS matrix.
cross-build:
	GO="$(GO)" BIN_DIR="$(BIN_DIR)" sh scripts/build-platforms.sh $(TARGETS)

install:
	CGO_ENABLED=0 $(GO) install -trimpath $(COMMAND)

# Match go install: GOBIN, otherwise the first GOPATH entry's bin directory.
uninstall:
	@set -eu; \
	gobin="$$($(GO) env GOBIN)"; \
	if [ -z "$$gobin" ]; then \
		gopath="$$($(GO) env GOPATH)"; \
		install_root=$${gopath%%:*}; \
		case "$$install_root" in ""|/) echo "Invalid GOPATH" >&2; exit 1 ;; esac; \
		gobin="$$install_root/bin"; \
	fi; \
	case "$$gobin" in ""|/) echo "Invalid GOBIN" >&2; exit 1 ;; esac; \
	echo "Removing $$gobin/$(BINARY)"; \
	rm -f -- "$$gobin/$(BINARY)"

run:
	$(GO) run $(COMMAND) $(ARGS)

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

lint: fmt-check vet

check: lint test-race

fmt:
	$(GO) fmt ./...

fmt-check:
	@set -eu; \
	gofmt_bin="$$($(GO) env GOROOT)/bin/gofmt"; \
	files="$$("$$gofmt_bin" -l cmd internal pkg)"; \
	if [ -n "$$files" ]; then \
		echo "Run make fmt to format:"; printf '%s\n' "$$files"; exit 1; \
	fi

coverage:
	$(GO) test -coverprofile="$(COVERAGE_FILE)" ./...
	$(GO) tool cover -func="$(COVERAGE_FILE)"

deps:
	$(GO) mod download

# Remove only known build outputs; never recurse into mail/config directories.
clean:
	rm -f -- "$(BIN)" "$(COVERAGE_FILE)"
	@for binary in "$(BIN_DIR)"/*/$(BINARY); do \
		if [ -f "$$binary" ]; then \
			rm -f -- "$$binary"; rmdir "$${binary%/*}" 2>/dev/null || true; \
		fi; \
	done
	@rmdir "$(BIN_DIR)" 2>/dev/null || true

help:
	@printf '%s\n' \
	  'HUST Mail CLI development targets:' \
	  '  make / make build   Build bin/hust-mail-cli without CGO' \
	  '  make all            Run checks and build' \
	  '  make install        Install to GOBIN or the first GOPATH/bin' \
	  '  make uninstall      Remove the installed executable only' \
	  '  make cross-build    Build all 15 Linux/macOS targets' \
	  '                      Subset: TARGETS="linux/loong64 darwin/arm64"' \
	  '  make run            Run locally; override ARGS="sync"' \
	  '  make test           Run unit and offline protocol tests' \
	  '  make test-race      Run tests with race detection' \
	  '  make vet            Run Go static checks' \
	  '  make lint           Check formatting and run vet' \
	  '  make check          Run lint and race tests' \
	  '  make fmt            Format Go source' \
	  '  make fmt-check      Check Go formatting without editing' \
	  '  make coverage       Generate coverage.out and print coverage' \
	  '  make deps           Download Go modules' \
	  '  make clean          Remove build outputs and coverage only' \
	  'Overrides: GO, GOBIN, BIN_DIR, COVERAGE_FILE, TARGETS, ARGS'
