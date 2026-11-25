# n-ary Result の pointer payload は verbatim のまま — Option は後包みか extern で選ぶ

- 状態: 採用
- 日付: 2026-11-25

## 文脈

`Gists.Create` のような `(*Gist, *Response, error)` を返す Go メソッドの
導出ビューは `Result (Ptr Gist, Ptr Response) GoError` と verbatim 固定で、
nil 監査の必要な位置では `Ptr.to_option` の後包みが要る（selfish 移植
stage 4、[sketch](../sketch/ja/2026-11-24-selfish-port.md)）。payload の
Option 宣言は extern 宣言側でのみ選べ、メソッド直呼びには出口がない。
spec 8.3 節はメソッドを持つ interface 型の要素だけを `Option` に包む。

## 検討した案

### A. pointer payload も n-ary Result で auto-wrap する（却下）

interface に適用されている nil 穴の論理は pointer にも同じくらい成立する
が、次の2点で割に合わない:

1. **arity 非一貫か大きな人間工学コストのどちらか**: n-ary だけ変えると
   `(T, error)` と `(T, U, error)` で意味が変わる。両方変えると
   `os.Open`/`os.Create` をはじめとする Go の多数派慣習
   （err == nil なら非 nil を返す契約）すべてに Option unwrap を強制する
2. **nil 返しは pointer では例外的**: interface 返しは nil を普通に返すが、
   pointer 返しで nil を返すのは API ごとの特殊な契約（selfish の
   `*github.Gist` も「ネットワーク入力なので」という個別の理由付けだった）。
   例外のために全員に unwrap を課すのは逆立ちしている

### B. 呼出し側で Option を宣言する仕組みの追加（見送り）

新しい宣言構文か属性が要る。`Ptr.to_option` の後包みは1行の合成可能な
コンビネータで、意味的には宣言的ビューと完全に等価（nil → None の読みは
境界でも後包みでも同じ）。複雑さに見合わない

## 決定

verbatim を維持し、慣用として **`Ptr.to_option` の後包み**を正とする。
関数については従来どおり extern 宣言で `Result (Option T, ...) GoError` の
宣言的ビューも選べる。interface 要素の auto-wrap は従来どおり残す
（nil interface は method call 以外に観測手段がなく、nil が普通に返る
ため — pointer とは例外的度合いが違う）。

## 検証

- `Ptr.to_option` の後包みが n-ary destructure と `try` の両方でそのまま
  機能することを確認（selfish 移植の `expect_gist` が実例）
- guide（go-interop reference）の `to_option` 記述は既存 — payload 位置への
  適用例を追記
