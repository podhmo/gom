# 非 cons 蓄積 quadratic の救済方針: β 誘導 + γ 警告 lint、α/δ は先送り

- 状態: adopted（podhmo 採択 2026-11-19 —「検出の実装を実験してみるというのはあっても良いかもな。warningとか出ると良い。迂闊でもマシになるコードを目指したい」「コンパイル時じゃなくても良い。vet的なものでも良い」「実装が手軽な方で良い。実装箇所に制限は設けない」「α は今は未対応で良いかな」）
- 日付: 2026-11-19
- 関連: [problems](../problems/2026-11-05-stdlib-cons-built-combinators-quadratic.md)、[sketch](../sketch/2026-11-19-wave23-noncons-accum.md)、[cons-recursion 特殊化](2026-11-18-cons-recursion-specialization.md)

## 背景

cons-recursion 特殊化が救わない 4 形（連鎖 append、fold 内 concat、
相互再帰、呼び出し結果 2 度読み）が n=8000 で被覆形の ~600–900 倍と
実測された（wave23 lane4）。救済候補 α–δ の採否が問われていた。

## 決定

- **β（Array イディオムへの誘導）を採用する**。言語は変えず、
  「cons でない蓄積は O(n²)。`Array` builder + `to_list` で書く」
  イディオムを gom-agent-guide に示す（wave23 締めで投入済み）
- **γ（診断ヒント）を採用する** — vet 的な警告 lint として
  実装。`lower` の Core IR pass が特殊化を外れた蓄積形を検出し、
  **W0801**（warning severity、PhaseLint）を出す。失敗にはしない。
  検出は「手軽な実装箇所」として lower を選んだ（consLeaf が
  弾く除外形と同じ IR を眺めるので一致が取りやすい）
- **α（SCC 拡張）は今は未対応**。相互再帰を 1 ループに畳む制御構造の
  実装コストに対し救えるのが Mutual 形だけなので効果対コストが悪い。
  「可能性を潰したくはない」ため、cons 特殊化の将来拡張余地として
  problems ファイルに残す
- **δ（List 表現変更）は problems に残す**。対象外形を構造的に
  全部救える唯一の案だが、P0-3 で実測のうえ選んだスライス表現の
  再設計に連鎖する。実害が具体化した時の再検討候補

## 根拠

- β は既存の `Array` builder + `to_list`（wave18 採択）に乗るだけで
  実装ゼロ。γ は変換なし検出だけなので (α) より小さく、沈黙の
  取りこぼしを可視化できる — 「迂闊でもマシになるコードを目指したい」
  に一致
- γ の実装位置は実験で確定: `internal/lower/conswarn.go`。
  `lintQuadraticAccum` がファイルローカル呼び出しグラフ上の
  再帰集合（自己ループ + 多関数サイクル）を求め、
  (A) 再帰呼び出し結果を `..rest` に置く cons 末尾、
  (B) 再帰呼び出し結果への `List.append`/`concat`、
  (C) accumulator に append する folder を持つ `List.fold`/`foldl`
  の 3 形を警告する。`experiments/w23/noncons-accum` の 4 除外形すべてに
  発火し、被覆形（cons_map）と testdata 全件には無発火を確認済み
- 警告は `gom build`/`test`/`run` で出る（lower は `gom check`
  では走らない — vet 的位置づけとして許容）

## 残件

- α の技術確認（相互再帰の真の 1 ループ化）は未実施のまま
- W0801 の誤検出余地: 入力が小さい用途は O(n²) でも無害 — 集まったら
  suppression 機構か検出絞り込みを別 decision で
- δ の再検討トリガは「β+γ でも残る痛みの具体例」
