package catalog_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aiveto/veto/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFirstServerWinsUntilANameIsSelected(t *testing.T) {
	empty := ""
	staging := "staging"
	missing := "missing"
	cases := []struct {
		name   string
		choose *string
		want   string
		err    string
	}{
		{name: "first server", want: "http://prod.example"},
		{name: "empty name", choose: &empty, want: "http://prod.example"},
		{name: "named server", choose: &staging, want: "http://stage.example"},
		{name: "unknown name", choose: &missing, err: "missing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "spec.yaml")
			require.NoError(t, os.WriteFile(path, []byte(twoServers), 0o644))
			cat, err := openapi.Load(context.Background(), path)
			require.NoError(t, err)
			if tc.choose != nil {
				err = cat.SelectServer(*tc.choose)
			}
			if tc.err != "" {
				assert.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, cat.ByID("assets.get").BaseURL)
		})
	}
}

const twoServers = `openapi: 3.0.3
info:
  title: Assets
  version: "1"
servers:
  - url: http://prod.example
  - url: http://stage.example
    description: staging
paths:
  /assets/{id}:
    get:
      operationId: assets.get
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
`
