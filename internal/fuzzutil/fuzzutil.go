// Package fuzzutil holds the machinery shared by the compiler's Go
// fuzz targets (docs/decisions/2026-11-02-fuzz-strategy.md): seed
// reading, the per-input watchdog and allocation guards, and the
// knownBugs bookkeeping that keeps already-filed bugs from failing
// the run. Only *_test.go files import it.
package fuzzutil

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// KnownBug links one checked-in repro input (by SHA-256 of its
// bytes) to the failure signature of a bug already filed in TODO.md.
// Match is the set of substrings a failure text must all contain to
// count as this bug; TODO must name a substring of an open
// (`- [ ]` or `- [-]`) TODO.md entry so entries cannot outlive their
// task.
//
// Skip marks inputs that cannot be executed at all: a non-terminating
// compile leaks its goroutine, and the leak keeps allocating until
// the whole test binary is OOM-killed — running the repro inside
// `go test` takes down the package suite. The corpus file and the
// TODO link still pin the bug, and CheckRegistry forces the entry
// out once the TODO item resolves.
type KnownBug struct {
	Match []string
	TODO  string
	Desc  string
	Skip  bool
}

// Sum256 keys an input by content.
func Sum256(b []byte) [32]byte { return sha256.Sum256(b) }

// Sum256Hex decodes a SHA-256 hex constant; it panics on a malformed
// string so a typo fails loudly at init.
func Sum256Hex(s string) [32]byte {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 32 {
		panic("bad knownBugs hash " + s)
	}
	return [32]byte(b)
}

// BugOf reports which registered bug a failure text belongs to — the
// one whose Match substrings all appear.
func BugOf(failure string, bugs map[[32]byte]KnownBug) (KnownBug, bool) {
	for _, bug := range bugs {
		if len(bug.Match) == 0 {
			continue
		}
		match := true
		for _, sub := range bug.Match {
			if !strings.Contains(failure, sub) {
				match = false
				break
			}
		}
		if match {
			return bug, true
		}
	}
	return KnownBug{}, false
}

// Report applies the knownBugs policy to one input's failures:
// failures matching a registered bug (its own input's signature or
// any signature — mutation runs keep rediscovering the same bug
// through new inputs) are logged and skipped; stale entries and new
// bugs fail the test.
func Report(t *testing.T, src []byte, failures []string, bugs map[[32]byte]KnownBug) {
	t.Helper()
	bug, known := bugs[Sum256(src)]
	if len(failures) == 0 {
		if known {
			t.Errorf("knownBugs entry is stale (bug fixed?): %q\n%s\n%s — remove the entry, its repro seed/corpus file and the TODO item",
				src, bug.Desc, bug.TODO)
		}
		return
	}
	if known {
		for _, s := range failures {
			if _, ok := BugOf(s, bugs); !ok {
				t.Errorf("input listed in knownBugs failed differently\n%s\n%s\n%s\ninput: %q",
					bug.Desc, bug.TODO, s, src)
				return
			}
		}
		t.Logf("known bug, not reporting %d failure(s): %s (%s)",
			len(failures), bug.Desc, bug.TODO)
		return
	}
	for _, s := range failures {
		if bug, ok := BugOf(s, bugs); ok {
			t.Logf("failure matches known bug signature %q — not reporting (input variant): %s",
				bug.Desc, s)
			continue
		}
		t.Errorf("%s\ninput: %q", s, src)
	}
}

// CheckRegistry pins the bookkeeping rules a fuzz target's knownBugs
// map must obey: the list stays under cap (a growing list means
// findings accumulate faster than fixes) and every entry names an
// open TODO.md item — an entry whose task was checked off (`- [x]`)
// is an orphan, even while the completed line still carries the text.
func CheckRegistry(t *testing.T, bugs map[[32]byte]KnownBug, cap int, todoPath string) {
	t.Helper()
	if len(bugs) > cap {
		t.Fatalf("knownBugs has %d entries, cap is %d", len(bugs), cap)
	}
	todo, err := os.ReadFile(todoPath)
	if err != nil {
		t.Fatalf("read TODO.md: %v", err)
	}
	for h, bug := range bugs {
		if len(bug.Match) == 0 || bug.TODO == "" {
			t.Errorf("knownBugs %x: Match and TODO must be non-empty", h[:8])
		}
		if !openItemsContain(string(todo), bug.TODO) {
			t.Errorf("knownBugs %x: TODO.md has no open entry containing %q", h[:8], bug.TODO)
		}
	}
}

// openItemsContain reports whether needle appears within one open
// task line of todoSrc — `- [ ]` unchecked or `- [-]` partially done
// (still live work under .claude/skills/todo-md). Checked-off `- [x]`
// lines and non-item text never satisfy a link: the entry is stale.
func openItemsContain(todoSrc, needle string) bool {
	for line := range strings.SplitSeq(todoSrc, "\n") {
		item := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(item, "- [ ]") && !strings.HasPrefix(item, "- [-]") {
			continue
		}
		if strings.Contains(line, needle) {
			return true
		}
	}
	return false
}

// SkipInput reports whether the input is pinned as unsafe to run —
// a confirmed repro whose sandbox run always burns its timeout
// budget for no new information.
func SkipInput(src []byte, bugs map[[32]byte]KnownBug) (KnownBug, bool) {
	bug, ok := bugs[Sum256(src)]
	return bug, ok && bug.Skip
}

// LooksHangSuspect reports whether src has the shape of the
// ctor-pattern + stray '-' infinite-loop family the fuzzer found
// (TODO "match の腕 パースが EOF で無限ループする" — since fixed,
// and the corpus entries now run live as regression seeds): every
// verified repro — `match r {A -`, `let A -`, `let Q a -`,
// `Num n - n` mid-match, `let Q0\xff-` — combines a
// `let`/`match`/`select` keyword, an uppercase-ident token (the
// ctor pattern head), and a '-' that is not part of '->'. Callers
// use it to shrink a suspect's phase timeout (RunCapped), never to
// skip running: the family is fixed but the mutator keeps emitting
// same-shape variants, and a regression or sibling hang class
// would burn the full timeout on each one. A suspect still runs in
// the sandbox and any non-hang failure reports normally. False
// positives only cost a shorter budget on a legit input.
func LooksHangSuspect(src []byte) bool {
	if !bytes.Contains(src, []byte("let")) &&
		!bytes.Contains(src, []byte("match")) &&
		!bytes.Contains(src, []byte("select")) {
		return false
	}
	upper := false
	for _, b := range src {
		if b >= 'A' && b <= 'Z' {
			upper = true
			break
		}
	}
	if !upper {
		return false
	}
	for i, b := range src {
		if b == '-' && (i+1 == len(src) || src[i+1] != '>') {
			return true
		}
	}
	return false
}

// EnvSandbox marks a helper test as the child side of Sandbox —
// the helper skips itself unless the variable is set, so `go test`
// never runs it directly.
const EnvSandbox = "GO_FUZZ_SANDBOX"

// SandboxPhase prints a phase marker on the helper's stdout; the
// parent attributes a killed hang to the last marker seen.
func SandboxPhase(name string) { fmt.Println("PHASE " + name) }

// SandboxFail prints one failure on the helper's stdout, base64 so
// embedded newlines and binary bytes survive the transport.
func SandboxFail(failure string) {
	fmt.Println("FAIL " + base64.StdEncoding.EncodeToString([]byte(failure)))
}

// ServeSandbox is the child-side loop inside an EnvSandbox-gated
// helper test: it reads length-prefixed inputs on stdin and calls
// fn for each, closing every round with a DONE marker. It returns
// when stdin closes; a hang or fatal crash inside fn kills only
// this process — the parent respawns it for the next input.
func ServeSandbox(t *testing.T, fn func(src []byte)) {
	t.Helper()
	r := bufio.NewReader(os.Stdin)
	for {
		var n uint64
		if err := binary.Read(r, binary.BigEndian, &n); err != nil {
			return
		}
		src := make([]byte, n)
		if _, err := io.ReadFull(r, src); err != nil {
			return
		}
		fn(src)
		fmt.Println("DONE")
	}
}

// Sandbox is a long-lived disposable subprocess running this test
// binary's EnvSandbox-gated helper test. It exists because an
// in-process hang cannot be undone: the match-arm loop (and any
// hang class not yet known) leaks a goroutine that allocates GB/s
// of live diagnostics, OOM-killing the whole fuzz worker seconds
// later — taking the repro down with it (observed repeatedly:
// workers die mid-minimize and the fuzzer files a non-reproducing
// corpus input). Routing EVERY input through a killable child is
// ~0.5 ms of IPC, removes the heuristics that guesses which inputs
// hang, and turns fatal crashes into captured failures instead of
// dead workers.
//
// Lifecycle: the child spawns lazily and is killed + respawned
// automatically after any timeout or crash. The child gets
// `ulimit -v` vsizeKB (via sh when available) so a diagnostic-spam
// loop dies on allocation failure in ~1–2 s rather than growing
// into unreapable D state (observed: a 6.7 GB spinning child that
// stalled every worker for minutes). Protocol: "PHASE <name>" as
// each stage begins, "FAIL <base64>" per failure, "DONE" per
// input. On timeout the parent synthesizes
// "<phase> did not return within <timeout>" — so knownBugs
// signatures classify it the same as any other reported hang.
type Sandbox struct {
	helper  string
	timeout time.Duration
	vsizeKB int

	// phases optionally overrides the timeout for a PHASE name —
	// a hanging parse needs ~1.5 s to confirm while a compile
	// phase legitimately takes seconds; without it a parse hang
	// inside the compile helper would burn the full compile
	// budget on every variant.
	phases map[string]time.Duration

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan string
	waited chan error
	stderr *truncWriter
}

// NewSandbox describes a disposable helper; nothing spawns until
// the first Run.
func NewSandbox(helper string, timeout time.Duration, vsizeKB int) *Sandbox {
	return &Sandbox{helper: helper, timeout: timeout, vsizeKB: vsizeKB}
}

// SetPhaseTimeout bounds one named PHASE's turn at d instead of
// the default timeout. Call before the first Run.
func (s *Sandbox) SetPhaseTimeout(phase string, d time.Duration) {
	if s.phases == nil {
		s.phases = map[string]time.Duration{}
	}
	s.phases[phase] = d
}

// Run feeds src to the child and returns its reported failures.
func (s *Sandbox) Run(t *testing.T, src []byte) []string {
	return s.RunCapped(t, src, nil)
}

// RunCapped is Run with per-input phase timeouts: caps[phase]
// replaces that phase's budget for this input only. Hang-family
// suspects get a tiny cap — a confirmed hang costs ~cap plus one
// respawn instead of the full budget — while non-hang failures
// still arrive as FAIL lines and report normally.
func (s *Sandbox) RunCapped(t *testing.T, src []byte, caps map[string]time.Duration) (failures []string) {
	t.Helper()
	if s.cmd == nil {
		if err := s.spawn(); err != nil {
			t.Fatalf("sandbox spawn: %v", err)
		}
	}
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(src)))
	_, werr := s.stdin.Write(n[:])
	if werr == nil {
		_, werr = s.stdin.Write(src)
	}
	if werr != nil {
		s.reap()
		return s.abnormal(src, "", s.timeout)
	}
	timer := time.NewTimer(s.timeout)
	defer timer.Stop()
	phase, budget := "", s.timeout
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				// stdout closed: child exited or was killed.
				s.reap()
				return s.abnormal(src, phase, budget)
			}
			if after, ok0 := strings.CutPrefix(line, "PHASE "); ok0 {
				phase = after
				d, ok := caps[phase]
				if !ok {
					d, ok = s.phases[phase]
				}
				if !ok {
					d = s.timeout
				}
				budget = d
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(d)
				continue
			}
			if line == "DONE" {
				return failures
			}
			if after, ok0 := strings.CutPrefix(line, "FAIL "); ok0 {
				dec, err := base64.StdEncoding.DecodeString(after)
				if err != nil {
					failures = append(failures, fmt.Sprintf("sandbox helper emitted bad FAIL line %q", line))
					continue
				}
				failures = append(failures, string(dec))
			}
		case <-timer.C:
			// Hang: kill and reap (bounded), attribute to the last
			// phase marker.
			s.kill()
			return []string{fmt.Sprintf(
				"%s did not return within %s for %d-byte input",
				orSandbox(phase), budget, len(src))}
		}
	}
}

// abnormal classifies a child that exited without DONE: a fatal
// crash (panic, stack overflow, runtime throw) is its own class
// and must not borrow the hang signature — only resource deaths
// fold into "did not return", the same class as a timeout. That
// includes every way an rlimit-capped child can die: plain builds
// throw "out of memory", -race builds throw "too many address
// space collisions for -race mode" (the race runtime maps shadow
// memory, so the cap kills it differently — observed as a
// -race-only test failure), and silent SIGKILL/OOM-killer exits
// leave no stderr at all.
func (s *Sandbox) abnormal(src []byte, phase string, budget time.Duration) []string {
	stderr := s.errText()
	resourceDeath := strings.Contains(stderr, "out of memory") ||
		strings.Contains(stderr, "address space collisions") ||
		strings.Contains(stderr, "cannot allocate memory")
	if !resourceDeath &&
		(strings.Contains(stderr, "panic") || strings.Contains(stderr, "fatal error")) {
		return []string{fmt.Sprintf(
			"sandbox helper crashed in %s phase for %d-byte input\nstderr:\n%.2000s",
			orSandbox(phase), len(src), stderr)}
	}
	return []string{fmt.Sprintf(
		"%s did not return within %s for %d-byte input (child exited abnormally)",
		orSandbox(phase), budget, len(src))}
}

func orSandbox(phase string) string {
	if phase == "" {
		return "sandbox"
	}
	return phase
}

// spawn starts a fresh helper child: length-prefixed inputs on
// stdin, PHASE/FAIL/DONE lines on stdout, bounded stderr for crash
// classification.
func (s *Sandbox) spawn() error {
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	argv := []string{"-test.run=^" + s.helper + "$", "-test.v"}
	var cmd *exec.Cmd
	// The `ulimit -v` vsize cap is skipped under -race: RLIMIT_AS
	// counts the race runtime's shadow address-space reservations,
	// so a capped child dies environment-dependently (the "address
	// space collisions" death abnormal() folds into the timeout
	// class). Real memory is still bounded by the kernel OOM killer
	// and GOMEMLIMIT.
	if sh, err := exec.LookPath("sh"); err == nil && !raceEnabled {
		args := append([]string{"-c",
			fmt.Sprintf(`ulimit -v %d 2>/dev/null; exec "$@"`, s.vsizeKB),
			"gom-fuzz-sandbox", bin}, argv...)
		cmd = exec.Command(sh, args...)
	} else {
		cmd = exec.Command(bin, argv...)
	}
	cmd.Env = append(os.Environ(), EnvSandbox+"=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	serr := &truncWriter{max: 1 << 20}
	cmd.Stderr = serr
	if err := cmd.Start(); err != nil {
		return err
	}
	s.cmd = cmd
	s.stdin = stdin
	lines := make(chan string, 64)
	s.lines = lines
	waited := make(chan error, 1)
	s.waited = waited
	// `lines`/`waited` are captured, not read off s: after a
	// respawn the old reader/waiter still belongs to the dead
	// child — sending into the old channel keeps a straggler's
	// PHASE/FAIL/DONE (or a stale exit result, which would fake a
	// reap of the LIVE child) out of the fresh child's protocol.
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	go func() { waited <- cmd.Wait() }()
	s.stderr = serr
	return nil
}

// reapBudget bounds how long kill/reap wait for a killed child to
// be collected. A SIGKILL'd spinning child normally exits in
// milliseconds; a gigabytes-fat one can take ~1 s of kernel
// teardown — but waiting longer than this per hang variant was
// measured to cost more throughput than the spin timeout itself
// (hang-variant storms held the whole run at ~0 execs/sec). A
// genuinely uncollectible (D-state) child is left to the Wait
// goroutine: it reaps whenever the kernel finishes, so moving on
// costs at most a transient zombie, never a parked worker.
const reapBudget = 1200 * time.Millisecond

// kill force-exits the current child and waits for the reap,
// bounded so an uninterruptible (D-state) process cannot park the
// worker — observed: a 6.7 GB spinning child that SIGKILL could
// not collect for minutes.
func (s *Sandbox) kill() {
	if s.cmd == nil {
		return
	}
	s.cmd.Process.Kill()
	select {
	case <-s.waited:
	case <-time.After(reapBudget):
	}
	s.drain()
	s.cmd = nil
	s.stdin = nil
	s.lines = nil
}

// reap collects an already-dead child the same way, without a
// fresh signal.
func (s *Sandbox) reap() {
	if s.cmd == nil {
		return
	}
	select {
	case <-s.waited:
	case <-time.After(reapBudget):
	}
	s.drain()
	s.cmd = nil
	s.stdin = nil
	s.lines = nil
}

// drain empties the line channel until the reader goroutine closes
// it (bounded), so a reader parked on a full channel is released to
// exit instead of leaking one goroutine per respawn. The wait is
// short: a still-dying child's leftovers are dropped when the next
// spawn swaps the channel anyway.
func (s *Sandbox) drain() {
	if s.lines == nil {
		return
	}
	t := time.After(300 * time.Millisecond)
	for {
		select {
		case _, ok := <-s.lines:
			if !ok {
				return
			}
		case <-t:
			return
		}
	}
}

func (s *Sandbox) errText() string {
	if s.stderr == nil {
		return ""
	}
	return s.stderr.String()
}

// Close kills the child if one is running.
func (s *Sandbox) Close() { s.kill() }

// truncWriter writes at most max bytes to itself, discarding the
// rest — for bounding captured stderr (a fatal exit prints a full
// goroutine dump which can be megabytes; classification needs only
// its opening lines).
type truncWriter struct {
	b   bytes.Buffer
	max int
}

func (w *truncWriter) Write(p []byte) (int, error) {
	room := w.max - w.b.Len()
	if room > 0 {
		if len(p) > room {
			w.b.Write(p[:room])
		} else {
			w.b.Write(p)
		}
	}
	return len(p), nil
}

func (w *truncWriter) String() string { return w.b.String() }

// ReadSeeds reads every file matching each glob (relative to the
// calling test's package dir). Missing matches are not an error —
// suites move — but a glob that matches nothing produces a log line
// so a stale pattern is visible.
func ReadSeeds(f *testing.F, globs ...string) [][]byte {
	f.Helper()
	var seeds [][]byte
	for _, glob := range globs {
		matches, err := filepath.Glob(glob)
		if err != nil {
			f.Fatal(err)
		}
		if len(matches) == 0 {
			f.Logf("seed glob matched nothing: %s", glob)
		}
		for _, path := range matches {
			b, err := os.ReadFile(path)
			if err != nil {
				f.Fatal(err)
			}
			seeds = append(seeds, b)
		}
	}
	return seeds
}
