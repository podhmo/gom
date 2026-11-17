# Go struct 構築の必要性の再調査（wave-15 lane 3）

2026-11-17。wave-15 lane 3 の (1): gist-cli の 2 つの adapter
（`host.NewClient` / `host.StringField`）が本当に struct 構築面を要するかの
再調査。決定は [decision](../decisions/2026-11-17-struct-construction.md)。

## StringField — 構築面なしで書けた

`json.Unmarshal` / `Decoder.Decode` の第 2 引数は `any` — 7 章の `any`
ボックス化で `Ptr T`（`Ptr.new ()` で作る Gom の mutable cell、Go 側は
`*T` に近い受け皿）をそのまま渡せる。decode destination を「書き込む」のは
encoding/json 側なので、Gom 側の struct 書き込み面は不要だった。

prototype（probe モジュールで型検査 + 生成コードを Go テストで実行）:

```gom
fn string_field (document: String) (key: String) -> Result String GoError = {
  let object : Ptr (Go.Map String json.RawMessage) = Ptr.new ()
  let dec = json.NewDecoder (strings.NewReader document)
  try dec.Decode object
  let extra : Ptr Go.Any = Ptr.new ()
  match dec.Decode extra {
    Err e -> if errors.Is (e, io.EOF) { () } else { try Err e }
    Ok _ -> try Err (fmt.Errorf "response has trailing JSON data")
  }
  let raw = try (match Go.Map.get (Ptr.get object) key { ... })
  let bytes : Go.Slice Byte = Go.conv raw : Go.Slice Byte
  let value : Ptr String = Ptr.new ()
  match json.Unmarshal (bytes, value) { ... }
  ...
}
```

確認したケース（生成 Go を `go test` で実行、5 件全て通過）: 正常抽出、
trailing data 拒否、missing field、非 string 値（unmarshal error の wrap）、
空文字拒否。

罠として確認したもの:

- 型位置の Go named 型は `pkg.Type` と綴る（`Go json.RawMessage` は E0201）
- `json.RawMessage` は Go 1.27 の json v2 系で `jsontext.Value` に写像される
  — そのまま `map[string]jsontext.Value` 宛にデコードされ、`Go.conv raw :
  Go.Slice Byte` で byte 列に戻せる
- `Result` の `Err` に入れた GoError を `%w` に渡すと生成コードでは `any`
  になって go vet が落ちる — メッセージでは `%v` を使う

## NewClient — 構築面では届かない

`&http.Client{Timeout: 30 * time.Second, CheckRedirect: f}` の障害は
2 つとも構築面と無関係:

- `CheckRedirect` — Gom の関数値を Go の `func(*http.Request,
  []*http.Request) error` として渡す interop が未提供。func-typed field が
  1 つでもあると (a) 構文でも (b) field 書き込みでも埋まらない
- `Timeout` — `30 * time.Second` は Duration 算術。`time.Second` は int64
  の typed 定数だが Gom の Int 算術では `time.Duration` 型の値は生まれず、
  `Go.conv` にも Duration への数値経路がない

## 結論

構築面を要する検証済みの障害は 0 件。決定は (c) constructor-extern —
`host.NewClient` は removal conditions を更新して残し、`string_field` は
main.gom に移した。
