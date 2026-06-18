package longmem

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"vclaw/internal/providers"
)

// Flusher writes long-term memory facts to cache/memory/ files.
// It is called after each successful session compaction.
type Flusher struct {
	dir      string
	provider providers.Provider
	model    string
}

// NewFlusher creates a Flusher that writes to dir using provider for LLM classification.
func NewFlusher(dir string, provider providers.Provider, model string) *Flusher {
	return &Flusher{dir: dir, provider: provider, model: model}
}

// Flush extracts facts from the compaction summary and appends them to USER.md
// and NOTES.md. If the LLM call fails, extractive regex is used as fallback
// (results go to NOTES.md only). Returns nil if summary is empty.
func (f *Flusher) Flush(ctx context.Context, summary string) error {
	if strings.TrimSpace(summary) == "" {
		return nil
	}

	existingUserMD := f.readFile("USER.md")
	existingNotesMD := f.readFile("NOTES.md")

	result, err := f.classifyWithLLM(ctx, summary, existingUserMD, existingNotesMD)
	if err != nil || (len(result.UserFacts) == 0 && len(result.NotesFacts) == 0) {
		// Fallback: regex extraction, all facts go to NOTES.md.
		fallbackFacts := extractiveFallback(summary)
		if len(fallbackFacts) == 0 {
			return nil
		}
		return f.updateFile("NOTES.md", func(existing string) string {
			return appendNotesFacts(existing, fallbackFacts)
		})
	}

	if len(result.UserFacts) > 0 {
		if err := f.updateFile("USER.md", func(existing string) string {
			return mergeUserFacts(existing, result.UserFacts)
		}); err != nil {
			return err
		}
	}
	if len(result.NotesFacts) > 0 {
		if err := f.updateFile("NOTES.md", func(existing string) string {
			return appendNotesFacts(existing, result.NotesFacts)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (f *Flusher) classifyWithLLM(ctx context.Context, summary, existingUserMD, existingNotesMD string) (ClassifyResult, error) {
	resp, err := f.provider.Generate(ctx, &providers.GenerateRequest{
		SystemPrompt: classifySystemPrompt(),
		UserPrompt:   classifyUserPrompt(summary, existingUserMD, existingNotesMD),
		Temperature:  0.2,
		MaxTokens:    512,
		Model:        f.model,
	})
	if err != nil {
		return ClassifyResult{}, err
	}
	return parseClassifyResponse(resp.Text), nil
}

// readFile reads the content of a memory file, returning empty string if not found.
func (f *Flusher) readFile(name string) string {
	data, err := os.ReadFile(filepath.Join(f.dir, name))
	if err != nil {
		return ""
	}
	return string(data)
}

// updateFile reads the current content of name, applies transform, and atomically
// writes the result back. Creates the file if it does not exist.
func (f *Flusher) updateFile(name string, transform func(string) string) error {
	path := filepath.Join(f.dir, name)
	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	}
	content := transform(existing)
	return atomicWriteFile(path, []byte(content))
}

// atomicWriteFile writes data to path using a temp-file-then-rename pattern
// to avoid partial writes being observed by readers.
func atomicWriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
