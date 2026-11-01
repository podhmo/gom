# 未決事項と future work

決着したら [decisions/](decisions/) に 1 件 1 ファイルで記録し、[spec.md](spec.md) / [design.md](design.md) を更新してから、この一覧から消す。決まっていて作業するだけのものは [../TODO.md](../TODO.md) に移す。

## P0 の実験で決めるもの

机上で決めず、手書きの Go 実装案を比較して決める（TODO.md の P0）。

- [ ] ADT の表現: sealed interface か、タグ付きの単一 struct か（[decisions/value-representation](decisions/2026-11-01-value-representation.md)）
- [ ] `List` の表現: スライスベースか、永続 cons リストか。スライスなら `[x, ..rest]` は O(1) だが先頭追加は O(n) で、`rest` が元の配列を保持し続ける（[decisions/value-representation](decisions/2026-11-01-value-representation.md)）
- [ ] `Result` の表現: sealed interface か、`struct { Value T; Error E; Ok bool }` か。大きな値型のコピーとゼロ値の扱いも比べる（[decisions/value-representation](decisions/2026-11-01-value-representation.md)）
- [ ] 関数値の表現: カリー化形 `func(A) func(B) C` か、arity 付きクロージャか。多相な定義の具体化の時点と、パッケージをまたぐ関数値の公開形も含む（[decisions/function-representation](decisions/2026-11-01-function-representation.md)）

## 未決事項

### 型システム

- [ ] 型クラスの代替: v2 で型クラスを入れるか、明示的な辞書渡し（`Eq a` レコードを引数で渡す）で済ませるか
- [ ] 実装の型がインターフェースより一般的な場合（実装が多相、`.gomi` が単相）を許すか
- [ ] 可変性: `Ref a` と可変配列 `Array a` 以外に可変構造を許すか

### エラー処理

- [ ] `try` でも `|?>` と同じく `GoError` を自動ラップするか（今は `try` は包まない）
- [ ] `|?>` の右辺に `Result` を返さない関数も置けるようにするか（今は `Result.map` か `Ok` で包む必要がある）

### Go 相互運用

- [ ] `*T` の既定: `Ptr T`（Go と同じく nil で panic、書きやすい）と `Option (Ptr T)`（安全だが冗長）のどちらを既定にするか
- [ ] Go ABI の方針: 生成される Go の API（型名・関数のシグネチャ・バリアントの struct 名）を、Gom のバージョンをまたいでどこまで安定させるか
- [ ] Gom の型が Go の interface を実装できるようにするか（v1 ではできない）。できるなら、どう宣言するか
- [ ] Go のジェネリック関数のうち、`any` / `comparable` 以外の制約を持つものをどう扱うか
- [ ] 標準のバインディング（`(T, bool)` を `Option` にする関数の一覧など）をどこで管理するか

### 並行処理

- [ ] `select` の腕の選び方: Go と同じくランダムに選ぶままにするか、優先度付きの書き方を用意するか

### ツール

- [ ] 冗長な腕をエラーにするか警告にするか（開発中の書きかけコードでは警告の方が扱いやすい）
- [ ] フォーマッタの空行: ブロック内の空行を保持するか、構文木だけから完全に決めるか
- [ ] 診断 JSON のスキーマ: 独自スキーマのままにするか、LSP の Diagnostic や SARIF に寄せるか（内部表現からの変換なので、どちらでも本体は変わらない）

## Future work（v1 では扱わないと決めたもの）

- ラベル付き引数の制限の緩和（型が未知の関数へのラベル付き適用、ラベル付き／なし関数の暗黙変換）
- 省略可能引数とデフォルト値
- `|?>` の自動ラップの文言を設定可能にする。候補は、書式（関数名・ソース位置・任意の文言）をパッケージ単位で指定する方法と、ステップごとに文言を書ける構文。`strconv` のようにエラーが関数名を含むパッケージで、関数名が二重に付く問題もここで扱う。エラーの構造化されたフレーム情報（呼び出しの由来の追跡）も候補
- ローカル let 多相（入れる場合は relaxed value restriction を検討）
- チャネルで送れる値を型で制限する仕組み（Rust の `Send` 相当）
- 型クラス、`deriving` 相当の自動生成
- インクリメンタルビルドのキャッシュ（P4。それまでは毎回変換し、Go のキャッシュに任せる）
- cgo、`unsafe`、`reflect` の本格的な扱い
- セルフホスト（コンパイラを Gom で書き直す）

型クラス、エフェクトシステム、高度な所有権型、独自 GC は急いで導入しない。
