package policy

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandNotifierStopsAHungCommand(t *testing.T) {
	prev := commandNotifyTimeout
	commandNotifyTimeout = 50 * time.Millisecond
	t.Cleanup(func() { commandNotifyTimeout = prev })
	n, err := NewWebhook("", []string{"sleep", "30"})
	require.NoError(t, err)
	start := time.Now()
	err = n.Pending(t.Context(), Notice{ID: "1", Operation: "orders.delete"})
	require.Error(t, err)
	assert.Less(t, time.Since(start), 2*time.Second)
}
