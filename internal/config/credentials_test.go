package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCredentials(t *testing.T) {
	t.Setenv(EnvTDXClientID, "id")
	t.Setenv(EnvTDXClientSecret, "secret")
	t.Setenv(EnvTelegramToken, "token")
	t.Setenv(EnvTelegramChatID, "chat")
	// The alias vars are cleared too, so a real .env sourced into the shell
	// (or CI) that happens to set TG_BOT_TOKEN/TG_CHAT_ID cannot change which
	// credential value wins: firstEnv always tries the canonical name first,
	// but only once every test controls both names is the outcome actually
	// pinned rather than incidentally correct.
	t.Setenv(envTelegramTokenAlt, "")
	t.Setenv(envTelegramChatIDAlt, "")

	c, err := LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if c.TDXClientID != "id" || c.TDXClientSecret != "secret" {
		t.Errorf("TDX credentials = %q/%q, want id/secret", c.TDXClientID, c.TDXClientSecret)
	}
	if !c.TelegramConfigured() {
		t.Error("Telegram should be reported as configured")
	}
}

// TestMissingTDXCredentials checks the TDX pair is required outright: without
// it there is nothing to fetch and no brief to produce.
func TestMissingTDXCredentials(t *testing.T) {
	t.Setenv(EnvTDXClientID, "")
	t.Setenv(EnvTDXClientSecret, "")

	_, err := LoadCredentials()
	if err == nil {
		t.Fatal("expected an error for missing TDX credentials")
	}
	for _, want := range []string{EnvTDXClientID, EnvTDXClientSecret} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, should name %s", err, want)
		}
	}
}

// TestTelegramOptional checks the bot credentials are checked separately, so
// -dry-run remains usable before a bot has been created.
func TestTelegramOptional(t *testing.T) {
	t.Setenv(EnvTDXClientID, "id")
	t.Setenv(EnvTDXClientSecret, "secret")
	// Both the canonical names and their aliases must be cleared: firstEnv
	// falls back to TG_BOT_TOKEN/TG_CHAT_ID, so a real .env or shell profile
	// exporting those (as a developer's own credentials file might) would
	// otherwise leak into this test and make Telegram look configured when
	// the test is deliberately simulating a fresh install that has neither.
	t.Setenv(EnvTelegramToken, "")
	t.Setenv(EnvTelegramChatID, "")
	t.Setenv(envTelegramTokenAlt, "")
	t.Setenv(envTelegramChatIDAlt, "")

	c, err := LoadCredentials()
	if err != nil {
		t.Fatalf("missing Telegram credentials should not block loading: %v", err)
	}
	if c.TelegramConfigured() {
		t.Error("Telegram should not be reported as configured")
	}

	// One half is not enough.
	t.Setenv(EnvTelegramToken, "token")
	c, _ = LoadCredentials()
	if c.TelegramConfigured() {
		t.Error("a token without a chat ID cannot deliver anything")
	}
}

// TestCredentialsStringMasksSecrets is M-11: a Credentials value printed with
// %v or %+v — in a log line, or in a panic message — must not leak the TDX
// client secret or the bot token.
func TestCredentialsStringMasksSecrets(t *testing.T) {
	c := Credentials{
		TDXClientID:     "public-id",
		TDXClientSecret: "super-secret",
		TelegramToken:   "bot-token-123",
		TelegramChatID:  "123456",
	}
	for _, got := range []string{fmt.Sprintf("%v", c), fmt.Sprintf("%+v", c), c.String()} {
		if strings.Contains(got, "super-secret") || strings.Contains(got, "bot-token-123") {
			t.Errorf("%q leaks a secret value", got)
		}
		if !strings.Contains(got, "public-id") || !strings.Contains(got, "123456") {
			t.Errorf("%q should still show the non-secret fields", got)
		}
	}
}

func TestLoadDotEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	body := "# a comment\n\n" +
		"TDX_CLIENT_ID=from-file\n" +
		`TDX_CLIENT_SECRET="quoted-secret"` + "\n" +
		"  TELEGRAM_BOT_TOKEN = spaced \n" +
		"NOT_A_PAIR\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	t.Setenv("TDX_CLIENT_ID", "")
	os.Unsetenv("TDX_CLIENT_ID")
	t.Setenv("TDX_CLIENT_SECRET", "")
	os.Unsetenv("TDX_CLIENT_SECRET")
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	os.Unsetenv("TELEGRAM_BOT_TOKEN")

	if err := LoadDotEnv(path); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}
	if got := os.Getenv("TDX_CLIENT_ID"); got != "from-file" {
		t.Errorf("TDX_CLIENT_ID = %q, want from-file", got)
	}
	if got := os.Getenv("TDX_CLIENT_SECRET"); got != "quoted-secret" {
		t.Errorf("quotes should be stripped, got %q", got)
	}
	if got := os.Getenv("TELEGRAM_BOT_TOKEN"); got != "spaced" {
		t.Errorf("surrounding space should be trimmed, got %q", got)
	}
}

// TestDotEnvDoesNotOverride checks the real environment wins, so an override
// set for a debugging session is not silently undone by a stale file.
func TestDotEnvDoesNotOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("TDX_CLIENT_ID=from-file\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	t.Setenv("TDX_CLIENT_ID", "from-environment")
	if err := LoadDotEnv(path); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}
	if got := os.Getenv("TDX_CLIENT_ID"); got != "from-environment" {
		t.Errorf("TDX_CLIENT_ID = %q, want the environment to win", got)
	}
}

// TestDotEnvMissingIsFine checks a missing file is not an error: in the
// container Docker supplies the environment and no such file exists.
func TestDotEnvMissingIsFine(t *testing.T) {
	if err := LoadDotEnv(filepath.Join(t.TempDir(), "absent")); err != nil {
		t.Errorf("a missing env file should be ignored, got %v", err)
	}
}

// TestDotEnvEmptyPathDisabled checks an empty path switches the file off. The
// container's entrypoint passes one so it uses only the environment it was given.
func TestDotEnvEmptyPathDisabled(t *testing.T) {
	if err := LoadDotEnv(""); err != nil {
		t.Errorf("an empty path should disable the env file, got %v", err)
	}
}

// TestTelegramAliases checks the shorter variable names are accepted. A .env
// written either way should work rather than silently reporting Telegram as
// unconfigured, which is a confusing way to discover a naming mismatch.
func TestTelegramAliases(t *testing.T) {
	t.Setenv(EnvTDXClientID, "id")
	t.Setenv(EnvTDXClientSecret, "secret")
	t.Setenv(EnvTelegramToken, "")
	t.Setenv(EnvTelegramChatID, "")
	t.Setenv("TG_BOT_TOKEN", "alias-token")
	t.Setenv("TG_CHAT_ID", "alias-chat")

	c, err := LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if !c.TelegramConfigured() {
		t.Fatal("the alias names should configure Telegram")
	}
	if c.TelegramToken != "alias-token" || c.TelegramChatID != "alias-chat" {
		t.Errorf("got %q/%q, want the alias values", c.TelegramToken, c.TelegramChatID)
	}

	// The canonical name wins when both are present.
	t.Setenv(EnvTelegramToken, "canonical-token")
	c, _ = LoadCredentials()
	if c.TelegramToken != "canonical-token" {
		t.Errorf("token = %q, want the canonical variable to take precedence", c.TelegramToken)
	}
}
