# wave48 prompts

- 日付: 2026-11-26
- 採否: A〜F すべて採用（podhmo、2026-11-26）。G は同日に追加指定
- 順序: A → B → C → D → E → F → G

wave47 の残件と TODO/open questions/implementation-gaps から。採否インタビューでは、
推薦どおり C は module-local source dependency 限定、D は `.gomi` directive 引き継ぎ、
E は byte 基礎 + 明示的な rune 面を第一案として採用された。

## 採用 lane

### A. `Unit` payload 束縛の生成 Go 未使用 bug（小）

`match r () { Ok n -> ... "${n}" ... }`（`r : Result Unit String`）が check/build を
通るのに、生成 Go では補間用の `"()"` リテラルだけを使い、束縛した一時変数が未使用で
`go build` に落ちる。`Unit` payload の束縛・表示・一時変数生成の整合を取り、生成 Go が
コンパイルできることを golden / spec で固定する。`Unit` 意味論の再設計はしない。

### B. incremental build cache の dependency hash race（中）

`.gomi` / Go dep / module file の fingerprint を compile 中に観測した内容ではなく
commit 時に再読した内容から記録すると、依存が compile 中に変わったとき古い依存版から
作った output が新しい fingerprint で commit されうる。wave47 後に入った Quint モデルが
witness を確認済み。manifest へ記録する依存内容を compile 時観測へ寄せ、race witness に
対応する regression を残す。cache 全体の再設計や新しい並列化はしない。

### C. `gom build` / `gom run` の module-local transitive emit（中〜大）

gom の `import` 先を持つ `package main` が `gom run` だけでは動かず、依存を先に
`gom build` する 2 段運用が要る。module-local な `.gom` source dependency を依存順に
emit し、`gom run` / `gom build` が単独でリンク可能な output を作る。scope は同一
module 内の source dependency に絞り、module cache 内の外部 package への書き込みや
`gom install` は対象外とする。B の cache 修正を先に入れて依存 fingerprint の前提を揃える。

### D. `.gomi` author metadata の fidelity（中）

wave47 lane C で extern escape directive の引き継ぎは直ったが、directive scan は spec の
attachment 規則より広く、abstract/private 意図は再生成で消えるまま残っている。
scanDirective を「宣言直上に連続するコメント行」へ合わせる修正と、abstract/private 意図を
再生成に残す仕組みを同じ `.gomi` metadata 面として扱う。抽象化・非公開化の記述は
`.gomi` directive（例: `//gom:abstract` / `//gom:private` 系）で表し `gom info` が
canonical signature が変わらない間だけ引き継ぐ。実装で directive 名を確定するときは
既存の `//gom:mutable` / escape directives と衝突しない語形にする。

### E. 文字列操作面の設計と最小実装（中）

`len(s)` / `s[i]` / `s[lo:hi]` 相当がなく、文字列走査は `strings.Split (s, "")` と
`List.get` の迂回になる。builtin `String` のメンバー面にするか stdlib package にするか、
byte index / rune index のどちらを正にするかを決め、最小の操作面を実装する。
Unicode 文字列処理全般や mutable string builder の設計は対象外。byte 長・byte 範囲を
基礎にし、rune 走査は明示的な面に分ける。

### F. spec / explain の小さな mismatch 掃除（小）

大きな言語変更を伴わない既知のずれをまとめて潰す。

- spec §7 に record の abstract 宣言を明記する
- 引数なし `@extern fn` が Go 側 `func name(_ struct{})` を要することを spec に例示する
- `gom explain E0513` の nil-able 一覧へ `Go.FuncN` を足す
- `Go.conv` の `List` → `Go.Slice` で gom 宣言要素型が E0507 になる現行制限を spec に明記する

### G. PR #256 owner improvement ideas の精査と台帳登録（小）

[PR #256](https://github.com/podhmo/gom/pull/256) の 20 件の改善案を、PR 上の Devin Review / podhmo コメントと wave48 lane A〜F 後の状態へ突き合わせる。採用可能な未決事項は open-questions、実施可能な内部作業は TODO、spec/implementation/docs mismatch は implementation-gaps、既存 problem への追記に分ける。U3（端末では text 既定）と U8（LSP）は podhmo コメントで却下済み。F1/F3 は wave48 lane E/D の採用済み decision と競合する部分を戻さない。コード変更はしない。

## 見送り候補

- record literal の E0407 曖昧さ緩和 — 段階型パターンでは価値が高いが、型推論の設計変更が
  大きい。wave48 では E とどちらを入れるかの選択肢とし、両方は入れない案
- `uint16` / `uint32` / `uintptr` — wave47 で需要が出るまで見送り済み。新しい阻塞はない
- Go 定数セット / `iota` 流宣言 — wave47 と同じく優先度低め
- `gom install` — C の transitive emit が前提。wave48 では scope に入れない
- `pkg/errors` 系の stack を持つ error 値生成、match 到達不能 arm の構造化、無名 record の
  `List.fold` acc 推論 — 実用の摩擦はあるが wave48 の上位候補ではない
- D-3 README / D-4 tutorial/examples module 配置整理 — ユーザー指定どおりプロジェクト終盤まで保留

## 採否インタビュー結果

1. A〜F をすべて採用し、順序は推薦どおり A → B → C → D → E → F。G は同日の追加指定として末尾に足す
2. C の scope は module-local source dependency に限定する
3. D の abstract/private 意図は `.gomi` directive で引き継ぐ
4. E は byte 長・byte 範囲を基礎にし、rune 走査を明示的な面に分ける
5. F の spec/explain 掃除も wave48 に入れる
6. G は PR #256 の精査と台帳登録に限定し、提案そのものの実装はしない
