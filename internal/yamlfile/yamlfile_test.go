package yamlfile

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPrepareRejectsASecondDocument(t *testing.T) {
	err := Prepare([]byte("contracts: [orders.yaml]\n---\npolicy: opa\n"))
	require.ErrorContains(t, err, "extra document")

	err = Prepare([]byte("contracts: [orders.yaml]\n---\n"))
	require.ErrorContains(t, err, "extra document")

	err = Prepare([]byte("contracts: [orders.yaml]\n---\n: [\n"))
	require.Error(t, err)
	require.NotContains(t, err.Error(), "extra document")
}

func TestPrepareBoundsAliasExpansion(t *testing.T) {
	var b strings.Builder
	b.WriteString("k0: &k0 [1]\n")
	for i := 1; i <= 9; i++ {
		b.WriteString("k")
		b.WriteString(itoa(i))
		b.WriteString(": &k")
		b.WriteString(itoa(i))
		b.WriteString(" [")
		for j := range 10 {
			if j > 0 {
				b.WriteString(", ")
			}
			b.WriteString("*k")
			b.WriteString(itoa(i - 1))
		}
		b.WriteString("]\n")
	}
	b.WriteString("root: *k9\n")
	body := b.String()
	require.Less(t, len(body), 2000)

	done := make(chan error, 1)
	go func() {
		done <- Prepare([]byte(body))
	}()
	select {
	case err := <-done:
		require.ErrorContains(t, err, "alias expansion exceeds limit")
	case <-time.After(2 * time.Second):
		t.Fatal("alias expansion did not finish")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
