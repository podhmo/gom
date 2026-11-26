# wave50 lane E: record literal 文脈推論の検証

- 日付: 2026-11-27
- lane: wave50 lane E
- decision: [record literal の文脈推論](../decisions/2026-11-27-record-contextual-inference.md)

## 実施内容

- 型名前置のない record literal が、静的に分かる record 期待型を field-set 推論より先に使うようにした
- generic な関数・constructor 適用では、結果の期待型と残余型を clone 上で speculative に単一化し、具体化が分かったパラメータだけを文脈期待型として使うようにした。callee の型が `Box a` のように部分的に開いているときは constructor ごとに型引数を merge し、推論済みの具体型を消さない
- 文脈期待型でチェックした引数は、そのあと callee 本来のパラメータ型へ `conformExpr` する。外部 meta を早く bind せず、局所 `let` の単相性も素通りしない
- 期待型の関数パラメータを受け取る注釈なしラムダでは、そのパラメータが record 型のときだけ同じ型で seed するようにした
- 無名 record literal を含む部分適用 callee に限り、`|>` / `<|` では pipe 結果の期待型を callee 側へ渡すようにした

## red-first 確認

wave-process の運用どおり、実装前に spec fixture を追加して red を確認した。

- `ok_record_context_list_fold_acc`: 直接適用の seed literal で E0407、fold 関数内の record update に連鎖する E0401 を確認
- 同 fixture に `xs |> List.fold ... { count = 0 }` を追加した時点でも、pipe 経由では期待型が callee へ届かず E0407/E0401 の 4 件を確認
- negative fixture として `E0407_record_context_no_backward` と `E0401_record_context_prefix_priority` を先に登録し、後続使用逆推論の禁止と型名前置優先を固定した
- review で escalated された nested generic record では、`ok_record_context_nested_generic` を先に追加し、内側 literal の E0407 と連鎖する 2 件の E0401 を確認してから修正した
- 続く review の curried callback 指摘では、`ok_record_context_curried_callback` を先に追加し、内側 literal の E0407 と結果側の E0401 を確認してから修正した。label 伝播と逆向き arity は統合 fixture では labeled lambda seeding の未決問題と分離できなかったため、merge 規則は unit test で固定した。label を含む文脈型は review で curried binding・conditional lowering・direct generic conformance の退行が分かり、wave50 の scope では propagation 自体を行わない境界へ絞った

## fixture / probe 結果

- `ok_record_context_simple_positions`: 型注釈、return、generic 呼出し引数（`id { count = x }`）、record field の nested literalがすべて通る
- `ok_record_context_constructor_payload`: `Ok { count = x }` が `Result Acc GoError` の文脈で通る
- `ok_record_context_list_fold_acc`: `List.fold` の無名 record accumulator が直接適用・`|>` の両方で通る。fold 関数内の `{ acc with ... }` も同じ `Acc` で検査される
- `ok_record_context_nested_generic`: `Box a` の `a` が戻り値の `Box Inner` から分かるとき、nested literal が引数順に依存せず `Inner` として通る。先行引数で `a` が確定済みの順序でも既存の推論を上書きしない
- `ok_record_context_curried_callback`: callback の開いた戻り値が `Int -> Acc` の関数として分かるとき、nested lambda 内の record literal まで文脈が届く
- `E0407_record_context_no_backward`: 注釈のない `let seed = { count = 0 }` は後続の `List.fold` 使用から `Acc` に逆推論されず E0407 のまま
- `E0401_record_context_prefix_priority`: `Other { ... }` は `Acc` 期待より型名前置が優先され、E0401 になる

## 自動検証

- `go test ./internal/check -count=1`: pass
- `go test ./internal/build -run 'Spec/(ok_record_context|E0407_record_context|E0401_record_context|E0401_go_any_lambda_use)' -count=1`: pass
- `go test ./...`: pass

最終確認: `gofmt -l .`（出力なし）、`go vet ./...`、`scripts/check-staticcheck.sh`、`scripts/check-sumtype.sh` はすべて pass。

## 境界と closeout 候補

- 静的に名指せる record 型がどこにもない ad hoc accumulator shape は依然として作れない。wave50 lane E の scope どおり匿名 record 型の発明や structural record typing は入っていない。必要なら「匿名 record 型 / structural record を導入するか」の open question 候補として closeout で扱う
- 注釈なしラムダのパラメータ seeding は record 型だけの限定例外。`Go.Any` など record 以外の expected parameter を seed するかは従来どおり別問題で、`E0401_go_any_lambda_use` の意味は変えていない
- pipe では無名 record literal を含み、かつ結果期待型に record 型を含む部分適用 callee にだけ結果期待型を渡す。record literal を含まない一般の pipe 推論は変えていない
- generic 適用の speculative propagation は、供給スロットが静的に決まり、かつ引数に直接の無名 record literal がある場合に限定している。spread や未決定スロット、over-application には広げていない
- 文脈期待側の型が関数パラメータ label を含むときは propagation しない。labeled expected signature の positional lambda parameter へ record seeding するか、および label を含む文脈型を安全に propagate するかは引き続き未決のまま
