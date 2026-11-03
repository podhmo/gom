# 未決事項と future work

決着したら [decisions/](decisions/) に 1 件 1 ファイルで記録し、[spec.md](spec.md) / [design.md](design.md) を更新してから、この一覧から消す。決まっていて作業するだけのものは [../TODO.md](../TODO.md) に移す。

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
- [ ] Go のジェネリック関数のうち、`any` / `comparable` 以外の制約を持つものをどう扱うか。P0-5 で、`any`/`comparable` だけの厳格な境界では `slices.Contains`（`[S ~[]E, E comparable]`）など `slices`/`maps`/`cmp` の主要な関数がほぼ全部呼べないことが分かった。P3-3 で `any`/`comparable` は呼び出し可能になったが、`~[]E`/`~map[K]V` などの構造制約は引き続き E0506 で非公開 — spec 8.1.6 の例とのずれは [gaps](implementation-gaps.md) 参照。広げるか、spec を「`any`/`comparable` のみ」と修正するかの判断が残る（[problems](problems/2026-11-04-generic-go-fns-hidden.md)、[go-interop-boundary](decisions/2026-11-01-go-interop-boundary.md)、[sketch: P0-5](sketch/2026-11-02-p0-5-go-types-mapping.md)）
- [ ] Go の struct 型の構築・書き込み: exported field の読み取りは [決定済み](decisions/2026-11-03-go-interface-assignment-and-fields.md)。コンポジットリテラル（`http.Server{Addr: ...}`）や field への代入を許すか、構築は Go の factory 関数や `extern` に限るか
- [ ] 標準のバインディング（`(T, bool)` を `Option` にする関数の一覧など）をどこで管理するか

### 並行処理

- [ ] `select` の腕の選び方: Go と同じくランダムに選ぶままにするか、優先度付きの書き方を用意するか

### ツール

- [ ] 冗長な腕をエラーにするか警告にするか（開発中の書きかけコードでは警告の方が扱いやすい）
- [ ] 同一パッケージ内の複数 `.gom` ファイルがトップレベルスコープを共有するか。現状は 1 ファイルずつ独立にコンパイルする（P1-11、[decision](decisions/2026-11-02-build-driver.md)）ため、`a.gom` から `b.gom` の定義は見えず E0201 になる。共有するならパッケージ全体を解決するフェーズか、兄弟ファイルの `.gomi` を読むかが要る
- [ ] フォーマッタの空行: ブロック内の空行を保持するか、構文木だけから完全に決めるか
- [ ] 診断 JSON のスキーマ: 独自スキーマのままにするか、LSP の Diagnostic や SARIF に寄せるか（内部表現からの変換なので、どちらでも本体は変わらない）
- [ ] ソース位置対応表（P4-5 のアンカー表）について、LSP はアンカー粒度（生成行 → 内側構文の span）より細かい列・範囲単位の対応を要するか。`//line` ドリフトの行エイリアシングの抜本対応（SourceMap-aware な逆引き＋曖昧時フォールバック）も P4-6 の設計入力
- [ ] panic スタックをユーザー向け出力で `.gom` 位置付きに再描画するか。現状は内部復元 API（`ResolveStack`）のみで `gom test` は raw 出力を保持。出すなら spec §12 で挙動を明文化する
- [ ] `.gomi` 適合検査（`checkInterfaces`）を `gom check`/`build` 以外のサブコマンドでも走らせるか。現状 `gom test`/`fix`/`info`/`dump` は `.gomi` 不整合のまま `Exports` 適用で生成物を書く（P4-8 レビュー指摘、仕様として build/check だけが契約ゲートなら問題なし）

## Future work（v1 では扱わないと決めたもの）

- ラベル付き引数の制限の緩和（型が未知の関数へのラベル付き適用、ラベル付き／なし関数の暗黙変換）
- 省略可能引数とデフォルト値
- `|?>` の自動ラップの文言を設定可能にする。候補は、書式（関数名・ソース位置・任意の文言）をパッケージ単位で指定する方法と、ステップごとに文言を書ける構文。`strconv` のようにエラーが関数名を含むパッケージで、関数名が二重に付く問題もここで扱う。エラーの構造化されたフレーム情報（呼び出しの由来の追跡）も候補
- ローカル let 多相（入れる場合は relaxed value restriction を検討）
- `List` のハイブリッド表現（chunked / unrolled list、所有権フラグ付きの反転ストレージなど）。先頭追加を償却 O(1) に戻す案。P0-3 でスライス表現（`type List[A] []A`）を採用したが、ユーザーレベルの `x :: acc` 型の逐次構築が支配的なワークロードで再検討する余地がある（[sketch: P0-3](sketch/2026-11-02-p0-3-list-representation.md)）
- チャネルで送れる値を型で制限する仕組み（Rust の `Send` 相当）
- 型クラス、`deriving` 相当の自動生成
- インクリメンタルビルドのキャッシュ（P4。それまでは毎回変換し、Go のキャッシュに任せる）
- cgo、`unsafe`、`reflect` の本格的な扱い
- セルフホスト（コンパイラを Gom で書き直す）

型クラス、エフェクトシステム、高度な所有権型、独自 GC は急いで導入しない。
