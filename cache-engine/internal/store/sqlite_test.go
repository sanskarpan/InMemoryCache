package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLiteStoreRoundTrip(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/cache.db", 0)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

	require.NoError(t, store.Set(context.Background(), "k", []byte("value")))
	got, err := store.Get(context.Background(), "k")
	require.NoError(t, err)
	assert.Equal(t, []byte("value"), got)

	store.SetLatency(5 * time.Millisecond)
	assert.Equal(t, 5*time.Millisecond, store.GetLatency())
}
