# incremental build cache の状態機械を Quint で検証したメモ

対象: `gom build` の incremental build cache（`cmd/gom/buildcache.go` と `cmd/gom/cli.go` の driver 側）。minigo の lazy package initialization と同じく、暗黙の状態遷移を明示モデルに起こして Quint（`spec/buildcache.qnt`）で検証した記録。規範ではない。

## なぜこの対象か

Quint / TLA+ 系が効くのは「普通のテストでは再現しにくい状態遷移の interleaving」がある場所だった。gom の中では incremental build cache が一番効く:

- プロセスをまたぐ永続状態（manifest）がある
- compile / interface check / output write / manifest commit の順序がある
- 失敗時に commit してはいけないという safety の問題がある
- ソース・依存・生成物は外部プロセスからいつでも変更されうる
- 「実装だけ変わった依存は downstream を再生成しない」という cache の中核命題がある

逆に型推論や codegen の意味論そのものには向かない。そちらは比較テスト・golden・fuzzing の方がよい。

## 抽出した状態機械

### 1. invocation 単位の有効/無効

cache は `GOM_BUILD_NOCACHE`、cache dir 解決、`-go`、`-vet`、`--strict` で invocation 単位に無効化される。無効な run でも compile と output write は行うが、hit も commit もしない。

`buildCache.key` は lazy に計算される (`keyOk: false -> true`)。これは正しさではなく subprocess 回避の性能状態なので Quint モデルには入れていない。

### 2. dir group 単位の build 状態

実装には enum はなく、`compileInputs` / `runBuild` の局所変数と map が暗黙の状態を構成する。

```text
unchecked
  |-- Hit valid ------------------------------> cached (terminal)
  `-- miss
       |
       v
   compiling
       |-- compile/interface failure ---------> failed (no commit)
       v
   compiled+noted
       |-- output missing/withheld/write error -> failed (no commit)
       v
   fully-written
       |-- manifest write failure ------------> done (cache miss next time)
       v
   committed
```

重要なのは `note()` が compile 直後に input snapshot を保持する一方、commit 自体は interface check と output write の後まで遅れること。失敗時には manifest を書かない、ではなく「commit 条件を満たさないので commit を呼ばない」という設計になっている。

### 3. manifest の永続状態

manifest は dir ごとに 1 つで、概ね以下の状態を取る。

```text
absent/corrupt --> committed(valid) --> stale (外部変更で無効化)
```

hit 判定では以下を全部再検証する:

- dir と `-o` の値
- compiler / rt/stdlib / Go 環境の key
- `.gom` input の canonical hash
- 記録された `.gomi` の canonical hash
- Go dep dir の file stat
- go.mod/go.sum/go.work の path + stat
- 記録された output の存在

読めない manifest・JSON 不正・書き込み失敗はすべて miss/no-commit に倒れる。cache が compile を壊さない方針はここに表れている。

### 4. dependency observation の hidden state

`seen` は「どの `.gomi` / Go dir を読んだか」の path 集合を貯めるが、読んだ時点の bytes/hash は保持しない。commit 時に `.gomi` を read し直し、Go dir / module file もその時点の stat を記録する。

ここが今回一番の insight だった。input と dependency で時間の扱いが非対称:

| input | compile で使う値 | manifest に記録する値 |
| --- | --- | --- |
| 自分の `.gom` | run 開始前に読んだ bytes | 同じ snapshot (`note`) |
| 依存 `.gomi` / Go dir / module file | compile 中に読んだ内容 | commit 時の現在の内容 |

つまり dependency が compile 中に変わると、manifest は「compile が見ていなかった新しい版」を記録できる。次回 hit はその manifest に一致するので、古い dependency で生成した output を fresh と誤認する。

### 5. output artifact の状態

output は manifest と独立したもう 1 つの状態機械:

```text
absent -> written/generated -> corrupted/deleted
```

hit 判定は output の「存在」だけを見て内容 hash は見ない。`writeGenerated` の marker ガードは手書きファイルの clobber 防止であって、cache の freshness 検証ではない。`TestBuildCache_SkipsOnIdenticalAndImplOnlyChange` の sentinel はこの性質を利用している: cache hit は hand-corrupted generated file を直さない。

これは bug というより「generated output は cache が所有する artifact で、利用者は編集しない」という暗黙の前提。明示する価値がある。

## Quint モデル

`spec/buildcache.qnt` は 1 package に抽象化した。複数 dir の並列 compile は入れず、外部ファイル変更だけを build と interleave させた。

抽象化:

- interface-affecting な依存材料（依存 `.gomi`、Go dep dir、go.mod/go.work）を `hardFp` に畳む
- implementation-only の依存変更を `implFp` に分ける
- compiler / rt/stdlib / Go env を `envFp` とする
- compile/write の成功失敗は nondeterministic
- hit 判定と miss 後の `compiling -> writing -> committing` を明示化
- `.gom` input だけ snapshot、dependency は compile 中観測 + commit 時再観測、という非対称をそのまま写す

`step` では同じ action を重複列挙している。遷移関係は変わらないが、Quint の random simulation は action をほぼ一様に選ぶので、重複がないと mutation ばかり選ばれて commit まで辿り着けなかった。これは検証用の pitfall。

## 検証結果

```sh
quint typecheck spec/buildcache.qnt
quint run spec/buildcache.qnt --backend=typescript \
  --max-steps=12 --max-samples=300 --invariant=allInvariants
JAVA_HOME=/usr/lib/jvm/java-21-openjdk-amd64 \
PATH=/usr/lib/jvm/java-21-openjdk-amd64/bin:$PATH \
quint verify spec/buildcache.qnt --invariant=allInvariants --max-steps=7
```

結果:

- typecheck: pass
- randomized simulation: `allInvariants` に違反なし（300 traces × 12 steps）
- Apalache symbolic verification: depth 7 まで `allInvariants` に違反なし
  - depth 7 は「start -> dependency read -> dependency edit -> compile -> write -> commit -> hit」の race witness 全体を覆う深さ

witness predicate も確認した。これらは invariant として書いてあるが、違反することが期待値:

| witness | 結果 | 意味 |
| --- | --- | --- |
| `noImplOnlyHit` | depth 7 で反例 | implementation-only dependency edit 後に hit できる（cache の目的どおり） |
| `noCorruptedHit` | depth 7 で反例 | output を corrupt しても存在していれば hit する（内容は未追跡） |
| `noStaleCommit` | depth 7 で反例 | compile 中の dependency 変更を commit 時の再読みが取り込める |
| `noStaleHit` | depth 7 で反例 | その manifest が後続 run で hit し、古い dependency で作った output を使える |

stale hit の本質はこの列:

```text
start build
read dependency hard=0
external edit hard=1
compile succeeds        # output is still based on hard=0
write output succeeds
commit manifest mHard=1 # commit re-reads the edited dependency
next build hits         # mHard == current hard, but output is stale
```

## pitfalls / 使い心地

- `quint run` の default Rust evaluator は初回に GitHub Releases を見に行き、rate limit で落ちる。`--backend=typescript` が無難。`quint verify` でも反例表示のために evaluator fetch を試みて warning が出た
- Apalache は Java 21 必須。system の Java 17 では動かない
- random simulation は「compile 成功 -> write 成功 -> commit 成功 -> 特定 mutation -> hit」のような稀有な列を素直には引かない。action 重複で進行方向に寄せるのが手軽だった
- verify は有界探索。「すべてのケース」ではなく、「この抽象化・この有限世界・この深さの全ケース」。今回は depth 7 が witness を覆うので意味がある
- 実装をなぞりすぎると状態爆発する。今回は Go/module/`.gomi` を `hardFp` に畳んだが、問いは dependency invalidation なので十分だった
- invariant は safety の健全性チェックにはなるが、一番情報量があったのは witness predicate の反例だった。形式検証を「証明」ではなく「シナリオ発生器」として使う感覚に近い

## 残件・follow-up

- dependency を読んだ時点の hash/stat を `seen` に記録し、commit 時に再読みしない（または再読み値が違ったら commit しない）方向が自然。input snapshot と同じく「compile が実際に見た世界」を manifest に残すべき
- output の content hash を manifest に入れるかは設計判断。入れると余計な再生成が増えるが、「generated output は編集しない」前提を cache が暗黙に信用しなくて済む
- module pool 単位で Go dep を保守的に共有する部分と複数 dir 並列 compile は今回のモデルに入れていない。必要なら別モデルに分けた方がよい
