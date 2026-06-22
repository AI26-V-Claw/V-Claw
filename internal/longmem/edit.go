package longmem

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadFiles reads USER.md and NOTES.md from dir.
// Returns empty strings (not error) when files don't exist.
func ReadFiles(dir string) (userMD, notesMD string, err error) {
	userMD, err = readFileSafe(filepath.Join(dir, "USER.md"))
	if err != nil {
		return "", "", fmt.Errorf("read USER.md: %w", err)
	}
	notesMD, err = readFileSafe(filepath.Join(dir, "NOTES.md"))
	if err != nil {
		return "", "", fmt.Errorf("read NOTES.md: %w", err)
	}
	return userMD, notesMD, nil
}

// AddUserFact adds a fact to USER.md under the given category.
// The category must be one of the userCategories. Deduplicates by normalized form.
func AddUserFact(dir, category, fact string) error {
	category = strings.TrimSpace(category)
	fact = strings.TrimSpace(fact)
	if fact == "" {
		return fmt.Errorf("fact must not be empty")
	}
	if !validCategory(category) {
		return fmt.Errorf("invalid category %q; must be one of: %s", category, strings.Join(userCategories, ", "))
	}

	path := filepath.Join(dir, "USER.md")
	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	}

	result := mergeUserFacts(existing, []CategorizedFact{{Category: category, Fact: fact}})
	return atomicWriteFile(path, []byte(result))
}

// RemoveUserFact removes lines from USER.md that contain pattern as a substring.
// Returns true if at least one line was removed.
func RemoveUserFact(dir, pattern string) (bool, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false, fmt.Errorf("pattern must not be empty")
	}

	path := filepath.Join(dir, "USER.md")
	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	}
	if strings.TrimSpace(existing) == "" {
		return false, nil
	}

	doc := parseUserDoc(existing)
	removed := false
	for _, heading := range doc.headings {
		before := len(doc.bullets[heading])
		doc.bullets[heading] = filterBullets(doc.bullets[heading], pattern)
		if len(doc.bullets[heading]) < before {
			removed = true
		}
	}

	if !removed {
		return false, nil
	}
	return true, atomicWriteFile(path, []byte(doc.render()))
}

// AddNotesFact adds a fact to NOTES.md. Deduplicates and trims if over token limit.
func AddNotesFact(dir, fact string) error {
	fact = strings.TrimSpace(fact)
	if fact == "" {
		return fmt.Errorf("fact must not be empty")
	}

	path := filepath.Join(dir, "NOTES.md")
	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	}

	result := appendNotesFacts(existing, []string{fact})
	return atomicWriteFile(path, []byte(result))
}

// RemoveNotesFact removes lines from NOTES.md that contain pattern as a substring.
// Returns true if at least one line was removed.
func RemoveNotesFact(dir, pattern string) (bool, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false, fmt.Errorf("pattern must not be empty")
	}

	path := filepath.Join(dir, "NOTES.md")
	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	}
	if strings.TrimSpace(existing) == "" {
		return false, nil
	}

	lines := strings.Split(existing, "\n")
	var kept []string
	removed := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Keep heading lines and lines that don't match the pattern.
		if !strings.HasPrefix(trimmed, "- ") || !strings.Contains(trimmed, pattern) {
			kept = append(kept, line)
		} else {
			removed = true
		}
	}

	if !removed {
		return false, nil
	}
	return true, atomicWriteFile(path, []byte(strings.Join(kept, "\n")))
}

// ResetAll deletes USER.md and NOTES.md, then recreates skeleton files.
func ResetAll(dir string) error {
	if err := atomicWriteFile(filepath.Join(dir, "USER.md"), []byte(userMDSkeleton())); err != nil {
		return fmt.Errorf("reset USER.md: %w", err)
	}
	if err := atomicWriteFile(filepath.Join(dir, "NOTES.md"), []byte(notesMDSkeleton())); err != nil {
		return fmt.Errorf("reset NOTES.md: %w", err)
	}
	return nil
}

// readFileSafe reads a file, returning "" if it doesn't exist.
func readFileSafe(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// validCategory checks whether category is in userCategories.
func validCategory(category string) bool {
	for _, c := range userCategories {
		if strings.EqualFold(category, c) {
			return true
		}
	}
	return false
}

// filterBullets keeps lines that do NOT contain pattern as a substring.
func filterBullets(bullets []string, pattern string) []string {
	var out []string
	for _, b := range bullets {
		t := strings.TrimSpace(b)
		if strings.HasPrefix(t, "- ") && strings.Contains(t, pattern) {
			continue
		}
		out = append(out, b)
	}
	return out
}
