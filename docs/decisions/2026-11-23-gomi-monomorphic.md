# `.gomi` が実装より単相の場合の許容

- 状態: draft
- 日付: 2026-11-23
- 問い: 実装が多相（`id : a -> a`）で `.gomi` が単相
  （`id : Int -> Int`）のとき、conformance を通すか —
  open-questions 型システム節の未決（wave33 lane E の調査）

## 現状

`iface.compare.go` の fn/let メンバ比較は `alphaEqual` — 型変数
名の違い以外を許さない厳格一致（spec 7.6）。多相実装 × 単相
インターフェースは E0603。type メンバはパラメータ数一致が
前提なので影響範囲は fn/let 署名の比較だけ。

## 意味の分析

許容は sound: `a -> a` の値は `Int -> Int` の契約を満たす。
判定は「interface 型が impl 型の instance か」— 実装の型変数を
fresh rigid に束縛する instance マッチで表せる:

- impl `a -> a` × iface `Int -> Int` → instance あり → 許す向き
- impl `a -> a` × iface `b -> Int` → instance なし（`b` は変数の
  ままで `Int` に確定しない）→ 拒否。許すと `.gomi` が戻り型を
  偽る（`f "x"` が String を返す）ので正しい
- impl `Int -> Int` × iface `a -> a` → instance なし → 拒否
  （現状どおり）

## 選択肢

### 案 A: instance チェックに緩める（保留候補）

fn/let 比較を `alphaEqual` から「iface ∈ inst(impl)」に置き換える
だけ — 機械的コストは小（compare.go 局所）。利用価値は
「意図的な API 狭化」— ただし生成 Go は相変わらずジェネリック
に emit されるので、狭化は Gom 側の import 面にしか効かず、
Go から見れば公開面は変わらない。見せかけの隠蔽になりやすい。

### 案 B: 厳格一致のまま（採択候補）

OCaml/SML の signature matching と同じく厳格のまま。狭めたい
契約は明示の単相 wrapper（`let id_int : Int -> Int = id`）で
書く — 意図がコードに残り、`.gomi` と実装の読み合わせも素直。
`.gomi` は「公開契約そのもの」であり、実装より強い主張も弱い
主張もしない方が読み間違いが少ない。

## 制限・補足

- 採択が案 A に変わる場合、spec 7.6 の「型変数名以外の一致」
  記述を instance 方向に更新する必要がある
- `let` メンバの多相化（generalized value-let）と絡むと
  impl 側 sig がどこまで多相に見えるかは loader の `sigs`
  登録に依存する（wave33 lane A で let メンバにも sig を
  載せたので instance 比較の材料は揃っている）
