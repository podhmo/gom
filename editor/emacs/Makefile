EMACS ?= emacs

.PHONY: test

test:
	$(EMACS) --batch -Q \
	  -L . \
	  -l test/gom-mode-test.el \
	  -f ert-run-tests-batch-and-exit
