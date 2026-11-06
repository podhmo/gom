EMACS ?= emacs
TREE_SITTER ?= tree-sitter
CC ?= cc

GRAMMAR_DIR := ../../tree-sitter-gom
TS_LIB_DIR := $(CURDIR)/.tree-sitter
TS_LIB := $(TS_LIB_DIR)/libtree-sitter-gom.so

.PHONY: test test-regex test-treesit

# `test` runs the suite twice: once with the regexp fallback (no
# grammar on the load path) and once through the tree-sitter path.
#
# test-regex points user-emacs-directory at a scratch dir so a gom
# grammar installed under ~/.emacs.d/tree-sitter cannot leak in and
# silently run the "regex" suite through treesit.  (A grammar placed
# in a system-wide tree-sitter directory would still be found; that is
# rare enough not to bother sandboxing.)
#
# test-treesit builds the grammar into .tree-sitter/ first.  It skips
# only when the grammar cannot be produced at all — no committed
# src/parser.c *and* no tree-sitter CLI, or no C compiler.  Once the
# sources exist a compile failure is real breakage and fails the
# target (parser.c and scanner.c are committed to the repository).
test: test-regex test-treesit

test-regex:
	$(EMACS) --batch -Q \
	  --eval "(setq user-emacs-directory (expand-file-name \"gom-ert-emacs.d/\" temporary-file-directory))" \
	  -L . \
	  -l test/gom-mode-test.el \
	  -f ert-run-tests-batch-and-exit

test-treesit:
	@mkdir -p $(TS_LIB_DIR)
	@if [ ! -f "$(GRAMMAR_DIR)/src/parser.c" ]; then \
	  if command -v $(TREE_SITTER) >/dev/null 2>&1; then \
	    (cd $(GRAMMAR_DIR) && $(TREE_SITTER) generate) || exit 1; \
	  else \
	    echo "test-treesit: no src/parser.c and no tree-sitter CLI; skipping"; \
	    exit 0; \
	  fi; \
	fi
	@if ! command -v $(CC) >/dev/null 2>&1; then \
	  echo "test-treesit: no C compiler ($(CC)); skipping"; \
	  exit 0; \
	fi
	@$(CC) -shared -fPIC -O2 -o $(TS_LIB) \
	  $(GRAMMAR_DIR)/src/parser.c $(GRAMMAR_DIR)/src/scanner.c \
	  || { echo "test-treesit: grammar failed to compile"; exit 1; }
	$(EMACS) --batch -Q \
	  -L . \
	  --eval "(setq treesit-extra-load-path '(\"$(TS_LIB_DIR)\"))" \
	  -l test/gom-mode-test.el \
	  -f ert-run-tests-batch-and-exit

clean:
	rm -rf $(TS_LIB_DIR)

.PHONY: clean
