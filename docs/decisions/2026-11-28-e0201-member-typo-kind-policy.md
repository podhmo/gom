# E0201 member typo 候補の参照位置 kind policy

- 状態: 採用（wave57 lane C で実装済み）
- 日付: 2026-11-28

## 決定

module member typo の候補は、参照位置で使える member kind だけに絞る。

- value position: var/func/constructor/builtin。ただし `pkg.Type.Ctor` のように nested select の qualifier hop では type member も namespace として使えるため対象に含める
- type position: type のみ
- constructor pattern position: constructor のみ

`Go.Map` / `Go.Slice` の operation member table は expression position では value surface なので従来どおり候補を出す。constructor pattern では operation は constructor ではないため出さない。member table 自体の解決規則や wrong-kind use の診断は変えない。

## 理由

`fmt.Printlm` は value position では `Println` を勧めるのが正しいが、型注釈や constructor pattern では `Println` に直しても同じ位置では使えない。候補は fix ではないが、kind が合わない候補は利用者を次のエラーへ進めるだけなので、bare value typo と同じく候補集合を参照位置の意味に合わせる。

## 却下・後送り

- constructor fallback table まで候補源を広げること: 今回は行わない。`Option.Som` に `Some` を出すには module member table とは別の ctor table を候補源へ合成する必要があり、kind filter とは別の設計である
- builtin Option/Result constructor が local type redefinition で使えないケースの filter: resolver symbol に constructor ownership がなく lane C の範囲を超えるため、別 follow-up とする
