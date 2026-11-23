# 普段 Go で作るちょっとしたツール集（gom のことは考えない）

雑に思いつく、手元で欲しくなるコマンドラインツール。Go で書くならこんな感じ、という
雑な実装イメージ付き。難易度の順に並べたつもり。

## 1. wc クローン (`wcl`)

標準入力かファイルを読んで、行数・単語数・バイト数を出す。

```go
// flag で -l -w -c を受けて、bufio.Scanner で行を回すだけ。
// 単語は strings.Fields で数える。
r := os.Stdin // or os.Open(path)
sc := bufio.NewScanner(r)
lines, words, bytes := 0, 0, 0
for sc.Scan() {
    lines++
    bytes += len(sc.Bytes()) + 1
    words += len(strings.Fields(sc.Text()))
}
```

## 2. base64 エンコーダ/デコーダ (`b64`)

`b64 foo.txt` でエンコード、`b64 -d` でデコード。stdin 対応。

```go
// io.ReadAll + encoding/base64。短い。
data, _ := io.ReadAll(in)
if dec { out, err = base64.StdEncoding.DecodeString(string(data)) }
else   { out = []byte(base64.StdEncoding.EncodeToString(data)) }
```

## 3. 行フィルタ (`pick`)

`pick <substr>` で substring マッチした行だけ通す簡易 grep。
`-v` で反転、`-n` で行番号付き。

```go
// bufio.Scanner + strings.Contains。case-insensitive オプションも欲しい。
```

## 4. JSON フィールド抽出 (`jqもどき`)

`jqp .users[0].name` みたいな path で値を取り出す。

```go
// encoding/json で any に decode して、path をトークン化して辿る。
// map[string]any / []any の型アサーションをひたすら書く。
// エラーメッセージをていねいに出すところが面倒ポイント。
```

## 5. メモ追記ツール (`jot`)

`jot 牛乳を買う` で `~/jot.log` にタイムスタンプ付きで追記。
`jot -l` で一覧、`jot -l 10` で最新10件。

```go
// os.OpenFile(O_APPEND|O_CREATE|O_WRONLY) + time.Now().Format
// args の join は strings.Join(os.Args[1:], " ")
```

## 6. sha256sum クローン (`s256`)

`s256 file1 file2 ...` で `hash  filename` を出力。

```go
// crypto/sha256 + io.Copy。Go では1行レベル。
h := sha256.New()
io.Copy(h, f)
fmt.Printf("%x  %s\n", h.Sum(nil), path)
```

## 7. ディレクトリサイズ集計 (`duu`)

`duu dir` で直下のエントリごとのサイズを大きい順に表示。

```go
// filepath.WalkDir でサイズ集計、sort.Slice で降順、人間可読フォーマット。
// シンボリックリンクの扱いとか権限エラーの握り方が気になるところ。
```

## 8. HTTP ステータスチェッカ (`hs`)

`hs https://example.com ...` で複数URLの status と応答時間を表示。
余裕があれば goroutine で並列化。

```go
// net/http + time.Since。並列化するなら sync.WaitGroup か channel。
// Go だと楽だけど gom では難しい気配がする（goroutine/channel 相当があるか）。
```

---

実装の優先度：1→3 が「確実にいけるはず」、4→6 が「たぶんいける」、7→8 が「怪しい」。
