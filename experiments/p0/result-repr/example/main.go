// Command example exercises both Result representations the way a Go
// program consuming generated Gom packages would: it constructs
// values, dispatches on them, converts to and from (T, error), probes
// the invalid-value surface, and compares type sizes.
package main

import (
	"errors"
	"fmt"
	"strconv"
	"unsafe"

	"github.com/podhmo/gom/experiments/p0/result-repr/flat"
	"github.com/podhmo/gom/experiments/p0/result-repr/flat2"
	"github.com/podhmo/gom/experiments/p0/result-repr/goerr"
	"github.com/podhmo/gom/experiments/p0/result-repr/payload"
	"github.com/podhmo/gom/experiments/p0/result-repr/sealed"
)

// describeSealed dispatches on a sealed.Result like a Go consumer
// would: an ordinary type switch, the Go idiom for a closed set of
// types.
func describeSealed(r sealed.Result[int64, error]) string {
	switch r := r.(type) {
	case sealed.Result_Ok[int64, error]:
		return fmt.Sprintf("Ok %d", r.V0)
	case sealed.Result_Err[int64, error]:
		return fmt.Sprintf("Err %v", r.V0)
	default:
		return "<invalid>"
	}
}

// describeFlat does the same for flat.Result: a plain boolean check.
// There is no third arm — every bit pattern reads as Ok or Err.
func describeFlat(r flat.Result[int64, error]) string {
	if r.Ok {
		return fmt.Sprintf("Ok %d", r.Value)
	}
	return fmt.Sprintf("Err %v", r.Error)
}

// panicMessage runs f and reports what it panicked with.
func panicMessage(f func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprint(r)
		}
	}()
	f()
	return "no panic"
}

func main() {
	fmt.Println("== construction & match ==")

	// sealed: composite literals + type switch
	sOk := sealed.Result_Ok[int64, error]{V0: 42}
	sErr := sealed.Result_Err[int64, error]{V0: errors.New("nope")}
	fmt.Println("sealed:", describeSealed(sOk), "|", describeSealed(sErr))

	// flat: helpers or composite literals + bool check
	fOk := flat.Ok[int64, error](42)            // Ok(42) cannot infer E
	fErr := flat.Err[int64](errors.New("nope")) // Err(err) cannot infer T
	fmt.Println("flat:  ", describeFlat(fOk), "|", describeFlat(fErr))
	fLit := flat.Result[int64, error]{Value: 7, Ok: true}
	fmt.Println("flat literal:", describeFlat(fLit))

	fmt.Println("\n== (T, error) <-> Result ==")

	// Go -> Gom: generated code inlines this; helpers show the shape.
	v, err := sealed.ResultToGo(sealed.ParsePort("8080"))
	fmt.Println("sealed ParsePort -> (T,error):", v, err)
	v, err = flat.ResultToGo(flat.ParsePort("8080"))
	fmt.Println("flat   ParsePort -> (T,error):", v, err)

	// wrapped |?> error keeps errors.Is/As on the original error
	_, werr := sealed.ResultToGo(sealed.ParsePort("notanum"))
	fmt.Println("wrapped error:", werr)
	fmt.Println("  errors.Is(*strconv.NumError reachable):", errors.As(werr, new(*strconvNumError)))

	fmt.Println("\n== zero values & invalid states ==")

	var sNil sealed.Result[int64, error] // nil interface: THE invalid value
	fmt.Println("sealed  var r (nil)     :", describeSealed(sNil),
		"| GetOrElse panics:", panicMessage(func() { sealed.GetOrElse(sNil, 9) }))
	var sTypedNil sealed.Result[int64, error] = (*sealed.Result_Ok[int64, error])(nil)
	fmt.Println("sealed  (*Ok)(nil)      :", describeSealed(sTypedNil),
		"| panics:", panicMessage(func() { sealed.GetOrElse(sTypedNil, 9) }))

	var fZero flat.Result[int64, error] // {0, nil, false}: reads as Err(nil)
	fmt.Println("flat    var r (zero)    :", describeFlat(fZero),
		"| GetOrElse:", flat.GetOrElse(fZero, 9), "(silent, no panic)")
	v0, e0 := flat.ResultToGo(fZero)
	fmt.Println("flat    ToGo(zero)      :", v0, e0, "(silent success! zero T, nil error)")
	fCorrupt := flat.Result[int64, error]{Value: 1, Ok: true, Error: errors.New("stray")}
	fmt.Println("flat    Ok+Error set    :", describeFlat(fCorrupt), "(unrejectable)")

	var f2Zero flat2.Result[int64, error] // tag 0: invalid, like nil for sealed
	fmt.Println("flat2   var r (zero)    : tag =", f2Zero.Tag(),
		"| panics:", panicMessage(func() { flat2.GetOrElse(f2Zero, 9) }))
	f2Lit := flat2.Result[int64, error]{Value: 1} // tag unsettable outside pkg
	fmt.Println("flat2   literal no tag  :", panicMessage(func() { flat2.ResultToGo(f2Lit) }))
	fmt.Println("flat2   Ok ctor + Unwrap:", func() string {
		v, e := flat2.Ok[int64, error](42).Unwrap()
		return fmt.Sprintf("%d %v", v, e)
	}())

	fmt.Println("\n== %w wrapping on Err(nil) ==")
	wrappedNil := fmt.Errorf("load_config: %w", fZero.Error)
	fmt.Printf("flat zero through wrap: %q (non-nil=%v)\n", wrappedNil.Error(), wrappedNil != nil)

	fmt.Println("\n== type sizes (bytes) ==")
	fmt.Printf("sealed.Result[int64,error] iface : %d (payload boxed)\n", unsafe.Sizeof(sealed.Result[int64, error](nil)))
	fmt.Printf("flat.Result[int64,error] struct  : %d\n", unsafe.Sizeof(flat.Result[int64, error]{}))
	fmt.Printf("sealed.Result[Big,error] iface   : %d (payload boxed once)\n", unsafe.Sizeof(sealed.Result[payload.Big, error](nil)))
	fmt.Printf("flat.Result[Big,error] struct    : %d (payload inline)\n", unsafe.Sizeof(flat.Result[payload.Big, error]{}))

	fmt.Println("\n== E at the boundary: error vs GoError ==")
	var e error = errors.New("x")
	var g goerr.GoErrorI = e // implicit both ways; but Result[T,GoErrorI] != Result[T,error]
	var back error = g
	fmt.Println("error <-> GoErrorI implicit assign:", back == e)
	fmt.Println("Result[int,GoErrorA] (alias) IS Result[int,error]")
	fmt.Println("Result[int,GoErrorI] (named iface) is a DIFFERENT type (invariant type args)")
}

type strconvNumError = strconv.NumError
