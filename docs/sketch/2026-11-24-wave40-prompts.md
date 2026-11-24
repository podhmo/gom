# wave40 lane prompts（草案）

- 日付: 2026-11-24
- 前提: wave39 スタック（#181–#185）が main にマージ済みの新 main
  から開始。前段の採否インタビュー結果のみを lane 化する
- レビュー: Devin Review が各 PR に findings を出す — 主セッションが
  トリアージ・修正する（sub-session は立てない）
- 前 wave の round 記録: [2026-11-24-wave39-round](2026-11-24-wave39-round.md)

## 採択 lane（採否インタビュー: 全6件採択、2026-11-24）

### A. 部分適用の握り潰し lint の実装（decision 採択確認→実装・規模: 中）

wave39 lane C の [decision draft](../decisions/2026-11-24-partial-application-lint.md):
`ignore`・`_` 束縛（package-level 含む）・match `_` 腕の 3 面で
捨てられる `*types.Func` 値を警告する。**decision は採択として扱う（podhmo 2026-11-24）**:

- W 系警告の新設（lint.go の規則群に並べる）+ spec 追記 + fixture
- `//gom:allow` を warning にも効かせる拡張（現状 error→warning の
  demote のみ）— この lane の前提機構
- `any` 引数経由の関数値渡しは lane の対象外（decision で明示的に
  open のまま）

### B. `match` の型テスト腕の実装（decision 採択確認→実装・規模: 中〜大）

wave39 lane E の [decision draft](../decisions/2026-11-24-match-type-test-arms.md):
`Go.Any` 系（named Go interface 含む）scrutinee に限り `p : T` 腕・`_` 腕必須・first-match・
Go type-switch emit。**decision は採択として扱う（podhmo 2026-11-24）**:

- pattern 検査（腕の型は `p : T` の `T` に縮小）+ type-switch emit +
  eval 対応 + spec 追記 + fixture
- 残問: subtype 重なりの静的検査（go/types oracle が要るか）、
  nil 腕の綴り — 採択時に lane 内で詰めるか open-questions へ残すか
- 採択されなければ wave41 以降へ延期

### C. E0513 の codes registry 登録（bug・規模: 小）

wave38 lane C の見落とし: `E0513`（`Go.nil` 非 nil-able 拒否）が
`internal/diag/codes.go` に未登録で `gom explain E0513` と
`docs/error-codes.md` が機能しない（implementation-gaps 記録済み）。
registry エントリ追加 + `go generate ./internal/diag` で解消。

### D. 1 フィールド record の `~` 制約受理（調査→decision・規模: 小〜中）

`type UserId = {v: Int}` は Go で `type UserId int64` の defined
type なので `~int64` を満たすはずだが、`boundaryTypeOf` が宣言型を
写せないため要素比較対象にならず E0508 のまま（implementation-gaps
記録済み、wave38 lane D レビューで露出）。`cmp.Ordered` 等を
named 1 フィールド record に効かせる面を調査 → decision。

### E. sum の `comparable` 実行時 panic 面（調査→decision・規模: 小〜中）

wave38 lane D の要素 fallback で `slices.Contains` に
`Go.Slice <ADT>` を渡せるようになったが、interface 値の `==` は
動的値が非 comparable（`List` ペイロード等）で実行時 panic。
Go と同じ意味論だが gom の宣言型が「比較可能」を見た目に依るのが
露出した — 制約側で reject するか、Go どおりとして残すか、
`goReprComparable` の受理範囲を絞るかの調査 → decision。

### F. `gom list` の外部 Gom パッケージ列挙（調査・規模: 中）

podhmo 2026-11-24:「標準ライブラリ以外もリストの対象にしたいかも
— 外部パッケージの参照ということができないかもしれないが」。
外部 module の `.gomi` 読み込みは `replace` 経由で既に動く
（`TestLoadExternalModulePackage`）— 欠けているのは列挙だけ。
「何を列挙対象にしうるか」（module cache・workspace・vendor ディ
レクトリ — vendor mode では loader が vendor を先に見るが、Go
workspace 下で参照する vendor は各 module 側であり workspace
root の vendor ではない、の区別を含む・replace 解決後の候補集合）
を sketch にまとめる調査 lane。

### G. `//gom:` directive の一覧表（doc・規模: 小）

directive が 5 系統に増え、各機能の節に散在している（podhmo
2026-11-24「一覧表ってあるんでしたっけ？」）。一覧表を置く:

- `//gom:allow`・`//gom:embed`・`//gom:noescape`・
  `//gom:result-notaint`・`//gom:mutable` の5件
- 列: directive・引数・付着位置（member 直前/ファイル内）・効果・
  関連エラーコード
- 置き場所: spec 付録（directive 索引節）or guide — spec 側に索引を
  置き各節からリンク、guide には要点のみ残す形が候補

## 延期候補

- D-3 `README.md` / D-4 tutorial go.mod 整理: ユーザー指定で
  プロジェクト終盤
- `gom fix` での `.gomi` 適合検査: 2026-11-05 に見送り済み
  （価値の具体化待ち）
