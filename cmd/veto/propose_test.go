package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProposeNeedsListedResponseFields(t *testing.T) {
	err := runPropose(catalogFlags{config: filepath.Join("..", "..", "testdata", "veto.yaml")})
	require.ErrorContains(t, err, "no relation joins two reads that list response fields")
}
