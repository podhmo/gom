# n-ary Result pointer payload — 見送り判断の検証スケッチ

- 日付: 2026-11-25
- decision: [2026-11-25-nary-result-ptr-verbatim.md](../decisions/2026-11-25-nary-result-ptr-verbatim.md)

## 検証したこと

1. **後包み idiom の成立**: `os.CreateTemp`（verbatim 導出ビュー）に対し
   `let f = try os.CreateTemp ("", "probe")` → `Ptr.to_option f` →
   `match opt { Some _ -> ... None -> ... }` が型検査を通る
   （probe は検証後に削除 — spec fixture 化の対象は既存の規則であり
   新規の受理ではないため）。nil → None の読みは境界宣言と後包みで
   意味的に等価
2. **実例の存在**: selfish 移植の `expect_gist`（`Gists.Create` の
   `*github.Gist` を `Ptr.to_option` で包み `None -> Err`）がこの idiom の
   実用例 — [sketch](ja/2026-11-24-selfish-port.md) stage 4
3. **auto-wrap の fallout 見積もり**: `os.Open`/`os.Create`/`os.CreateTemp`
   など err==nil ⟹ non-nil 契約の多数派 API すべてが Option unwrap 強制に
   変わる — errwalk fixture だけでも `os.CreateTemp`・`os.Open` の2箇所が
   影響を受け、ユーザーコードの摩擦は payload nil が実害になる場面より
   圧倒的に多い

## 結論

verbatim 維持 + 後包み idiom が正。guide に payload 位置への適用例を追記済み。
