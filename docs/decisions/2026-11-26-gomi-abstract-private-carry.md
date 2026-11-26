# `.gomi` の abstract / private 意図は directive で再生成に引き継ぐ

- 状態: 採用
- 日付: 2026-11-26

## 文脈

`gom info` の draft は全メンバを公開し、型の実体も透明に出す。著者はそこから
不要なメンバを消したり、`type T` の抽象宣言へ絞ったりして公開面を作るが、
次の再生成ではその意図が失われ、private メンバと型の実体が再び現れた。
コメントである `.gomi` directive に意図を残せば、公開面の構文そのものを
変えずに再生成へ伝えられる。

## 決定

- メンバ directive の読み取りは spec の付着規則に合わせる。`//gom:mutable`、
  `//gom:noescape`、`//gom:result-notaint` は、宣言またはその attribute 行の
  直上に連続するコメント行 run の中にあるものだけを読む。空行・非コメント行・
  ブロックコメントで run が切れた directive は無効
- abstract 化は `//gom:abstract <name>` を抽象型宣言へ付着させて表す。
  `gom info` は、同名で型パラメータ数の同じ実装型を再生成するとき、実体の
  代わりに `type Name ...` の抽象宣言を書き、directive を引き継ぐ。型
  パラメータ数が変わったら stale な抽象意図として落とし、実体を出す
- private 化はファイルレベルの `//gom:private <declaration>` で表す。
  隠したい宣言が `.gomi` に存在しないため付着先を持てないので、例外的に
  付着規則は適用しない。`<declaration>` は隠す対象の `.gomi` 宣言本文で、
  `gom info` は再生成した同名メンバの canonical な宣言文言が一致するときだけ
  そのメンバを省き、directive を引き継ぐ。シグネチャが変われば公開面に
  戻し、directive も落とす
- どちらの引き継ぎも、既存の escape / mutable directive と同じく、同じ
  package directory の prior `.gomi` だけを種にする

## 却下した案

- prior `.gomi` にないメンバを無条件に private と推測する → 再生成時に
  新規メンバと既存の非公開メンバを区別できない
- `//gom:private <name>` の名前だけにする → canonical signature 不変条件を
  判定できず、実装シグネチャを変えても stale な非公開意図が残る
- abstract を directive なしで推測する → 実体を一度公開したい型まで、prior が
  たまたま abstract だったという理由だけで隠し続ける

## 検証

prototype 記録: [wave48 lane D .gomi metadata verify](../sketch/2026-11-26-wave48-lane-d-gomi-metadata-verify.md)。
