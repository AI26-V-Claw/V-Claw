package intent

// ConfidenceConfig defines confidence thresholds for different intent types.
// These thresholds are derived from the intent_classification_spec.md.
type ConfidenceConfig struct {
	GreetingMin        float64 // GREETING: always accept (0.0)
	ReadInfoMin        float64 // READ_INFO: >= 0.70
	DangerousActionMin float64 // DANGEROUS_ACTION: >= 0.90
	CompositeActionMin float64 // COMPOSITE_ACTION: >= 0.85

	// When confidence falls in this range, show multiple-choice clarification
	AmbiguousLow  float64 // 0.60
	AmbiguousHigh float64 // 0.85
}

// DefaultConfig provides production-ready confidence thresholds.
var DefaultConfig = ConfidenceConfig{
	GreetingMin:        0.0,
	ReadInfoMin:        0.70,
	DangerousActionMin: 0.90,
	CompositeActionMin: 0.85,
	AmbiguousLow:       0.60,
	AmbiguousHigh:      0.85,
}

// MinConfidenceFor returns the minimum confidence required for a given intent type.
func (c *ConfidenceConfig) MinConfidenceFor(t IntentType) float64 {
	switch t {
	case TypeGreeting:
		return c.GreetingMin
	case TypeReadInfo:
		return c.ReadInfoMin
	case TypeDangerousAction:
		return c.DangerousActionMin
	case TypeComposite:
		return c.CompositeActionMin
	default:
		return c.AmbiguousLow
	}
}

// IsAmbiguous returns true if the confidence falls in the ambiguous range
// where the system should present multiple-choice clarification.
func (c *ConfidenceConfig) IsAmbiguous(confidence float64) bool {
	return confidence >= c.AmbiguousLow && confidence <= c.AmbiguousHigh
}
