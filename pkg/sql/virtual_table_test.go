// Copyright 2020 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package sql

import (
	"context"
	"testing"

	"github.com/cockroachdb/cockroach/pkg/sql/sem/tree"
	"github.com/cockroachdb/cockroach/pkg/sql/sqlbase"
	"github.com/cockroachdb/cockroach/pkg/util/leaktest"
	"github.com/cockroachdb/cockroach/pkg/util/log"
	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/require"
)

func TestVirtualTableGenerators(t *testing.T) {
	defer leaktest.AfterTest(t)()
	defer log.Scope(t).Close(t)
	t.Run("test cleanup", func(t *testing.T) {
		ctx := context.Background()
		worker := func(pusher rowPusher) error {
			if err := pusher.pushRow(tree.NewDInt(1)); err != nil {
				return err
			}
			if err := pusher.pushRow(tree.NewDInt(2)); err != nil {
				return err
			}
			return nil
		}

		next, cleanup := setupGenerator(ctx, worker)
		d, err := next()
		if err != nil {
			t.Fatal(err)
		}
		require.Equal(t, tree.Datums{tree.NewDInt(1)}, d)

		// Check that we can safely cleanup in the middle of execution.
		cleanup()
	})

	t.Run("test worker error", func(t *testing.T) {
		// Test that if the worker returns an error we catch it.
		ctx := context.Background()
		worker := func(pusher rowPusher) error {
			if err := pusher.pushRow(tree.NewDInt(1)); err != nil {
				return err
			}
			if err := pusher.pushRow(tree.NewDInt(2)); err != nil {
				return err
			}
			return errors.New("dummy error")
		}
		next, cleanup := setupGenerator(ctx, worker)
		_, err := next()
		require.NoError(t, err)
		_, err = next()
		require.NoError(t, err)
		_, err = next()
		require.Error(t, err)
		cleanup()
	})

	t.Run("test no next", func(t *testing.T) {
		ctx := context.Background()
		// Test we don't leak anything if we call cleanup before next.
		worker := func(pusher rowPusher) error {
			return nil
		}
		_, cleanup := setupGenerator(ctx, worker)
		cleanup()
	})

	t.Run("test context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		// Test cancellation before asking for any rows.
		worker := func(pusher rowPusher) error {
			if err := pusher.pushRow(tree.NewDInt(1)); err != nil {
				return err
			}
			if err := pusher.pushRow(tree.NewDInt(2)); err != nil {
				return err
			}
			return nil
		}
		next, cleanup := setupGenerator(ctx, worker)
		cancel()
		_, err := next()
		// There is a small chance that we race and don't return
		// a query canceled here. So, only check the error if
		// it is non-nil.
		if err != nil {
			require.Equal(t, sqlbase.QueryCanceledError, err)
		}
		cleanup()

		// Test cancellation after asking for a row.
		ctx, cancel = context.WithCancel(context.Background())
		next, cleanup = setupGenerator(ctx, worker)
		row, err := next()
		require.NoError(t, err)
		require.Equal(t, tree.Datums{tree.NewDInt(1)}, row)
		cancel()
		_, err = next()
		require.Equal(t, sqlbase.QueryCanceledError, err)
		cleanup()

		// Test cancellation after asking for all the rows.
		ctx, cancel = context.WithCancel(context.Background())
		next, cleanup = setupGenerator(ctx, worker)
		_, err = next()
		require.NoError(t, err)
		_, err = next()
		require.NoError(t, err)
		cancel()
		cleanup()
	})
}

func BenchmarkVirtualTableGenerators(b *testing.B) {
	defer leaktest.AfterTest(b)()
	defer log.Scope(b).Close(b)
	ctx := context.Background()
	worker := func(pusher rowPusher) error {
		for {
			if err := pusher.pushRow(tree.NewDInt(tree.DInt(1))); err != nil {
				return err
			}
		}
	}
	b.Run("bench read", func(b *testing.B) {
		next, cleanup := setupGenerator(ctx, worker)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := next()
			require.NoError(b, err)
		}
		cleanup()
	})
}
