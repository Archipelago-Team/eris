// Package eris is an error handling library with readable stack traces and flexible formatting support.
package eris

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
)

// New creates a new root error with a static message.
func New(msg string) error {
	stack := callers(3) // callers(3) skips this method, stack.callers, and runtime.Callers
	return &rootError{
		global: stack.isGlobal(),
		msg:    msg,
		stack:  stack,
	}
}

// Errorf creates a new root error with a formatted message.
func Errorf(format string, args ...interface{}) error {
	stack := callers(3)
	return &rootError{
		global: stack.isGlobal(),
		msg:    fmt.Sprintf(format, args...),
		stack:  stack,
	}
}

// Join returns an error that wraps the given errors.
// Any nil error values are discarded.
// Join returns nil if every value in errs is nil.
// The error formats as the concatenation of the strings obtained
// by calling the Error method of each element of errs, with a newline
// between each string.
//
// A non-nil error returned by Join implements the Unwrap() []error method.
func Join(errs ...error) error {
	return errors.Join(errs...)
}

// Wrap adds additional context to all error types while maintaining the type of the original error.
//
// This method behaves differently for each error type. For root errors, the stack trace is reset to the current
// callers which ensures traces are correct when using global/sentinel error values. Wrapped error types are simply
// wrapped with the new context. For external types (i.e. something other than root or wrap errors), this method
// attempts to unwrap them while building a new error chain. If an external type does not implement the unwrap
// interface, it flattens the error and creates a new root error from it before wrapping with the additional
// context.
func Wrap(err error, msg ...string) error {
	if len(msg) == 0 {
		return wrap(err, "")
	}
	return wrap(err, strings.Join(msg, " "))
}

// Wrapf adds additional context to all error types while maintaining the type of the original error.
//
// This is a convenience method for wrapping errors with formatted messages and is otherwise the same as Wrap.
func Wrapf(err error, format string, args ...interface{}) error {
	return wrap(err, fmt.Sprintf(format, args...))
}

func wrap(err error, msg string) error {
	if err == nil {
		panic("eris: error cannot be nil")
	}

	// callers(4) skips runtime.Callers, stack.callers, this method, and Wrap(f)
	stack := callers(4)
	// caller(3) skips stack.caller, this method, and Wrap(f)
	// caller(skip) has a slightly different meaning which is why it's not 4 as above
	frame := caller(3)
	switch e := err.(type) {
	case *rootError:
		if e.global {
			// create a new root error for global values to make sure nothing interferes with the stack
			err = &rootError{
				global: e.global,
				msg:    e.msg,
				stack:  stack,
			}
		} else {
			// insert the frame into the stack
			e.stack.insertPC(*stack)
		}
	case *wrapError:
		// insert the frame into the stack
		if root, ok := Cause(err).(*rootError); ok {
			root.stack.insertPC(*stack)
		}
	default:
		// return a new root error that wraps the external error
		return &rootError{
			msg:   msg,
			ext:   e,
			stack: stack,
		}
	}

	return &wrapError{
		msg:   msg,
		err:   err,
		frame: frame,
	}
}

// Unwrap returns the result of calling the Unwrap method on err, if err's type contains an Unwrap method
// returning error. Otherwise, Unwrap returns nil.
func Unwrap(err error) error {
	return errors.Unwrap(err)
}

// Is reports whether any error in err's chain matches target.
//
// The chain consists of err itself followed by the sequence of errors obtained by repeatedly calling Unwrap.
//
// An error is considered to match a target if it is equal to that target or if it implements a method
// Is(error) bool such that Is(target) returns true.
func Is(err, target error) bool {
	return errors.Is(err, target)
}

// As finds the first error in err's chain that matches target. If there's a match, it sets target to that error
// value and returns true. Otherwise, it returns false.
//
// The chain consists of err itself followed by the sequence of errors obtained by repeatedly calling Unwrap.
//
// An error matches target if the error's concrete value is assignable to the value pointed to by target,
// or if the error has a method As(interface{}) bool such that As(target) returns true.
func As(err error, target interface{}) bool {
	return errors.As(err, target)
}

// Cause returns the root cause of the error, which is defined as the first error in the chain. The original
// error is returned if it does not implement `Unwrap() error` and nil is returned if the error is nil.
func Cause(err error) error {
	for {
		switch x := err.(type) {
		case interface{ Unwrap() error }:
			uerr := x.Unwrap()
			if uerr == nil {
				return err
			}
			err = uerr
		case interface{ Unwrap() []error }:
			uerrs := x.Unwrap()
			if len(uerrs) == 0 {
				return err
			}
			err = uerrs[0]
		default:
			return err
		}
	}
}

// StackFrames returns the trace of an error in the form of a program counter slice.
// Use this method if you want to pass the eris stack trace to some other error tracing library.
func StackFrames(err error) []uintptr {
	for err != nil {
		switch err := err.(type) {
		case *rootError:
			return err.StackFrames()
		case *wrapError:
			return err.StackFrames()
		default:
			return []uintptr{}
		}
	}
	return []uintptr{}
}

type rootError struct {
	global bool   // flag indicating whether the error was declared globally
	msg    string // root error message
	ext    error  // error type for wrapping external errors
	stack  *stack // root error stack trace
}

func (e *rootError) Error() string {
	return fmt.Sprint(e)
}

func (e *rootError) Format(s fmt.State, verb rune) {
	printError(e, s, verb)
}

func (e *rootError) Is(target error) bool {
	if err, ok := target.(*rootError); ok {
		return e.msg == err.msg
	}
	return e.msg == target.Error()
}

func (e *rootError) As(target interface{}) bool {
	t := reflect.Indirect(reflect.ValueOf(target)).Interface()
	if err, ok := t.(*rootError); ok {
		if e.msg == err.msg {
			reflect.ValueOf(target).Elem().Set(reflect.ValueOf(err))
			return true
		}
	}
	return false
}

func (e *rootError) Unwrap() error {
	return e.ext
}

// StackFrames returns the trace of a root error in the form of a program counter slice.
// This method is currently called by an external error tracing library (Sentry).
func (e *rootError) StackFrames() []uintptr {
	return *e.stack
}

type wrapError struct {
	msg   string // wrap error message
	err   error  // error type representing the next error in the chain
	frame *frame // wrap error stack frame
}

func (e *wrapError) Error() string {
	return fmt.Sprint(e)
}

func (e *wrapError) Format(s fmt.State, verb rune) {
	printError(e, s, verb)
}

func (e *wrapError) Is(target error) bool {
	if err, ok := target.(*wrapError); ok {
		return e.msg == err.msg
	}
	return e.msg == target.Error()
}

func (e *wrapError) As(target interface{}) bool {
	t := reflect.Indirect(reflect.ValueOf(target)).Interface()
	if err, ok := t.(*wrapError); ok {
		if e.msg == err.msg {
			reflect.ValueOf(target).Elem().Set(reflect.ValueOf(err))
			return true
		}
	}
	return false
}

func (e *wrapError) Unwrap() error {
	return e.err
}

// StackFrames returns the trace of a wrap error in the form of a program counter slice.
// This method is currently called by an external error tracing library (Sentry).
func (e *wrapError) StackFrames() []uintptr {
	return []uintptr{e.frame.pc()}
}

func printError(err error, s fmt.State, verb rune) {
	var withTrace bool
	switch verb {
	case 'v':
		if s.Flag('+') {
			withTrace = true
		}
	}
	str := ToString(err, withTrace)
	_, _ = io.WriteString(s, str)
}
