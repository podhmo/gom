# agent skill の inventory はツール問い合わせへ寄せる

- 状態: 採用
- 日付: 2026-11-27

## Problem

`skills/gom-agent-guide/SKILL.md` は stdlib module の代表的な member 名や short name 一覧を静的に転写していた。これらは compiler / stdlib の変更で最初に腐る情報で、agent が guide の記憶だけを頼りにすると存在しない member を書く。一方で、data-last convention、`Array` freeze 後の書き込み禁止、`Ref` の non-nil 性のような規範・落とし穴はツール出力からは読み取りにくく、guide が担う必要がある。

## Proposal

`SKILL.md` では stdlib inventory を転写せず、`gom list` で importable short name を調べ、`gom info <short-name>` で現在の `.gomi` member/directive/doc を確認する workflow を示す。`gom list` の出力例となる具体的 short name 一覧も guide へは書かない。

規範と落とし穴は guide に残す。ただし `SKILL.md` 本体には library-wide の data-last convention だけを置き、compiler-registered で `gom info` に出ない `View`/`Array`/`Ref`/`Chan`/`Task` の詳細は `references/` 側へ置く。`List.map` のような member 名は規範を示す最小限の例としてだけ使い、inventory として並べない。

## Verification

- `gom list` が importable stdlib short name を列挙すること
- `gom info list` が現在の `stdlib/list/list.gomi` を読み出すこと
- 検証レポート: [wave51 lane F verification](../sketch/2026-11-27-wave51-lane-f-agent-skill-verify.md)
