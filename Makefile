# Local equivalent of .github/workflows/ci.yml.
#
# Requires Go >= 1.27 (see go.mod). `go` / `gofmt` are taken from PATH on
# purpose: with the default GOTOOLCHAIN=auto the go command fetches the
# toolchain named by go.mod when the installed one is too old. gofmt has
# no toolchain switching, so point GOFMT at a 1.27+ binary if the one in
# PATH lags behind (e.g. `make check GOFMT=~/sdk/go1.27.1/bin/gofmt`).

GO ?= go
GOFMT ?= gofmt

.PHONY: check fmt-check vet test check-experiments

check: fmt-check vet test check-experiments

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
