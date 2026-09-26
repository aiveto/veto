package telemetry_test

import (
	"context"
	"testing"

	"github.com/aiveto/veto/telemetry"
)

func TestInstallEmptyKeepsNoop(t *testing.T) {
	stop, err := telemetry.Install("")
	if err != nil {
		t.Fatal(err)
	}
	if err := stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	span := telemetry.StartSpan(context.Background(), "agent.run")
	span.End()
}
