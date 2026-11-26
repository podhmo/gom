package iface

import "testing"

func TestScanDirectiveRequiresAttachment(t *testing.T) {
	src := []byte(`package m

//gom:noescape detached 0

fn detached (f: Int -> Int) -> Int

//gom:noescape attached 0
fn attached (f: Int -> Int) -> Int

/// docs and directives share one comment run.
//gom:noescape documented 0
fn documented (f: Int -> Int) -> Int

//gom:noescape import_only 0
import "x"

fn later (f: Int -> Int) -> Int
`)
	got := ScanNoescape(src)
	if len(got) != 2 || len(got["attached"]) != 1 || len(got["documented"]) != 1 {
		t.Fatalf("ScanNoescape = %#v, want only attached/documented", got)
	}
}

func TestScanDirectiveAttachesToAttributeLine(t *testing.T) {
	src := []byte(`package m

//gom:noescape wrapped 0
@extern fn wrapped (f: Int -> Int) -> Int
`)
	if got := ScanNoescape(src); len(got["wrapped"]) != 1 {
		t.Fatalf("ScanNoescape = %#v, want wrapped", got)
	}
}

func TestScanMutableRequiresAttachment(t *testing.T) {
	src := []byte(`package m

//gom:mutable Detached

type Detached

//gom:mutable Attached
type Attached
`)
	got := ScanMutable(src)
	if len(got) != 1 || got[0] != "Attached" {
		t.Fatalf("ScanMutable = %#v, want [Attached]", got)
	}
}

func TestScanDirectiveAcceptsTabAfterKeyword(t *testing.T) {
	src := []byte("package m\n\n//gom:noescape tabbed 0\nfn\ttabbed (f: Int -> Int) -> Int\n")
	if got := ScanNoescape(src); len(got["tabbed"]) != 1 {
		t.Fatalf("ScanNoescape = %#v, want tabbed", got)
	}
}

func TestScanAbstractRequiresTypeTarget(t *testing.T) {
	src := []byte(`package m

//gom:abstract Queue
fn unrelated () -> Int

type Queue
`)
	if got := scanAbstract(src); got["Queue"] {
		t.Fatalf("scanAbstract = %#v, want no cross-declaration claim", got)
	}
}
