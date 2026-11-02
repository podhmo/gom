// Package voidhost provides observable zero-result calls for compiler tests.
package voidhost

import (
	"fmt"
	"io"
)

func Mark(text string) { fmt.Print(text) }

type Name string

func Named(value Name) { fmt.Print(value) }
func Slice(value []string) {
	if value != nil {
		panic("wanted nil slice")
	}
	fmt.Print("s")
}
func Interface(value io.Writer) {
	if value != nil {
		panic("wanted nil interface")
	}
	fmt.Print("w")
}

type Probe string

func NewProbe() Probe             { return "m" }
func (p Probe) Touch(text string) { fmt.Print(string(p) + text) }
func UnitValue() struct{}         { return struct{}{} }
