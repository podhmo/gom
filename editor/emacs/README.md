# gom-mode for Emacs

Major mode for Gom source (`.gom`) and interface (`.gomi`) files.
Regexp based (Gom has no tree-sitter grammar yet); requires Emacs 27.1+.

```elisp
(add-to-list 'load-path "/path/to/gom/editor/emacs")
(require 'gom-mode)
(add-hook 'gom-mode-hook #'flymake-mode)   ; optional: gom check on save
```

- font-lock, indentation (`gom fmt` layout), imenu (`fn` / `type` / `let` / `test`)
- `M-x gom-format-buffer` — runs `gom fmt -`
- Flymake backend `gom-flymake` — runs `gom check` on the file's directory (saved files only)
- `gom-command` sets the `gom` executable

Tests: `make test` (font-lock tests need Emacs 30+ for `ert-font-lock`;
the `gom fmt` / `gom check` tests run when `gom` is on PATH).
Design notes: [docs/sketch/2026-11-05-emacs-gom-mode.md](../../docs/sketch/2026-11-05-emacs-gom-mode.md).
