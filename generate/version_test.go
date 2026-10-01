package generate

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVersionFromUsesTheVetoModule(t *testing.T) {
	host := &debug.BuildInfo{
		Main: debug.Module{Path: "example.com/app", Version: "v1.2.3"},
		Deps: []*debug.Module{{Path: vetoModulePath, Version: "v0.4.1"}},
	}
	assert.Equal(t, "v0.4.1", versionFrom(host))

	own := &debug.BuildInfo{Main: debug.Module{Path: vetoModulePath, Version: "v0.9.0"}}
	assert.Equal(t, "v0.9.0", versionFrom(own))

	replaced := &debug.BuildInfo{
		Main: debug.Module{Path: "example.com/app", Version: "v3.0.0"},
		Deps: []*debug.Module{{
			Path:    vetoModulePath,
			Version: "v0.4.1",
			Replace: &debug.Module{Path: "../veto", Version: ""},
		}},
	}
	assert.Equal(t, "v0.0.0", versionFrom(replaced))

	devel := &debug.BuildInfo{Main: debug.Module{Path: vetoModulePath, Version: "(devel)"}}
	assert.Equal(t, "v0.0.0", versionFrom(devel))

	missing := &debug.BuildInfo{Main: debug.Module{Path: "example.com/app", Version: "v1.2.3"}}
	assert.Equal(t, "v0.0.0", versionFrom(missing))
}
