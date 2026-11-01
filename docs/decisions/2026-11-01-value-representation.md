# 値の表現（ADT・List・Result）

- 状態: 採用（P0-1・P0-3・P0-4 の実験で決定）
- 日付: 2026-11-01

## 決まったこと

- `List a` の意味: 不変の値として扱うシーケンス。Go のスライスとの境界では両方向ともコピーする。コピーを避けたいときは Go が所有する可変の `Go.Slice a` を使う
- `Array a` は Gom の可変配列
- ADT の網羅性が保証するのは、Gom の中で作られた値だけ。Go から渡された不正値（`nil` など）は契約違反とし、`match` で型名を含むメッセージで panic する

## ADT: sealed interface＋バリアント struct（P0-1）

- ADT は sealed interface＋バリアント struct（design.md 4.1 の案を採用）。タグ付きの単一 struct は不採用: 構造的 `==` を素朴には満たせない（再帰 payload はポインタなので実体比較にならず、バリアントごとの `Equal` メソッド生成が要る）、再帰位置を inline にするかポインタにするかをコンパイラがバリアントごとに判断する必要がある、生成コード量が約 4 割増える。性能も、葉バリアントのボックス化を避けられる構築ではタグ付き struct が速いが、走査・再帰・大きな値の受け渡しでは sealed が同等かそれ以上だった
- **ジェネリックな ADT の罠（全 generic sealed ADT に効く）**: マーカーメソッドのシグネチャに型パラメータを含める。`interface{ isOption() A }`、複数なら `interface{ isEither() (A, B) }`。シグネチャに型パラメータが出ないと phantom になり、呼び出し側の型推論が型引数を束縛できない。P0-1 と P0-3 で独立に確認された罠で、最悪の形は `Option[int64]` の値が `Option[int]` の引数を構造的に満たしてコンパイルが通り、実行時に type switch を素通りして `default` に落ちる（あるいは `default` のないコードで静かに誤った結果になる）静かな誤推論。パラメータをシグネチャに書けば実体化どうしが別型になるので、残る誤推論もコンパイルエラーになる（P0-3 は `isList(List[A])` という自己参照形でも直ると報告しているが、シグネチャへの直接記述の方が簡潔なのでこちらに統一する）
- バリアントはポインタ型ではなく値 struct にする。Go 側から typed nil が渡されても `case` に合致せず、`default` で `rt.InvalidVariant` に落ちる
- 根拠: [sketch: P0-1](../sketch/2026-11-02-p0-1-adt-representation.md)

## `List`: スライス表現（P0-3）

- `List a` は `type List[A] []A`。`nil` は有効な空リスト。`len`・インデックス・`range` がそのまま使え、Go 側からも最も自然な形
- `[x, ..rest]` パターン（`Tail` / drop 相当）: O(1) のビュー。ただし `rest` は元の配列全体を保持し続ける（捨てた先頭側と、ポインタ要素が指すオブジェクトも含む）。大きなリストの小さい末尾を長期保持する用途向けに、コピーして配列を解放する `List.compact` 系の関数を stdlib に用意する
- `x :: rest` / `[x, ..rest]` の構築: O(n)（新しい配列の割り当て＋全体コピー）。cons リストの O(1) 先頭追加を利用者に期待させない。逐次構築はコンビネータか、`Array` をビルダーとして `to_list` に渡す形に誘導する
- 永続 cons リストは不採用: prepend 構築だけは大きく勝つ（n=10k で約 700 倍）が、走査が 4〜6 倍遅く、要素あたりのメモリ・GC が走るヒープオブジェクトが増え、`Len` が O(n)、境界変換でも要素ごとのアロケーションが要るため。ADT としてコンパイルした cons（sealed interface 版）は不透明 cons よりさらに悪い
- chunked / unrolled などのハイブリッド表現は v1 では採らず future work（open-questions.md）
- 根拠: [sketch: P0-3](../sketch/2026-11-02-p0-3-list-representation.md)

## `Result`: 非公開タグ付き struct（P0-4）

- `Result a e` は `struct { state ResultTag; Value T; Error E }`（tag は非公開。0=invalid・1=Ok・2=Err）。sealed interface でも、`struct { Value T; Error E; Ok bool }` でもない: bool 判別子はゼロ値が `Err(zero E)` として読めてしまい、`(T, error)` 境界で `(zero T, nil)` の偽の成功を作り出す。3 状態タグはゼロ値を `rt.InvalidVariant` の対象に戻しつつ、構築・判定・分解・`(T, error)` 変換で sealed の 2〜20 倍（Ok パスでボックス割り当てなし）
- コンストラクタは `Ok[T,E](v)` / `Err[T,E](e)`（関数。tag が非公開なので Go 側はリテラルでは作れない）。match での分解は `Tag()` スイッチ＋`default: rt.InvalidVariant`。bare な comma-ok 型アサーション（`r.(Result_Err[...])` 型）は invalid 値を Ok 側のパスに流すので使わない
- `E` の型引数には組み込み `error` を使う。名前付き `GoError` interface を型引数にしない（ジェネリックの実体化は不変なので `Result[T, GoError]` と `Result[T, error]` が別型になる。`GoError` は Gom 側の名前、または alias に留める）
- `Result` だけの特別扱いで、ユーザー定義 ADT は P0-1 の sealed encoding のまま。`Result` は保持するデータではなく失敗しうる呼び出し規約であり、construct→check→destructure の一生が flat の得意な領域で完結するうえ、`|?>` の融合生成（design.md 4.6）でステップ間の中間 `Result` はそもそも作らないため、sealed の利点（共有・表現の統一）が生きない
- 根拠: [sketch: P0-4](../sketch/2026-11-02-p0-4-result-representation.md)
