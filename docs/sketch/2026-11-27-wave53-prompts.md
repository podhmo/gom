# wave53 prompts

- 日付: 2026-11-27
- 状態: 採択済み（podhmo interview、2026-11-27）
- 入力: [wave52 round](2026-11-27-wave52-round.md)、[TODO](../../TODO.md)、[open-questions](../open-questions.md)、[implementation-gaps](../implementation-gaps.md)

wave52 は labeled lambda seeding、standalone init checksum、診断コード方向、coverage evidence、DMMF fit example を処理した。wave53 は wave52 で表面化した init failure atomicity と推論境界を先に実装し、その結果を踏まえて診断 UX の次段を調査する。

採用 lane は **B → A → C**。B は小さく独立しているため先に処理する。A の推論結果は二次 E0401 の扱いに影響するため、C の診断 UX 比較は A の後に置く。

## lane B. `gom init` failure atomicity（小）

podhmo 採択方針: **失敗時は init 由来の変更をすべて rollback する**。

- 新規 init では standalone validation 前に作成した `go.mod` / 生成されうる `go.sum` が validation failure で残る現状を直す
- `main.gom` は validation 成功後にだけ作られる。validation 後の `main.gom` write failure でも、init が作成・変更した file はすべて rollback する
- 既存 module への additive edit rollback は wave52 で済んでいるが、その rollback が `main.gom` write failure でも効くようにする
- directory 自体やユーザー既存ファイルの non-overwrite 契約は変えない。init が作っていない file は消さない
- 方針を decision/spec/test で固定する

## lane A. callback result への前向き record propagation（小〜中）

podhmo 採択方針: **callback result への前向き propagation を実装する**。

- `input.lines |> ... |> Result.map (fn lines -> { ... })` のように、generic higher-order callback の本体結果位置にある無名 record literal へ、静的に分かる expected type を前向きに伝える
- 対象は pipe / generic application の静的に分かる result expectation に限定する。後続使用からの逆推論は入れない
- lambda 本体の任意位置ではなく、結果として使われる式の形を red-first fixture で固定する。if/match などの分岐結果を対象にするかは lane 内で fixture から決める
- bare literal が解決できない形で二次 E0401 が出る場合、抑制できるかを fixture で確認する
- 型名前置 literal の優先順位、positional/labeled lambda の既存規則、monomorphic local inference は変えない
- implementation gap と open question を解消し、spec/decision/検証レポートを同期する

## lane C. E0401 provenance / details 昇格の次段調査（中、調査）

podhmo 採択方針: **4 論点を比較し、実装候補を 1 つだけ decision へ絞る**。

比較対象:

- if/match 腕・呼出し引数・record field への expected type provenance 伝播
- structured data の details 昇格基準
- 近接 span の text snippet 統合
- `gom explain` fragment example の検証方針

実装は lane に入れない。lane A/B の結果を踏まえ、次に実装すべき 1 面と red-first fixture 方針だけを decision へ記録する。選ばれなかった論点は open question へ残す。

## 採否インタビュー結果

- B/A/C を採用。D/E は見送り
- lane 順は B→A→C
- B は失敗時に init 由来の変更をすべて rollback する
- A は callback result への前向き propagation を実装する
- C は 4 論点を比較し、実装候補を 1 つだけ decision へ絞る

## 見送り候補（少なくとも現時点では lane にしない）

- checker 内部状態の分割 — 有効な内部改善だが、wave53 は wave52 直結の残件を優先する
- benchmark 基盤 — 現状すぐ使う最適化計画がなく、基盤だけ先に置く優先度は低い
- match narrowing の次段 precision — DMMF 再評価でも主用途は解消済み。guard/literal/field path は需要が出るまで保留
- stdlib `str` 拡張、Option traverse、panic capture adapter — 需要確認が弱い
- rt 表示一元化 — 有効だが checker 分割より後でよい
- `gom install`、Go 定数セット、cgo/unsafe/reflect、セルフホスト — future work 維持
