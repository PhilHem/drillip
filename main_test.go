package main

import (
	"os"
	"testing"
)

func TestEnvOverrides(t *testing.T) {
	// Just verify env vars are read (don't actually start server)
	os.Setenv("DRILLIP_DB", "/tmp/custom.db")
	os.Setenv("DRILLIP_ADDR", "0.0.0.0:9999")
	defer os.Unsetenv("DRILLIP_DB")
	defer os.Unsetenv("DRILLIP_ADDR")

	dbPath := "errors.db"
	addr := "127.0.0.1:8300"
	if v := os.Getenv("DRILLIP_DB"); v != "" {
		dbPath = v
	}
	if v := os.Getenv("DRILLIP_ADDR"); v != "" {
		addr = v
	}

	if dbPath != "/tmp/custom.db" {
		t.Fatalf("expected custom db path, got %q", dbPath)
	}
	if addr != "0.0.0.0:9999" {
		t.Fatalf("expected custom addr, got %q", addr)
	}
}
