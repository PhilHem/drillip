package main

import "testing"

func TestEnvOverrides(t *testing.T) {
	t.Setenv("DRILLIP_DB", "/tmp/custom.db")
	t.Setenv("DRILLIP_ADDR", "0.0.0.0:9999")
	cfg := loadConfig()
	if cfg.DB != "/tmp/custom.db" {
		t.Fatalf("expected custom db path, got %q", cfg.DB)
	}
	if cfg.Addr != "0.0.0.0:9999" {
		t.Fatalf("expected custom addr, got %q", cfg.Addr)
	}
}
