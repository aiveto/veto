package execute

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectPageDoesNotOverflow(t *testing.T) {
	raw := []byte(`[{"id":"a"},{"id":"b"}]`)
	body, page, _, err := projectBody(raw, Projection{Fields: []string{"id"}, Offset: 1, Limit: math.MaxInt}, false, 1<<20)
	require.NoError(t, err)
	assert.JSONEq(t, `[{"id":"b"}]`, string(body))
	require.NotNil(t, page)
	assert.Equal(t, 1, page.Offset)
	assert.Equal(t, 1, page.Returned)
}

func TestProjectPageRejectsANegativeWindow(t *testing.T) {
	raw := []byte(`[{"id":"a"},{"id":"b"}]`)
	for _, p := range []Projection{
		{Fields: []string{"id"}, Offset: -1, Limit: 1},
		{Fields: []string{"id"}, Offset: 0, Limit: -1},
	} {
		_, _, _, err := projectBody(raw, p, false, 1<<20)
		require.Error(t, err)
		assert.ErrorContains(t, err, "out of range")
	}
}

func FuzzProjectPage(f *testing.F) {
	f.Add(0, 1, 2)
	f.Add(1, int(math.MaxInt), 2)
	f.Add(-1, 1, 2)
	f.Add(0, -5, 2)
	f.Add(int(math.MaxInt), int(math.MaxInt), 2)
	f.Fuzz(func(t *testing.T, offset, limit, n int) {
		if n < 0 {
			n = 0
		}
		if n > 8 {
			n = 8
		}
		var b strings.Builder
		b.WriteByte('[')
		for i := range n {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `{"id":"%d"}`, i)
		}
		b.WriteByte(']')
		_, _, _, err := projectBody([]byte(b.String()), Projection{Fields: []string{"id"}, Offset: offset, Limit: limit}, false, 1<<20)
		if offset < 0 || limit < 0 {
			require.Error(t, err)
			return
		}
		require.NoError(t, err)
	})
}

func FuzzProjectDecode(f *testing.F) {
	f.Add([]byte(`[{"id":"a"}]`))
	f.Add([]byte(`{"id":"a"}`))
	f.Add([]byte(`{"id":`))
	f.Add([]byte(`not-json`))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, raw []byte) {
		_, _, err := decodeContainer(raw)
		if err != nil && len(raw) == 0 {
			require.Error(t, err)
		}
	})
}
