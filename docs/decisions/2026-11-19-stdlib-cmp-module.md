# stdlib に Eq/Ord 辞書を取る cmp モジュールを追加する

- 状態: adopted
- 日付: 2026-11-19
- 問い: [ad-hoc-poly-dict-passing](2026-11-19-ad-hoc-poly-dict-passing.md)
  が残した「stdlib に比較器辞書を取る API を追加するか」の判断

## 決定

追加する。`stdlib/cmp` モジュール（`import "cmp"` で読み込み）に
`Eq a` / `Ord a` 辞書型、スカラー型ごとのインスタンス、辞書を
第 1 引数に取る `sort` / `min` / `max` / `member`、辞書合成の
`desc` / `eq_of` を載せる（wave24 lane D）。

## 理由

- 辞書渡しを採用する以上、ユーザーごとに辞書型とインスタンスを
  書き直すのはイディオムの立ち上げに対して直接の増税。型名・
  関数名が stdlib で固定されると、プロジェクト間でも読み味が
  揃う
- 辞書引数は第 1 引数、コレクションは末尾 — `xs |> cmp.sort
  cmp.int_ord` と pipe に乗せられる形に揃える（元 decision の
  「追加するなら」条件どおり）
- `cmp` は builtin-scope に加えず `import "cmp"` 導線の通常
  stdlib モジュールとする。builtin-scope は組み込み型（`List` /
  `Option` / `Result` ほか型名と同名のモジュール）に限定されて
  おり、辞書束にそれは馴染まない。短名マップ（`internal/gomimport`
  の `stdlibPaths`）に `"cmp"` を足す

## 影響

- spec.md §7（import）の stdlib 短名の列挙に `"cmp"` を追加
- design.md の stdlib 構成（DAG: `cmp` は `list` に依存）
- `Byte` は `<` を持たない（E0401「ordered type」）ため
  `byte_eq` のみ、`byte_ord` は無し
- deriving 相当は引き続き open-questions の future work
