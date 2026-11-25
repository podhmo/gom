// buildcache.qnt — a Quint model of gom's incremental build cache.
//
// This is a transcription of the explicit state model in
// docs/sketch/ja/2026-11-26-buildcache-quint.md. The model focuses on
// one package directory and the process-crossing manifest lifecycle:
//
//   idle --miss--> compiling --> writing --> committing --> idle
//   idle --hit--> idle
//
// Simplifications:
//   - one package build is modeled; parallel dir compiles are outside
//     the interesting cache protocol. External file mutations may still
//     interleave with the build.
//   - every interface-affecting dependency input (.gomi files, imported
//     Go package dirs, module/workspace files) is folded into hardFp.
//     Implementation-only dependency edits live in implFp. This is the
//     central boundary the cache is designed around.
//   - the compiler/environment fingerprint cannot change mid-run; it is
//     mutable only while idle. Source and dependency files may change
//     mid-run because they are ordinary files another process can edit.
//   - compile success and write success are nondeterministic booleans.
//     Their internal compiler phases are not modeled.
//
// Important asymmetry transcribed from the implementation:
//   - the package's own .gom inputs are read before compilation starts,
//     so inputSnapshot is stable for the whole run;
//   - dependency fingerprints are observed while compiling, but commit
//     re-reads the current files. A dependency edit between those two
//     observations can therefore make the manifest describe a different
//     dependency version than the compiled output was built against.
//
// Run witnesses with:
//   quint run spec/buildcache.qnt --backend=typescript \
//     --max-steps=12 --max-samples=300 --invariant=noImplOnlyHit
//   quint run spec/buildcache.qnt --backend=typescript \
//     --max-steps=12 --max-samples=300 --invariant=noCorruptedHit
//   quint run spec/buildcache.qnt --backend=typescript \
//     --max-steps=12 --max-samples=500 --invariant=noStaleHit
module buildcache {
  pure val IDLE = "idle"
  pure val COMPILING = "compiling"
  pure val WRITING = "writing"
  pure val COMMITTING = "committing"

  pure val OUT_A = "out-a"
  pure val OUT_B = "out-b"

  // ---- external world ---------------------------------------------
  var srcFp: int       // canonical fingerprint of this package's .gom inputs
  var hardFp: int      // interface-affecting dependency/material fingerprint
  var implFp: int      // implementation-only dependency fingerprint
  var envFp: int       // compiler + rt/stdlib ABI + Go build conditions
  var outputExists: bool
  var outputCorrupt: bool

  // ---- manifest -----------------------------------------------------
  var manifest: bool
  var mSrc: int
  var mHard: int
  var mEnv: int
  var mOut: str

  // ---- one invocation -----------------------------------------------
  var phase: str
  var runOut: str
  var cacheEnabled: bool
  var inputSnapshot: int
  var depsRead: bool
  var compiledHard: int
  var compiledImpl: int

  // ---- ghost counters ------------------------------------------------
  var hits: int
  var staleHits: int       // hits whose compiled dep observation is no longer current
  var corruptedHits: int   // hits serving an output whose contents were corrupted
  var implOnlyHits: int    // hits after an implementation-only dependency edit
  var commits: int
  var staleCommits: int    // commits whose recorded dep fingerprint was not compiled against

  action init = all {
    srcFp' = 0,
    hardFp' = 0,
    implFp' = 0,
    envFp' = 0,
    outputExists' = false,
    outputCorrupt' = false,
    manifest' = false,
    mSrc' = 0,
    mHard' = 0,
    mEnv' = 0,
    mOut' = OUT_A,
    phase' = IDLE,
    runOut' = OUT_A,
    cacheEnabled' = true,
    inputSnapshot' = 0,
    depsRead' = false,
    compiledHard' = 0,
    compiledImpl' = 0,
    hits' = 0,
    staleHits' = 0,
    corruptedHits' = 0,
    implOnlyHits' = 0,
    commits' = 0,
    staleCommits' = 0,
  }

  pure def hitHolds(manifestPresent, ms, mh, me, mo, out, src, hard, env, outExists) =
    manifestPresent and
    mo == out and
    ms == src and
    mh == hard and
    me == env and
    outExists

  // Start one `gom build` for this package. A hit is atomic: the driver
  // validates the manifest and skips compilation. A miss enters the
  // compile/write/commit pipeline. useCache=false models -go/-vet,
  // --strict, or GOM_BUILD_NOCACHE.
  action startBuild(out, useCache) = all {
    phase == IDLE,
    runOut' = out,
    cacheEnabled' = useCache,
    inputSnapshot' = srcFp,
    depsRead' = false,
    any {
      all {
        useCache,
        hitHolds(manifest, mSrc, mHard, mEnv, mOut, out,
                 srcFp, hardFp, envFp, outputExists),
        hits' = hits + 1,
        staleHits' = staleHits + (if (compiledHard == hardFp) 0 else 1),
        // Isolate the witnesses: a corrupted/impl-only hit counts only
        // when the interface-affecting dependency observation is still
        // coherent. Otherwise the trace is already a stale-hit witness.
        corruptedHits' = corruptedHits +
          (if (outputCorrupt and compiledHard == hardFp) 1 else 0),
        implOnlyHits' = implOnlyHits +
          (if (compiledImpl != implFp and compiledHard == hardFp) 1 else 0),
        phase' = IDLE,
      },
      all {
        not(useCache) or
          not(hitHolds(manifest, mSrc, mHard, mEnv, mOut, out,
                       srcFp, hardFp, envFp, outputExists)),
        hits' = hits,
        staleHits' = staleHits,
        corruptedHits' = corruptedHits,
        implOnlyHits' = implOnlyHits,
        phase' = COMPILING,
      },
    },
    srcFp' = srcFp,
    hardFp' = hardFp,
    implFp' = implFp,
    envFp' = envFp,
    outputExists' = outputExists,
    outputCorrupt' = outputCorrupt,
    manifest' = manifest,
    mSrc' = mSrc,
    mHard' = mHard,
    mEnv' = mEnv,
    mOut' = mOut,
    compiledHard' = compiledHard,
    compiledImpl' = compiledImpl,
    commits' = commits,
    staleCommits' = staleCommits,
  }

  // Dependency material is read lazily during compilation: .gomi files
  // when the import loader resolves them, Go/module metadata when the
  // importer runs `go list`. The implementation does not hash the bytes
  // here; it only remembers paths. The model therefore keeps the read
  // observation in compiledHard so the later commit can be compared
  // against it.
  action readDeps = all {
    phase == COMPILING,
    not(depsRead),
    depsRead' = true,
    compiledHard' = hardFp,
    compiledImpl' = implFp,
    srcFp' = srcFp,
    hardFp' = hardFp,
    implFp' = implFp,
    envFp' = envFp,
    outputExists' = outputExists,
    outputCorrupt' = outputCorrupt,
    manifest' = manifest,
    mSrc' = mSrc,
    mHard' = mHard,
    mEnv' = mEnv,
    mOut' = mOut,
    phase' = phase,
    runOut' = runOut,
    cacheEnabled' = cacheEnabled,
    inputSnapshot' = inputSnapshot,
    hits' = hits,
    staleHits' = staleHits,
    corruptedHits' = corruptedHits,
    implOnlyHits' = implOnlyHits,
    commits' = commits,
    staleCommits' = staleCommits,
  }

  // The compile either fails (including type/check/interface-contract
  // failures that withhold Output) or reaches the write step.
  action finishCompile = all {
    phase == COMPILING,
    depsRead,
    any {
      all {
        // compile failure: no output write and no commit
        phase' = IDLE,
      },
      all {
        phase' = WRITING,
      },
    },
    srcFp' = srcFp,
    hardFp' = hardFp,
    implFp' = implFp,
    envFp' = envFp,
    outputExists' = outputExists,
    outputCorrupt' = outputCorrupt,
    manifest' = manifest,
    mSrc' = mSrc,
    mHard' = mHard,
    mEnv' = mEnv,
    mOut' = mOut,
    runOut' = runOut,
    cacheEnabled' = cacheEnabled,
    inputSnapshot' = inputSnapshot,
    depsRead' = depsRead,
    compiledHard' = compiledHard,
    compiledImpl' = compiledImpl,
    hits' = hits,
    staleHits' = staleHits,
    corruptedHits' = corruptedHits,
    implOnlyHits' = implOnlyHits,
    commits' = commits,
    staleCommits' = staleCommits,
  }

  // Output writing is all-or-nothing at the package granularity for the
  // manifest: a failed member/write never reaches the commit step. A
  // successful write refreshes the generated output. A cache-disabled
  // invocation still writes outputs but has no commit step.
  action finishWrite = all {
    phase == WRITING,
    any {
      all {
        // write failure (or a withheld package member): no commit
        phase' = IDLE,
        outputExists' = outputExists,
        outputCorrupt' = outputCorrupt,
      },
      all {
        phase' = if (cacheEnabled) COMMITTING else IDLE,
        outputExists' = true,
        outputCorrupt' = false,
      },
    },
    srcFp' = srcFp,
    hardFp' = hardFp,
    implFp' = implFp,
    envFp' = envFp,
    manifest' = manifest,
    mSrc' = mSrc,
    mHard' = mHard,
    mEnv' = mEnv,
    mOut' = mOut,
    runOut' = runOut,
    cacheEnabled' = cacheEnabled,
    inputSnapshot' = inputSnapshot,
    depsRead' = depsRead,
    compiledHard' = compiledHard,
    compiledImpl' = compiledImpl,
    hits' = hits,
    staleHits' = staleHits,
    corruptedHits' = corruptedHits,
    implOnlyHits' = implOnlyHits,
    commits' = commits,
    staleCommits' = staleCommits,
  }

  // Manifest commit. Input hashes come from inputSnapshot, but dependency
  // material is re-read here from the live filesystem. This asymmetry is
  // deliberate: it is the implementation's TOCTOU window. Cache-write
  // failure degrades to no manifest, by design.
  action commitManifest = all {
    phase == COMMITTING,
    cacheEnabled,
    any {
      all {
        manifest' = true,
        mSrc' = inputSnapshot,
        mHard' = hardFp,
        mEnv' = envFp,
        mOut' = runOut,
        commits' = commits + 1,
        staleCommits' = staleCommits + (if (compiledHard == hardFp) 0 else 1),
      },
      all {
        manifest' = manifest,
        mSrc' = mSrc,
        mHard' = mHard,
        mEnv' = mEnv,
        mOut' = mOut,
        commits' = commits,
        staleCommits' = staleCommits,
      },
    },
    phase' = IDLE,
    srcFp' = srcFp,
    hardFp' = hardFp,
    implFp' = implFp,
    envFp' = envFp,
    outputExists' = outputExists,
    outputCorrupt' = outputCorrupt,
    runOut' = runOut,
    cacheEnabled' = cacheEnabled,
    inputSnapshot' = inputSnapshot,
    depsRead' = depsRead,
    compiledHard' = compiledHard,
    compiledImpl' = compiledImpl,
    hits' = hits,
    staleHits' = staleHits,
    corruptedHits' = corruptedHits,
    implOnlyHits' = implOnlyHits,
  }

  // ---- external mutations -------------------------------------------
  // These are the environment's edits, not actions of gom. They may
  // interleave with a build because source trees are ordinary files.

  action editSource = all {
    srcFp' = srcFp + 1,
    hardFp' = hardFp,
    implFp' = implFp,
    envFp' = envFp,
    outputExists' = outputExists,
    outputCorrupt' = outputCorrupt,
    manifest' = manifest,
    mSrc' = mSrc,
    mHard' = mHard,
    mEnv' = mEnv,
    mOut' = mOut,
    phase' = phase,
    runOut' = runOut,
    cacheEnabled' = cacheEnabled,
    inputSnapshot' = inputSnapshot,
    depsRead' = depsRead,
    compiledHard' = compiledHard,
    compiledImpl' = compiledImpl,
    hits' = hits,
    staleHits' = staleHits,
    corruptedHits' = corruptedHits,
    implOnlyHits' = implOnlyHits,
    commits' = commits,
    staleCommits' = staleCommits,
  }

  action editHardDep = all {
    hardFp' = hardFp + 1,
    srcFp' = srcFp,
    implFp' = implFp,
    envFp' = envFp,
    outputExists' = outputExists,
    outputCorrupt' = outputCorrupt,
    manifest' = manifest,
    mSrc' = mSrc,
    mHard' = mHard,
    mEnv' = mEnv,
    mOut' = mOut,
    phase' = phase,
    runOut' = runOut,
    cacheEnabled' = cacheEnabled,
    inputSnapshot' = inputSnapshot,
    depsRead' = depsRead,
    compiledHard' = compiledHard,
    compiledImpl' = compiledImpl,
    hits' = hits,
    staleHits' = staleHits,
    corruptedHits' = corruptedHits,
    implOnlyHits' = implOnlyHits,
    commits' = commits,
    staleCommits' = staleCommits,
  }

  action editImplOnlyDep = all {
    implFp' = implFp + 1,
    srcFp' = srcFp,
    hardFp' = hardFp,
    envFp' = envFp,
    outputExists' = outputExists,
    outputCorrupt' = outputCorrupt,
    manifest' = manifest,
    mSrc' = mSrc,
    mHard' = mHard,
    mEnv' = mEnv,
    mOut' = mOut,
    phase' = phase,
    runOut' = runOut,
    cacheEnabled' = cacheEnabled,
    inputSnapshot' = inputSnapshot,
    depsRead' = depsRead,
    compiledHard' = compiledHard,
    compiledImpl' = compiledImpl,
    hits' = hits,
    staleHits' = staleHits,
    corruptedHits' = corruptedHits,
    implOnlyHits' = implOnlyHits,
    commits' = commits,
    staleCommits' = staleCommits,
  }

  action editEnvironment = all {
    phase == IDLE,
    envFp' = envFp + 1,
    srcFp' = srcFp,
    hardFp' = hardFp,
    implFp' = implFp,
    outputExists' = outputExists,
    outputCorrupt' = outputCorrupt,
    manifest' = manifest,
    mSrc' = mSrc,
    mHard' = mHard,
    mEnv' = mEnv,
    mOut' = mOut,
    phase' = phase,
    runOut' = runOut,
    cacheEnabled' = cacheEnabled,
    inputSnapshot' = inputSnapshot,
    depsRead' = depsRead,
    compiledHard' = compiledHard,
    compiledImpl' = compiledImpl,
    hits' = hits,
    staleHits' = staleHits,
    corruptedHits' = corruptedHits,
    implOnlyHits' = implOnlyHits,
    commits' = commits,
    staleCommits' = staleCommits,
  }

  action corruptOutput = all {
    outputExists,
    not(outputCorrupt),
    outputCorrupt' = true,
    outputExists' = outputExists,
    srcFp' = srcFp,
    hardFp' = hardFp,
    implFp' = implFp,
    envFp' = envFp,
    manifest' = manifest,
    mSrc' = mSrc,
    mHard' = mHard,
    mEnv' = mEnv,
    mOut' = mOut,
    phase' = phase,
    runOut' = runOut,
    cacheEnabled' = cacheEnabled,
    inputSnapshot' = inputSnapshot,
    depsRead' = depsRead,
    compiledHard' = compiledHard,
    compiledImpl' = compiledImpl,
    hits' = hits,
    staleHits' = staleHits,
    corruptedHits' = corruptedHits,
    implOnlyHits' = implOnlyHits,
    commits' = commits,
    staleCommits' = staleCommits,
  }

  action deleteOutput = all {
    outputExists,
    outputExists' = false,
    outputCorrupt' = false,
    srcFp' = srcFp,
    hardFp' = hardFp,
    implFp' = implFp,
    envFp' = envFp,
    manifest' = manifest,
    mSrc' = mSrc,
    mHard' = mHard,
    mEnv' = mEnv,
    mOut' = mOut,
    phase' = phase,
    runOut' = runOut,
    cacheEnabled' = cacheEnabled,
    inputSnapshot' = inputSnapshot,
    depsRead' = depsRead,
    compiledHard' = compiledHard,
    compiledImpl' = compiledImpl,
    hits' = hits,
    staleHits' = staleHits,
    corruptedHits' = corruptedHits,
    implOnlyHits' = implOnlyHits,
    commits' = commits,
    staleCommits' = staleCommits,
  }

  // Repeated alternatives do not change the transition relation, but
  // they do make randomized simulation less dominated by unrelated file
  // mutations. The interesting build pipeline is rare enough otherwise
  // that even the desired cache-hit witnesses are hard to sample.
  action step = any {
    startBuild(OUT_A, true),
    startBuild(OUT_A, true),
    startBuild(OUT_A, true),
    startBuild(OUT_A, false),
    startBuild(OUT_B, true),
    startBuild(OUT_B, false),
    readDeps,
    readDeps,
    readDeps,
    finishCompile,
    finishCompile,
    finishCompile,
    finishWrite,
    finishWrite,
    finishWrite,
    commitManifest,
    commitManifest,
    commitManifest,
    editSource,
    editHardDep,
    editImplOnlyDep,
    editEnvironment,
    corruptOutput,
    deleteOutput,
  }

  // ---- invariants over the model itself ------------------------------
  pure def isPhase(p) =
    p == IDLE or p == COMPILING or p == WRITING or p == COMMITTING

  val phaseCoherent =
    isPhase(phase) and
    (not(phase == COMMITTING) or cacheEnabled) and
    (not(phase == WRITING or phase == COMMITTING) or depsRead)

  val countersBounded =
    staleHits <= hits and
    corruptedHits <= hits and
    implOnlyHits <= hits and
    staleCommits <= commits

  val allInvariants = phaseCoherent and countersBounded

  // ---- witness predicates -------------------------------------------
  // These are written as invariants so a violation gives a witness trace.
  // They are expected to be violated:
  //   - impl-only hits are the desired cache boundary;
  //   - corrupted-output hits document that outputs are existence-checked
  //     but content-untracked;
  //   - stale commits/hits expose the dependency re-read race.
  val noImplOnlyHit = implOnlyHits == 0
  val noCorruptedHit = corruptedHits == 0
  val noStaleCommit = staleCommits == 0
  val noStaleHit = staleHits == 0
}
