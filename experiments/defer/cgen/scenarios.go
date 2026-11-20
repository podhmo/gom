// Hand-written: what a compiler-integrated scope would emit for
// dscope/scenarios.gom. When the scope body is the whole Gom function
// (or an IIFE for a nested scope) and every registration sits in the
// same Go function, each `s.Defer` becomes a plain Go `defer` at the
// registration site — inside the `if` too, because Go defers are
// function-scoped. Result-observing cleanups write the named result.
package main

import (
	"github.com/podhmo/gom/experiments/defer/fake"
	"github.com/podhmo/gom/stdlib/result"
)

func copyFile(log *fake.Log, src, dst, data string) (res result.Result[struct{}, error]) {
	r, err := fake.Open(log, src)
	if err != nil {
		return result.Err[struct{}, error](err)
	}
	defer func() { _ = r.Close() }()
	w, err := fake.Open(log, dst)
	if err != nil {
		return result.Err[struct{}, error](err)
	}
	defer func() {
		if cerr := w.Close(); cerr != nil && res.IsOk() {
			res = result.Err[struct{}, error](cerr)
		}
	}()
	if _, err := r.Read(); err != nil {
		return result.Err[struct{}, error](err)
	}
	return result.FromErr(w.Write(data))
}

func guarded(log *fake.Log, mu *fake.Mutex, need bool) struct{} {
	if need {
		mu.Lock()
		defer mu.Unlock()
	}
	log.Add("work")
	return struct{}{}
}

func transfer(log *fake.Log, q1, q2 string) (res result.Result[struct{}, error]) {
	tx := fake.Begin(log)
	ok := false
	defer func() {
		if !ok || !res.IsOk() {
			tx.Rollback()
		}
	}()
	if err := tx.Exec(q1); err != nil {
		ok = true
		return result.Err[struct{}, error](err)
	}
	if err := tx.Exec(q2); err != nil {
		ok = true
		return result.Err[struct{}, error](err)
	}
	res = result.FromErr(tx.Commit())
	ok = true
	return res
}

func eachFile(log *fake.Log, names []string) result.Result[struct{}, error] {
	for {
		if len(names) == 0 {
			return result.Ok[struct{}, error](struct{}{})
		}
		n := names[0]
		r := func() (res result.Result[struct{}, error]) {
			r, err := fake.Open(log, n)
			if err != nil {
				return result.Err[struct{}, error](err)
			}
			defer func() {
				if cerr := r.Close(); cerr != nil && res.IsOk() {
					res = result.Err[struct{}, error](cerr)
				}
			}()
			return result.FromErr(r.Write("x"))
		}()
		if !r.IsOk() {
			return r
		}
		names = names[1:]
	}
}

func boom(log *fake.Log) struct{} {
	defer log.Add("cleanup")
	fake.Boom(log)
	return struct{}{}
}

func main() {}
