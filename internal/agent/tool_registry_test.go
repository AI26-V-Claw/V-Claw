package agent

import (
	"testing"
)

func TestGetTool(t *testing.T) {
	tests := []struct {
		name      string
		toolName  string
		expectErr bool
	}{
		{
			name:      "Get read_file tool",
			toolName:  "read_file",
			expectErr: false,
		},
		{
			name:      "Get delete_file tool",
			toolName:  "delete_file",
			expectErr: false,
		},
		{
			name:      "Get non-existent tool",
			toolName:  "non_existent",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool, err := GetTool(tt.toolName)

			if tt.expectErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if tool.Name != tt.toolName {
					t.Errorf("Expected tool name %q, got %q", tt.toolName, tool.Name)
				}
			}
		})
	}
}

func TestGetToolsByCategory(t *testing.T) {
	tests := []struct {
		name         string
		category     ToolCategory
		expectedMin  int // Minimum expected tools
	}{
		{
			name:        "Get safe read tools",
			category:    ToolCategorySafeRead,
			expectedMin: 2, // read_file, list_directory, web_search
		},
		{
			name:        "Get dangerous write tools",
			category:    ToolCategoryDangerousWrite,
			expectedMin: 2, // delete_file, write_file
		},
		{
			name:        "Get execution tools",
			category:    ToolCategoryExecution,
			expectedMin: 1, // exec
		},
		{
			name:        "Get communication tools",
			category:    ToolCategoryCommunication,
			expectedMin: 1, // send_email
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := GetToolsByCategory(tt.category)

			if len(tools) < tt.expectedMin {
				t.Errorf("Expected at least %d tools in category %s, got %d",
					tt.expectedMin, tt.category, len(tools))
			}

			// Verify all tools have correct category
			for _, tool := range tools {
				if tool.Category != tt.category {
					t.Errorf("Tool %q has wrong category: expected %s, got %s",
						tool.Name, tt.category, tool.Category)
				}
			}
		})
	}
}

func TestIsDangerousTool(t *testing.T) {
	tests := []struct {
		toolName   string
		isDangerous bool
	}{
		{"read_file", false},
		{"list_directory", false},
		{"web_search", false},
		{"delete_file", true},
		{"write_file", true},
		{"exec", true},
		{"send_email", true},
		{"non_existent", false}, // Non-existent tools are not dangerous
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			result := IsDangerousTool(tt.toolName)

			if result != tt.isDangerous {
				t.Errorf("Expected IsDangerousTool(%q) = %v, got %v",
					tt.toolName, tt.isDangerous, result)
			}
		})
	}
}

func TestToolDefinition_ReadFile(t *testing.T) {
	tool, err := GetTool("read_file")
	if err != nil {
		t.Fatalf("Failed to get read_file tool: %v", err)
	}

	// Check basic properties
	if tool.Name != "read_file" {
		t.Errorf("Expected name 'read_file', got %q", tool.Name)
	}

	if tool.Category != ToolCategorySafeRead {
		t.Errorf("Expected category SAFE_READ, got %s", tool.Category)
	}

	if tool.Dangerous {
		t.Error("read_file should not be dangerous")
	}

	if tool.RequiresConfirm {
		t.Error("read_file should not require confirmation")
	}

	// Check parameters
	if len(tool.Parameters) != 1 {
		t.Errorf("Expected 1 parameter, got %d", len(tool.Parameters))
	}

	if tool.Parameters[0].Name != "path" {
		t.Errorf("Expected parameter 'path', got %q", tool.Parameters[0].Name)
	}

	if !tool.Parameters[0].Required {
		t.Error("path parameter should be required")
	}
}

func TestToolDefinition_DeleteFile(t *testing.T) {
	tool, err := GetTool("delete_file")
	if err != nil {
		t.Fatalf("Failed to get delete_file tool: %v", err)
	}

	// Check basic properties
	if tool.Name != "delete_file" {
		t.Errorf("Expected name 'delete_file', got %q", tool.Name)
	}

	if tool.Category != ToolCategoryDangerousWrite {
		t.Errorf("Expected category DANGEROUS_WRITE, got %s", tool.Category)
	}

	if !tool.Dangerous {
		t.Error("delete_file should be dangerous")
	}

	if !tool.RequiresConfirm {
		t.Error("delete_file should require confirmation")
	}

	// Check parameters
	if len(tool.Parameters) != 2 {
		t.Errorf("Expected 2 parameters, got %d", len(tool.Parameters))
	}

	// Check path parameter
	pathParam := tool.Parameters[0]
	if pathParam.Name != "path" {
		t.Errorf("Expected first parameter 'path', got %q", pathParam.Name)
	}
	if !pathParam.Required {
		t.Error("path parameter should be required")
	}

	// Check confirm parameter
	confirmParam := tool.Parameters[1]
	if confirmParam.Name != "confirm" {
		t.Errorf("Expected second parameter 'confirm', got %q", confirmParam.Name)
	}
	if !confirmParam.Required {
		t.Error("confirm parameter should be required")
	}
}

func TestToolDefinition_SendEmail(t *testing.T) {
	tool, err := GetTool("send_email")
	if err != nil {
		t.Fatalf("Failed to get send_email tool: %v", err)
	}

	// Check it's dangerous
	if !tool.Dangerous {
		t.Error("send_email should be dangerous")
	}

	if !tool.RequiresConfirm {
		t.Error("send_email should require confirmation")
	}

	// Check parameters
	expectedParams := []string{"to", "subject", "body"}
	if len(tool.Parameters) != len(expectedParams) {
		t.Errorf("Expected %d parameters, got %d", len(expectedParams), len(tool.Parameters))
	}

	for i, expectedName := range expectedParams {
		if i >= len(tool.Parameters) {
			break
		}
		if tool.Parameters[i].Name != expectedName {
			t.Errorf("Expected parameter %d to be %q, got %q",
				i, expectedName, tool.Parameters[i].Name)
		}
		if !tool.Parameters[i].Required {
			t.Errorf("Parameter %q should be required", expectedName)
		}
	}
}

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name      string
		email     interface{}
		expectErr bool
	}{
		{
			name:      "Valid email",
			email:     "user@example.com",
			expectErr: false,
		},
		{
			name:      "Valid email with subdomain",
			email:     "user@mail.example.com",
			expectErr: false,
		},
		{
			name:      "Valid email with plus",
			email:     "user+tag@example.com",
			expectErr: false,
		},
		{
			name:      "Invalid email - no @",
			email:     "userexample.com",
			expectErr: true,
		},
		{
			name:      "Invalid email - no domain",
			email:     "user@",
			expectErr: true,
		},
		{
			name:      "Invalid email - no TLD",
			email:     "user@example",
			expectErr: true,
		},
		{
			name:      "Invalid type - not string",
			email:     123,
			expectErr: true,
		},
		{
			name:      "Empty string",
			email:     "",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEmail(tt.email)

			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected error for email %v, but got none", tt.email)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for email %v: %v", tt.email, err)
				}
			}
		})
	}
}

func TestValidatePath(t *testing.T) {
	tests := []struct {
		name      string
		path      interface{}
		expectErr bool
	}{
		{
			name:      "Valid absolute path",
			path:      "/home/user/file.txt",
			expectErr: false,
		},
		{
			name:      "Valid relative path",
			path:      "config/settings.json",
			expectErr: false,
		},
		{
			name:      "Valid path with spaces",
			path:      "/home/user/my file.txt",
			expectErr: false,
		},
		{
			name:      "Invalid - directory traversal",
			path:      "../../../etc/passwd",
			expectErr: true,
		},
		{
			name:      "Invalid - home expansion",
			path:      "~/file.txt",
			expectErr: true,
		},
		{
			name:      "Invalid - variable expansion",
			path:      "$HOME/file.txt",
			expectErr: true,
		},
		{
			name:      "Invalid - pipe",
			path:      "file.txt | cat",
			expectErr: true,
		},
		{
			name:      "Invalid - command separator",
			path:      "file.txt; rm -rf /",
			expectErr: true,
		},
		{
			name:      "Invalid - redirection",
			path:      "file.txt > output",
			expectErr: true,
		},
		{
			name:      "Invalid - background execution",
			path:      "file.txt &",
			expectErr: true,
		},
		{
			name:      "Invalid type - not string",
			path:      123,
			expectErr: true,
		},
		{
			name:      "Empty string",
			path:      "",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePath(tt.path)

			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected error for path %v, but got none", tt.path)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for path %v: %v", tt.path, err)
				}
			}
		})
	}
}

func TestToolRegistry_AllToolsHaveRequiredFields(t *testing.T) {
	for name, tool := range ToolRegistry {
		t.Run(name, func(t *testing.T) {
			// Check name matches key
			if tool.Name != name {
				t.Errorf("Tool key %q doesn't match tool name %q", name, tool.Name)
			}

			// Check description exists
			if tool.Description == "" {
				t.Error("Tool description is empty")
			}

			// Check category is valid
			validCategories := []ToolCategory{
				ToolCategorySafeRead,
				ToolCategoryDangerousWrite,
				ToolCategoryExecution,
				ToolCategoryCommunication,
			}
			validCategory := false
			for _, cat := range validCategories {
				if tool.Category == cat {
					validCategory = true
					break
				}
			}
			if !validCategory {
				t.Errorf("Invalid category: %s", tool.Category)
			}

			// Check timeout is reasonable
			if tool.Timeout <= 0 || tool.Timeout > 300 {
				t.Errorf("Unreasonable timeout: %d seconds", tool.Timeout)
			}

			// Check dangerous tools require confirmation
			if tool.Dangerous && !tool.RequiresConfirm {
				t.Error("Dangerous tool should require confirmation")
			}

			// Check parameters
			for i, param := range tool.Parameters {
				if param.Name == "" {
					t.Errorf("Parameter %d has empty name", i)
				}
				if param.Type == "" {
					t.Errorf("Parameter %q has empty type", param.Name)
				}
				if param.Description == "" {
					t.Errorf("Parameter %q has empty description", param.Name)
				}
			}
		})
	}
}

func BenchmarkGetTool(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = GetTool("read_file")
	}
}

func BenchmarkGetToolsByCategory(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = GetToolsByCategory(ToolCategorySafeRead)
	}
}

func BenchmarkIsDangerousTool(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = IsDangerousTool("delete_file")
	}
}

func BenchmarkValidateEmail(b *testing.B) {
	email := "user@example.com"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateEmail(email)
	}
}

func BenchmarkValidatePath(b *testing.B) {
	path := "/home/user/file.txt"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidatePath(path)
	}
}
