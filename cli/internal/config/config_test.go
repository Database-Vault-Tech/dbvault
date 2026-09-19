package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadAndEnvOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	t.Setenv("DBVAULT_CONFIG", path)
	t.Setenv("DBVAULT_SERVER", "")
	t.Setenv("DBVAULT_TOKEN", "")
	t.Setenv("DBVAULT_ORG", "")

	c := &Config{Server: "http://localhost:3000", Token: "dbv_secret", Organization: "org-1"}
	if _, err := c.Save(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("config must be private, got %v", fi.Mode().Perm())
	}
	loaded, err := Load()
	if err != nil || loaded.Token != "dbv_secret" || loaded.Server != "http://localhost:3000" {
		t.Fatalf("load: %+v %v", loaded, err)
	}
	t.Setenv("DBVAULT_TOKEN", "dbv_from_env")
	t.Setenv("DBVAULT_ORG", "ci-org")
	loaded, _ = Load()
	if loaded.Token != "dbv_from_env" || loaded.Organization != "ci-org" {
		t.Fatalf("env overrides not applied: %+v", loaded)
	}
}

func TestLoadMissingFile(t *testing.T) {
	t.Setenv("DBVAULT_CONFIG", filepath.Join(t.TempDir(), "none.json"))
	c, err := Load()
	if err != nil || c.Token != "" {
		t.Fatalf("missing config should load empty: %+v %v", c, err)
	}
}
