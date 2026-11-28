# wave57 lane C: member typo candidate kind filter 検証

- 日付: 2026-11-28
- lane: C（実装）
- 対応 decision: [../decisions/2026-11-28-e0201-member-typo-kind-policy.md](../decisions/2026-11-28-e0201-member-typo-kind-policy.md)

## 実施内容

member typo candidate を参照位置の kind policy で絞った。value position は var/func/constructor/builtin、type position は type、constructor pattern position は constructor。`pkg.Type.Ctor` の nested qualifier hop では type member も namespace として使えるため value policyへ加える。`Go.Map` / `Go.Slice` operation table は expression position では従来どおり候補を出す。

## red-first

fixture 追加後、実装前の `go test ./internal/build -run 'TestSpec/E0201_member_typo_(type_negative|pattern_negative)' -count=1` は、`fmt.Printlm` に対して使えない `Println` candidate が出るため red だった。実装後は同じテストが pass した。

## fixture

- `E0201_member_typo_type_negative`: type annotation の `fmt.Printlm` に func `Println` を出さない
- `E0201_member_typo_type_positive`: type annotation の `fmt.Stringe` に type `Stringer` を出す
- `E0201_member_typo_pattern_negative`: constructor pattern の `fmt.Printlm` に func `Println` を出さない
- `E0201_member_typo_pattern_positive`: constructor pattern の `foreignpkg.Circl` に constructor `Circle` を出す
- `E0201_go_package_member_typo`: 通常 value position の `fmt.Printlm` → `Println` 非退行
- `E0201_go_map_member_typo` / `E0201_go_slice_member_typo`: `Go` operation table の候補非退行
- `TestMemberTypoCandidateKindPolicy`: value/type/constructor/nested qualifier hop の kind policy unit regression

## 文書同期

- `docs/spec.md` §12: member candidate の参照位置 kind を更新
- `docs/design.md` 2.3: resolver の context-aware kind policy を更新
- `internal/diag/codes.go` / `docs/error-codes.md`: E0201 explanation を更新

## 残件

constructor fallback table を候補源へ合成するかは未実施。builtin Option/Result constructor が local type redefinition で使えないケースの filter は resolver symbol に ownership 情報がなく、lane B review から escalate された別 follow-up。
