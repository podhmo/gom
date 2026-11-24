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
  （`Result` がある分尚更）。同じく `%+v` で stack trace を出す `pkg/errors` 系も shim 必須。
- **メソッド宣言ができない**。`ClientType`（`type X = {value: String}` → `type X string`）に
  `UnmarshalText` を実装したくても gom では書けない。同パッケージの `shim.go` に Go で書けば
  生成型にメソッドが生やせるので回避可能だが、言語仕様としての不足。
- **3つ以上の戻り値（`(v, resp, err)`）を呼べない**。`github.Gists.Create` 系は shim でラップして
  `Result` に畳んだ。gom に tuple/多値がない以上、Go API 利用時に頻出しそう。
- **カリー適用の複数行記述がパースに弱い**（`List.fold\n  (fn ...)\n  acc\n  xs` で E0401）。
  lambda を `let` に取り出すと解決するが、書き味として引っかかりやすい。
- **無名レコードの `List.fold` acc は型推論が通らない（E0407）**。named type 必須。
  これは上の sumtypes メモと同じ系統の DX 摩擦。
- **`filter_map`/`find_map` が stdlib にない**。`List.fold` で回した。小さいが欲しい。
- **match の到達不能 arm は `todo ()` に頼る**。catch-all `action ->` の内側で sub-match する形に
  なると網羅性チェックの恩恵が薄れる。

## oracle との残差（修正不可／意図的）

- `Usage of <argv0>:` — 両者とも `os.Args[0]` を使う正しい振る舞い。バイナリ名を揃えれば一致。
- `--delete` の not-found エラー — oracle は `pkg/errors` の stack trace を `%+v` で出力。
  `stack_errorf` shim で同じ形式を再現したが、**frame の関数名・ファイルパスはビルド由来**なので
  一致しない（oracle 自身もビルド場所が変われば変わる）。これは「振る舞いの一致」の限界点。

## まとめ

1400行の実用 Go CLI が、数箇所の shim（`%w`・3値戻り値・ポインタ boxing・メソッド）を除いて
ほぼそのまま書けた。sum type への置き換えで `run` の分岐が「決定→実行」の木になり、
純粋関数 `decide` がテスト可能になったのは移植ならではの改善。
一番の改善要望は error wrapping（`%w`/`%+v`）と multi-return の言語サポート。
