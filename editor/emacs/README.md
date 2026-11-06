# gom-mode for Emacs

Major mode for Gom source (`.gom`) and interface (`.gomi`) files.
Requires Emacs 27.1+; on Emacs 29+ with the [tree-sitter-gom
grammar](../../tree-sitter-gom/) installed, font-lock, imenu and
indentation run on `treesit`.  Otherwise the mode falls back to a
regexp-based implementation — set `gom-use-tree-sitter` to nil to
force the fallback even when the grammar is available.

```elisp
(add-to-list 'load-path "/path/to/gom/editor/emacs")
(require 'gom-mode)
(add-hook 'gom-mode-hook #'flymake-mode)   ; optional: gom check on save
```

To install the grammar with `treesit-install-language-grammar`:

```elisp
(add-to-list 'treesit-language-source-alist
             '(gom . ("file:///path/to/gom/tree-sitter-gom")))
;; M-x treesit-install-language-grammar RET gom RET
```

- font-lock, indentation (`gom fmt` layout), imenu (`fn` / `type` / `let` / `test` / `extern`)
- `M-x gom-format-buffer` — runs `gom fmt -`
- Flymake backend `gom-flymake` — runs `gom check` on the file's directory (saved files only)
- `gom-command` sets the `gom` executable
- `gom-use-tree-sitter` (default t) — use the grammar when `treesit-ready-p` finds it

Tests: `make test` runs the suite twice — once against the regexp
fallback, once with the grammar compiled into `.tree-sitter/`
(`make test-regex` / `make test-treesit` individually).  Font-lock
tests need Emacs 30+ for `ert-font-lock`; the `gom fmt` / `gom check`
tests run when `gom` is on PATH.

Byte-compiling on Emacs 27 warns about `treesit-*` functions — they
are only called behind the `fboundp`/`gom-use-tree-sitter` guards, so
the warnings are harmless noise on versions without tree-sitter.

Design notes: [docs/sketch/2026-11-05-emacs-gom-mode.md](../../docs/sketch/2026-11-05-emacs-gom-mode.md).
