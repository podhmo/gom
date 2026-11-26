# wave48 lane D: `.gomi` metadata fidelity 検証

## 実施

- directive attachment: 空行で切れた `//gom:noescape` / `//gom:mutable` は読まず、
  宣言直上と `///` doc を含む連続 run、および `@extern` などの attribute 行に
  付着する run だけを読むことを `internal/iface` の scan test で確認
- mutable loader fixture を spec の attachment 規則へ合わせ、ブロックコメント内の
  directive が効かない既存境界を維持
- `//gom:abstract Queue` を付けた abstract `type Queue a` が、`gom info` 再生成後も
  実体 `QNil` / `QCons` を出さず directive ごと残ることを `TestInfoCarriesAbstractDirective` で確認
- `//gom:private type Secret = { x : Int }` と `//gom:private fn helper ...` が
  再生成後もメンバを公開面から外したまま残ること、引数名を変えたシグネチャでは
  private claim が stale となりメンバが公開面へ戻ることを `TestInfoCarriesPrivateClaims` で確認

## 境界

- private directive は隠れた宣言そのものに付着できないため、ファイルレベルの
  例外 directive とした。宣言本文を directive に保持するのは canonical signature
  不変条件を機械判定するためで、人が手で書きやすさより `gom info` が一意に
  引き継げることを優先した
- 複数名を一度に束ねる `let` の private 判定は個別メンバへ分解せず、この lane
  では単一名束縛だけを対象にした
