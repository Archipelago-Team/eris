// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package eris

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGroupZero(t *testing.T) {
	err1 := New("errgroup_test: 1")
	err2 := New("errgroup_test: 2")

	cases := []struct {
		errs []error
	}{
		{errs: []error{}},
		{errs: []error{nil}},
		{errs: []error{err1}},
		{errs: []error{err1, nil}},
		{errs: []error{err1, nil, err2}},
	}

	for _, tc := range cases {
		g := new(Group)

		var firstErr error
		for i, err := range tc.errs {
			err := err
			g.Go(func() error { return err })

			if firstErr == nil && err != nil {
				firstErr = err
			}

			if gErr := g.Wait(); gErr != firstErr {
				t.Errorf("after %T.Go(func() error { return err }) for err in %v\n"+
					"g.Wait() = %v; want %v",
					g, tc.errs[:i+1], err, firstErr)
			}
		}
	}
}

func TestGroupWithContext(t *testing.T) {
	errDoom := New("group_test: doomed")

	cases := []struct {
		errs []error
		want error
	}{
		{want: nil},
		{errs: []error{nil}, want: nil},
		{errs: []error{errDoom}, want: errDoom},
		{errs: []error{errDoom, nil}, want: errDoom},
	}

	for _, tc := range cases {
		g, ctx := WithContext(context.Background())

		for _, err := range tc.errs {
			err := err
			g.Go(func() error { return err })
		}

		if err := g.Wait(); err != tc.want {
			t.Errorf("after %T.Go(func() error { return err }) for err in %v\n"+
				"g.Wait() = %v; want %v",
				g, tc.errs, err, tc.want)
		}

		canceled := false
		select {
		case <-ctx.Done():
			canceled = true
		default:
		}
		if !canceled {
			t.Errorf("after %T.Go(func() error { return err }) for err in %v\n"+
				"ctx.Done() was not closed",
				g, tc.errs)
		}
	}
}

func TestGroupTryGo(t *testing.T) {
	g := &Group{}
	n := 42
	g.SetLimit(42)
	ch := make(chan struct{})
	fn := func() error {
		ch <- struct{}{}
		return nil
	}

	for i := 0; i < n; i++ {
		if !g.TryGo(fn) {
			t.Fatalf("TryGo should succeed but got fail at %d-th call.", i)
		}
	}
	if g.TryGo(fn) {
		t.Fatalf("TryGo is expected to fail but succeeded.")
	}
	go func() {
		for i := 0; i < n; i++ {
			<-ch
		}
	}()
	g.Wait()

	if !g.TryGo(fn) {
		t.Fatalf("TryGo should success but got fail after all goroutines.")
	}
	go func() { <-ch }()
	g.Wait()

	// Switch limit.
	g.SetLimit(1)
	if !g.TryGo(fn) {
		t.Fatalf("TryGo should success but got failed.")
	}
	if g.TryGo(fn) {
		t.Fatalf("TryGo should fail but succeeded.")
	}
	go func() { <-ch }()
	g.Wait()

	// Block all calls.
	g.SetLimit(0)
	for i := 0; i < 1<<10; i++ {
		if g.TryGo(fn) {
			t.Fatalf("TryGo should fail but got succeded.")
		}
	}
	g.Wait()

	g.SetLimit(-1)
	g.TryGo(func() error { return New("err") })
	if err := g.Wait(); err == nil {
		t.Fatalf("expected error; got nil")
	}
}

func TestGroupLimit(t *testing.T) {
	const limit = 10

	g := &Group{}
	g.SetLimit(limit)
	var active int32
	for i := 0; i <= 1<<10; i++ {
		g.Go(func() error {
			n := atomic.AddInt32(&active, 1)
			if n > limit {
				return fmt.Errorf("saw %d active goroutines; want ≤ %d", n, limit)
			}
			time.Sleep(1 * time.Microsecond) // Give other goroutines a chance to increment active.
			atomic.AddInt32(&active, -1)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestGroupContinue(t *testing.T) {
	err1 := New("group_test: 1")
	err2 := New("group_test: 2")

	cases := []struct {
		errs      []error
		nilResult bool
	}{
		{errs: []error{}, nilResult: true},
		{errs: []error{nil}, nilResult: true},
		{errs: []error{err1}},
		{errs: []error{err1, nil}},
		{errs: []error{err1, nil, err2}},
	}

	for _, tc := range cases {
		var g Group
		g.Continue()

		for _, err := range tc.errs {
			err := err
			g.Go(func() error { return err })

		}

		gErr := g.Wait()
		if gErr != nil {
			for i := range tc.errs {
				if tc.errs[i] != nil && !strings.Contains(gErr.Error(), tc.errs[i].Error()) {
					t.Fatalf("expected error to contain %q, actual: %v", tc.errs[i].Error(), gErr)
				}
			}
		} else if !tc.nilResult {
			t.Fatalf("Group.Wait() should not have returned nil for errs: %v", tc.errs)
		}
	}
}

func TestGroupContinueTryGo(t *testing.T) {
	err1 := New("group_test: 1")
	err2 := New("group_test: 2")

	cases := []struct {
		errs      []error
		nilResult bool
	}{
		{errs: []error{}, nilResult: true},
		{errs: []error{nil}, nilResult: true},
		{errs: []error{err1}},
		{errs: []error{err1, nil}},
		{errs: []error{err1, nil, err2}},
	}

	for _, tc := range cases {
		var g Group
		g.Continue()

		for _, err := range tc.errs {
			err := err
			g.TryGo(func() error { return err })

		}

		gErr := g.Wait()
		if gErr != nil {
			for i := range tc.errs {
				if tc.errs[i] != nil && !strings.Contains(gErr.Error(), tc.errs[i].Error()) {
					t.Fatalf("expected error to contain %q, actual: %v", tc.errs[i].Error(), gErr)
				}
			}
		} else if !tc.nilResult {
			t.Fatalf("Group.Wait() should not have returned nil for errs: %v", tc.errs)
		}
	}
}
