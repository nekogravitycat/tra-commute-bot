package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Credential environment variable names.
const (
	EnvTDXClientID     = "TDX_CLIENT_ID"
	EnvTDXClientSecret = "TDX_CLIENT_SECRET"
	EnvTelegramToken   = "TELEGRAM_BOT_TOKEN"
	EnvTelegramChatID  = "TELEGRAM_CHAT_ID"
)

// Shorter aliases accepted for the Telegram pair. The names above are
// canonical and are what the deployed environment file uses; these exist so a
// local .env written either way works without silently falling back to
// "Telegram not configured", which is an unhelpful way to learn about a typo.
const (
	envTelegramTokenAlt  = "TG_BOT_TOKEN"
	envTelegramChatIDAlt = "TG_CHAT_ID"
)

// firstEnv returns the first of names that is set to a non-empty value.
func firstEnv(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}

// LoadCredentials reads the credentials from the environment.
//
// Credentials never come from the YAML file. In production Docker supplies
// them via an env file (docker-compose.yml's env_file, or `docker run
// --env-file`); keeping them out of the config means the config can stay
// readable, diffable and in version control.
func LoadCredentials() (Credentials, error) {
	c := Credentials{
		TDXClientID:     os.Getenv(EnvTDXClientID),
		TDXClientSecret: os.Getenv(EnvTDXClientSecret),
		TelegramToken:   firstEnv(EnvTelegramToken, envTelegramTokenAlt),
		TelegramChatID:  firstEnv(EnvTelegramChatID, envTelegramChatIDAlt),
	}
	var missing []string
	if c.TDXClientID == "" {
		missing = append(missing, EnvTDXClientID)
	}
	if c.TDXClientSecret == "" {
		missing = append(missing, EnvTDXClientSecret)
	}
	if len(missing) > 0 {
		return c, fmt.Errorf("missing credentials: %s", strings.Join(missing, ", "))
	}
	return c, nil
}

// TelegramConfigured reports whether a message can actually be delivered.
// Checked separately from the TDX credentials because -dry-run is useful
// without a bot, while a real run is not.
func (c Credentials) TelegramConfigured() bool {
	return c.TelegramToken != "" && c.TelegramChatID != ""
}

// String masks the secret fields so a stray %v/%+v on a Credentials value —
// in a log line or a panic message — cannot leak the TDX client secret or the
// bot token. TelegramChatID is not a secret (Telegram treats it as a plain
// numeric identifier) and is left visible, which is useful on its own for
// telling one deployment's logs from another's.
func (c Credentials) String() string {
	return fmt.Sprintf("Credentials{TDXClientID:%q, TDXClientSecret:%s, TelegramToken:%s, TelegramChatID:%q}",
		c.TDXClientID, mask(c.TDXClientSecret), mask(c.TelegramToken), c.TelegramChatID)
}

// mask reports only whether a secret is set, never any part of its value.
func mask(secret string) string {
	if secret == "" {
		return "<empty>"
	}
	return "<redacted>"
}

// LoadDotEnv reads a KEY=VALUE file into the environment without overwriting
// variables that are already set.
//
// This exists for local development only. In the container Docker's own
// env file does the same job, and a missing file here is not an error
// precisely so that the same binary works in both places.
func LoadDotEnv(path string) error {
	// An empty path disables the file entirely, which is how the container's
	// entrypoint (CMD's -env-file "") makes sure it never picks up a stray
	// .env from the working directory instead of the environment it was given.
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		// The real environment always wins, so a shell override during
		// debugging is never silently undone by a stale file.
		if _, set := os.LookupEnv(key); !set {
			_ = os.Setenv(key, value)
		}
	}
	return sc.Err()
}
