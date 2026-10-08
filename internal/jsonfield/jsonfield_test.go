package jsonfield

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStringReadsAnObjectAndTheFirstArrayElement(t *testing.T) {
	got, ok := String(`{"customerId":"cus_priya"}`, "customerId")
	assert.True(t, ok)
	assert.Equal(t, "cus_priya", got)

	got, ok = String(`[{"customerId":null},{"id":"1","customerId":"cus_priya"}]`, "customerId")
	assert.True(t, ok)
	assert.Equal(t, "cus_priya", got)

	_, ok = String(`[]`, "customerId")
	assert.False(t, ok)
	_, ok = String(`{"id":"1"}`, "customerId")
	assert.False(t, ok)
}
