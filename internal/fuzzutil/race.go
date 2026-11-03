//go:build race

package fuzzutil

// raceEnabled reports whether the binary was built with -race. The
// race runtime reserves shadow address space that RLIMIT_AS counts,
// so a `ulimit -v`-capped child can die environment-dependently with
// "too many address space collisions for -race mode" (see abnormal).
const raceEnabled = true
