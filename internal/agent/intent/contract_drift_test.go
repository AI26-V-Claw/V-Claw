package intent

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"vclaw/internal/contracts"
)

type contractToolRow struct {
	risk     contracts.RiskLevel
	approval bool
}

func TestIntentRegistryDoesNotDriftFromContract(t *testing.T) {
	rows := readContractToolRows(t)
	for name := range Registry {
		row, ok := rows[name]
		if !ok {
			t.Fatalf("tool %s is missing from docs/03-contracts.md", name)
		}
		tool, err := LookupTool(name)
		if err != nil {
			t.Fatalf("LookupTool(%q): %v", name, err)
		}
		if tool.DefaultRiskLevel != row.risk {
			t.Fatalf("%s risk = %q, contract = %q", name, tool.DefaultRiskLevel, row.risk)
		}
		if tool.RequiresApproval != row.approval {
			t.Fatalf("%s approval = %v, contract = %v", name, tool.RequiresApproval, row.approval)
		}
	}
}

func readContractToolRows(t *testing.T) map[string]contractToolRow {
	t.Helper()
	path := filepath.Join("..", "..", "..", "docs", "03-contracts.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	rowPattern := regexp.MustCompile(`(?m)^\|\s+` + "`" + `([^` + "`" + `]+)` + "`" + `\s+\|\s+[^|]+\|\s+` + "`" + `([^` + "`" + `]+)` + "`" + `\s+\|\s+(Yes|No)\s+\|`)
	rows := map[string]contractToolRow{}
	for _, match := range rowPattern.FindAllStringSubmatch(string(data), -1) {
		rows[match[1]] = contractToolRow{
			risk:     contracts.RiskLevel(match[2]),
			approval: match[3] == "Yes",
		}
	}
	if len(rows) == 0 {
		t.Fatal("no tool rows parsed from contract")
	}
	return rows
}
