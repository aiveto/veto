package mcpserver_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOfficialRegistryOwnsTheGHCRImage(t *testing.T) {
	raw, err := os.ReadFile("../server.json")
	require.NoError(t, err)
	var server struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Packages    []struct {
			RegistryType string `json:"registryType"`
			Identifier   string `json:"identifier"`
			Transport    struct {
				Type string `json:"type"`
			} `json:"transport"`
			PackageArguments []struct {
				Type  string `json:"type"`
				Value string `json:"value"`
				Name  string `json:"name"`
			} `json:"packageArguments"`
		} `json:"packages"`
	}
	require.NoError(t, json.Unmarshal(raw, &server))

	const name = "io.github.aiveto/veto"
	assert.Equal(t, name, server.Name)
	assert.LessOrEqual(t, len(server.Description), 100)
	require.Len(t, server.Packages, 1)
	pkg := server.Packages[0]
	assert.Equal(t, "oci", pkg.RegistryType)
	assert.True(t, strings.HasPrefix(pkg.Identifier, "ghcr.io/aiveto/veto:"))
	assert.NotContains(t, pkg.Identifier, "latest")
	assert.Equal(t, "stdio", pkg.Transport.Type)

	var serve, config bool
	for _, arg := range pkg.PackageArguments {
		if arg.Type == "positional" && arg.Value == "serve" {
			serve = true
		}
		if arg.Type == "named" && arg.Name == "--config" {
			config = true
		}
	}
	assert.True(t, serve)
	assert.True(t, config)

	df, err := os.ReadFile("../Dockerfile")
	require.NoError(t, err)
	assert.Contains(t, string(df), `LABEL io.modelcontextprotocol.server.name="`+name+`"`)
}
