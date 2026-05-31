package evaluation

import (
	"encoding/json"
	"fmt"
	"os"
)

// TestCase represents a single test case for intent classification
type TestCase struct {
	ID                      string                 `json:"id"`
	Input                   string                 `json:"input"`
	ExpectedIntent          string                 `json:"expected_intent"`
	ExpectedConfidenceMin   float64                `json:"expected_confidence_min,omitempty"`
	ExpectedConfidenceMax   float64                `json:"expected_confidence_max,omitempty"`
	ExpectedToolCalls       []string               `json:"expected_tool_calls"`
	ExpectedParams          map[string]interface{} `json:"expected_params,omitempty"`
	ExpectedMissingParams   []string               `json:"expected_missing_params,omitempty"`
	ExpectedNeedsConfirm    bool                   `json:"expected_needs_confirm"`
	Complexity              string                 `json:"complexity"` // simple, medium, hard
	Language                string                 `json:"language"`   // vi, en, mixed
	Notes                   string                 `json:"notes,omitempty"`
}

// TestDataset represents the complete evaluation dataset
type TestDataset struct {
	Metadata  DatasetMetadata `json:"metadata"`
	TestCases []TestCase      `json:"test_cases"`
}

// DatasetMetadata contains metadata about the dataset
type DatasetMetadata struct {
	Version               string            `json:"version"`
	CreatedDate           string            `json:"created_date"`
	TotalSamples          int               `json:"total_samples"`
	Description           string            `json:"description"`
	TargetAccuracy        float64           `json:"target_accuracy"`
	Distribution          map[string]int    `json:"distribution"`
	ComplexityDistribution map[string]int   `json:"complexity_distribution"`
	Languages             []string          `json:"languages"`
}

// GenerateGreetingCases generates greeting test cases
func GenerateGreetingCases() []TestCase {
	cases := []TestCase{
		// Vietnamese greetings
		{ID: "GREETING_016", Input: "Xin chào", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.95, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "vi"},
		{ID: "GREETING_017", Input: "Chào bạn", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.95, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "vi"},
		{ID: "GREETING_018", Input: "Chào anh", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.95, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "vi"},
		{ID: "GREETING_019", Input: "Chào chị", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.95, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "vi"},
		{ID: "GREETING_020", Input: "Chào buổi chiều", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.95, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "vi"},
		{ID: "GREETING_021", Input: "Chào buổi tối", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.95, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "vi"},
		{ID: "GREETING_022", Input: "Cảm ơn nhiều", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.95, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "vi"},
		{ID: "GREETING_023", Input: "Cảm ơn rất nhiều", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.95, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "vi"},
		{ID: "GREETING_024", Input: "Xin cảm ơn", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.95, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "vi"},
		{ID: "GREETING_025", Input: "Hẹn gặp lại", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.95, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "vi"},
		
		// English greetings
		{ID: "GREETING_026", Input: "Good afternoon", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.95, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "en"},
		{ID: "GREETING_027", Input: "Good evening", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.95, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "en"},
		{ID: "GREETING_028", Input: "Good night", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.95, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "en"},
		{ID: "GREETING_029", Input: "Hey", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.95, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "en"},
		{ID: "GREETING_030", Input: "Hi!", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.95, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "en"},
		{ID: "GREETING_031", Input: "Thank you very much", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.95, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "en"},
		{ID: "GREETING_032", Input: "Thanks a lot", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.95, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "en"},
		{ID: "GREETING_033", Input: "Bye", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.95, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "en"},
		{ID: "GREETING_034", Input: "Bye bye", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.95, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "en"},
		{ID: "GREETING_035", Input: "See you", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.95, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "en"},
		
		// Conversational
		{ID: "GREETING_036", Input: "Bạn có khỏe không?", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "vi"},
		{ID: "GREETING_037", Input: "Dạo này thế nào?", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.85, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "medium", Language: "vi"},
		{ID: "GREETING_038", Input: "How's it going?", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "en"},
		{ID: "GREETING_039", Input: "What's up?", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "en"},
		{ID: "GREETING_040", Input: "How have you been?", ExpectedIntent: "GREETING", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "en"},
	}
	
	return cases
}

// GenerateReadInfoCases generates read info test cases
func GenerateReadInfoCases() []TestCase {
	cases := []TestCase{
		// File reading - Vietnamese
		{ID: "READ_INFO_016", Input: "Mở file data.txt", ExpectedIntent: "READ_INFO", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{"read_file"}, ExpectedParams: map[string]interface{}{"path": "data.txt"}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "vi"},
		{ID: "READ_INFO_017", Input: "Xem file log.txt", ExpectedIntent: "READ_INFO", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{"read_file"}, ExpectedParams: map[string]interface{}{"path": "log.txt"}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "vi"},
		{ID: "READ_INFO_018", Input: "Hiển thị nội dung file settings.json", ExpectedIntent: "READ_INFO", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{"read_file"}, ExpectedParams: map[string]interface{}{"path": "settings.json"}, ExpectedNeedsConfirm: false, Complexity: "medium", Language: "vi"},
		{ID: "READ_INFO_019", Input: "Cho tôi biết file .env có gì", ExpectedIntent: "READ_INFO", ExpectedConfidenceMin: 0.85, ExpectedToolCalls: []string{"read_file"}, ExpectedParams: map[string]interface{}{"path": ".env"}, ExpectedNeedsConfirm: false, Complexity: "medium", Language: "vi"},
		{ID: "READ_INFO_020", Input: "Đọc file trong thư mục /var/log/app.log", ExpectedIntent: "READ_INFO", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{"read_file"}, ExpectedParams: map[string]interface{}{"path": "/var/log/app.log"}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "vi"},
		
		// File reading - English
		{ID: "READ_INFO_021", Input: "Open file data.csv", ExpectedIntent: "READ_INFO", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{"read_file"}, ExpectedParams: map[string]interface{}{"path": "data.csv"}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "en"},
		{ID: "READ_INFO_022", Input: "View file report.pdf", ExpectedIntent: "READ_INFO", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{"read_file"}, ExpectedParams: map[string]interface{}{"path": "report.pdf"}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "en"},
		{ID: "READ_INFO_023", Input: "Display content of index.html", ExpectedIntent: "READ_INFO", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{"read_file"}, ExpectedParams: map[string]interface{}{"path": "index.html"}, ExpectedNeedsConfirm: false, Complexity: "medium", Language: "en"},
		{ID: "READ_INFO_024", Input: "What's in the Dockerfile?", ExpectedIntent: "READ_INFO", ExpectedConfidenceMin: 0.85, ExpectedToolCalls: []string{"read_file"}, ExpectedParams: map[string]interface{}{"path": "Dockerfile"}, ExpectedNeedsConfirm: false, Complexity: "medium", Language: "en"},
		{ID: "READ_INFO_025", Input: "Cat /etc/hosts", ExpectedIntent: "READ_INFO", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{"read_file"}, ExpectedParams: map[string]interface{}{"path": "/etc/hosts"}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "en"},
		
		// Directory listing - Vietnamese
		{ID: "READ_INFO_026", Input: "Liệt kê file trong thư mục hiện tại", ExpectedIntent: "READ_INFO", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{"list_directory"}, ExpectedParams: map[string]interface{}{"path": "."}, ExpectedNeedsConfirm: false, Complexity: "medium", Language: "vi"},
		{ID: "READ_INFO_027", Input: "Xem có gì trong /home", ExpectedIntent: "READ_INFO", ExpectedConfidenceMin: 0.85, ExpectedToolCalls: []string{"list_directory"}, ExpectedParams: map[string]interface{}{"path": "/home"}, ExpectedNeedsConfirm: false, Complexity: "medium", Language: "vi"},
		{ID: "READ_INFO_028", Input: "Cho tôi xem các file trong src", ExpectedIntent: "READ_INFO", ExpectedConfidenceMin: 0.85, ExpectedToolCalls: []string{"list_directory"}, ExpectedParams: map[string]interface{}{"path": "src"}, ExpectedNeedsConfirm: false, Complexity: "medium", Language: "vi"},
		
		// Directory listing - English
		{ID: "READ_INFO_029", Input: "List files in current directory", ExpectedIntent: "READ_INFO", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{"list_directory"}, ExpectedParams: map[string]interface{}{"path": "."}, ExpectedNeedsConfirm: false, Complexity: "medium", Language: "en"},
		{ID: "READ_INFO_030", Input: "Show me what's in /var/www", ExpectedIntent: "READ_INFO", ExpectedConfidenceMin: 0.85, ExpectedToolCalls: []string{"list_directory"}, ExpectedParams: map[string]interface{}{"path": "/var/www"}, ExpectedNeedsConfirm: false, Complexity: "medium", Language: "en"},
		{ID: "READ_INFO_031", Input: "ls /usr/local/bin", ExpectedIntent: "READ_INFO", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{"list_directory"}, ExpectedParams: map[string]interface{}{"path": "/usr/local/bin"}, ExpectedNeedsConfirm: false, Complexity: "simple", Language: "en"},
		
		// Web search - Vietnamese
		{ID: "READ_INFO_032", Input: "Tìm kiếm về Go programming", ExpectedIntent: "READ_INFO", ExpectedConfidenceMin: 0.85, ExpectedToolCalls: []string{"web_search"}, ExpectedParams: map[string]interface{}{"query": "Go programming"}, ExpectedNeedsConfirm: false, Complexity: "medium", Language: "vi"},
		{ID: "READ_INFO_033", Input: "Tra cứu thông tin về Docker", ExpectedIntent: "READ_INFO", ExpectedConfidenceMin: 0.85, ExpectedToolCalls: []string{"web_search"}, ExpectedParams: map[string]interface{}{"query": "Docker"}, ExpectedNeedsConfirm: false, Complexity: "medium", Language: "vi"},
		{ID: "READ_INFO_034", Input: "Google về Kubernetes best practices", ExpectedIntent: "READ_INFO", ExpectedConfidenceMin: 0.85, ExpectedToolCalls: []string{"web_search"}, ExpectedParams: map[string]interface{}{"query": "Kubernetes best practices"}, ExpectedNeedsConfirm: false, Complexity: "medium", Language: "vi"},
		
		// Web search - English
		{ID: "READ_INFO_035", Input: "Search for TypeScript tutorial", ExpectedIntent: "READ_INFO", ExpectedConfidenceMin: 0.85, ExpectedToolCalls: []string{"web_search"}, ExpectedParams: map[string]interface{}{"query": "TypeScript tutorial"}, ExpectedNeedsConfirm: false, Complexity: "medium", Language: "en"},
		{ID: "READ_INFO_036", Input: "Look up information about GraphQL", ExpectedIntent: "READ_INFO", ExpectedConfidenceMin: 0.85, ExpectedToolCalls: []string{"web_search"}, ExpectedParams: map[string]interface{}{"query": "GraphQL"}, ExpectedNeedsConfirm: false, Complexity: "medium", Language: "en"},
		{ID: "READ_INFO_037", Input: "Find documentation for Redis", ExpectedIntent: "READ_INFO", ExpectedConfidenceMin: 0.85, ExpectedToolCalls: []string{"web_search"}, ExpectedParams: map[string]interface{}{"query": "Redis documentation"}, ExpectedNeedsConfirm: false, Complexity: "medium", Language: "en"},
	}
	
	return cases
}

// GenerateDangerousActionCases generates dangerous action test cases
func GenerateDangerousActionCases() []TestCase {
	cases := []TestCase{
		// File deletion - Vietnamese
		{ID: "DANGEROUS_ACTION_016", Input: "Xóa file temp.txt", ExpectedIntent: "DANGEROUS_ACTION", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{"delete_file"}, ExpectedParams: map[string]interface{}{"path": "temp.txt"}, ExpectedMissingParams: []string{"confirm"}, ExpectedNeedsConfirm: true, Complexity: "simple", Language: "vi"},
		{ID: "DANGEROUS_ACTION_017", Input: "Xóa file /tmp/cache.dat", ExpectedIntent: "DANGEROUS_ACTION", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{"delete_file"}, ExpectedParams: map[string]interface{}{"path": "/tmp/cache.dat"}, ExpectedMissingParams: []string{"confirm"}, ExpectedNeedsConfirm: true, Complexity: "simple", Language: "vi"},
		{ID: "DANGEROUS_ACTION_018", Input: "Xóa bỏ file log.txt", ExpectedIntent: "DANGEROUS_ACTION", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{"delete_file"}, ExpectedParams: map[string]interface{}{"path": "log.txt"}, ExpectedMissingParams: []string{"confirm"}, ExpectedNeedsConfirm: true, Complexity: "simple", Language: "vi"},
		{ID: "DANGEROUS_ACTION_019", Input: "Xóa file", ExpectedIntent: "DANGEROUS_ACTION", ExpectedConfidenceMin: 0.70, ExpectedToolCalls: []string{"delete_file"}, ExpectedParams: map[string]interface{}{}, ExpectedMissingParams: []string{"path", "confirm"}, ExpectedNeedsConfirm: true, Complexity: "hard", Language: "vi", Notes: "Missing file path"},
		{ID: "DANGEROUS_ACTION_020", Input: "Xóa file này đi", ExpectedIntent: "DANGEROUS_ACTION", ExpectedConfidenceMin: 0.70, ExpectedToolCalls: []string{"delete_file"}, ExpectedParams: map[string]interface{}{}, ExpectedMissingParams: []string{"path", "confirm"}, ExpectedNeedsConfirm: true, Complexity: "hard", Language: "vi", Notes: "Vague reference 'này' (this)"},
		
		// File deletion - English
		{ID: "DANGEROUS_ACTION_021", Input: "Delete file backup.zip", ExpectedIntent: "DANGEROUS_ACTION", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{"delete_file"}, ExpectedParams: map[string]interface{}{"path": "backup.zip"}, ExpectedMissingParams: []string{"confirm"}, ExpectedNeedsConfirm: true, Complexity: "simple", Language: "en"},
		{ID: "DANGEROUS_ACTION_022", Input: "Remove file /var/tmp/old.log", ExpectedIntent: "DANGEROUS_ACTION", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{"delete_file"}, ExpectedParams: map[string]interface{}{"path": "/var/tmp/old.log"}, ExpectedMissingParams: []string{"confirm"}, ExpectedNeedsConfirm: true, Complexity: "simple", Language: "en"},
		{ID: "DANGEROUS_ACTION_023", Input: "Delete the file", ExpectedIntent: "DANGEROUS_ACTION", ExpectedConfidenceMin: 0.70, ExpectedToolCalls: []string{"delete_file"}, ExpectedParams: map[string]interface{}{}, ExpectedMissingParams: []string{"path", "confirm"}, ExpectedNeedsConfirm: true, Complexity: "hard", Language: "en", Notes: "Missing file path"},
		{ID: "DANGEROUS_ACTION_024", Input: "Remove this file", ExpectedIntent: "DANGEROUS_ACTION", ExpectedConfidenceMin: 0.70, ExpectedToolCalls: []string{"delete_file"}, ExpectedParams: map[string]interface{}{}, ExpectedMissingParams: []string{"path", "confirm"}, ExpectedNeedsConfirm: true, Complexity: "hard", Language: "en", Notes: "Vague reference 'this'"},
		{ID: "DANGEROUS_ACTION_025", Input: "rm important.txt", ExpectedIntent: "DANGEROUS_ACTION", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{"delete_file"}, ExpectedParams: map[string]interface{}{"path": "important.txt"}, ExpectedMissingParams: []string{"confirm"}, ExpectedNeedsConfirm: true, Complexity: "simple", Language: "en"},
		
		// Command execution - Vietnamese
		{ID: "DANGEROUS_ACTION_026", Input: "Chạy lệnh ls -la", ExpectedIntent: "DANGEROUS_ACTION", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{"exec"}, ExpectedParams: map[string]interface{}{"command": "ls -la"}, ExpectedMissingParams: []string{"confirm"}, ExpectedNeedsConfirm: true, Complexity: "simple", Language: "vi"},
		{ID: "DANGEROUS_ACTION_027", Input: "Thực thi npm test", ExpectedIntent: "DANGEROUS_ACTION", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{"exec"}, ExpectedParams: map[string]interface{}{"command": "npm test"}, ExpectedMissingParams: []string{"confirm"}, ExpectedNeedsConfirm: true, Complexity: "simple", Language: "vi"},
		{ID: "DANGEROUS_ACTION_028", Input: "Chạy docker-compose up", ExpectedIntent: "DANGEROUS_ACTION", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{"exec"}, ExpectedParams: map[string]interface{}{"command": "docker-compose up"}, ExpectedMissingParams: []string{"confirm"}, ExpectedNeedsConfirm: true, Complexity: "simple", Language: "vi"},
		
		// Command execution - English
		{ID: "DANGEROUS_ACTION_029", Input: "Run command git pull", ExpectedIntent: "DANGEROUS_ACTION", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{"exec"}, ExpectedParams: map[string]interface{}{"command": "git pull"}, ExpectedMissingParams: []string{"confirm"}, ExpectedNeedsConfirm: true, Complexity: "simple", Language: "en"},
		{ID: "DANGEROUS_ACTION_030", Input: "Execute make build", ExpectedIntent: "DANGEROUS_ACTION", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{"exec"}, ExpectedParams: map[string]interface{}{"command": "make build"}, ExpectedMissingParams: []string{"confirm"}, ExpectedNeedsConfirm: true, Complexity: "simple", Language: "en"},
		{ID: "DANGEROUS_ACTION_031", Input: "Run go test ./...", ExpectedIntent: "DANGEROUS_ACTION", ExpectedConfidenceMin: 0.90, ExpectedToolCalls: []string{"exec"}, ExpectedParams: map[string]interface{}{"command": "go test ./..."}, ExpectedMissingParams: []string{"confirm"}, ExpectedNeedsConfirm: true, Complexity: "simple", Language: "en"},
	}
	
	return cases
}

// GenerateCompositeActionCases generates composite action test cases
func GenerateCompositeActionCases() []TestCase {
	cases := []TestCase{
		{ID: "COMPOSITE_ACTION_007", Input: "Tìm file *.tmp và xóa", ExpectedIntent: "COMPOSITE_ACTION", ExpectedConfidenceMin: 0.85, ExpectedToolCalls: []string{"find_files", "delete_files"}, ExpectedParams: map[string]interface{}{"pattern": "*.tmp"}, ExpectedMissingParams: []string{"confirm"}, ExpectedNeedsConfirm: true, Complexity: "medium", Language: "vi"},
		{ID: "COMPOSITE_ACTION_008", Input: "Find *.bak files and delete them", ExpectedIntent: "COMPOSITE_ACTION", ExpectedConfidenceMin: 0.85, ExpectedToolCalls: []string{"find_files", "delete_files"}, ExpectedParams: map[string]interface{}{"pattern": "*.bak"}, ExpectedMissingParams: []string{"confirm"}, ExpectedNeedsConfirm: true, Complexity: "medium", Language: "en"},
		{ID: "COMPOSITE_ACTION_009", Input: "Backup database rồi restart service", ExpectedIntent: "COMPOSITE_ACTION", ExpectedConfidenceMin: 0.80, ExpectedToolCalls: []string{"backup_database", "restart_service"}, ExpectedParams: map[string]interface{}{}, ExpectedMissingParams: []string{"database_name", "service_name", "confirm"}, ExpectedNeedsConfirm: true, Complexity: "hard", Language: "vi"},
		{ID: "COMPOSITE_ACTION_010", Input: "Backup database and restart service", ExpectedIntent: "COMPOSITE_ACTION", ExpectedConfidenceMin: 0.80, ExpectedToolCalls: []string{"backup_database", "restart_service"}, ExpectedParams: map[string]interface{}{}, ExpectedMissingParams: []string{"database_name", "service_name", "confirm"}, ExpectedNeedsConfirm: true, Complexity: "hard", Language: "en"},
	}
	
	return cases
}

// GenerateAllTestCases generates all test cases
func GenerateAllTestCases() TestDataset {
	allCases := []TestCase{}
	
	// Add greeting cases
	allCases = append(allCases, GenerateGreetingCases()...)
	
	// Add read info cases
	allCases = append(allCases, GenerateReadInfoCases()...)
	
	// Add dangerous action cases
	allCases = append(allCases, GenerateDangerousActionCases()...)
	
	// Add composite action cases
	allCases = append(allCases, GenerateCompositeActionCases()...)
	
	// Calculate distribution
	distribution := make(map[string]int)
	complexityDist := make(map[string]int)
	
	for _, tc := range allCases {
		distribution[tc.ExpectedIntent]++
		complexityDist[tc.Complexity]++
	}
	
	metadata := DatasetMetadata{
		Version:                "1.0",
		CreatedDate:            "2026-05-31",
		TotalSamples:           len(allCases),
		Description:            "Evaluation dataset for Intent Classification System",
		TargetAccuracy:         0.80,
		Distribution:           distribution,
		ComplexityDistribution: complexityDist,
		Languages:              []string{"vi", "en", "mixed"},
	}
	
	return TestDataset{
		Metadata:  metadata,
		TestCases: allCases,
	}
}

// SaveToFile saves the dataset to a JSON file
func SaveToFile(dataset TestDataset, filename string) error {
	data, err := json.MarshalIndent(dataset, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal dataset: %w", err)
	}
	
	err = os.WriteFile(filename, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	
	return nil
}

// LoadFromFile loads the dataset from a JSON file
func LoadFromFile(filename string) (*TestDataset, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	
	var dataset TestDataset
	err = json.Unmarshal(data, &dataset)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal dataset: %w", err)
	}
	
	return &dataset, nil
}
