;;; gom-mode.el --- Major mode for Gom  -*- lexical-binding: t; -*-

;; Author: podhmo
;; Keywords: gom languages
;; Version: 0.1.0
;; Package-Requires: ((emacs "27.1"))

;; This file is NOT part of GNU Emacs.

;;; Commentary:

;; `gom-mode' is a major mode for Gom source files (.gom) and interface
;; files (.gomi).  A .gomi uses the same tokens as a .gom and only holds
;; body-less declarations, so one mode serves both.
;;
;; Gom has no tree-sitter grammar yet, so highlighting and imenu are
;; regexp based.  Indentation follows the layout `gom fmt' produces.
;;
;; Features:
;;   - font-lock
;;   - indentation (2 spaces, bracket depth + continuation lines)
;;   - imenu for fn / type / let / test
;;   - `gom-format-buffer' (gom fmt -)
;;   - Flymake backend `gom-flymake' (gom check DIR)
;;
;; Usage:
;;   (add-to-list 'load-path "/path/to/gom/editor/emacs")
;;   (require 'gom-mode)
;;   (add-hook 'gom-mode-hook #'flymake-mode)     ; optional

;;; Code:

(require 'json)
(require 'flymake)

(defgroup gom nil
  "Major mode for the Gom language."
  :group 'languages
  :prefix "gom-")

(defcustom gom-indent-offset 2
  "Indentation width for `gom-mode'.  `gom fmt' uses 2."
  :type 'integer
  :safe #'integerp
  :group 'gom)

(defcustom gom-command "gom"
  "Name or path of the gom executable."
  :type 'string
  :group 'gom)

;;; Faces

(defface gom-doc-comment-face
  '((t :inherit font-lock-doc-face))
  "Face for /// doc comments."
  :group 'gom)

(defface gom-label-face
  '((t :inherit font-lock-constant-face))
  "Face for labeled arguments such as ~name."
  :group 'gom)

(defface gom-number-face
  '((t :inherit font-lock-constant-face))
  "Face for numeric literals."
  :group 'gom)

(defface gom-interpolation-face
  '((t :inherit font-lock-variable-name-face))
  "Face for ${...} inside string literals."
  :group 'gom)

;;; Syntax table

(defvar gom-mode-syntax-table
  (let ((table (make-syntax-table)))
    (modify-syntax-entry ?_  "_"      table)
    (modify-syntax-entry ?/  ". 124b" table)  ; // and /* */
    (modify-syntax-entry ?*  ". 23"   table)
    (modify-syntax-entry ?\n "> b"    table)
    (modify-syntax-entry ?\\ "\\"     table)
    (modify-syntax-entry ?\" "\""     table)
    (modify-syntax-entry ?\' "\""     table)  ; char literal; Gom has no primed idents
    (dolist (c '(?+ ?- ?= ?< ?> ?| ?& ?% ?~ ?: ?. ?@ ?? ?!))
      (modify-syntax-entry c "." table))
    table)
  "Syntax table for `gom-mode'.")

;;; Font-lock

(defconst gom-keywords
  '("as" "default" "else" "extern" "false" "fn" "for" "go" "if" "ignore"
    "import" "in" "let" "match" "package" "select" "send" "test" "true"
    "try" "type" "with")
  "Reserved words (docs/grammar.ebnf 2.2).")

(defconst gom-builtin-functions
  '("assert" "assert_eq" "todo" "not" "show" "compare")
  "Ordinary functions provided by the language (spec 1.1, 11.4).")

(defconst gom--not-after-dot "\\(?:^\\|[^.[:alnum:]_]\\)"
  "Regexp that matches the position before a name not preceded by `.'.
After `.' any keyword is a plain member name (`Chan.send').")

(defun gom--interpolation-matcher (limit)
  "Find the next ${...} inside a string before LIMIT."
  (let (found)
    (while (and (not found)
                (re-search-forward "\\${[^}\n]*}" limit t))
      (when (nth 3 (save-excursion (syntax-ppss (match-beginning 0))))
        (setq found t)))
    found))

(defun gom--doc-comment-matcher (limit)
  "Find the next /// comment before LIMIT."
  (let (found)
    (while (and (not found)
                (re-search-forward "///.*$" limit t))
      (when (nth 4 (save-excursion (syntax-ppss (+ 2 (match-beginning 0)))))
        (setq found t)))
    found))

(defconst gom-font-lock-keywords
  `(;; doc comments
    (gom--doc-comment-matcher 0 'gom-doc-comment-face t)
    ;; ${...} inside strings
    (gom--interpolation-matcher 0 'gom-interpolation-face t)
    ;; attributes: @test @tailrec
    ("@[[:alpha:]_][[:alnum:]_]*" . font-lock-preprocessor-face)
    ;; keywords (not after `.')
    (,(concat gom--not-after-dot "\\(" (regexp-opt gom-keywords 'words) "\\)")
     1 font-lock-keyword-face)
    ;; fn NAME (  — declarations and extern items; lambdas have no name
    ("\\_<fn[ \t]+\\([[:alpha:]_][[:alnum:]_]*\\)[ \t]*("
     1 font-lock-function-name-face)
    ;; type NAME
    ("\\_<type[ \t]+\\([[:upper:]][[:alnum:]_]*\\)" 1 font-lock-type-face)
    ;; let NAME
    ("\\_<let[ \t]+\\([[:lower:]_][[:alnum:]_]*\\)" 1 font-lock-variable-name-face)
    ;; package NAME / import [go] ALIAS "path"
    ("\\_<package[ \t]+\\([[:alpha:]_][[:alnum:]_]*\\)" 1 font-lock-constant-face)
    ("\\_<import[ \t]+\\(?:go[ \t]+\\)?\\([[:alpha:]_][[:alnum:]_]*\\)[ \t]+\""
     1 font-lock-constant-face)
    ;; labels: ~name
    ("~[[:lower:]_][[:alnum:]_]*" . 'gom-label-face)
    ;; builtin functions (not after `.')
    (,(concat gom--not-after-dot "\\(" (regexp-opt gom-builtin-functions 'symbols) "\\)")
     1 font-lock-builtin-face)
    ;; uppercase names: types, constructors, modules (not Go members after `.')
    (,(concat gom--not-after-dot "\\(\\_<[[:upper:]][[:alnum:]_]*\\_>\\)")
     1 font-lock-type-face)
    ;; numbers
    ("\\_<\\(?:0[xX][[:xdigit:]_]+\\|0[oO][0-7_]+\\|0[bB][01_]+\\|[0-9][0-9_]*\\(?:\\.[0-9][0-9_]*\\)?\\(?:[eE][-+]?[0-9]+\\)?\\)\\_>"
     . 'gom-number-face))
  "Font-lock keywords for `gom-mode'.")

;;; Indentation

(defconst gom--continuation-end-re
  (concat "\\(?:" (regexp-opt '("=" "->" "<-" "|>" "<|" "|?>" ">>" "<<"
                                "||" "&&" "==" "!=" "<" "<=" ">" ">="
                                "+" "-" "*" "/" "%" "|" ":"))
          "\\)[ \t]*$")
  "A line ending with one of these continues on the next line.")

(defconst gom--continuation-start-re
  (concat "[ \t]*\\(?:" (regexp-opt '("|>" "<|" "|?>" ">>" "<<" "||" "&&"
                                      "==" "!=" "<=" ">=" "+" "*" "%"
                                      "|"))
          "\\|-[^>]\\|<[^-|<=]\\|>[^>=]\\|/[^/*]\\)")
  "A line starting with one of these continues the previous line.")

(defun gom--line-code-end ()
  "Return the position after the last code character on the current line."
  (save-excursion
    (end-of-line)
    (let ((bol (line-beginning-position))
          (ppss (syntax-ppss)))
      (when (nth 4 ppss) (goto-char (nth 8 ppss)))
      (skip-chars-backward " \t" bol)
      (while (and (> (point) (1+ bol))
                  (eq (char-before) ?/)
                  (eq (char-before (1- (point))) ?*)
                  (nth 4 (syntax-ppss (1- (point)))))
        (forward-comment -1)
        (skip-chars-backward " \t" bol))
      (max (point) bol))))

(defun gom--previous-code-line ()
  "Move to the previous line that has code; return non-nil on success."
  (let (found)
    (while (and (not found) (zerop (forward-line -1)))
      (back-to-indentation)
      (unless (or (eolp)
                  (nth 4 (syntax-ppss))
                  (looking-at-p "//\\|/\\*"))
        (setq found t)))
    found))

(defun gom--next-code-line ()
  "Move to the next line that has code; return non-nil on success."
  (let (found)
    (while (and (not found) (zerop (forward-line 1)) (not (eobp)))
      (back-to-indentation)
      (unless (or (eolp)
                  (nth 4 (syntax-ppss))
                  (looking-at-p "//\\|/\\*"))
        (setq found t)))
    found))

(defun gom--line-ends-continued-p ()
  "Non-nil if the current line's code ends with a continuation operator."
  (let ((end (gom--line-code-end)))
    (save-excursion
      (goto-char end)
      (and (> end (line-beginning-position))
           (looking-back gom--continuation-end-re (line-beginning-position))
           ;; `|' alone ends a variant only in "| Ctor |" chains; treat as continued
           t))))

(defun gom--previous-continued-line (opener)
  "Return the start of the previous code line if the current line continues it.
The previous line must lie after OPENER at the same bracket depth."
  (save-excursion
    (back-to-indentation)
    (let ((depth (nth 0 (syntax-ppss)))
          (starts-continued (looking-at-p gom--continuation-start-re)))
      (when (and (gom--previous-code-line)
                 (or (null opener) (> (point) opener))
                 (= (nth 0 (syntax-ppss)) depth)
                 (or starts-continued (gom--line-ends-continued-p)))
        (point)))))

(defun gom--statement-start (opener)
  "Return the indentation column of the statement the current line belongs to.
OPENER bounds the search: lines before it belong to an outer level."
  (save-excursion
    (let (prev)
      (while (setq prev (gom--previous-continued-line opener))
        (goto-char prev)))
    (current-indentation)))

(defun gom--calculate-indentation ()
  "Return the column the current line should be indented to, or nil."
  (save-excursion
    (back-to-indentation)
    (let* ((ppss (syntax-ppss))
           (opener (nth 1 ppss))
           (closing (looking-at-p "[])}]"))
           (base (if opener
                     (save-excursion
                       (goto-char opener)
                       (+ (current-indentation)
                          (if closing 0 gom-indent-offset)))
                   0)))
      (cond
       ((or (nth 3 ppss) (nth 4 ppss)) nil)     ; inside string / block comment
       (closing base)
       ((looking-at-p "//\\|/\\*")
        ;; A comment line takes the indentation of the code it precedes
        ;; (`/// docs' before `| Ctor'), unless that code closes the block.
        (let ((next (save-excursion
                      (and (gom--next-code-line)
                           (eq (nth 1 (syntax-ppss)) opener)
                           (not (looking-at-p "[])}]"))
                           (gom--calculate-indentation)))))
          (or next base)))
       ((gom--previous-continued-line opener)
        (save-excursion
          (goto-char (gom--previous-continued-line opener))
          (+ (gom--statement-start opener) gom-indent-offset)))
       (t base)))))

(defun gom-indent-line ()
  "Indent the current line as Gom code."
  (interactive)
  (let ((col (gom--calculate-indentation)))
    (when col
      (if (<= (current-column) (current-indentation))
          (indent-line-to col)
        (save-excursion (indent-line-to col))))))

;;; Imenu

(defvar gom-imenu-generic-expression
  '(("Test" "^test[ \t]+\\(\"[^\"\n]*\"\\)" 1)
    ("Let" "^let[ \t]+\\([[:lower:]_][[:alnum:]_]*\\)" 1)
    ("Type" "^type[ \t]+\\([[:upper:]][[:alnum:]_]*\\)" 1)
    ("Extern" "^[ \t]+fn[ \t]+\\([[:alpha:]_][[:alnum:]_]*\\)[ \t]*(" 1)
    (nil "^fn[ \t]+\\([[:alpha:]_][[:alnum:]_]*\\)" 1))
  "Imenu entries for top-level Gom declarations.")

;;; Formatting

;;;###autoload
(defun gom-format-buffer ()
  "Format the current buffer with `gom fmt -'.
On failure the buffer is left unchanged and the diagnostics are shown."
  (interactive)
  (unless (executable-find gom-command)
    (user-error "%s executable not found in PATH" gom-command))
  (let ((out (generate-new-buffer " *gom-fmt-output*"))
        (err (make-temp-file "gom-fmt-error-")))
    (unwind-protect
        (let ((code (call-process-region (point-min) (point-max) gom-command
                                         nil (list out err) nil "fmt" "-")))
          (if (zerop code)
              (progn
                (unless (zerop (compare-buffer-substrings
                                nil nil nil out nil nil))
                  (with-no-warnings (replace-buffer-contents out)))
                (message "gom fmt: formatted"))
            (let ((msgs (gom--fmt-error-messages err)))
              (message "gom fmt failed: %s"
                       (if msgs (mapconcat #'identity msgs "; ")
                         (format "exit %d" code))))))
      (kill-buffer out)
      (delete-file err))))

(defun gom--fmt-error-messages (file)
  "Return \"LINE:COL: message\" strings for diagnostics in FILE."
  (with-temp-buffer
    (insert-file-contents file)
    (let (msgs)
      (dolist (obj (gom--read-json-lines))
        (when (equal (alist-get 'kind obj) "diagnostic")
          (let-alist obj
            (push (format "%s:%s: %s" .span.start.line .span.start.col .message)
                  msgs))))
      (nreverse msgs))))

;;; Flymake

(defun gom--read-json-lines ()
  "Parse every JSON object line in the current buffer into alists."
  (let (objs)
    (save-excursion
      (goto-char (point-min))
      (while (not (eobp))
        (when (looking-at-p "{")
          (condition-case nil
              (push (let ((json-object-type 'alist)
                          (json-array-type 'list))
                      (json-read-from-string
                       (buffer-substring-no-properties
                        (point) (line-end-position))))
                    objs)
            (error nil)))
        (forward-line 1)))
    (nreverse objs)))

(defun gom--position (source line col)
  "Return the position of LINE / COL (both 1-based, COL in chars) in SOURCE."
  (with-current-buffer source
    (save-excursion
      (save-restriction
        (widen)
        (goto-char (point-min))
        (forward-line (1- line))
        (min (+ (point) (max 0 (1- col))) (line-end-position))))))

(defun gom-flymake--diagnostics (source)
  "Turn `gom check' output in the current buffer into Flymake diagnostics.
Only diagnostics for SOURCE's file are kept."
  (let ((file (expand-file-name (buffer-file-name source)))
        diags)
    (dolist (obj (gom--read-json-lines))
      (let-alist obj
        (when (and (equal .kind "diagnostic")
                   (stringp .span.file)
                   (equal (expand-file-name .span.file) file))
          (let* ((beg (gom--position source .span.start.line .span.start.col))
                 (end (gom--position source .span.end.line .span.end.col))
                 (end (if (<= end beg)
                          (with-current-buffer source
                            (save-excursion
                              (goto-char beg)
                              (if (eolp) (min (point-max) (1+ beg))
                                (gom--symbol-end-or-next beg))))
                        end)))
            (push (flymake-make-diagnostic
                   source beg end
                   (pcase .severity
                     ("error" :error)
                     ("warning" :warning)
                     (_ :note))
                   (if .code (format "%s %s" .code .message) .message))
                  diags)))))
    (nreverse diags)))

(defun gom--symbol-end-or-next (pos)
  "Return the end of the symbol at POS, or POS + 1."
  (save-excursion
    (goto-char pos)
    (if (> (skip-syntax-forward "w_") 0) (point) (1+ pos))))

(defvar-local gom-flymake--proc nil)

(defun gom-flymake (report-fn &rest _args)
  "Flymake backend running `gom check' on the buffer's directory.
`gom check' does not read stdin, so the saved file is checked.
REPORT-FN is Flymake's callback."
  (when (process-live-p gom-flymake--proc)
    (kill-process gom-flymake--proc))
  (let ((source (current-buffer)))
    (cond
     ((not (buffer-file-name))
      (funcall report-fn :panic :explanation "buffer has no file"))
     ((not (executable-find gom-command))
      (funcall report-fn :panic
               :explanation (format "%s not found" gom-command)))
     (t
      (let* ((dir (file-name-directory (expand-file-name (buffer-file-name))))
             (out (generate-new-buffer " *gom-flymake*"))
             (default-directory dir))
        (setq gom-flymake--proc
              (make-process
               :name "gom-flymake"
               :buffer out
               :command (list gom-command "check" dir)
               :noquery t
               :connection-type 'pipe
               :stderr (get-buffer-create " *gom-flymake-stderr*")
               :sentinel
               (lambda (p _event)
                 (unless (process-live-p p)
                   (unwind-protect
                       (when (and (buffer-live-p source)
                                  (eq p (buffer-local-value
                                         'gom-flymake--proc source)))
                         (if (memq (process-exit-status p) '(0 1))
                             (with-current-buffer out
                               (funcall report-fn
                                        (gom-flymake--diagnostics source)))
                           (funcall report-fn :panic
                                    :explanation
                                    (format "gom check exited with %d"
                                            (process-exit-status p)))))
                     (kill-buffer out)))))))))))

;;; Major mode

;;;###autoload
(define-derived-mode gom-mode prog-mode "Gom"
  "Major mode for Gom source (.gom) and interface (.gomi) files.

\\{gom-mode-map}"
  :group 'gom
  :syntax-table gom-mode-syntax-table
  (setq-local comment-start "// ")
  (setq-local comment-end "")
  (setq-local comment-start-skip "\\(?://+\\|/\\*+\\)[ \t]*")
  (setq-local font-lock-defaults '(gom-font-lock-keywords))
  (setq-local indent-line-function #'gom-indent-line)
  (setq-local indent-tabs-mode nil)
  (setq-local electric-indent-chars
              (append '(?\} ?\) ?\] ?|) electric-indent-chars))
  (setq-local imenu-generic-expression gom-imenu-generic-expression)
  (add-hook 'flymake-diagnostic-functions #'gom-flymake nil t))

;;;###autoload
(add-to-list 'auto-mode-alist '("\\.gomi?\\'" . gom-mode))

(provide 'gom-mode)

;;; gom-mode.el ends here
