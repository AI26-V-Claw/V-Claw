package evaluation

// TestDataset represents a collection of test cases for evaluation.
type TestDataset struct {
	Metadata  DatasetMetadata `json:"metadata"`
	TestCases []TestCase      `json:"test_cases"`
}

// DatasetMetadata contains information about the dataset.
type DatasetMetadata struct {
	Version                string            `json:"version"`
	CreatedDate            string            `json:"created_date"`
	TotalSamples           int               `json:"total_samples"`
	Description            string            `json:"description"`
	TargetAccuracy         float64           `json:"target_accuracy"`
	Distribution           map[string]int    `json:"distribution"`
	ComplexityDistribution map[string]int    `json:"complexity_distribution"`
	Languages              []string          `json:"languages"`
}

// TestCase represents a single test case.
type TestCase struct {
	ID                     string                 `json:"id"`
	Input                  string                 `json:"input"`
	ExpectedIntent         string                 `json:"expected_intent"`
	ExpectedConfidenceMin  float64                `json:"expected_confidence_min,omitempty"`
	ExpectedConfidenceMax  float64                `json:"expected_confidence_max,omitempty"`
	ExpectedToolCalls      []string               `json:"expected_tool_calls,omitempty"`
	ExpectedParams         map[string]interface{} `json:"expected_params,omitempty"`
	ExpectedMissingParams  []string               `json:"expected_missing_params,omitempty"`
	ExpectedNeedsConfirm   bool                   `json:"expected_needs_confirm"`
	Complexity             string                 `json:"complexity"`
	Language               string                 `json:"language"`
	Notes                  string                 `json:"notes,omitempty"`
}
