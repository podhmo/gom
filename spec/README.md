# spec — formal models of gom's implicit state machines

Quint specifications transcribing implicit state machines in the
compiler toolchain. `buildcache.qnt` models the incremental build
cache behind `gom build`; see
[../docs/sketch/ja/2026-11-26-buildcache-quint.md](../docs/sketch/ja/2026-11-26-buildcache-quint.md)
for the analysis and verification notes (Japanese).

## Running

```sh
npm install -g @informalsystems/quint

# typecheck
quint typecheck spec/buildcache.qnt

# randomized simulation (the typescript backend avoids the rust
# evaluator's GitHub release download, which rate-limits easily)
quint run spec/buildcache.qnt --backend=typescript \
  --max-steps=12 --max-samples=300 --invariant=allInvariants

# witnesses: each of these MUST report a violation; the violating
# trace is the interesting behavior
quint run spec/buildcache.qnt --backend=typescript \
  --max-steps=10 --max-samples=1000 --invariant=noImplOnlyHit
quint run spec/buildcache.qnt --backend=typescript \
  --max-steps=10 --max-samples=1000 --invariant=noCorruptedHit
quint run spec/buildcache.qnt --backend=typescript \
  --max-steps=14 --max-samples=3000 --invariant=noStaleHit

# symbolic verification via Apalache (needs Java 21+). Depth 7 covers
# the complete race witness: start, read deps, edit, compile, write,
# commit, then hit.
quint verify spec/buildcache.qnt --invariant=allInvariants --max-steps=7
```

`_apalache-out/` is model-checker scratch output and is ignored.
