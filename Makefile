# Local equivalent of .github/workflows/ci.yml.
#
# Requires Go >= 1.27 (see go.mod). `go` / `gofmt` are taken from PATH on
# purpose: with the default GOTOOLCHAIN=auto the go command fetches the
# toolchain named by go.mod when the installed one is too old. gofmt has
# no toolchain switching, so point GOFMT at a 1.27+ binary if the one in
# PATH lags behind (e.g. `make check GOFMT=~/sdk/go1.27.1/bin/gofmt`).

GO ?= go
GOFMT ?= gofmt

.PHONY: check fmt-check vet test check-experiments check-sumtype check-staticcheck check-fuzz

check: fmt-check vet test check-sumtype check-staticcheck check-experiments

# gofmt is module-agnostic: one pass covers the root module and the
# nested modules under experiments/.
# `|| exit 1` also catches unparseable files: gofmt reports them on
# stderr and exits non-zero without listing them.
fmt-check:
	@unformatted="$$($(GOFMT) -l .)" || exit 1; \
	if [ -n "$$unformatted" ]; then \
		echo "files need gofmt:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

vet:
	$(GO) vet ./...

test:
	$(GO) test -race ./...

# Bounded fuzz run for the compiler fuzz targets
# (docs/decisions/2026-11-02-fuzz-strategy.md). NOT part of `check`:
# the targets suppress only bugs already registered in knownBugs —
# a run that surfaces a NEW bug class is a finding to file in
# TODO.md + the registry (the failure prints the input; the Go
# fuzzer also writes it under testdata/fuzz/), not a signal that
# `check` should stay red over. CI runs this as an advisory job.
#
# -parallel is pinned: each worker can hold one sandbox child whose
# rlimit is 3 GiB, so workers x 3 GiB must stay inside machine RAM —
# a default GOMAXPROCS=16 on a 15 GiB box was enough to OOM-kill
# workers mid-run. -run='^$$' skips regular tests; the fuzz session
# itself replays the seed corpus as baseline coverage.
#
# `go clean -fuzzcache` first: the shared build-cache corpus
# inherits every variant ever generated — including hundreds of
# inputs in the known hang family — and replaying them can burn a
# whole bounded window at ~0 execs/sec (observed). A bounded run
# should explore fresh from the checked-in corpus, matching what CI
# sees on a clean box.
FUZZTIME ?= 25s
FUZZPARALLEL ?= 4

check-fuzz:
	$(GO) clean -fuzzcache
	GOMEMLIMIT=2GiB $(GO) test ./internal/syntax -run='^$$' -fuzz='^FuzzParse$$' -fuzztime=$(FUZZTIME) -parallel=$(FUZZPARALLEL)
	GOMEMLIMIT=2GiB $(GO) test ./internal/build -run='^$$' -fuzz='^FuzzCompile$$' -fuzztime=$(FUZZTIME) -parallel=$(FUZZPARALLEL)
	# -fuzz takes one regex and errors when it matches multiple
	# fuzz targets, so each format fuzzer is anchored explicitly;
	# each gets its own FUZZTIME window.
	GOMEMLIMIT=2GiB $(GO) test ./internal/format -run='^$$' -fuzz='^FuzzFormat$$' -fuzztime=$(FUZZTIME) -parallel=$(FUZZPARALLEL)
	GOMEMLIMIT=2GiB $(GO) test ./internal/format -run='^$$' -fuzz='^FuzzFormatGen$$' -fuzztime=$(FUZZTIME) -parallel=$(FUZZPARALLEL)

# go-check-sumtype verifies type-switch exhaustiveness on the sealed
# interfaces annotated with //sumtype:decl (docs/design.md section 2.4).
# The script also runs the tool's own canary: testdata/sumtype is a
# nested module whose nonexhaustive package MUST fail the check.
check-sumtype:
	GO='$(GO)' sh scripts/check-sumtype.sh

# staticcheck over every module in the repo. Like go-check-sumtype the
# tool runs as `go run <pkg>@<pinned>` so it never enters go.mod.
check-staticcheck:
	GO='$(GO)' sh scripts/check-staticcheck.sh

# experiments/ hold nested Go modules that `./...` never reaches. Vet
# and test each module anyway so the verification code does not rot.
check-experiments:
	@set -e; \
	test -d experiments || exit 0; \
	for mod in $$(find experiments -name go.mod | sort); do \
		dir=$$(dirname "$$mod"); \
		echo "== $$dir"; \
		$(GO) -C "$$dir" vet ./...; \
		$(GO) -C "$$dir" test -race ./...; \
	done
