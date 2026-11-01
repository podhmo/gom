# testdata

Test fixtures for the gom compiler (docs/design.md section 3):

- `spec/` — one `.gom` file per spec rule, together with the expected
  diagnostics as JSON
- `golden/` — golden files of the Go code produced by codegen
- `fmt/` — formatter inputs and their expected outputs
- `sumtype/` — canary module for the go-check-sumtype CI step:
  `nonexhaustive/` MUST keep failing the check (scripts/check-sumtype.sh)

The `.gitkeep` files are placeholders so git tracks the directories until
real fixtures land.
