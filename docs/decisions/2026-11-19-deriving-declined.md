# deriving（コンパイラ mint の辞書生成）は入れない

- 状態: adopted
- 日付: 2026-11-19
- 問い: open-questions「`deriving` 相当の自動生成 — ユーザー型の
  `Eq`/`Ord`/`Show` 辞書をコンパイラが合成する仕組みの要否」

## 決定

deriving は入れない。`Ord` だけの限定導入も含めて見送り —
辞書は利用者が辞書渡しの形で全部記述する方針を維持する
（podhmo、PR #77 スレッド 2026-11-19）。

## 理由

- [ad-hoc-poly-dict-passing](2026-11-19-ad-hoc-poly-dict-passing.md)
  で型クラス不採用・明示的辞書渡しを採用した方針の延長 —
  コンパイラ mint は「辞書を誰が作るか」の自動化であって、
  現時点では手作業コストに見合うだけの規模の言語面増加を
  正当化できないと判断
- 調査（[sketch](../sketch/2026-11-19-wave25-deriving.md)）でも、
  derive が買うのは主に record/ADT の構造 `Ord`/`Show` で、
  `Eq` は `{ eq = fn a b -> a == b }` の 1 行で既に `gomEqualN`
  mint の恩恵を受けられる — 買いが薄い

## 影響

- open-questions の「`deriving` 相当の自動生成」項を閉じる
- `stdlib/cmp` への辞書合成関数（`option_ord`/`list_ord`/
  `pair_ord` 形 — 「辞書→辞書」の普通の Gom 関数）は言語機能
  ではなくライブラリ拡張なので本決定と矛盾しない。導入するかは
  別 lane（wave26 prompts C 項）の採否に委ねる
- 将来ユースケースが集まって再検討する場合の入口:
  [sketch](../sketch/2026-11-19-wave25-deriving.md) の案 2 節に
  設計空間（多相フィールドの辞書引数 plan kind・宣言駆動 mint
  経路）が整理済み
