package format

// Inline repros of the deep-nesting exponential-layout bug pinned
// in knownGenFormatBugs — the exact bytes gomgen emits for each
// (mode, seed) pair, stashed here rather than under testdata/ so
// TestRepoFiles (which formats every .gom in the repo) cannot
// reach them. Regenerate with internal/gomgen:
//   gomgen.Generate(g, rand.New(rand.NewPCG(seed, 0x9e3779b9+seed)),
//                   gomgen.Options{Typed: true})

const genHangTyped62 = `package not
test"gen 823"{let count={not false
}
};
test"gen 378"{assert((fn d8 ->true)<|("line"))
}
fn pick22(item: Int)->Result Float String=if(true)||(false){pick22 item
}else{let s07=if true{let x=0e2
let n01=true
x
}else{()
let seed=if(item)==(item){()
item
}else{let y={let p4=30
todo ()
}
5_791
}
73.10
}
let _={let(v, x02)=if not false{let t9=if(false){()
let(b5, n)=match("done q")==("err happened"){true->todo ()
false->if false{let z9=match None{Some _->{"hello"
}
None->if([])==([]){()
let res04={let t5=(s07)==(s07)
try(Err s07)
}
"${(s07)+(s07)}"
}else{"${todo ()}line"
}
}
for value in[]{let(n, m)=try(if value{let c3=(item)+(item)
let t9=(item)%(71)
Ok(todo (),{let idx=s07
c3
})
}else{let rest=({let r9=try(try(Err 78))
for limit in[]{let ys=(item)+(0b10101)
for s03 in[]{for idx in[]{()
}
let limit=(s03)|>fn rest ->item
()
}
()
}
item
})/(0xff9)
Ok(if value{let item=(item)*(item)
let res03=(386.85)%(if(rest)==(44){let(x, seed)=try({Ok(todo (),if value{item
}else{0b100010
})
})
()
74e0
}else{()
let next=todo ()
s07
})
res03
}else{let acc=try(match None{d9 if value->Ok None
Some y5->Ok None
None->Ok None
})
s07
},-5)
})
()
}
((14e2)+(18.20),item)
}else{for x1 in[]{let w1=if(4_142)<(42){'Z'
}else{'0'
}
let value={()
let(n06, s5)=todo ()
item
}
()
}
(if true{s07
}else{s07
},item)
}
}
Some true
}else{None
}
let idx=Some("${item}back\\slash${true}")
(0x198,"${false}${s07}")
}else{let _=4_301
()
(56,"line abc back\\slash")
}
for value in[]{let _=[s07]
for base in[]{let data=item
()
}
()
}
v
}
Err"hello q some text"
}
`

const genHangTyped162 = `package next;let _=if true{let(res06, e0)=(fn y1 ->if y1{(573.48,if y1{544.29
}else{()
let item=(10)|>fn s08 ->s08
385.92
})
}else{((2.379409)+(883.81),538.44)
})<|(false)
let tmp=not true
("tab\there",{"err happened...n:"
})
}else{for xs in[]{()
}
let limit=false
({"x y z"
},{let count=if limit{let count=(todo ())!=(69)
for x0 in[]{let idx="...${if count{let y04=todo ()
92
}else{let x4=(x0)/(x0)
x4
}}line"
()
}
15
}else{let acc04={let total=if(49)==(67){let _=limit
(match None{Some acc->41
None->14
},false)
}else{()
let tmp=if(false)&&(limit){let data=not limit
let count=ignore((56e1)+(945.45))
389.19
}else{()
0e2
}
((20)%(0xd2c),(17)==(13))
}
todo ()
}
let(value, y07)={let tmp02=limit
if limit{let step=for d1 in[]{let acc08=acc04
let value={()
()
(acc04)-(acc04)
}
()
}
let e4=match not limit{true->acc04
false->acc04
}
((not tmp02)&&(limit),67.52e-2)
}else{((todo ())!=(acc04),{let data=if limit{let(m, k5)={let base={{let v=tmp02
for c7 in[]{let(q2, q8)=todo ()
()
()
}
({true
},if true{let a0=Some 809.81
"${(acc04)%(4_233)}q done"
}else{let e3=match(Ok 6e2){Ok rest->if tmp02{"tab\there ok quote\"in"
}else{"tab\there tab\there"
}
Err _->if tmp02{let y7={let step={match[]{[]->()
[seed, ..xs]->()
}
}
()
if v{let y=Some"${((fn ys ->20e0)<|(true))+(983.42)}"
Some 86e0
}else{for n07 in[]{()
}
()
None
}
}
"${todo ()}${7.7e-0}lit"
}else{"back\\slash"
}
}
()
e3
})
}
}
{()
(match None{Some _->acc04
None->acc04
},970.17)
}
}
let t2=false
'#'
}else{()
let ys=None
'b'
}
for s05 in[]{()
}
33e1
})
}
}
acc04
}
for idx in[]{let(u5, base)=(limit,limit)
let count=Some base
()
}
"${"hello..."}"
})
}
`

const genHangTyped168 = `package v388;
let(y1, acc)=if true{let acc08=(12)%(94)
({let ys=Some true
let value={let limit=not(true)
let r0=()
match None{Some res->[]
None->[]
}
}
false
},if(false)&&(false){let _=not false
()
69e1
}else{let y=("some text")|>fn k ->if not false{let p8=((true)|>fn limit ->{let base=acc08
let item=if limit{let count='#'
[limit]
}else{let n=todo ()
let ys={let(cur, res)=match limit{true->{let _=base
(acc08,acc08)
}
false->{({for k in[]{let step=if limit{let n=limit
k
}else{let res09=match(Ok acc08){Ok tmp->true
Err idx->limit
}
let limit=limit
"abc quote\"in"
}
()
}
acc08
},(base)-(2_806))
}
}
let v={let(x08, cur)=todo ()
let z=cur
{()
let count=k
x08
}
}
todo ()
}
[]
}
72.35e-0
})==(43e3)
(false,match p8{x if p8->4
true->acc08
false->55
})
}else{()
let t0=3.501299
({let t4=if true{'\n'
}else{' '
}
let res08=acc08
true
},0o30)
}
999.71
})
}else{let c1=(fn count ->Some(0o66))<|(todo ())
let y3=if([])!=([37.68e-3]){let y={let tmp=todo ()
tmp
}
let acc=(87.63)|>fn next ->(next)|>fn seed ->true
"${todo ()}"
}else{let r1=(if true{5.454319
}else{let c6=(630.89)>=(if not false{()
let data=not(if(true)==(false){let(c8, res03)=if(false){let d3=if(false)||(true){let(k, rest)=todo ()
Err 0x387
}else{for tmp01 in[]{let v=tmp01
let y=(todo ())/(1_960)
()
}
()
Ok 53
}
let m=match c1{Some _->match(Err"v="){idx if false->Ok 67
Ok e5->Ok 0o54
Err p8->Err 432.43
}
None->select{default->Err 428.33
}
}
((2_717)|>fn x02 ->true,79e0)
}else{()
(true,9.831155)
}
let p4=None
c8
}else{let n00=true
true
})
6.620991
}else{97.35e-1
})
()
38.20
})-(767.26)
for x03 in[]{let tmp=r1
()
}
"ok${61.9e-0}"
}
(true,256.00)
}
`

const genHangTyped172 = `package not

let acc=(6.587066)+(626.71)
;@test fn map5()->Option Int=match(not false){true->{for k in[]{let u0=({let(res09, e7)=todo ()
match res09{t9 if res09->[114.44]
true->[]
false->[]
}
})==((fn n ->(n)|>fn base ->[acc])<|(0x1a7))
()
}
None
}
false->((acc)==(183.05))|>fn x4 ->if(true)||(false){let y=not x4
let n=if y{let count=match(true){x07 if x4->if(83e2)>(121.22){let x09=if(true)&&(false){()
()
}else{let y06=if x07{let seed={for count in[]{let x08=if(x07){let count={let base=(match(Ok x4){Ok acc->count
Err count->15
})/(count)
if x07{()
for total in[]{let m=match(Ok"x y z${"back\\slash"}line"){step if(x07)||(false)->(count)+(count)
Ok y->base
Err _->base
}
()
()
}
0o2
}else{let c5="err happened"
base
}
}
622.26
}else{let k8=Ok count
19.62e-1
}
let d5=acc
()
}
let(d7, w7)={()
()
{(if false{let z2=match[]{[]->{()
[]
}
[v, ..x]->select{default->[]
}
}
let idx=y
57
}else{33
},acc)
}
}
not x4
}
let item="hello lit gom"
item
}else{let(k7, n09)=(fn n09 ->match[]{[]->({0o15
},acc)
[s1, ..xs]->(if x07{let v=71
2
}else{84
},select{default->857.58
})
})<|("back\\slash")
"lit back\\slash"
}
let z=todo ()
()
}
None
}else{None
}
true->{Some 75e2
}
false->None
}
[478.42]
}else{let ys=Some 0b1110
let(x4, value)=todo ()
[]
}
None
}else{None
}
};
;
`

const genHangTyped176 = `package not

import go"strings"
extern go"errors"{fn Node(b7: Int)(m: Bool)->Int
fn Node(count: Bool)->Int
};type Kind55=|Box0 Float(step: Bool)|Cell1 Int Int|Some22;fn mul77(x07: Int)->List Int=if((94e0)>(86.48e-0)){mul77 x07
}else{let z=if(3_721)>=(94){()
Some22
}else{let x4=if(false){let total=if not true{()
30e0
}else{let _=todo ()
let count=Box0 150.46~step:true
1.627196
}
let y02=(total)*(896.35)
"${{for e3 in[]{()
}
(0o10)+(x07)
}}a,b,c"
}else{let a4=40.35e-2
let x=if(true){x07
}else{let b2=if(x07)==(x07){for limit in[]{let res00=x07
()
}
88
}else{let acc03=x07
let step="back\\slash done line"
0xa59
}
let acc=todo ()
37
}
"done hello"
}
let m=if(Some22)==(Some22){let k2={let y={let x1=if(false)==(false){18.54e-3
}else{let res02=todo ()
978.80
}
not false
}
match None{Some n06->((n06)||(false),x07)
None->(todo (),(10)+(0xad1))
}
}
let total={let x02=Some22
let value=(458.80)|>fn tmp ->if(17.13e-1)>(2.441102){false
}else{()
false
}
if value{let d5=x4
(false,{let(res02, x08)={let w2=if value{let c2=value
({let rest={(4.91471)*(684.91)
}
rest
},878.21)
}else{()
let w0=((d5)|>fn cur ->x07)+(x07)
(965.28,-373.87)
}
let w5=if value{for rest in[]{()
}
5.183761
}else{let _=(36)*(x07)
()
7.894162
}
{let(tmp, k6)=(x4)|>fn x02 ->if value{let step=x07
((value)&&(value),todo ())
}else{({()
true
},{for y in[]{let y04='x'
()
()
}
for e3 in[]{let(y05, e1)=if(w5)!=(w5){for y in[]{()
let(step, y)=match Box0 418.47~step:true{Box0 _ seed->(d5)|>fn k ->(if false{let(d5, b4)=if e3{(0x6e7,not false)
}else{(x07,value)
}
0o76
}else{x07
},85)
Cell1 cur k2->(x07,99)
Some22->(5_741,24)
}
()
}
let _=x07
(x07,x02)
}else{()
let total=x07
(66,"a,b,c ok")
}
()
()
}
"q${value}"
})
}
let _=0x15f
(w5,k6)
}
}
res02
})
}else{for y09 in[]{let c3=None
()
}
(true,489.06)
}
}
Some22
}else{let item=0x889
Some22
}
Some22
}
[]
};fn show282(~n03: ())->Int=47;import go"strings"
`
