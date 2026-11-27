# 継続行らしき字下げ行の誤パース診断

- 状態: 採用（調査済み・実装は未実施）
- 日付: 2026-11-27

## 決定

複数行カリー適用の誤パースについては、専用診断へ連鎖を畳み込まず、**既存 E0402 へ継続行疑いの note を追加する**方針を採用する。NL-2 の文法変更、字下げだけで括る自動 fix、独立式の意味を変える rewrite は行わない。

最初の mid-block 式が関数型で、次の block 式が後続行かつ深い字下げにある場合にだけ、E0402 へ次の note を付ける。

```text
if these lines were meant as one application, use |> or wrap the whole application in parentheses
```

note は人間向けの案内であり、fix は追加しない。既存の `ignore` fix 規則は変えない。

## 理由

- 現状は `List.fold` を字下げで複数行へ書くと、先頭の E0402 に加えて E0407/E0402/E0401 が連鎖し、本因である「改行で式が切れた」が見えない。最初の E0402 は連鎖の先頭に出るため、そこへ pipe/parens の案内を付けるだけで次の行動は分かる
- 専用診断へ畳み込むには、block 内の独立式と継続行を字下げから推測し、連鎖診断を suppression する仕組みが要る。どこまでが同じ適用か、末尾式の型エラーを残すか、独立したエラーを隠さないかの境界が複雑で、false positive の cost が高い
- 既存診断への hint は診断を抑制しないため、独立式へ誤って付いてもコンパイル結果や他診断を変えない。影響は E0402 をすでに出している erroneous input に限られる
- 現状維持は `ignore` fix だけが見えるため、ユーザーが意図した適用継続とは逆方向へ誘導しやすい

## 実装候補の境界

- 判定は block 内の mid-block statement に限る。ファイルスコープの E0101、match/if arm 内でない式、末尾式だけの E0401 には広げない
- 先頭 statement の型が `types.Func` のときだけ候補にする。generic な部分適用で内部に meta を含んでいても、外側が関数型ならよい
- 次の statement が直後の行にある必要はないが、同一 block 内で後続行かつ先頭 statement より深い列にあることだけを見る。字下げ幅や空行・コメントの解釈は実装時に fixture へ固定する
- 診断 suppression はしない。E0407/E0402/E0401 の連鎖自体は残り、最初の E0402 の note が本因への導線になる
- hint から括弧で括る fix を出さない。indentation は意味を持たないため、継続行と断定した rewrite は unsafe である

## red-first fixture 方針

1. `List.fold` を字下げ 4 行へ分けた fixtureを追加し、先頭 E0402 に上記 note があることを先に固定する。実装前は note がなく red になる
2. 先頭が単なる値や Unit 式の場合には note がない negative fixture を足す
3. 同じ深さの独立した mid-block 式では note がない negative fixture を足す
4. 実装後に text output と JSON output の両方で note が読めることを確認する

検証記録: [sketch/2026-11-27-wave54-lane-f-continuation-diag-verify](../sketch/2026-11-27-wave54-lane-f-continuation-diag-verify.md)。
