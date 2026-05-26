package lru

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLRU_SetCopiesValue(t *testing.T) {
	c := New(2)
	value := []byte("value")
	require.NoError(t, c.Set("k", value, 0))
	value[0] = 'X'

	got, ok := c.Get("k")
	require.True(t, ok)
	assert.Equal(t, []byte("value"), got)
}

func TestLRU_GetReturnsCopy(t *testing.T) {
	c := New(2)
	require.NoError(t, c.Set("k", []byte("value"), 0))

	got, ok := c.Get("k")
	require.True(t, ok)
	got[0] = 'X'

	again, ok := c.Get("k")
	require.True(t, ok)
	assert.Equal(t, []byte("value"), again)
}
