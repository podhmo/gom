// Package goiface supplies named Go interfaces and channels for compiler fixtures.
package goiface

import "fmt"

type Empty interface{}

func Kind(value Empty) string       { return fmt.Sprintf("%T", value) }
func Channel() chan string          { return nil }
func ReceiveChannel() <-chan string { return nil }
func SendChannel() chan<- string    { return nil }

type Container struct{ Count int }

func NewContainer() *Container { return &Container{Count: 41} }
