# `gom list -external` — vendor mode の列挙

- 状態: 採用（wave42 lane D で実装）
- 日付: 2026-11-24

Supersedes: [2026-11-24-gom-list-external.md](2026-11-24-gom-list-external.md)
の「vendor 列挙は範囲外で future work」という範囲指定を、wave42 の
採否インタビューで取り込んだ。

## 決定

vendor mode が有効（vendor ディレクトリがあり、GOFLAGS に vendor
以外の `-mod` が無い）なら、`<modRoot>/vendor` 配下も walk し、
`.gomi` がちょうど 1 つのディレクトリを「vendor/ からの相対パス」
（＝vendored パス）で列挙する。`resolvesTo` の検査は掛けない —
vendored 所在自体が dirOf の vendor 優先解決と同じ解決の証である。
build list 側の列挙と重複する import パスは 1 件に潰す。

あわせて:

- vendor mode 下で `go list -m all` が失敗（offline 等）しても
  vendored 集合は列挙する — vendored パッケージは module graph を
  必要としない。
- vendoring 判定の GOFLAGS は `go env GOFLAGS` を読む — `go env -w`
  で永続化された `-mod` 値も go コマンドと同じ効力を持つ。

参照: [実装](../../internal/gomimport/loader.go)、
[テスト](../../cmd/gom/cli_test.go)（`TestListExternalVendor`）
