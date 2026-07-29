// Package config reads the API's configuration from the environment.
//
// Everything the process needs is read once, at start, and validated before
// the server binds. A missing required value is a startup failure with the
// variable named — never a nil dereference on the first request that needs it.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the whole of the API's configuration.
type Config struct {
	Env             string        // dev, uat, prod
	Port            int           // HTTP listen port
	DatabaseURL     string        // PostgreSQL DSN, from the CNPG app secret
	ShutdownGrace   time.Duration // how long in-flight requests get on SIGTERM
	LogLevel        string
	SandboxOrgSlugs []string // organisations that hold demonstration data (M19)
}

// Load reads configuration from the environment.
func Load() (Config, error) {
	c := Config{
		Env:           get("APP_ENV", "dev"),
		LogLevel:      get("LOG_LEVEL", "info"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		ShutdownGrace: 20 * time.Second,
	}

	port, err := strconv.Atoi(get("PORT", "8080"))
	if err != nil {
		return Config{}, fmt.Errorf("PORT must be a number, got %q", os.Getenv("PORT"))
	}
	c.Port = port

	if slugs := os.Getenv("SANDBOX_ORG_SLUGS"); slugs != "" {
		c.SandboxOrgSlugs = strings.Split(slugs, ",")
	}

	if grace := os.Getenv("SHUTDOWN_GRACE_SECONDS"); grace != "" {
		s, err := strconv.Atoi(grace)
		if err != nil {
			return Config{}, fmt.Errorf("SHUTDOWN_GRACE_SECONDS must be a number, got %q", grace)
		}
		c.ShutdownGrace = time.Duration(s) * time.Second
	}

	return c, c.validate()
}

// validate fails startup rather than the first request that needs the value.
func (c Config) validate() error {
	if c.Env != "dev" && c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required outside dev")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("PORT out of range: %d", c.Port)
	}
	return nil
}

// IsProd reports whether this process is serving real money.
func (c Config) IsProd() bool { return c.Env == "prod" }

func get(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
