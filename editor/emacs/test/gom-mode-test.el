;;; test/gom-mode-test.el --- ERT tests for gom-mode  -*- lexical-binding: t; -*-

;;; Commentary:
;;
;; Run from editor/emacs:
;;
;;   make test            # or: emacs --batch -Q -L . -l test/gom-mode-test.el -f ert-run-tests-batch-and-exit
;;
;; Font-lock tests use ert-font-lock annotations (Emacs 30+); they are
;; skipped on older Emacs.  Indentation tests re-indent `gom fmt' outputs
;; from the repository and expect no change.

;;; Code:

(require 'ert)
(require 'imenu)
(require 'gom-mode)
(require 'ert-font-lock nil t)

(defvar gom-mode-test--dir
  (file-name-directory (or load-file-name buffer-file-name))
  "Directory of this test file.")

(defvar gom-mode-test--repo-root
  (expand-file-name "../../../" gom-mode-test--dir)
  "Root of the gom repository.")

(defun gom-mode-test--testdata (name)
  "Return the absolute path of test/testdata/NAME."
  (expand-file-name (concat "testdata/" name) gom-mode-test--dir))

;;; Font-lock

(ert-deftest gom-mode-font-lock-gom ()
  "Validate ert-font-lock annotations in font-lock.gom."
  (skip-unless (featurep 'ert-font-lock))
  (ert-font-lock-test-file (gom-mode-test--testdata "font-lock.gom") 'gom-mode))

(ert-deftest gom-mode-font-lock-gomi ()
  "Validate ert-font-lock annotations in font-lock.gomi."
  (skip-unless (featurep 'ert-font-lock))
  (ert-font-lock-test-file (gom-mode-test--testdata "font-lock.gomi") 'gom-mode))

;;; Indentation

(defun gom-mode-test--reindent (text)
  "Return TEXT after flattening every line and re-indenting it in `gom-mode'."
  (with-temp-buffer
    (insert text)
    (gom-mode)
    (goto-char (point-min))
    (while (not (eobp))
      (unless (nth 3 (syntax-ppss (line-beginning-position)))
        (delete-horizontal-space)
        (back-to-indentation)
        (delete-region (line-beginning-position) (point)))
      (forward-line 1))
    (indent-region (point-min) (point-max))
    (buffer-string)))

(defun gom-mode-test--formatted-files ()
  "Return `gom fmt' outputs and formatted sources in the repository."
  (append
   (directory-files (expand-file-name "testdata/fmt" gom-mode-test--repo-root)
                    t "\\.out\\.gomi?\\'")
   (directory-files-recursively
    (expand-file-name "stdlib" gom-mode-test--repo-root) "\\.gomi?\\'")
   (list (gom-mode-test--testdata "indent.gom"))))

(ert-deftest gom-mode-indent-formatted-files ()
  "Re-indenting gom-fmt-formatted files leaves them unchanged."
  (dolist (file (gom-mode-test--formatted-files))
    (let* ((orig (with-temp-buffer
                   (insert-file-contents file)
                   (buffer-string)))
           (got (gom-mode-test--reindent orig)))
      (unless (equal orig got)
        (ert-fail (list file (gom-mode-test--first-diff orig got)))))))

(defun gom-mode-test--first-diff (a b)
  "Return the first differing line of A and B as (LINE EXPECTED GOT)."
  (let ((la (split-string a "\n")) (lb (split-string b "\n")) (n 1) res)
    (while (and (not res) (or la lb))
      (unless (equal (car la) (car lb))
        (setq res (list n (car la) (car lb))))
      (setq la (cdr la) lb (cdr lb) n (1+ n)))
    res))

;;; Imenu

(ert-deftest gom-mode-imenu ()
  "Imenu lists top-level declarations."
  (with-temp-buffer
    (insert-file-contents (gom-mode-test--testdata "font-lock.gom"))
    (gom-mode)
    (let* ((index (funcall imenu-create-index-function))
           (names (lambda (group) (mapcar #'car (cdr (assoc group index))))))
      (should (member "area" (mapcar #'car index)))
      (should (member "Shape" (funcall names "Type")))
      (should (member "pi" (funcall names "Let")))
      (should (member "\"rect area\"" (funcall names "Test")))
      (should (member "NewBufferString" (funcall names "Extern"))))))

;;; auto-mode-alist

(ert-deftest gom-mode-auto-mode ()
  "Both .gom and .gomi open in gom-mode."
  (dolist (name '("font-lock.gom" "font-lock.gomi"))
    (let ((buf (find-file-noselect (gom-mode-test--testdata name))))
      (unwind-protect
          (with-current-buffer buf (should (eq major-mode 'gom-mode)))
        (kill-buffer buf)))))

;;; Flymake

(ert-deftest gom-mode-flymake-diagnostics ()
  "`gom check' JSON Lines become Flymake diagnostics for this file only."
  (let ((src (find-file-noselect (gom-mode-test--testdata "check/bad.gom"))))
    (unwind-protect
        (with-temp-buffer
          (insert-file-contents (gom-mode-test--testdata "check/output.ndjson"))
          (goto-char (point-min))
          (while (search-forward "@DIR@" nil t)
            (replace-match (gom-mode-test--testdata "check") t t))
          (let ((diags (gom-flymake--diagnostics src)))
            (should (= (length diags) 1))
            (let ((d (car diags)))
              (should (eq (flymake-diagnostic-type d) :error))
              (should (equal (flymake-diagnostic-text d)
                             "E0201 undefined name `undefined_x`"))
              (with-current-buffer src
                (should (equal (buffer-substring
                                (flymake--diag-beg d)
                                (flymake--diag-end d))
                               "undefined_x"))))))
      (kill-buffer src))))

(ert-deftest gom-mode-flymake-live ()
  "Run the backend against the real `gom check' when gom is installed."
  (skip-unless (executable-find gom-command))
  (let ((src (find-file-noselect (gom-mode-test--testdata "check/bad.gom")))
        result)
    (unwind-protect
        (with-current-buffer src
          (gom-flymake (lambda (diags &rest _) (setq result (or diags 'none))))
          (with-timeout (30 (ert-fail "gom check timed out"))
            (while (not result) (accept-process-output nil 0.1)))
          (should (listp result))
          (should (= (length result) 1))
          (should (equal (buffer-substring
                          (flymake--diag-beg (car result))
                          (flymake--diag-end (car result)))
                         "undefined_x")))
      (kill-buffer src))))

;;; Formatting

(ert-deftest gom-mode-format-buffer ()
  "`gom-format-buffer' rewrites the buffer with `gom fmt' output."
  (skip-unless (executable-find gom-command))
  (with-temp-buffer
    (insert "package main\nlet x=1\nfn f (a: Int) -> Int = a+1\n")
    (gom-mode)
    (gom-format-buffer)
    (should (equal (buffer-string)
                   "package main\n\nlet x = 1\n\nfn f (a: Int) -> Int = a + 1\n"))))

(ert-deftest gom-mode-format-buffer-error ()
  "`gom-format-buffer' leaves the buffer unchanged on a syntax error."
  (skip-unless (executable-find gom-command))
  (with-temp-buffer
    (insert "package main\nlet x =\n")
    (gom-mode)
    (gom-format-buffer)
    (should (equal (buffer-string) "package main\nlet x =\n"))))

(provide 'gom-mode-test)

;;; gom-mode-test.el ends here
