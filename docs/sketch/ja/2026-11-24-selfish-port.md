# selfish を gom に移植して分かったこと — 2026-11-24

[podhmo/selfish](https://github.com/podhmo/selfish)（GitHub Gist CLI、Go 約1400行）を gom に移植した記録。
実コードへの圧力テストとして、4段階の stacked PR で実施した。

| stage | PR | 内容 |
| --- | --- | --- |
| 1 | #208 | 忠実移植（oracle を `sandbox/selfish/` にコピー＋ `gomcommithistory`/`gselfish` に port） |
| 2 | #209 | idiomatic 化（Ref 撤廃 → `List.fold`/`Option`、型と見た目の commit を分離） |
| 3 | #211 | 広域リファクタ： `run` を純粋な `decide`（sum type `Action`）→ `execute` の木に切り分け → 局所モダナイズ |
| 4 | 本 PR | oracle 検証 + 本ドキュメント |

検証は oracle バイナリとの stdout/stderr diff（`--client fake`）。13 ケース中、
argv[0] 由来の `Usage of <name>:` と stack trace のパス以外は全一致。

## 検証レシピ

```sh
# 準備: 入力ファイル（readme.md = "# Hello Title" の markdown、hello.txt、
# bin.dat = バイナリ）、および ~/.config/selfish/config.json = {"access_token": "dummy-token"}
cd ~/repos/gom/sandbox/selfish && go build -o /tmp/gom-selfish ./gselfish
cd /path/to/oracle && go build -o /tmp/oracle-selfish .
cd /tmp/selfish-test   # 入力を置いた作業 dir

run_case() {
  /tmp/oracle-selfish "$@" > /tmp/o1 2>&1
  /tmp/gom-selfish    "$@" > /tmp/o2 2>&1
  # 除外: log の timestamp、stack trace の frame（関数行+パス行）、argv[0] の Usage 行
  sed -i 's/^[0-9\/]* [0-9:]* //' /tmp/o1 /tmp/o2
  diff <(grep -vE '^[[:space:]]|^Usage of |^\S*\.\S*$' /tmp/o1) \
       <(grep -vE '^[[:space:]]|^Usage of |^\S*\.\S*$' /tmp/o2)
}
```

注意: `--client fake` は `with_dryrun` で **history へ書き込まない**（oracle も同じ）。
そのため alias 系のケースは常に「履歴なし」状態で実行される — 2 連続実行しても
update ではなく create のまま（両バイナリで一致することを確認済み）。
実ファイル `~/.config/selfish/selfish.history` は読まれるだけで変更されないので
事前の削除は不要（同名 alias が履歴に残っていると update/delete 分岐に入るが、
それでも両者は同じ出力を返す）。

実行したケース: `--client fake` + `{readme.md hello.txt, --debug readme.md, bin.dat,
--alias m readme.md （dry-run のため連続実行しても create のまま）, --alias m --delete,
--alias z --delete （履歴なし alias=not-found）, --secret --silent readme.md,
"readme spaced.md" "ñ file.txt", nonexistent.txt, 引数なし}`, `--client bogus`, `--help`。

## oracle との残差（修正不可／意図的）

- `Usage of <argv0>:` — 両者とも `os.Args[0]` を使う正しい振る舞い。バイナリ名を揃えれば一致。
- `--delete` の not-found エラー — oracle は `pkg/errors` の stack trace を `%+v` で出力。
  `stack_errorf` shim で同じ形式を再現したが、**frame の関数名・ファイルパスはビルド由来**なので
  一致しない（oracle 自身もビルド場所が変われば変わる）。これは「振る舞いの一致」の限界点。

## うまくいったところ

- **`Result`/`Option`/`try` で error handling がそのまま書ける**。Go の `if err != nil` 連打が
  `try` と `|?>` でかなり読みやすくなった。
- **sum type が「処理の木」に効いた**。`Action = DeleteGist | CreateGist | UpdateGist Commit | SkipGist Commit`
  にすると、oracle で散らばっていた分岐（delete? alias? files 空?）がデータになり、
  `decide` が純粋関数としてそのまま `test` block で検証できた。これは移植で得た一番の収穫。
- **Go interop は実用レベル**。`github.Client`、`oauth2`、`Ptr`/`Go.Map`、構造体タグ（`flag:`/`json:`）まで通る。
  `flagstruct` が `json:`/`flag:` タグを読むので flag 定義がほぼそのまま移植できる。
- **生成コードのスタック trace が `.gom` のパス・行番号を指す**（`//line` pragma）。
  `pkg/errors` の `%+v` が `gselfish/app.gom:64` を出すのを確認した。デバッグ体験として正しい。
- **同型レコード acc は named type にすれば fold 推論が通る**（`type GistAcc = {...}`）。

## 言語側の穴・はまりどころ

- **`host.gom`→`host.go` で手書きファイルを上書きする**。生成物と同名の手書き `.go` を同じパッケージに
  置くと clobber される。手書きは `shim.go` に寄せる慣習が要る。
- **`@extern fn name () -> T` が `name(struct{}{})` を生成する**。Go 側は `func name(_ struct{})` で受ける
  必要がある。ドキュメントか、引数なし extern の生成規約を見直す余地。
- **生成される `fmt.Errorf` が引数を `any()` に box するため `go vet` で `%w` が弾かれる**。
  `wrap`/`wrapf` はすべて `@extern` shim へ退避した。error wrapping は言語側で第一級サポートしたい
  （`Result` がある分尚更）。なお `%+v` の出力自体は生成コードのまま問題なく動く —
  `pkg/errors` の shim が必要なのは、フォーマットではなく「stack を持つ error 値を作る側」。
- **`@method` はあるが `Result` 返りを禁じている（E0420）**ため、`encoding.TextMarshaler` 系
  （`UnmarshalText([]byte) error` など error を返す Go interface）には使えない。
  `ClientType`（`type X = {value: String}` → `type X string`）の `UnmarshalText` は
  同パッケージの `shim.go` に Go で書いた — 生成型へのメソッド追加は同一パッケージなら自由。
  `Result` を返さないメソッド（`http.Handler` 等）は `@method` で書けるので、穴は限定的。
- ~~**3つ以上の戻り値を呼べない**~~ → **解消済み**。stage 1 時点では shim 化したが、
  `(T, U, ..., error)` → `Result (T, U, ...) GoError` の n 値 fold（decisions/2026-11-18）で
  `let (g, _resp) = try (c.github.Gists.Create ctx gist)` と直接呼べることを stage 4 で確認し、
  shim を削除した（`c.Gists` フィールド経由の method 呼出しも通る）。残る制約は上限8値と
  `Option` 要素の扱いぐらい。
- **カリー適用の複数行記述がパースに弱い**（`List.fold\n  (fn ...)\n  acc\n  xs` で E0401）。
  lambda を `let` に取り出すと解決するが、書き味として引っかかりやすい。
- **無名レコードの `List.fold` acc は型推論が通らない（E0407）**。named type 必須。
  これは上の sumtypes メモと同じ系統の DX 摩擦。
- **`filter_map`/`find_map` が stdlib にない**。`List.fold` で回した。小さいが欲しい。
- **match の到達不能 arm は `todo ()` に頼る**。catch-all `action ->` の内側で sub-match する形に
  なると網羅性チェックの恩恵が薄れる。

## まとめ

1400行の実用 Go CLI が、数箇所の shim（`%w`・stack 付き error・`*int` boxing・`Result` 返すメソッド）を除いて
ほぼそのまま書けた。sum type への置き換えで `run` の分岐が「決定→実行」の木になり、
純粋関数 `decide` がテスト可能になったのは移植ならではの改善。
一番の改善要望は error wrapping（`%w`）の言語サポートと `@method` の `Result` 返し許可。
