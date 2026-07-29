package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load() returned %v, want no error in dev without a database", err)
	}
	if c.Port != 8080 {
		t.Errorf("Port = %d, want 8080", c.Port)
	}
	if c.ShutdownGrace != 20*time.Second {
		t.Errorf("ShutdownGrace = %v, want 20s", c.ShutdownGrace)
	}
}

func TestDatabaseRequiredOutsideDev(t *testing.T) {
	t.Setenv("APP_ENV", "prod")
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() succeeded in prod without DATABASE_URL, want a startup failure")
	}
}

func TestPortMustBeANumber(t *testing.T) {
	t.Setenv("PORT", "eighty-eighty")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted a non-numeric PORT, want an error naming the variable")
	}
}
