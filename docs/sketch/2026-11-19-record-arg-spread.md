# wave27 lane D: record→ラベル引数 spread（`f ~..opts`）の調査

- 日付: 2026-11-19
- 状態: 調査完了・採否は podhmo に委ねる
- 関連: [wave26 lane D](2026-11-19-labeled-args.md)（省略可能引数
  は defaults record + `with` に収束、spread は future work と
  メモ）、spec §2（labeled 呼出し規約）、spec §3（record は
  名前的・構造的部分型なし）、
  [label-args-into-returned-functions](../decisions/2026-11-06-label-args-into-returned-functions.md)

## 問い

`f ~..opts` / `f ..opts` — record のフィールドを labeled 仮引数に
展開して渡す（TS `f({...opts})` / Python `f(**opts)` 相当）が
欲しくなる条件を測る。wave26 の結論「モジュールが defaults
record を公開し `{defaults with x = v}` で潰して record 引数と
して渡す」で足りる境界線を引き、足りないところだけが spread の
受け持ちになるはず。

## 前提（現行コード上の事実）

- record はすべて `type` 宣言に由来する名前的型 —
  `checkRecordLit` は want か field 集合から `typeInfo` を特定
  するので、spread 対象のフィールド集合は**静的に列挙できる**
  （構造的部分型・width subtyping はない）。
- `checkGomApply`（internal/check/apply.go）は引数ごとに
  `paramByLabel` で flat パラメータ列のスロットを引き、
  `used[]` と `argToParam` を作って `ClassMappedCall` が
  飽和判定する。ラベルはカリー化鎖の全レベルを指す（spec
  §2.6 — flatten 済みの `params` に対して引く）。
- `..` という綴りは既存構文: リスト `[x, ..rest]`、tuple arg
  の Go 可変長 `f("tag:", ..xs)`（内部は `syntax.Spread`）。
  Gom 呼出しの arg 位置の裸 `..e` は**現在未使用**（パースも
  しない）。
- 既存の診断: E0404 未充足 labeled、E0405 重複、E0406 未知
  ラベル、E0408 callee 型未知。

## 構文候補

- **`f ~..opts`** — `~` が labeled 文脈の印、`..` が展開。
  Gom/Go 呼出しどちらでも `..xs`（可変長）と衝突しない。最も
  一貫性がある。
- **`f ..opts`** — Gom 呼出しでは空き構文。ただし Go 呼出し
  の `(a, ..xs)` と見た目が近く、「record 展開」と「variadic
  splice」を文脈で読み分ける必要がある。
- `f ~..`（pun 的に同名変数を展開）も自然な派生形。

`~..` を推す。`..` 単独は Go 呼出し規約との混線を避けられない。

## 型検査への影響範囲（採る場合）

`f ~..opts` は check 時に「record 型の全フィールドを、同名
label のスロットへ写像する」規則で既存の枠に乗る:

- `opts` の型は record con に確定している必要がある — メタなら
  E0408 系（「callee/record 型が既知を要求」は labeled 呼出し
  規約と同じ前提）。
- フィールド名 → `paramByLabel` → `used[]` マーキング。未知
  フィールドは E0406 系、明示の `~a:` との重複は E0405 系、
  未充足 labeled は既存の E0404 / 部分適用規約そのまま
  （record が param の部分集合しか覆わなければ residual fn —
  spec §2.5 と一致するので **arity 規則の追加は不要**）。
- フィールドの型は対応 param の want で unify — 各スロットは
  通常の `~a:` 引数と同じ扱い。
- 評価順（spec §2.4 左から右）: `opts` は書いた位置で 1 回だけ
  評価する必要がある。実装は 2 案:
  1. **check 時 desugar**: `f ~a: opts.a ~b: opts.b` 相当に
     `SelectExpr` を展開。core/codegen は無改修だが、opts が
     Var でなければ N 回評価になるので一時束縛の生成が要る
     （check IR で新しい `let` を差し込む — やや侵入的）。
  2. **core Arg に spread フラグ**: `Arg{Spread: true}` を
     足し、param 順に `opts.f` 読みを emit。評価 1 回を保証
     できるが core.Arg・検証・codegen の引数 emit に波及。
- `ClassMappedCall` は 1 arg → N slot の写像に対応が要る
  （`argToParam` が per-arg int なので、desugar 案なら展開後の
  per-slot 写像をそのまま渡せる — 案 1 の方が綺麗）。
- `.gomi`・Go 境界への影響なし（呼出し側の構文のみ）。

## 「record 引数1個で足りる」境界線

足りる領域（今日の idioms）:

- **デフォルト・合成**: `{defaults with a = 1}` の `with` 連鎖
- **forwarding**: wrapper が `opts` をそのまま下へ渡す
- **両端が record 規約**: callee が `(opts: Opts)` を受ける設計

spread が要る領域 — 上のどれにも当たらないもの:

- callee が **labeled 規約**（`~to:`/`~amount:` — 公開 API で
  既に確立）なのに、caller 側が **record** を持っている橋渡し。
  例: TOML 由来の `Config` record → labeled コンストラクタや
  `Transfer ~..opts` のような sum バリアント構築。
- wrapper が `(opts: Opts)` で受けて labeled callee に流す:
  `inner ~..opts` でフィールド列挙を書かずに済む。

ただし重要な制約: record は名前的で厳密なので、spread 可能な
record 型は実質「その callee のラベル集合用に宣言された」型に
なりがち — ならば callee が最初からその record を受ける設計で
吸収できる。厳密照合を緩めて「未知フィールドは無視」にすると
部分利用（record の一部だけ渡す）はできるが、typo を静かに
通すので非推奨。

## 推奨

**見送り寄り（条件付き）**。主用途の forwarding/defaults は
`with` + record 引数が既に担っており、spread の固有価値は
「labeled 規約の callee × record 規約の caller」の混在点に
限られる。gom では API 設計時にどちらかを選べるので、その
混在の発生頻度は低いはず。採用する場合の最小経路は `~..e`
構文 + check 時 desugar（上の案 1）。残る論点 — 未知
フィールドの扱い（strict error 推奨）、generic record の
型変数 fill、評価 1 回化の一時束縛 — は採用時に decision へ。
