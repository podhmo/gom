# tutorial に足りないものの列挙（spec / design / SKILL / examples の対照）

- 日付: 2026-11-24
- 関連: [examples 案の列挙](2026-11-21-example-candidates.md)、
  [tutorial/README.md](../../../tutorial/README.md)、`docs/spec.md`、
  `docs/design.md`、`skills/gom-agent-guide/SKILL.md` と references/、`examples/`
- 状態: 調査記録と今回追加した step の対応表（規範ではない）

## 方法

`tutorial/01-hello`〜`12-result-values` が見せている機能を、
`docs/spec.md` の章立て・`skills/gom-agent-guide` の言語ツアー・
`examples/`（gist-cli / linkcheck / rgrep）が使う機能と突き合わせて洗い出した。
制約として、tutorial の各 step は「1 ファイルの `main.gom` + `expected.txt`」か
「壊れた `.gom` + `expected-check.txt`」の 2 形しか run.sh / `go test` が駆動しない。
そのため「動く 1 ファイルで見せられるもの」と「複数ファイル・手書き Go・
データファイルを要して今のハーネスでは動かせないもの」に分けた。

## tutorial が未カバーだった機能

### 今回 step として追加したもの（1 ファイルで動く）

| 機能 | spec | 追加 step |
| --- | --- | --- |
| `for x in xs`（List / View / Go.Slice / タプル・record 分解） | 1.1.7 | 13 |
| `@tailrec`（自己末尾再帰の強制と `for` への変換保証） | 1.1.8 | 13 |
| stdlib 短名 import（`import "list"` 等）と `List.*` / `Option.*` / `Result.*` / `cmp` の辞書 | 5 | 14 |
| `Ref` / `Array`（可変セルと構築バッファ） | 3.1, 8.3 | 15 |
| `Scope`（`Scope.run` / `defer` / `on_err` / `run_result` / `defer_err`） | 6.1 | 16 |
| `test` ブロック / `@test` / `assert` / `assert_eq` / `gom test` | 11 | 17 |
| `go {}` / `Chan` / `RecvChan` / `SendChan` / `select`（受信・送信・`default` 腕） / `Task` | 10 | 18 |
| `Ptr`（`Ptr.new` + フィールド書き込みによる Go struct 構築、`get`/`set`/`is_nil`/`to_option`） | 8.3, 8.5 | 19 |
| `Go.conv` / `Go.nil` / `Go.Slice` / `Go.Map` / named 型の算術 | 8.3, 8.5, 8.6 | 19 |
| 可変長引数の `..xs` スプレッド | 8.1 | 19 |
| `Go.FuncN` / `Go.funcN` / `Go.fnN`（コールバックの行き来） | 8.8 | 20 |
| `@method`（record を Go の interface に適合させる） | 8.9 | 20 |
| `Go.Any` / `Go.try_as` | 8.3, 8.6 | 20 |
| record spread 引数 `~..opts` / ラベル付きラムダ `fn ~x ->` / `type alias` | 2, 1.2 | 21 |

### ハーネス上は今回見送り（残りの gap）

- **複数ファイル 1 パッケージ / `.gomi` / `import "mod/path"` / `gom info`**
  （spec 5, 7）。`gom build <dir>` は 1 パッケージとして複数ファイルを通すが、
  run.sh / tutorial_test.go は `main.gom` 単体をコンパイルして生成 `main.go` と
  compiler API の出力を突き合わせるので、step にするには両ドライバの拡張が要る。
  別パッケージ import はさらに `.gomi` とモジュール解決（生成コード側の import
  解決も必要）が絡む。実用サイズの見せ方は `examples/` が担う領域 —
  gist-cli は複数ファイル 1 パッケージ、linkcheck/rgrep も同構成。
  `.gomi` で公開するライブラリ例は
  [example 案 B](2026-11-21-example-candidates.md)（`examples/semver`）に残っている。
- **`@extern fn`（シグネチャのみ宣言、実体は手書き Go）**（spec 8.4）。
  生成パッケージと同じ Go パッケージに手書き実装が要るため 1 ファイルでは
  動かせない。gist-cli の経緯は
  [extern 検証](../2026-11-03-extern-binding-verification.md)。
- **`@extern let` + `//gom:embed`**（spec 8.10）。埋め込み対象のデータ
  ファイルを生成 Go と同じディレクトリに置く必要があり、run.sh の
  scratch module 構成ではコピーされない。
- **`gom run` / `gom fix` / `gom explain` / `gom dump` / `gom schema` /
  `--strict` / `//gom:allow`**（spec 12）。言語機能ではなくツールチェーン —
  実行結果を `expected.txt` で検証する step の形に乗らない。
  README の「tools の早見」として触れる程度が妥当。
- **`rt.StepErr` を `errors.As`/`errors.Is` で読む**（07 章の延長）。
  `|?>` の step-name wrapping は 07 で言及済み。掘り方は
  [examples 案の表](2026-11-21-example-candidates.md)にも残るネタ。
- **生成 API を Go から呼ぶ向き**（spec 8.7）。`.gomi` が要るので同上。
- **小粒の構文**: ビット演算子（`&&&`/`&&^`/`|||`/`^^^`/`<<<`/`>>>`）、
  `Char` リテラル・`Byte`・`Int32` などのサイズ付き型、record pun
  `{x}`/`{x,}`、`@tailrec` の制約（E0902–E0905）、`//gom:` ディレクティブ群
  （付録 A）。いずれも既存 step のコメントか README の「気づくこと」に
  足す程度で、独立した step にはしなかった。

### README 側の古い記述（今回直す）

- 「`List.map`・`Option.get`・`Chan`・`Ref` は未実装（P4-8）」— 現在は
  `stdlib/{list,option,result,cmp}` が実装済みで `import "list"` などで使え、
  `Chan`/`Ref` は組み込みモジュールとして存在する（14 / 15 / 18 章）。
- 「パッケージ内のファイル間参照はまだ解決しない」— `gom build <dir>` は
  ディレクトリの `*.gom` を 1 パッケージとしてコンパイルする（examples は
  すべてこの形）。`main.gom` 単体を渡す今の step 構成では使わないだけ。

## 執筆中に見つけたこと

- **生成 Go が `declared and not used` になるケース**: `match r () { Ok n -> ignore (fmt.Println "ok ${n}") ... }`（`r : Result Unit String`）は check/build を通るが、補間は `"()"` リテラルに展開され束縛 `tmp` が未参照になり `go build` に落ちる（"declared and not used: tmp3"）。16 章は `Ok _ ->` で回避。[implementation-gaps に登録](../../implementation-gaps.md)。
- **tutorial_test.go の loader 配線が CLI とずれていた**: `compileStep` は `goimport.Loader` をそのまま `Loader`/`Sigs` に渡しており、`import "cmp"` が E0203（"not a Go package (use `import go`)"）に落ちた。CLI（`cmd/gom/cli.go` の `compileOptions`）は `gomimport` の composite を挟むので、同じ形に合わせた（`Modules`/`Types`/`FuncArity`/`SelfImportPath` も同じ）。今回の差分の本質は「ハーネスが Go-only loader だった」で、14 章で初めて `import "mod"` を使う step になって初めて露出。
- **`gom test` が吐く `main_test.go` が .gitignore 対象外だった**: 従来のルールは `[0-9]*/main.go` と `[0-9]*/badmatch.go` だけ。`[0-9]*/*.go` に広げた。

## 検証

- `./tutorial/run.sh`: 21 step すべて PASS（10 は `gom check` の E0301 検証）。
- `gofmt -l .` 空、`go vet ./...` クリーン、`go test ./...` 全 ok
  （`./tutorial/` も含む）。
- 各 step は先に `/tmp` の scratch module（`replace` でリポジトリ参照）で
  個別に build/run して expected.txt を採取した。

## 追記（2026-11-24、review 後）

- `22-struct-tags` を追加: record フィールド行の verbatim バッククォート
  struct tag（spec 3.2、[decision](../../decisions/2026-11-24-record-field-struct-tag.md)）。
  Marshal/Unmarshal の往復と `Ptr` 経由の `*User` 受け渡しを 1 ファイルで。
- `17-tests` は `expected-test.txt` 規約をハーネスに追加して `gom test` を
  実走する形に（review 指摘から。`elapsed` は正規化、生成ファイルは
  snapshot/restore）。
