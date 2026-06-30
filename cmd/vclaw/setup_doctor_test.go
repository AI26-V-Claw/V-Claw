package main

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTopLevelRunAliasParsesHelp(t *testing.T) {
	if err := run(context.Background(), []string{"run", "--help"}); err != nil {
		t.Fatalf("run --help failed: %v", err)
	}
}

func TestUpdateDotEnvFilePreservesCommentsAndUpdatesKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	lines := []string{"# comment", "OPENAI_API_KEY=old", "TELEGRAM_BOT_TOKEN="}
	updates := map[string]string{
		"OPENAI_API_KEY":             "new-key",
		"TELEGRAM_BOT_TOKEN":         "bot-token",
		"ALLOWED_TELEGRAM_USER_ID":   "12345",
		"VCLAW_SKILL_NUDGE_INTERVAL": "0",
	}
	if err := updateDotEnvFile(path, lines, updates); err != nil {
		t.Fatal(err)
	}
	got := mustReadFile(t, path)
	for _, want := range []string{"# comment", "OPENAI_API_KEY=new-key", "TELEGRAM_BOT_TOKEN=bot-token", "ALLOWED_TELEGRAM_USER_ID=12345", "VCLAW_SKILL_NUDGE_INTERVAL=0"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in %q", want, got)
		}
	}
}

func TestPromptEnvValueRejectsNonNumericAllowedUser(t *testing.T) {
	reader := bytes.NewBufferString("abc\n123\n")
	value, changed, err := promptEnvValue(bufio.NewReader(reader), "", "ALLOWED_TELEGRAM_USER_ID", true)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || value != "123" {
		t.Fatalf("expected numeric retry value, changed=%v value=%q", changed, value)
	}
}

func TestPromptEnvValueStripsBOM(t *testing.T) {
	reader := bytes.NewBufferString("\ufeffsk-test\n")
	value, changed, err := promptEnvValue(bufio.NewReader(reader), "", "OPENAI_API_KEY", false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || value != "sk-test" {
		t.Fatalf("expected BOM-stripped value, changed=%v value=%q", changed, value)
	}
}

func TestRunDoctorFailsMalformedDotEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("BROKEN\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runDoctor(path); err == nil {
		t.Fatal("expected malformed .env blocker")
	}
}

func TestRunDoctorFailsRequiredToolModesWhenDependenciesMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := strings.Join([]string{
		"OPENAI_API_KEY=test-key",
		"TELEGRAM_BOT_TOKEN=test-token",
		"ALLOWED_TELEGRAM_USER_ID=12345",
		"VCLAW_SKILL_NUDGE_INTERVAL=0",
		"VCLAW_GOOGLE_TOOLS_MODE=required",
		"VCLAW_GOOGLE_CREDENTIALS_PATH=missing-credentials.json",
		"VCLAW_GOOGLE_TOKEN_PATH=missing-token.json",
		"VCLAW_WEB_TOOLS_MODE=required",
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("ALLOWED_TELEGRAM_USER_ID", "")
	t.Setenv("VCLAW_GOOGLE_TOOLS_MODE", "")
	t.Setenv("VCLAW_WEB_TOOLS_MODE", "")
	t.Setenv("TAVILY_API_KEY", "")
	if err := runDoctor(path); err == nil {
		t.Fatal("expected required tool mode blockers")
	}
}

func TestRunSetupCreatesDotEnvWithoutExampleFile(t *testing.T) {
	previousInput := setupInput
	setupInput = bytes.NewBufferString("test-openai-key\ntest-telegram-token\n12345\n")
	defer func() { setupInput = previousInput }()

	previousWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(previousWd)

	if err := runSetup(); err != nil {
		t.Fatalf("runSetup failed without .env.example: %v", err)
	}
	got := mustReadFile(t, filepath.Join(tempDir, ".env"))
	for _, want := range []string{"OPENAI_API_KEY=test-openai-key", "TELEGRAM_BOT_TOKEN=test-telegram-token", "ALLOWED_TELEGRAM_USER_ID=12345", "VCLAW_GOOGLE_TOOLS_MODE=auto", "VCLAW_WEB_TOOLS_MODE=auto"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in generated .env: %q", want, got)
		}
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(bytes)
}
