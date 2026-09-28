package bootstrap

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	integrations "github.com/PhilHem/drillip/internal/adapter/out/observability"
	notify "github.com/PhilHem/drillip/internal/adapter/out/smtp"
	"github.com/PhilHem/drillip/internal/domain"
)

// config holds all environment-based configuration.
type config struct {
	DB           string
	Addr         string
	Project      string // project name for notifications
	SMTP         notify.SMTPConfig
	SMTPCooldown time.Duration
	SMTPDigest   time.Duration
	ResolveAfter time.Duration
	RetainFor    time.Duration
	Integrations integrations.Config
}

func loadConfig() config {
	cfg := config{
		DB:   "errors.db",
		Addr: "127.0.0.1:8300",
	}
	if v := os.Getenv("DRILLIP_DB"); v != "" {
		cfg.DB = v
	}
	if v := os.Getenv("DRILLIP_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := os.Getenv("DRILLIP_PROJECT"); v != "" {
		cfg.Project = v
	}
	if v := os.Getenv("DRILLIP_UNIT"); v != "" {
		cfg.Integrations.Unit = v
	}
	if v := os.Getenv("DRILLIP_VM_URL"); v != "" {
		cfg.Integrations.VMURL = v
	}
	if v := os.Getenv("DRILLIP_VT_URL"); v != "" {
		cfg.Integrations.VTURL = v
	}
	if v := os.Getenv("DRILLIP_PYROSCOPE_URL"); v != "" {
		cfg.Integrations.PyroscopeURL = v
	}
	if v := os.Getenv("DRILLIP_SERVICE"); v != "" {
		cfg.Integrations.Service = v
	}
	if v := os.Getenv("DRILLIP_SMTP_HOST"); v != "" {
		cfg.SMTP.Host = v
	}
	if v := os.Getenv("DRILLIP_SMTP_PORT"); v != "" {
		cfg.SMTP.Port = v
	}
	if v := os.Getenv("DRILLIP_SMTP_FROM"); v != "" {
		cfg.SMTP.From = v
	}
	if v := os.Getenv("DRILLIP_SMTP_TO"); v != "" {
		cfg.SMTP.To = v
	}
	if v := os.Getenv("DRILLIP_SMTP_USER"); v != "" {
		cfg.SMTP.User = v
	}
	if v := os.Getenv("DRILLIP_SMTP_PASS"); v != "" {
		cfg.SMTP.Pass = v
	}
	if v := os.Getenv("DRILLIP_SMTP_SKIP_VERIFY"); v == "true" || v == "1" {
		cfg.SMTP.SkipVerify = true
	}
	cfg.SMTPCooldown = 60 * time.Second // default
	if v := os.Getenv("DRILLIP_SMTP_COOLDOWN"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.SMTPCooldown = d
		} else {
			slog.Warn("invalid DRILLIP_SMTP_COOLDOWN, using default", "value", v, "default", cfg.SMTPCooldown)
		}
	}
	cfg.SMTPDigest = 5 * time.Minute // default
	if v := os.Getenv("DRILLIP_SMTP_DIGEST"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.SMTPDigest = d
		} else {
			slog.Warn("invalid DRILLIP_SMTP_DIGEST, using default", "value", v, "default", cfg.SMTPDigest)
		}
	}
	cfg.ResolveAfter = 24 * time.Hour // default
	if v := os.Getenv("DRILLIP_RESOLVE_AFTER"); v != "" {
		if d, err := domain.ParseDuration(v); err == nil {
			cfg.ResolveAfter = d
		} else {
			slog.Warn("invalid DRILLIP_RESOLVE_AFTER, using default", "value", v, "default", cfg.ResolveAfter)
		}
	}
	cfg.RetainFor = 90 * 24 * time.Hour // default 90 days
	if v := os.Getenv("DRILLIP_RETAIN"); v != "" {
		if d, err := domain.ParseDuration(v); err == nil {
			cfg.RetainFor = d
		} else {
			slog.Warn("invalid DRILLIP_RETAIN, using default", "value", v, "default", cfg.RetainFor)
		}
	}
	return cfg
}

func validateConfig(cfg config) {
	if cfg.SMTP.Host != "" && cfg.SMTP.To == "" {
		slog.Warn("DRILLIP_SMTP_HOST set but DRILLIP_SMTP_TO empty, notifications disabled")
	}
	if cfg.SMTP.Host != "" && cfg.SMTP.From == "" {
		slog.Warn("DRILLIP_SMTP_HOST set but DRILLIP_SMTP_FROM empty")
	}
	slog.Info("config loaded", "db", cfg.DB, "addr", cfg.Addr, "resolve_after", cfg.ResolveAfter, "cooldown", cfg.SMTPCooldown, "digest", cfg.SMTPDigest)
}

func initLogger(stderr io.Writer) {
	level := slog.LevelInfo
	if v := os.Getenv("DRILLIP_LOG_LEVEL"); v != "" {
		switch strings.ToLower(v) {
		case "debug":
			level = slog.LevelDebug
		case "warn", "warning":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		}
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level})))
}
