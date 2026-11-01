// Canary fixtures for the go-check-sumtype step of `make check`
// (TODO B-3). This is a nested module so that the root `./...`
// patterns never see it: go-check-sumtype must report the deliberate
// missing case in ./nonexhaustive, and scripts/check-sumtype.sh
// asserts that failure. Do not fix the switch there — the missing
// variant is the point.
module github.com/podhmo/gom/testdata/sumtype

go 1.27
