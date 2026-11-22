# ローカル let の多相化

- 状態: 採択（案 A、2026-11-23 wave35 開始前インタビュー）
- 日付: 2026-11-23
- 問い: body 内の `let` を一般化するか — spec §3.3 の v1 型規則
  （ローカルの `let` は一般化しない）の見直し。入れる場合は
  relaxed value restriction を検討する、という future work の
  棚卸し（wave34 lane E の調査）

## 現状

トップレベルの `let` は syntactic value restriction 付きで
一般化する（decisions/2026-11-01-value-restriction.md —
右辺が関数か構文的な値のときだけ）。一般化された束縛は
generics 付きのトップレベル宣言として emit される
（decisions/2026-11-02-let-polymorphism-emission.md — ラムダは
`func f[T...]`、その他の値は niladic な `func x[T...]() T`
thunk）。

一方 body 内の `let`（`checkLet`、internal/check/expr.go）は
束縛の型をひとつの meta で受け、全使用がその meta を共有する
— 完全に単相。型変数を書いた注釈は未束縛変数で E0412。

### 境界例（wave34 で確認）

```gom
fn main() -> Unit = {
    let f = fn (x: b) -> x   // E0412: unbound type variable `b`
    let _a = f 1             // E0401: expected a numeric type, got b
    let _b = f "s"           // E0401: expected b, got String
    ()
}
```

```gom
let f = id       // f : Int -> Int に一度だけ解ける（単相）
let g = const_ 1 // g : b -> Int に一度だけ解ける（同上）
```

いずれも 2 型目の使用で E0401。`let` の右辺が syntactic value
でも適用式でも、body 内では一般化しない点は共通。

## 分析 — 障壁は value restriction ではなく emission

検討対象として挙がっていた relaxed value restriction は、
OCaml で「expansive な右辺（適用式）でも結果型に covariant に
だけ現れる型変数は一般化してよい」という緩和。これは
*どの束縛が一般化できるか* の判定を広げる話であって、
*一般化した束縛をどう emit するか* とは別の問題。

Gom では emission 側が先に詰まる:

- Go にはジェネリックなローカル関数・ローカル変数が存在
  しない。型パラメータを持てるのはトップレベルの func と
  type だけ
- トップレベル `let` の一般化が成立するのは、束縛が
  パッケージレベルの generic 宣言に落ちるから。body 内の
  束縛を同じ形にするには **トップレベルへの持ち上げ**
  （合成された `func f__local[T...]`）か **モノモルフィ
  ゼーション**（使用ごとに本体を複製）が要る
- 持ち上げは右辺がローカル変数をキャプチャしないときだけ
  可能。`let f = fn (x: Int) -> x + base` のような普通の
  クロージャ（`base` を捕捉）は持ち上げると `base` を余計な
  パラメータとして通す closure conversion が要り、値の形が
  変わる — 推論だけでなく核心の呼び出し規約に触れる大掛
  かりな変更
- relaxed VR が緩和する典型例 `let g = const_ 1`（`b -> Int`
  の `b` は関数域の contravariant 位置）では OCaml でも
  relaxed VR 下で一般化されない。緩和で拾えるのは
  `let l = map f []` 系の covariant な例に限られ、それも
  上記 emission の壁はそのまま

つまり relaxed VR の採否以前に、ローカル多相を入れるには
emission 機構（持ち上げかモノモルフィゼーション）が必須
であり、その大半の費用は型検査ではなく lower/codegen にある。

## 選択肢

**A. 現状維持（採択）** — spec §3.3 の型規則どおり。多相が
要るヘルパはトップレベルに書く（そこでは既に一般化する）。
コスト 0、emission モデルと整合。実害は「トップレベルに
引っ越す手間」だけで、表現不能なプログラムはない

**B. キャプチャなし syntactic value のトップレベル持ち上げ** —
ローカル自由変数を持たない右辺だけ一般化し、合成名の
パッケージレベル generic 宣言として emit。`let f = fn (x) ->
x` の 2 型再利用が書ける。費用: lower にキャプチャ解析＋
名前合成＋`.gomi` への影響整理。利得は限定的（ユーザーが
トップレベルに書けば済む話）で、キャプチャ有りの素振りと
の非対称が新たな驚きになる

**C. relaxed value restriction 導入** — 一般化可能な右辺の
クラスを covariant 判定で広げる。emission の壁はそのまま
なので単独では着地せず、B との組み合わせでも拾える例が
少ない（covariant かつ expansive かつキャプチャなしの右辺
は稀）

**D. モノモルフィゼーション** — 使用ごとに束縛本体を複製。
Go の gc 自身の方針ではあるが、use-site 追跡と core/codegen
の複製機構が要り、診断の位置帰属も曖昧になる。他の理由で
モノモルフィゼーション基盤が入るまで手が出ない

## 決定

**案 A**。ローカル `let` の単相性を v1 型規則として維持する。
relaxed value restriction は「一般化の判定」に関する緩和で
あり、先に解くべき emission の制約（Go にローカル generics
は存在しない）とは直交 — 導入しても着地できない。多相
ヘルパの退避先はトップレベル `let`/`fn`（既に一般化する）
で足りている。

spec §3.3 の記述は現行どおりで変更なし。open-questions の
項目は本 decision への参照に置き換える。
