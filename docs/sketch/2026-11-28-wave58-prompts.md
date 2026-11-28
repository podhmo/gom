# wave58 prompts 草案

状態: 草案（採否インタビュー未実施）

## lane A: 未 import Gom stdlib module candidate の実装

TODO。`undefinedTerm` の unresolved module name に限定し、`list`/`option`/`result`/`cmp`/`str` の exact match を import 忘れ、OSA 距離 1 を typo candidate として note/data に出す。index は `build.Options` → `resolve.Config` へ注入する。positive/negative/exact/typo/truncate、text/JSON fixture。safe fix・Go package 探索は対象外。

## lane B: builtin constructor shadowing metadata の調査

wave57 lane B review follow-up。local type shadowing 下で `Some` などの bare builtin constructor candidate をどう所有・遮蔽するか調査する。候補は非修飾名であり、`Option.Some` の qualified fallback 候補とは別に扱う。constructor table ownership、scope shadowing、candidate usability を fixture で比較し decision へ落とす。実装は lane に入れない。

## lane C: dependency `.gomi` diagnostic path の調査

module-relative 表示へ正規化するかを調査する。JSON consumer が実 path を必要とするか、fixture portability、text/JSON の表示責任を分ける。実装は lane に入れない。

## lane D: module/type fallback constructor candidate の調査

`Option.Some` 系の constructor fallback を E0201 typo 候補に混ぜるかを、利用頻度・false positive・所有関係で評価する。lane B と重なる ownership metadata は共有を検討する。実装は lane に入れない。

## lane E: call argument provenance の調査

E0401 provenance を呼出し引数へ広げるかを調査する。parameter span ownership、curried/flattened/labeled/deferred replay の index mapping を fixture 例で比較する。generic record field は別論点のまま。実装は lane に入れない。

## 候補外

- snippet 統合: wave56 lane D で見送り。頻出ノイズ実例が集まるまで戻さない
- message/details/note/fix title sanitization: source snippet とは別の attacker-controlled text 設計が必要
- import 追加 safe fix: lane A の後に別 decision
