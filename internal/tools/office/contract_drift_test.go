package office_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"vclaw/internal/tools/office/docs"
	"vclaw/internal/tools/office/drive"
	"vclaw/internal/tools/office/sheets"
)

type contractRow struct {
	risk     string
	approval bool
}

func TestWorkspaceRegistryEntriesDoNotDriftFromContract(t *testing.T) {
	rows := readRows(t)
	for _, entry := range appendRegistryEntries(drive.RegistryEntries, docs.RegistryEntries, sheets.RegistryEntries) {
		row, ok := rows[entry.name]
		if !ok {
			t.Fatalf("tool %s is missing from docs/03-contracts.md", entry.name)
		}
		if entry.risk != row.risk {
			t.Fatalf("%s risk = %q, contract = %q", entry.name, entry.risk, row.risk)
		}
		if entry.approval != row.approval {
			t.Fatalf("%s approval = %v, contract = %v", entry.name, entry.approval, row.approval)
		}
	}
}

type registryEntry struct {
	name     string
	risk     string
	approval bool
}

func appendRegistryEntries(groups ...any) []registryEntry {
	var out []registryEntry
	for _, group := range groups {
		value := reflect.ValueOf(group)
		for i := 0; i < value.Len(); i++ {
			item := value.Index(i)
			out = append(out, registryEntry{
				name:     item.FieldByName("Name").String(),
				risk:     item.FieldByName("DefaultRiskLevel").String(),
				approval: item.FieldByName("RequiresApproval").Bool(),
			})
		}
	}
	return out
}

func readRows(t *testing.T) map[string]contractRow {
	t.Helper()
	path := filepath.Join("..", "..", "..", "docs", "03-contracts.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	rowPattern := regexp.MustCompile(`(?m)^\|\s+` + "`" + `([^` + "`" + `]+)` + "`" + `\s+\|\s+[^|]+\|\s+` + "`" + `([^` + "`" + `]+)` + "`" + `\s+\|\s+(Yes|No)\s+\|`)
	rows := map[string]contractRow{}
	for _, match := range rowPattern.FindAllStringSubmatch(string(data), -1) {
		rows[match[1]] = contractRow{risk: match[2], approval: match[3] == "Yes"}
	}
	return rows
}
