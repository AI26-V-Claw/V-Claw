package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvSetsMissingValues(t *testing.T) {
	t.Setenv("VCLAW_TEST_FROM_DOTENV", "")
	if err := os.Unsetenv("VCLAW_TEST_FROM_DOTENV"); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), ".env")
	content := `
# comment
VCLAW_TEST_FROM_DOTENV="hello world"
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	if err := loadDotEnv(path); err != nil {
		t.Fatalf("loadDotEnv failed: %v", err)
	}
	if got := os.Getenv("VCLAW_TEST_FROM_DOTENV"); got != "hello world" {
		t.Fatalf("expected value from .env, got %q", got)
	}
}

func TestLoadDotEnvDoesNotOverrideExistingEnv(t *testing.T) {
	t.Setenv("VCLAW_TEST_KEEP_EXISTING", "from-shell")

	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("VCLAW_TEST_KEEP_EXISTING=from-file\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := loadDotEnv(path); err != nil {
		t.Fatalf("loadDotEnv failed: %v", err)
	}
	if got := os.Getenv("VCLAW_TEST_KEEP_EXISTING"); got != "from-shell" {
		t.Fatalf("expected existing env to win, got %q", got)
	}
}

func TestLoadDotEnvIgnoresMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := loadDotEnv(path); err != nil {
		t.Fatalf("missing .env should be ignored: %v", err)
	}
}

func TestLoadDotEnvRejectsMalformedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("BROKEN_LINE\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := loadDotEnv(path); err == nil {
		t.Fatal("expected malformed line error")
	}
}
