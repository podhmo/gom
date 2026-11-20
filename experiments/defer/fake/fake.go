// Package fake provides in-memory resources that record every acquire
// and release into a Log, so each experiment can assert the cleanup
// order without touching the file system.
package fake

import (
	"errors"
	"strings"
)

type Log struct{ Events []string }

func NewLog() *Log          { return &Log{} }
func (l *Log) Add(s string) { l.Events = append(l.Events, s) }
func (l *Log) String() string {
	return strings.Join(l.Events, " ")
}

// Res is a file-like resource. Open fails for names starting with
// "missing"; Close fails for names starting with "failclose"; Write
// fails for the data "bad".
type Res struct {
	log  *Log
	name string
}

func Open(l *Log, name string) (*Res, error) {
	if strings.HasPrefix(name, "missing") {
		l.Add("open-fail:" + name)
		return nil, errors.New("open " + name + ": not found")
	}
	l.Add("open:" + name)
	return &Res{log: l, name: name}, nil
}

func (r *Res) Read() (string, error) { r.log.Add("read:" + r.name); return "data", nil }

func (r *Res) Write(s string) error {
	if s == "bad" {
		r.log.Add("write-fail:" + r.name)
		return errors.New("write " + r.name + ": bad data")
	}
	r.log.Add("write:" + r.name)
	return nil
}

func (r *Res) Close() error {
	r.log.Add("close:" + r.name)
	if strings.HasPrefix(r.name, "failclose") {
		return errors.New("close " + r.name + ": flush failed")
	}
	return nil
}

type Mutex struct{ log *Log }

func NewMutex(l *Log) *Mutex { return &Mutex{log: l} }
func (m *Mutex) Lock()       { m.log.Add("lock") }
func (m *Mutex) Unlock()     { m.log.Add("unlock") }

// Tx is a transaction: Exec fails for the query "bad".
type Tx struct{ log *Log }

func Begin(l *Log) *Tx { l.Add("begin"); return &Tx{log: l} }
func (t *Tx) Exec(q string) error {
	if q == "bad" {
		t.log.Add("exec-fail")
		return errors.New("exec: bad query")
	}
	t.log.Add("exec:" + q)
	return nil
}
func (t *Tx) Commit() error { t.log.Add("commit"); return nil }
func (t *Tx) Rollback()     { t.log.Add("rollback") }

// Boom panics, standing in for a Go callee that panics mid-body.
func Boom(l *Log) { l.Add("boom"); panic("boom") }
