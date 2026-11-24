# wave41 prompts（採択済み）

- 日付: 2026-11-24
- 前提: wave40 スタック（#187–#194、stack 189）が main にマージ済みの
  新 main から開始。単一エージェント運用・Devin Review が品質ゲート。
  lane 実施はマージ後に開始する。
- 採否: A/B/C 採択（lane 実行へ）。vendor 関連はすべて future work —
  lane D（workspace vendoring）は lane 化せず open-questions の Future
  work へ、lane C の vendor 列挙部分（module 側 vendor walk）も同様に
  future work 扱いで C は main module + build list の列挙のみ実施する。

## 候補

### A. 1 フィールド record の `~` 制約受理（decision 採択→実装・規模: 小〜中）

wave40 lane D の decision draft（[PR #191]）が「`type UserId = {v: Int}` が
`~int64` を満たすはずだが E0508 のまま」という実在の gap に対し、checker 側で
`~` 項のみ underlying へ peel する案を推薦。**decision 自体の採択確認を兼ねる** —
採択なら `goSatisfiesGomElems` 周辺に peel を実装 + spec/fixture。

### B. sum/Option の comparable 狭め + `Go.FuncN` gap（decision 採択→実装・規模: 中）

wave40 lane E の decision draft（[PR #192]）: `slices.Contains` に
`Go.Slice Shape` が渡せるようになった副作用で、payload が非 comparable
な sum が interface `==` で実行時 panic する。推薦は `goReprComparable` を
kindSum（全 variant フィールド comparable）と Option（要素 comparable）で
payload 検査に狭める。ついでに同 decision が発見した **`Go.FuncN` が
comparable を誤って許容する gap** も同じ整理で直す。**decision 採択確認を兼ねる。**

### C. `gom list` 外部列挙の実装（sketch→実装・規模: 中）

wave40 lane F の sketch（[PR #193]）: 候補集合 = main module walk +
`go list -m all` の Dir walk（post-replace・workspace 含む、Dir 空は
skip）+ vendorEnabled 時は module 側 vendor walk。`oneGomi` でフィルタ。
default の stdlib 出力は保ち、外部列挙は opt-in フラグ
（`gom list -external` 案）でコスト明示。

### D. workspace vendoring（`go work vendor`）対応（調査・規模: 小〜中）

lane F が発見した gap: Go 1.22+ の workspace vendoring は
`<workroot>/vendor` を使うが loader は module 側 `vendor/` のみ見る。
`go.work` の検出と vendor 解決・列挙への影響を調査し、必要なら decision。

### E. なし（wave40 の残件処理のみ）
