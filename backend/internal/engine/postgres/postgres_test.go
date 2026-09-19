package postgres

import (
	"errors"
	"testing"

	"github.com/dbvault/dbvault/backend/internal/engine"
	"github.com/dbvault/dbvault/backend/internal/pgtools"
)

func TestRegistry(t *testing.T) {
	reg := engine.NewRegistry(New(pgtools.Tools{}, t.TempDir()))
	for _, name := range []string{"", "postgres"} {
		d, err := reg.Get(name)
		if err != nil || d.Name() != engine.Postgres {
			t.Fatalf("Get(%q) = %v, %v", name, d, err)
		}
	}
	if _, err := reg.Get("oracle"); !errors.Is(err, engine.ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got %v", err)
	}
}

func TestDriverDefaults(t *testing.T) {
	d := New(pgtools.Tools{}, t.TempDir())
	if d.DefaultPort() != 5432 || d.DefaultSSLMode() != "prefer" || d.FileExtension() != ".dump" {
		t.Fatalf("unexpected defaults")
	}
	spec := d.Sandbox(0, "pw")
	if spec.Image != "postgres:17-alpine" || spec.Port != 5432 || spec.Password != "pw" {
		t.Fatalf("unexpected sandbox spec %+v", spec)
	}
	if got := d.Sandbox(15, "pw").Image; got != "postgres:15-alpine" {
		t.Fatalf("sandbox image = %s", got)
	}
}
