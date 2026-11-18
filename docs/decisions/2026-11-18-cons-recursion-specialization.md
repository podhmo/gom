# lower の cons-recursion 特殊化: match-cons 蓄積形を buffer loop に変換する

- 状態: 採択
- 日付: 2026-11-18

## 状況

`List` は Go slice 表現（P0-3）なので `[x, ..rest]` の構築は O(n) であり、`match` の cons 腕で自分を呼ぶ蓄積再帰（`map`/`filter`/`append` 様）は結果の構築コストだけで O(n²) になる。wave18 では stdlib 側を `Array` builder と `@extern fn` で O(n) 化した（[cons-buffer-array-extern-impl](2026-11-18-cons-buffer-array-extern-impl.md)）。残件だった「ユーザーコードの cons 蓄積を自動で救うか」は検出ポリシーと等価性の保守が重いとして future work に残し、wave19 の採択で自動変換を入れることになった。

## 決定

lower の `markTailCalls` の直後に `markConsCalls`（internal/lower/consrec.go）を走らせ、cons 位置の自己呼び出しを「蓄積 buffer 付きのループ再始動」に書き換える。

### 検出する形状

葉（`If`/`Match`/`SelectExpr` の各分岐の末尾に達する本体ブロック）について、次をすべて満たすものだけ:

1. 葉本体の tail が `*core.ListNew` で、`Rest` が `*core.Var`（`[h, ..rest]` 形）
2. `rest` がその葉の**最後の `let`** で、自己 `*core.CallDirect` に束縛されている（ANF が `[e, ..f args]` を `let rest = f args; [e, ..rest]` に正規化した形だけを見る — 見かけの AST ではなく ANF の形で判定するので、「呼び出しの結果をそのまま cons する」コードはすべてこの形に落ちる）
3. その `CallDirect` は `Callee == 自分`、`!Adapter`、`len(Args) == len(fn.Params)`、`TypeArgs` が宣言の `TypeParams` と位置ごとに同一（恒等インスタンシエーション — 末尾呼び出し変換と同じ要件）
4. `rest` の使用が `..rest` の 1 箇所だけ（head 要素や他の式で `rest` を読む場合は呼び出し結果を 2 度消費するので変換できない — 例: `[List.length rest + h, ..rest]`）

`rest` を束縛する `let` より前にある `let` 列はそのまま残るので、head 要素が任意の式（`f h + 1` やネストした式）でも対象になる。

### 書き換え

条件を満たしたら `rest` の `let` を落とし、tail を `TailCall{To: fn, Args: 呼び出し引数, Prepend: head 要素列}` にする。`Func.ConsLoop` を立て（`TailLoop` と必ず同時）、codegen は結果用 buffer を `for` の前に宣言して全 tail を `buf = append(buf, tail...)` に向け、`Prepend` 付き `TailCall` は再代入の前に `buf = append(buf, h...)` を出す。素の `TailCall`（`[] -> f ys` のような蓄積なし再始動）と混在しても同じループで動く。

### 等価性の根拠

再帰版は反復ごとに「head 列を評価 → 末尾呼び出し」で、結果は「先に進んだ反復の head が結果リストの後ろに来る」順に cons される。ループ版は「head を buffer に append → 引数を再代入して継続」なので同じ順で buffer が育ち、基底葉の `buf = append(buf, tail...)` が残りを一度に継ぐ。head 評価が呼び出しより先であることは ANF の let 列が保証する。`rest` を 2 度読む形を要件 4 で除くので、append 済みの中間リストを読むコードパスは残らない。

### 取りこぼしの扱い

検出は上の形状に限定する。外れた形（`rest` の 2 度読み、adapter 経由、非恒等インスタンシエーション、`select`/`try` 境界越し）は診断を出さず従来どおりの再帰コンパイルに留まる — 意味は変わらず性能だけの問題なので沈黙でよい。

### `@tailrec` との関係

変換済みの cons 呼び出しは `TailCall` として残るので `@tailrec` の R1/R2 検査でも末尾呼び出しとして数えられる — ループにコンパイルされる以上スタックは増えず、「`@tailrec` はスタックが増えないことを要求する」という属性の趣旨と一致する。したがって `@tailrec fn m xs = match xs { [] -> []; [h,..t] -> [h, ..m t] }` は受理される。

## 検証

`testdata/golden/cons_loop.gom` が変換・混在・複数 head・取りこぼし（`rest` 2 度読み）の 4 形を固定する。手動検証で 100,000 要素の `mymap` が 0.17s で完走することを確認した（再帰版はスタック超過）。多相関数（`fn pmap [a,b] ...`）の恒等インスタンシエーション要件も手動で確認。

## 関連

- [cons-buffer-array-extern-impl](2026-11-18-cons-buffer-array-extern-impl.md) — stdlib 側の O(n) 化。本決定はユーザーコード側
- [problems/stdlib-cons-built-combinators-quadratic](../problems/2026-11-05-stdlib-cons-built-combinators-quadratic.md)
- design.md 4.5 節
