# Wave 3 オーケストレーション用プロンプト案

2026-11-03 時点の main 状態（Wave 1+2 統合済み: P2-3, P2-5, P4-1, P4-3, P4-4, P4-9, spec 例整理）。V-4 はこの記録と同タイミングでマージ済み前提。

このドキュメントは次セッションで子エージェントに渡すプロンプトのたたき台。実施時は各プロンプト末尾の「共通指示」を必ず付ける。

## 今回のマージで新たに依存クリアしたタスク

- P3-3（P2-5 ✓）、P3-4 / P3-5（P3-1 ✓）
- P4-5（P1-11 ✓）、P4-7 / P4-8（P4-1 ✓、P4-8 は P2-5 も ✓）
- V-6（P4-1 ✓）
- P2-4b（P2-4a ✓）、P2-2c（P2-2b ✓）

## 推奨 Wave 3 構成（領域が重ならない 4 件）

| # | タスク | 主な触る領域 | worktree |
|---|---|---|---|
| 1 | P3-3 スライス・map・ジェネリック関数 | types / goimport / check | gom-wt-p3-3-go-coll |
| 2 | P4-5 ソース位置対応表 | codegen emit / diag / build | gom-wt-p4-5-srcloc |
| 3 | P4-8 stdlib `list`/`option`/`result` | stdlib/*.gom（新規中心）+ build | gom-wt-p4-8-stdlib |
| 4 | P2-4b try パイプ融合 | lower / codegen | gom-wt-p2-4b-pipefuse |

同時に走らせない方がよい組み合わせ: P2-2c と P2-4b（共に lower/codegen の初期化・評価順を触る）、P3-3 と P3-5（共に goimport の型変換表を触る）。P3-5 と extern arity バグ（TODO bugs 先頭）は 1 タスクにまとめて Wave 4 へ。

## 個別プロンプト

### 1. P3-3 — `wt/p3-3-go-coll` @ gom-wt-p3-3-go-coll

```
Gom コンパイラの P3-3 を実装する。TODO.md の P3-3 項を正とする:
- `Go.Slice T` / `Go.Map K V` 型（または spec が定める形）と Gom の
  `List` との境界コピー変換、`nil` スライスの扱い
- `go/types` の代入規則に従う named type / interface との相互作用は
  P3-2 の既存実装に乗る
- any/comparable 制約の Go ジェネリック関数の呼び出し（型引数の明示
  または推論）。P2-5 で入った Gom 側ジェネリクス出力との整合に注意
依存: P3-1 ✓, P2-5 ✓。先に spec 8 章と docs/design.md、既存の
goimport/check の相互運用コード、skills/gom-agent-guide の Go interop
節を読む。Go 相互運用の変更なので Gom→Go と Go→Gom 両方向のテストを
足す（nil、スライス、named type、interface 各ケース）。
```

### 2. P4-5 — `wt/p4-5-srcloc` @ gom-wt-p4-5-srcloc

```
Gom コンパイラの P4-5 を実装する。design.md 2.2 の Gom → Core IR →
Go 対応表。現在 codegen は //line プラグマを既に出しているので、
その精度の検証と、対応表データ自体のエクスポート（build.Result 経由）
が主な仕事:
- `go build` / `go vet` のエラー出力を .gom 位置へ写す写像（build 層）
- panic スタックの Gom 位置への復元（rt または診断ユーティリティ）
- golden / spec fixture で //line の正しさを pin
依存: P1-11 ✓。成果物は後続 P4-6 (LSP) の土台になるので、写像は
内部 API として取得可能な形にする。
```

### 3. P4-8 — `wt/p4-8-stdlib` @ gom-wt-p4-8-stdlib

```
Gom コンパイラの P4-8 を実装する。stdlib/list, stdlib/option,
stdlib/result を Gom で書き、各パッケージに .gomi を付けて
コンパイラ自身でビルドする:
- spec の冒頭例（List.map / List.sum）が実際に動くことを目標に API を選定
- P4-1 の .gomi 機構と P2-5 のジェネリクス出力の両方を実使用する
  最初の大規模ユーザー。不足が見つかったらコンパイラ側の問題として
  TODO.md / docs/problems に記録し、無理に work around しない
- 生成 Go がコンパイルされ動作することを go test で検証
依存: P4-1 ✓, P2-5 ✓。skills/gom-agent-guide を先に読むこと。
```

### 4. P2-4b — `wt/p2-4b-pipefuse` @ gom-wt-p2-4b-pipefuse

```
Gom コンパイラの P2-4b を実装する。TODO.md の P2-4b 項と
docs/sketch/2026-11-03-result-pipe-local-boundary.md を正とする:
- 明示的 try を含むパイプ・連続チェーンで中間 Result の生成を削減
- 脱出先（最も内側の関数境界）は decisions/try-boundary のとおり
  保存する。意味を変えないことが最優先（現在の保守的経路と
  eval-vs-compiled で同じ結果になること）
- ラムダ内だけに try があるケースも対象
依存: P2-4a ✓。生成コードの変更なので testdata/golden 更新と
V-3 の eval/compiled 比較テストの拡充を含める。
```

## バグバッチ（Wave 3 の 5 件目、または Wave 4 冒頭の小タスク）

```
TODO.md bugs 節のうち局所的な 3 件をまとめて修正:
- Go.Any への数値リテラルが Gom 具体型を保たない（conv/boxing 経路）
- Float 推論の整数リテラル束縛が Go int で生成される（codegen リテラル出力）
- extern `Option (Ptr Flag)` の壊れた arity 診断（P3-5 と同じ経路。
  P3-5 と同日に走らせるならこちらは診断文言だけに留める）
各バグに testdata/spec か codegen テストの回帰を足す。
大きい 2 件はこのバッチに入れない: 遅延束縛レシーバの Go メソッド誤分類
（推論層の設計変更が要る、P3-4 と同領域）、Float 定数畳み込み・
バリアント .f の divergence 2 件（spec の意味決定が先）。
```

## Wave 4 候補（Wave 3 完了後）

- P3-5 extern + extern arity バグ（P3-3 と goimport が重なるため分離）
- P3-4 メソッド呼び出し・`_` プレースホルダ（遅延束縛レシーババグと同領域、
  一緒に設計したい）
- P3-6 / P3-7 Go 相互運用の完了確認（P3-2〜P3-5 全部の後）
- P4-6 LSP（P4-5 依存）、P4-7 増分キャッシュ（iface.Hash の消費者）、
  P5-1 並行性、V-6 複数ファイル縦断、P2-2c 循環関数値
- 意味決定が要る divergence 2 件（Float 定数畳み込み、バリアント .f）は
  実装タスクではなく spec 決定タスクとして扱う

## 共通指示（各プロンプト末尾に付ける）

```
完了前に gofmt -l .（出力なし）、go vet ./...、go test ./... を通す。
実装中に決めたことは docs/decisions に 状態: draft で追加し、検証結果を
docs/sketch に書いてから状態を更新する。言語規則の変更は testdata/spec、
生成コードの変更は testdata/golden、Go 相互運用は両方向のテストを足す。
.gom/.gomi を書く前に skills/gom-agent-guide を読む。
```
