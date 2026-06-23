// Package contracts_test contains drift-guard tests that verify tool metadata
// (Capability, RiskLevel, RequiresApproval) and RiskLevel enum values have not
// drifted away from the contract committed in the shared contracts package.
//
// These tests act as a safety net: if anyone changes a tool's risk classification
// or renames a RiskLevel constant, the test will fail with a clear "contract drift
// detected" message before the change reaches production.
package contracts_test

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/policies"
	"vclaw/internal/tools"
	"vclaw/internal/tools/office/calendar"
	"vclaw/internal/tools/office/chat"
	"vclaw/internal/tools/office/docs"
	"vclaw/internal/tools/office/drive"
	"vclaw/internal/tools/office/gmail"
	"vclaw/internal/tools/office/people"
	"vclaw/internal/tools/office/sheets"
	"vclaw/internal/tools/os/filesystem"
	"vclaw/internal/tools/web"
	"vclaw/internal/tools/memory"
)

const docsToolRegistryPath = "../../docs/03-contracts.md"

// ─── Memory Tool Metadata in Docs anti-drift ───────────────────────────────────
// Verifies that all memory tools declared in internal/tools/memory/RegistryEntries
// are present in the Tool Registry table within docs/03-contracts.md. This prevents
// self-referential drift where tests only compare against the Go source and not
// the canonical docs.
func TestMemoryToolMetadataDocsMatchesRegistry(t *testing.T) {
	registryPath := docsToolRegistryPath
	docs, err := os.ReadFile(registryPath)
	if err != nil {
		t.Skipf("cannot read docs file for drift test: %v", err)
	}

	// Parse the Memory section table in docs.
	docsContent := string(docs)
	startIdx := strings.Index(docsContent, "### Memory")
	if startIdx < 0 {
		t.Fatalf("docs missing '### Memory' section")
	}
	section := docsContent[startIdx:]

	// Extract tool names from the markdown table lines (| `tool.name` |).
	var docsTools = map[string]bool{}
	lines := strings.Split(section, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		// Format: | `tool.name` | Owner | Risk | Approval |
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimSpace(strings.Trim(strings.TrimSpace(parts[1]), "`"))
		if name != "" {
			docsTools[name] = true
		}
	}

	// Compare with memory.RegistryEntries.
	for _, entry := range memory.RegistryEntries {
		name := entry.Name
		if !docsTools[name] {
			t.Errorf("contract drift: tool %q missing in docs/03-contracts.md Tool Registry (Memory section)", name)
		}
	}
}

// ─── RiskLevel enum drift ─────────────────────────────────────────────────────

// expectedRiskLevels defines the canonical set of RiskLevel values shared between
// the tools and contracts packages. Both must stay in sync.
var expectedRiskLevels = []string{
	"safe_read",
	"safe_compute",
	"sensitive_read",
	"external_write",
	"local_write",
	"code_execution",
	"destructive",
}

func TestRiskLevelEnumNoDriftBetweenToolsAndContracts(t *testing.T) {
	toolsLevels := map[string]tools.RiskLevel{
		"safe_read":      tools.RiskLevelSafeRead,
		"safe_compute":   tools.RiskLevelSafeCompute,
		"sensitive_read": tools.RiskLevelSensitiveRead,
		"external_write": tools.RiskLevelExternalWrite,
		"local_write":    tools.RiskLevelLocalWrite,
		"code_execution": tools.RiskLevelCodeExecution,
		"destructive":    tools.RiskLevelDestructive,
	}

	contractsLevels := map[string]contracts.RiskLevel{
		"safe_read":      contracts.RiskLevelSafeRead,
		"safe_compute":   contracts.RiskLevelSafeCompute,
		"sensitive_read": contracts.RiskLevelSensitiveRead,
		"external_write": contracts.RiskLevelExternalWrite,
		"local_write":    contracts.RiskLevelLocalWrite,
		"code_execution": contracts.RiskLevelCodeExecution,
		"destructive":    contracts.RiskLevelDestructive,
	}

	for _, key := range expectedRiskLevels {
		toolVal, ok := toolsLevels[key]
		if !ok {
			t.Errorf("contract drift detected: RiskLevel %q missing from tools package", key)
			continue
		}
		contractVal, ok := contractsLevels[key]
		if !ok {
			t.Errorf("contract drift detected: RiskLevel %q missing from contracts package", key)
			continue
		}
		if string(toolVal) != string(contractVal) {
			t.Errorf("contract drift detected: RiskLevel %q: tools=%q contracts=%q", key, toolVal, contractVal)
		}
	}

	// Also verify counts match to catch additions in one package without the other.
	if len(toolsLevels) != len(expectedRiskLevels) {
		t.Errorf("tools package has %d RiskLevel values; expected %d — update expectedRiskLevels", len(toolsLevels), len(expectedRiskLevels))
	}
	if len(contractsLevels) != len(expectedRiskLevels) {
		t.Errorf("contracts package has %d RiskLevel values; expected %d — update expectedRiskLevels", len(contractsLevels), len(expectedRiskLevels))
	}
}

// ─── Filesystem tool metadata drift ──────────────────────────────────────────

// expectedFilesystemToolMeta defines the committed metadata contract for each
// filesystem tool. Changing any of these values requires a deliberate contract update.
type toolMetaFixture struct {
	name             string
	capability       tools.Capability
	riskLevel        tools.RiskLevel
	requiresApproval bool
}

var filesystemToolFixtures = []toolMetaFixture{
	{
		name:             filesystem.ToolNameListDir,
		capability:       tools.CapabilityReadOnly,
		riskLevel:        tools.RiskLevelSafeRead,
		requiresApproval: false,
	},
	{
		name:             filesystem.ToolNameReadFile,
		capability:       tools.CapabilityReadOnly,
		riskLevel:        tools.RiskLevelSafeRead,
		requiresApproval: false,
	},
	{
		name:             filesystem.ToolNameFileInfo,
		capability:       tools.CapabilityReadOnly,
		riskLevel:        tools.RiskLevelSafeRead,
		requiresApproval: false,
	},
	{
		name:             filesystem.ToolNameWriteFile,
		capability:       tools.CapabilityMutating,
		riskLevel:        tools.RiskLevelLocalWrite,
		requiresApproval: true, // MUST always require approval — HITL invariant
	},
}

func TestFilesystemToolMetadataNoDrift(t *testing.T) {
	registry := tools.NewToolRegistry()
	// Register with empty Config so PathGuard allows all paths (test mode)
	if err := filesystem.RegisterTools(registry, filesystem.Config{}); err != nil {
		t.Fatalf("RegisterTools: %v", err)
	}

	for _, fix := range filesystemToolFixtures {
		fix := fix
		t.Run(fix.name, func(t *testing.T) {
			def, ok := registry.GetDefinition(fix.name)
			if !ok {
				t.Fatalf("contract drift detected: tool %q not found in registry", fix.name)
			}
			if def.Capability != fix.capability {
				t.Errorf("contract drift detected: %s.Capability = %q, want %q", fix.name, def.Capability, fix.capability)
			}
			if def.RiskLevel != fix.riskLevel {
				t.Errorf("contract drift detected: %s.RiskLevel = %q, want %q", fix.name, def.RiskLevel, fix.riskLevel)
			}
			if def.RequiresApproval != fix.requiresApproval {
				t.Errorf("contract drift detected: %s.RequiresApproval = %v, want %v", fix.name, def.RequiresApproval, fix.requiresApproval)
			}
			if def.Group != "filesystem" {
				t.Errorf("contract drift detected: %s.Group = %q, want filesystem", fix.name, def.Group)
			}
		})
	}
}

// ─── Web tool metadata drift ──────────────────────────────────────────────────

var webToolFixtures = []toolMetaFixture{
	{
		name:             web.ToolNameSearch,
		capability:       tools.CapabilityReadOnly,
		riskLevel:        tools.RiskLevelSafeRead,
		requiresApproval: false,
	},
	{
		name:             web.ToolNameFetch,
		capability:       tools.CapabilityReadOnly,
		riskLevel:        tools.RiskLevelSafeRead,
		requiresApproval: false,
	},
}

func TestWebToolMetadataNoDrift(t *testing.T) {
	registry := tools.NewToolRegistry()
	// nil service — we only check metadata, not execution
	if err := web.RegisterTools(registry, nil); err != nil {
		t.Fatalf("RegisterTools: %v", err)
	}

	for _, fix := range webToolFixtures {
		fix := fix
		t.Run(fix.name, func(t *testing.T) {
			def, ok := registry.GetDefinition(fix.name)
			if !ok {
				t.Fatalf("contract drift detected: tool %q not found in registry", fix.name)
			}
			if def.Capability != fix.capability {
				t.Errorf("contract drift detected: %s.Capability = %q, want %q", fix.name, def.Capability, fix.capability)
			}
			if def.RiskLevel != fix.riskLevel {
				t.Errorf("contract drift detected: %s.RiskLevel = %q, want %q", fix.name, def.RiskLevel, fix.riskLevel)
			}
			if def.RequiresApproval != fix.requiresApproval {
				t.Errorf("contract drift detected: %s.RequiresApproval = %v, want %v", fix.name, def.RequiresApproval, fix.requiresApproval)
			}
			if def.Group != "web" {
				t.Errorf("contract drift detected: %s.Group = %q, want web", fix.name, def.Group)
			}
		})
	}
}

// ─── Built-in tool metadata drift ────────────────────────────────────────────

var builtinToolFixtures = []toolMetaFixture{
	{
		name:             "calculator",
		capability:       tools.CapabilityReadOnly,
		riskLevel:        tools.RiskLevelSafeCompute,
		requiresApproval: false,
	},
	{
		name:             "get_current_time",
		capability:       tools.CapabilityReadOnly,
		riskLevel:        tools.RiskLevelSafeRead,
		requiresApproval: false,
	},
}

func TestBuiltinToolMetadataNoDrift(t *testing.T) {
	registry := tools.NewToolRegistry()
	if err := tools.RegisterBuiltInTools(registry); err != nil {
		t.Fatalf("RegisterBuiltInTools: %v", err)
	}

	for _, fix := range builtinToolFixtures {
		fix := fix
		t.Run(fix.name, func(t *testing.T) {
			def, ok := registry.GetDefinition(fix.name)
			if !ok {
				t.Fatalf("contract drift detected: tool %q not found in registry", fix.name)
			}
			if def.Capability != fix.capability {
				t.Errorf("contract drift detected: %s.Capability = %q, want %q", fix.name, def.Capability, fix.capability)
			}
			if def.RiskLevel != fix.riskLevel {
				t.Errorf("contract drift detected: %s.RiskLevel = %q, want %q", fix.name, def.RiskLevel, fix.riskLevel)
			}
			if def.RequiresApproval != fix.requiresApproval {
				t.Errorf("contract drift detected: %s.RequiresApproval = %v, want %v", fix.name, def.RequiresApproval, fix.requiresApproval)
			}
		})
	}
}

// ─── Google Workspace tool metadata drift ─────────────────────────────────────

func TestGoogleWorkspaceToolMetadataNoDrift(t *testing.T) {
	registry := tools.NewToolRegistry()
	registerers := []struct {
		name string
		fn   func(*tools.ToolRegistry) error
	}{
		{name: "calendar", fn: func(r *tools.ToolRegistry) error { return calendar.RegisterTools(r, nil) }},
		{name: "chat", fn: func(r *tools.ToolRegistry) error { return chat.RegisterTools(r, nil) }},
		{name: "docs", fn: func(r *tools.ToolRegistry) error { return docs.RegisterTools(r, nil) }},
		{name: "drive", fn: func(r *tools.ToolRegistry) error { return drive.RegisterTools(r, nil, nil) }},
		{name: "gmail", fn: func(r *tools.ToolRegistry) error { return gmail.RegisterTools(r, nil) }},
		{name: "people", fn: func(r *tools.ToolRegistry) error { return people.RegisterTools(r, nil) }},
		{name: "sheets", fn: func(r *tools.ToolRegistry) error { return sheets.RegisterTools(r, nil) }},
	}
	for _, registerer := range registerers {
		if err := registerer.fn(registry); err != nil {
			t.Fatalf("register %s tools: %v", registerer.name, err)
		}
	}

	expected := map[string]toolMetaFixture{}
	addGoogleWorkspaceFixtures(expected, calendar.RegistryEntries)
	addGoogleWorkspaceFixtures(expected, chat.RegistryEntries)
	addGoogleWorkspaceFixtures(expected, docs.RegistryEntries)
	addGoogleWorkspaceFixtures(expected, drive.RegistryEntries)
	addGoogleWorkspaceFixtures(expected, gmail.RegistryEntries)
	addGoogleWorkspaceFixtures(expected, people.RegistryEntries)
	addGoogleWorkspaceFixtures(expected, sheets.RegistryEntries)

	for name, fix := range expected {
		fix := fix
		t.Run(name, func(t *testing.T) {
			def, ok := registry.GetDefinition(fix.name)
			if !ok {
				t.Fatalf("contract drift detected: tool %q not found in registry", fix.name)
			}
			if def.Capability != fix.capability {
				t.Errorf("contract drift detected: %s.Capability = %q, want %q", fix.name, def.Capability, fix.capability)
			}
			if def.RiskLevel != fix.riskLevel {
				t.Errorf("contract drift detected: %s.RiskLevel = %q, want %q", fix.name, def.RiskLevel, fix.riskLevel)
			}
			if def.RequiresApproval != fix.requiresApproval {
				t.Errorf("contract drift detected: %s.RequiresApproval = %v, want %v", fix.name, def.RequiresApproval, fix.requiresApproval)
			}
			if def.Group != "google_workspace" {
				t.Errorf("contract drift detected: %s.Group = %q, want google_workspace", fix.name, def.Group)
			}
			if def.RiskLevel == tools.RiskLevelDestructive && !def.RequiresApproval {
				t.Errorf("contract drift detected: destructive tool %s must require approval", fix.name)
			}
			if def.Capability == tools.CapabilityMutating && !def.RequiresApproval {
				t.Errorf("contract drift detected: mutating tool %s must require approval", fix.name)
			}
		})
	}
}

func TestDriveDocsSheetsReadFirstAndWriteToolsRequireHITL(t *testing.T) {
	registry := tools.NewToolRegistry()
	for _, registerer := range []func(*tools.ToolRegistry) error{
		func(r *tools.ToolRegistry) error { return drive.RegisterTools(r, nil, nil) },
		func(r *tools.ToolRegistry) error { return docs.RegisterTools(r, nil) },
		func(r *tools.ToolRegistry) error { return sheets.RegisterTools(r, nil) },
	} {
		if err := registerer(registry); err != nil {
			t.Fatalf("register Drive/Docs/Sheets tool: %v", err)
		}
	}
	policy := policies.NewToolPolicy()
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)

	readTools := []string{
		drive.ToolNameListFiles,
		drive.ToolNameGetFile,
		drive.ToolNameExportFile,
		drive.ToolNameDownloadFile,
		drive.ToolNameListPermissions,
		docs.ToolNameGetDocument,
		sheets.ToolNameGetSpreadsheet,
		sheets.ToolNameReadValues,
		sheets.ToolNameBatchGetValues,
	}
	for _, name := range readTools {
		name := name
		t.Run("read_first/"+name, func(t *testing.T) {
			def, ok := registry.GetDefinition(name)
			if !ok {
				t.Fatalf("tool %q not registered", name)
			}
			if def.Capability != tools.CapabilityReadOnly {
				t.Fatalf("%s capability = %s, want read_only", name, def.Capability)
			}
			wantRisk := tools.RiskLevelSafeRead
			wantApproval := false
			if name == docs.ToolNameGetDocument || name == sheets.ToolNameReadValues || name == sheets.ToolNameBatchGetValues {
				wantRisk = tools.RiskLevelSensitiveRead
				wantApproval = true
			}
			if def.RiskLevel != wantRisk {
				t.Fatalf("%s risk = %s, want %s", name, def.RiskLevel, wantRisk)
			}
			if def.RequiresApproval != wantApproval {
				t.Fatalf("%s requires approval = %v, want %v", name, def.RequiresApproval, wantApproval)
			}
			decision := policy.DecideToolCall("call_"+safeTestName(name), def, true, now)
			wantDecision := contracts.RiskDecisionAllow
			if wantRisk == tools.RiskLevelSensitiveRead {
				wantDecision = contracts.RiskDecisionRequiresApproval
			}
			if decision.Decision != wantDecision {
				t.Fatalf("%s policy decision = %s, want %s", name, decision.Decision, wantDecision)
			}
		})
	}

	writeTools := []string{
		drive.ToolNameCreateFolder,
		drive.ToolNameCreateFile,
		drive.ToolNameUploadFile,
		drive.ToolNameUpdateFileMetadata,
		drive.ToolNameShareFile,
		drive.ToolNameRevokePermission,
		drive.ToolNameMoveFile,
		drive.ToolNameMoveFiles,
		drive.ToolNameTrashFile,
		drive.ToolNameUntrashFile,
		docs.ToolNameCreateDocument,
		docs.ToolNameAppendText,
		docs.ToolNameReplaceText,
		docs.ToolNameInsertText,
		docs.ToolNameDeleteContent,
		sheets.ToolNameCreateSpreadsheet,
		sheets.ToolNameUpdateValues,
		sheets.ToolNameBatchUpdateValues,
		sheets.ToolNameAppendValues,
		sheets.ToolNameClearValues,
		sheets.ToolNameAddSheet,
		sheets.ToolNameRenameSheet,
		sheets.ToolNameDeleteSheet,
		sheets.ToolNameDuplicateSheet,
	}
	for _, name := range writeTools {
		name := name
		t.Run("hitl_write/"+name, func(t *testing.T) {
			def, ok := registry.GetDefinition(name)
			if !ok {
				t.Fatalf("tool %q not registered", name)
			}
			if def.Capability != tools.CapabilityMutating {
				t.Fatalf("%s capability = %s, want mutating", name, def.Capability)
			}
			if !def.RequiresApproval {
				t.Fatalf("%s must require HITL approval", name)
			}
			decision := policy.DecideToolCall("call_"+safeTestName(name), def, true, now)
			if decision.Decision != contracts.RiskDecisionRequiresApproval {
				t.Fatalf("%s policy decision = %s, want requires_approval", name, decision.Decision)
			}
			if !decision.RequiresApproval {
				t.Fatalf("%s policy decision must preserve RequiresApproval=true", name)
			}
		})
	}
}

func safeTestName(name string) string {
	name = strings.ReplaceAll(name, ".", "_")
	name = strings.ReplaceAll(name, "-", "_")
	return name
}

func addGoogleWorkspaceFixtures(dest map[string]toolMetaFixture, entries any) {
	values := reflect.ValueOf(entries)
	for i := 0; i < values.Len(); i++ {
		entry := values.Index(i)
		name := entry.FieldByName("Name").String()
		riskLevel := tools.RiskLevel(entry.FieldByName("DefaultRiskLevel").String())
		requiresApproval := entry.FieldByName("RequiresApproval").Bool()
		// Capability follows the risk level, not the approval flag: a read that
		// requires approval (e.g. gmail.getEmail / sensitive_read) is still
		// read-only, not mutating. Approval and capability are independent axes.
		capability := tools.CapabilityMutating
		switch riskLevel {
		case tools.RiskLevelSafeRead, tools.RiskLevelSensitiveRead, tools.RiskLevelSafeCompute:
			capability = tools.CapabilityReadOnly
		}
		dest[name] = toolMetaFixture{
			name:             name,
			capability:       capability,
			riskLevel:        riskLevel,
			requiresApproval: requiresApproval,
		}
	}
}

// ─── Memory tool metadata drift ──────────────────────────────────────────────

func TestMemoryToolMetadataNoDrift(t *testing.T) {
	registry := tools.NewToolRegistry()
	if err := memory.RegisterTools(registry, t.TempDir(), nil); err != nil {
		t.Fatalf("RegisterTools: %v", err)
	}

	for _, entry := range memory.RegistryEntries {
		entry := entry
		t.Run(entry.Name, func(t *testing.T) {
			def, ok := registry.GetDefinition(entry.Name)
			if !ok {
				t.Fatalf("contract drift detected: tool %q not found in registry", entry.Name)
			}
			if def.Capability != entry.Capability {
				t.Errorf("contract drift detected: %s.Capability = %q, want %q", entry.Name, def.Capability, entry.Capability)
			}
			if def.RiskLevel != entry.RiskLevel {
				t.Errorf("contract drift detected: %s.RiskLevel = %q, want %q", entry.Name, def.RiskLevel, entry.RiskLevel)
			}
			if def.RequiresApproval != entry.RequiresApproval {
				t.Errorf("contract drift detected: %s.RequiresApproval = %v, want %v", entry.Name, def.RequiresApproval, entry.RequiresApproval)
			}
			if def.Group != "memory" {
				t.Errorf("contract drift detected: %s.Group = %q, want memory", entry.Name, def.Group)
			}
			if def.Capability == tools.CapabilityMutating && !def.RequiresApproval {
				t.Errorf("contract drift detected: mutating tool %s must require approval", entry.Name)
			}
			if def.RiskLevel == tools.RiskLevelDestructive && !def.RequiresApproval {
				t.Errorf("contract drift detected: destructive tool %s must require approval", entry.Name)
			}
		})
	}
}

// ─── ToolResult shape contract ────────────────────────────────────────────────

// TestToolResultShapeContract verifies that the fields required by the shared
// contract are present and correctly populated by actual tool executions.
// This is a "golden shape" test — it catches shape drift caused by refactors.
func TestToolResultShapeContract(t *testing.T) {
	registry := tools.NewToolRegistry()
	fixedTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if err := registry.Register(tools.NewCurrentTimeToolWithClock(func() time.Time { return fixedTime })); err != nil {
		t.Fatalf("register: %v", err)
	}

	result := registry.Execute(context.Background(), tools.ToolCall{
		ID:   "shape_test_001",
		Name: "get_current_time",
	})

	// Required fields — must always be populated regardless of tool.
	if result.ToolCallID == "" {
		t.Error("ToolResult.ToolCallID must not be empty")
	}
	if result.ToolName == "" {
		t.Error("ToolResult.ToolName must not be empty")
	}
	if !result.Success {
		t.Errorf("expected Success=true, got false; error: %#v", result.Error)
	}
	if result.ContentForLLM == "" {
		t.Error("ToolResult.ContentForLLM must not be empty on success")
	}
	if result.ContentForUser == "" {
		t.Error("ToolResult.ContentForUser must not be empty on success")
	}
	// On success, Error must be nil (matches ValidateToolResult contract).
	if result.Error != nil {
		t.Errorf("ToolResult.Error must be nil on success; got: %#v", result.Error)
	}
}

// TestToolResultErrorShapeContract verifies error results also comply with shape.
func TestToolResultErrorShapeContract(t *testing.T) {
	registry := tools.NewToolRegistry()

	result := registry.Execute(context.Background(), tools.ToolCall{
		ID:   "shape_err_001",
		Name: "nonexistent.tool",
	})

	if result.ToolCallID != "shape_err_001" {
		t.Errorf("ToolCallID must echo the call ID; got %q", result.ToolCallID)
	}
	if result.ToolName != "nonexistent.tool" {
		t.Errorf("ToolName must echo the tool name; got %q", result.ToolName)
	}
	if result.Success {
		t.Error("missing tool must return Success=false")
	}
	if result.Error == nil {
		t.Fatal("missing tool must return a non-nil Error")
	}
	if result.Error.Code != tools.ErrorToolNotFound {
		t.Errorf("expected error code %s, got %s", tools.ErrorToolNotFound, result.Error.Code)
	}
	if result.ContentForLLM == "" {
		t.Error("ToolResult.ContentForLLM must not be empty even for error results")
	}
}

