package agent

// ConfidenceConfig defines confidence thresholds for different intent types
type ConfidenceConfig struct {
	GreetingMinConfidence        float64 // 0.0 (always accept)
	ReadInfoMinConfidence        float64 // 0.70
	DangerousActionMinConfidence float64 // 0.90
	CompositeActionMinConfidence float64 // 0.85

	// When confidence is in this range, show multiple choice
	AmbiguousRangeLow  float64 // 0.60
	AmbiguousRangeHigh float64 // 0.85
}

// DefaultConfidenceConfig provides default confidence thresholds
var DefaultConfidenceConfig = ConfidenceConfig{
	GreetingMinConfidence:        0.0,
	ReadInfoMinConfidence:        0.70,
	DangerousActionMinConfidence: 0.90,
	CompositeActionMinConfidence: 0.85,
	AmbiguousRangeLow:            0.60,
	AmbiguousRangeHigh:           0.85,
}

// GetMinConfidence returns the minimum confidence threshold for an intent type
func (c *ConfidenceConfig) GetMinConfidence(intentType IntentType) float64 {
	switch intentType {
	case IntentGreeting:
		return c.GreetingMinConfidence
	case IntentReadInfo:
		return c.ReadInfoMinConfidence
	case IntentDangerousAction:
		return c.DangerousActionMinConfidence
	case IntentComposite:
		return c.CompositeActionMinConfidence
	default:
		return 0.5 // Default threshold for unknown types
	}
}

// IsAmbiguous checks if confidence score falls in ambiguous range
func (c *ConfidenceConfig) IsAmbiguous(confidence float64) bool {
	return confidence >= c.AmbiguousRangeLow && confidence <= c.AmbiguousRangeHigh
}
