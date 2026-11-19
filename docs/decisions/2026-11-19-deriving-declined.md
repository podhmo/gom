# deriving（コンパイラ mint の辞書生成）— `Ord` のための導入は見送り

- 状態: adopted
- 日付: 2026-11-19
- 問い: open-questions「`deriving` 相当の自動生成 — ユーザー型の
  `Eq`/`Ord`/`Show` 辞書をコンパイラが合成する仕組みの要否」

## 決定

`Ord` だけの限定導入のために deriving を入れることは見送り。
辞書は利用者が辞書渡しの形で記述する方針を維持する
（podhmo、PR #77・#78 スレッド 2026-11-19）。

deriving 全般を今後も否定する決定ではない — `Ord` 以外にも
実装できるものが豊富にあり、導入で十分に便利になると見込める
なら再考の余地がある。

## 理由

- [ad-hoc-poly-dict-passing](2026-11-19-ad-hoc-poly-dict-passing.md)
  で型クラス不採用・明示的辞書渡しを採用した方針の延長
- 調査（[sketch](../sketch/2026-11-19-wave25-deriving.md)）でも、
  `Ord` 限定なら derive が買うのは record/ADT の構造比較のみで
  言語面の増加に見合わない。`Eq` は `{ eq = fn a b -> a == b }`
  の 1 行で既に `gomEqualN` mint の恩恵を受けられる
- 一方で derive の対象が広がる（`Show`・多相フィールド・他の
  辞書種）場合の価値は別評価になる — 判断は「対象が `Ord`
  だけ」に限ったもの

## 影響

- open-questions の「`deriving` 相当の自動生成」項は本決定の
  記録に置き換えて閉じる — 再検討の入口（設計空間）は
  [sketch](../sketch/2026-11-19-wave25-deriving.md) の案 2 節に
  残る（多相フィールドの辞書引数 plan kind・宣言駆動 mint 経路）
- `stdlib/cmp` への辞書合成関数（`option_ord`/`list_ord`/
  `pair_ord` 形 — 「辞書→辞書」の普通の Gom 関数）は言語機能
  ではなくライブラリ拡張なので本決定と矛盾しない。導入するかは
  別 lane（wave26 prompts C 項）の採否に委ねる
