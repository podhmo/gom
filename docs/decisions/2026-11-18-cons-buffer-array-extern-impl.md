# Cons 構築の buffer 化: `Array` 操作面 + `@extern fn` extern-impl 機構

- 状態: 採用（prototype 検証済み — docs/sketch/2026-11-18-cons-buffer-verification.md）
- 日付: 2026-11-18
- Supersedes: なし

## Context

[problems/stdlib-cons-built-combinators-quadratic](../problems/2026-11-05-stdlib-cons-built-combinators-quadratic.md):
slice 表現 `List` の構築側 `[x, ..rest]` が O(n) で、`Map`/`Filter`/`Append`/`Rev`/`Range` が O(n²)。
ユーザーの cons 蓄積コードも同じ壁に当たる。再計測（wave18 pin, VM）: Map n=2048 ≈4.0ms/17.7MB、
Range n=2048 ≈6.5ms/35.5MB。

wave18 prompts で podhmo が採択: (a) `Array` builder + `to_list` と
(b) intrinsic/extern-impl の両立。(c) lower の cons-recursion 特殊化は future work。

## Decision

### (a) `Array` モジュール（rt-backed builtin member table）

`Array a`（`[]A` に emit）は型として既存だが操作面が空だった。
`Ptr`/`Go.Map` と同じ機構でコンパイラ内蔵の `Array` モジュールを加える:

| member | Gom signature | Go 意味 |
| --- | --- | --- |
| `Array.make` | `Int -> Array a` | `make([]A, n)` — 負長は Go と同じく panic |
| `Array.length` | `Array a -> Int` | `len(a)` |
| `Array.get` | `Array a -> Int -> a` | `a[i]` — 範囲外は Go と同じく panic |
| `Array.set` | `Array a -> Int -> a -> Unit` | `a[i] = x` — 範囲外 panic |
| `Array.from_list` | `List a -> Array a` | `slices.Clone` — 以後の書き込みは元 List に届かない |
| `Array.to_list` | `Array a -> List a` | `List[A](xs)` — **freeze/引き渡し**。以後その `Array` への書き込みは List の不変性契約違反（spec 8.7 と同じ OCaml 型契約、診断なし） |

`make`+`set`+`to_list` で長さ既知の構築が O(n)。`push` は置かない — `Array a` が `[]A`
そのままの表現では append の再割当を破壊的には書き戻せない（`push` が新配列を返す API は
builder 名を騙る値 API になり、誤用を招く）。長さ不明の構築は count→fill の 2 パスで線形。

### (b) `@extern fn`: 同パッケージ hand-written Go への宣言

`@extern fn name (p: T) -> R` — `fn` のシグネチャ宣言で、本体は同じ生成パッケージ内の
hand-written Go 関数 `ExportName(name)` が提供する。

- 解決・型検査は通常の `fn` と同一（`KindFunc`、宣言 arity、`.gomi` との適合は
  `iface.Check` の MemberFn 経路をそのまま通る — シグネチャの一致が検査される）。
- lower は Body==nil の FnDecl を emit しない（宣言だけが codegen の名予約に残る）。
- 呼び出し側は無変更: 同パッケージから `Map(...)`、外部から `list.Map[A,B](...)` —
  Gom 表現そのままの generic 呼び出しで境界変換は起きない（`List a` は `list.List[A]` verbatim）。
- `.gomi` にはマーカーを置かない: 実装の出どころはインターフェースの関心事ではない。
  `@extern` に対応する hand-written Go が無い/シグネチャが違うパッケージは `go build` /
  そのパッケージの `gom test` で失敗する — これが対応検査の実効層。
- パラメータ・戻り型のアノテーションは必須（`@extern` に本体の推論が無い）。
  `@extern` に `= body` を書く、let/type/test への付与は E0101。

stdlib への適用: `stdlib/list` の `map`/`filter`/`append`/`rev`/`range` を
`@extern fn` 宣言に切り替え、hand-written `stdlib/list/impl.go` が buffer loop で実装。
`rev_append`/`concat`/`sum` 等の既存線形実装は `.gom` のまま（変更理由がない）。

## Rejected alternatives

- **`.gomi` 側マーカー**（problems file の原案どおり interface に `@extern` 等を置く）:
  不採用。`.gomi` は利用者側の契約で、実装が Go か Gom かは契約外 — しかし決定的な欠点は、
  `.gomi` だけだと同パッケージ `.gom` の内部参照（`concat_go` の `append` 呼出し等）が
  解決できない。`.gom` 側の signature-only 宣言なら包スコープのシンボルとして自然に解決し、
  `.gomi` 適合も既存経路のまま通る。
- **`extern` キーワードの同パッケージ形**（`extern fn map ...`）: `extern go "pkg"` は
  Go 側の型で署名する別機構（境界変換がかかる）で、同パッケージ extern が要るのは
  Gom 表現のままの署名 — 混同を避けて宣言修飾の属性形に寄せた。
- **`Go.Slice` への書き込み面を足す**: `Go.Slice` は「Gom は読むだけ」（
  [decisions/go-slice-ops](2026-11-05-go-slice-ops.md)）の契約 — 書き込みを足すと
  境界の read-only 契約が壊れる。`Array` は Gom 自身の可変配列で別物。
- **`Array.push` / capacity 付き builder**: 上記のとおり `[]A` 表現と矛盾。
- **lower の cons-recursion 特殊化 (c)**: podhmo 採択で future work — パターン検出の
  保守面が重く、(a)+(b) で stdlib とユーザーの両方に到達できる。

## 結果へ影響する範囲

- `docs/spec.md`: `Array` モジュール表、`@extern` 属性（spec-visible）
- `docs/grammar.ebnf`: attribute 定義の注記
- `stdlib/list/list.gom`: 5 decl を `@extern fn` 化、`stdlib/list/impl.go` 新設
- `testdata/spec/`: `ok_extern_fn`、`E0101_extern_*` fixtures
- `testdata/golden/`: stdlib_modules 等の生成形変化
- problems file 状態更新、(c) は open-questions の Future work へ
