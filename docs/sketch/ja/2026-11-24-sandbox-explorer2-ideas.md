> 転記元: [PR #196](https://github.com/podhmo/gom/pull/196) の `sandbox/ideas.md`（sandbox 探索 run 2）

# 普段 Go で作るちょっとしたツール集（run 2 — gom のことは考えない）

前回 (PR #163) は行処理・base64・JSON・ファイルサイズ・HTTP・テンプレ・プロセス起動。
今回は別の角度: CSV・日時・ネットワーク・集計・リネーム・表出力・乱数。

## 1. CSV 列選択 (`csvcut`)

`csvcut -f 1,3 file.csv` で指定列だけ抜き出す。ヘッダ名指定もしたい。

```go
// encoding/csv の Reader/Writer。fields の split と列 index の解決だけ。
r := csv.NewReader(in); w := csv.NewWriter(out)
for { rec, _ := r.Read(); pick(rec); w.Write(out) }
```

## 2. 日付カウントダウン (`until`)

`until 2026-12-31` で「あと N 日」を出す。複数日付、`-q` で数字だけ。

```go
// time.Parse("2006-01-02", s) + time.Until / Sub。Date だけの計算は
// 時刻をまたぐ端数処理（Truncate(24h) か日付同士の diff）が面倒ポイント。
```

## 3. TCP ポートスキャナ (`pscan`)

`pscan example.com 1-1024` で開いてるポートを出す。並列にしないと遅い。

```go
// net.DialTimeout("tcp", host:port, 200ms) を goroutine で fan-out。
// 結果を channel で集めるのが Go 流。
```

## 4. 単語頻度カウンタ (`freq`)

`freq file.txt` で単語→出現回数を降順表示。`top` オプション。

```go
// bufio.Scanner(ScanWords) + map[string]int + sort.Slice。
// map の反復と集計ソートが本題。
```

## 5. 正規表現バッチリネーム (`rmv`)

`rmv '(\d+)' 'img_$1' *.jpg` で rename の dry-run。`-x` で実行。

```go
// regexp.ReplaceAllString で新名を計算、os.Rename。dry-run が default。
```

## 6. テーブル成形 (`tabl`)

TSV/CSV を読んで `text/tabwriter` で桁揃え表示。

```go
// csv.Reader(Comma='\t') → tabwriter.Writer で Fprintln。
// Go の tabwriter は `w.Write` → `Flush` の2段。
```

## 7. パスワード生成 (`passgen`)

`passgen -n 20` で英数記号からランダム文字列。

```go
// crypto/rand.Int or rand.Read で文字集合から拾う。
// math/rand じゃなく crypto/rand を使うのが作法。
```

## 8. JSONL → CSV (`jsonl2csv`)

1行1JSON のファイルを CSV に変換。キー集合をヘッダに。

```go
// bufio.Scanner + json.Unmarshal(map[string]any) でキー集め → csv.Writer。
// any の中身の表示が面倒ポイント（float64 が 1e+06 になる等）。
```
