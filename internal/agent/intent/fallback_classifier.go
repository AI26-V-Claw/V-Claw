package intent

import (
	"context"
	"fmt"
)

const (
	ClassifierModeFallback  = "fallback"
	ClassifierModeLLM       = "llm"
	ClassifierModeHeuristic = "heuristic"
)

type ClassifierRunner interface {
	Classify(ctx context.Context, userInput string) (*ClassificationOutput, error)
}

type MemoryAwareClassifierRunner interface {
	ClassifyWithMemoryIsolation(ctx context.Context, userInput string, recentHistory []string) (*ClassificationOutput, error)
}

type HeuristicRunner struct {
	classifier *Classifier
}

func NewHeuristicRunner(cfg ConfidenceConfig) *HeuristicRunner {
	return &HeuristicRunner{classifier: NewClassifier(cfg)}
}

func (r *HeuristicRunner) Classify(ctx context.Context, userInput string) (*ClassificationOutput, error) {
	if r == nil || r.classifier == nil {
		return nil, fmt.Errorf("heuristic classifier is required")
	}
	return Classify(ctx, r.classifier, userInput)
}

type FallbackClassifier struct {
	primary  ClassifierRunner
	fallback ClassifierRunner
}

func NewFallbackClassifier(primary ClassifierRunner, fallback ClassifierRunner) *FallbackClassifier {
	return &FallbackClassifier{
		primary:  primary,
		fallback: fallback,
	}
}

func (c *FallbackClassifier) Classify(ctx context.Context, userInput string) (*ClassificationOutput, error) {
	if c == nil {
		return nil, fmt.Errorf("fallback classifier is required")
	}
	if c.primary != nil {
		output, err := c.primary.Classify(ctx, userInput)
		if err == nil && output != nil && output.Intent != nil {
			return output, nil
		}
		if c.fallback == nil {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("primary classifier returned empty output")
		}
	}
	if c.fallback == nil {
		return nil, fmt.Errorf("classifier fallback is required")
	}
	return c.fallback.Classify(ctx, userInput)
}

func (c *FallbackClassifier) ClassifyWithMemoryIsolation(ctx context.Context, userInput string, recentHistory []string) (*ClassificationOutput, error) {
	if c == nil {
		return nil, fmt.Errorf("fallback classifier is required")
	}
	if c.primary != nil {
		if memoryAware, ok := c.primary.(MemoryAwareClassifierRunner); ok {
			output, err := memoryAware.ClassifyWithMemoryIsolation(ctx, userInput, recentHistory)
			if err == nil && output != nil && output.Intent != nil {
				return output, nil
			}
			if c.fallback == nil {
				if err != nil {
					return nil, err
				}
				return nil, fmt.Errorf("primary classifier returned empty output")
			}
		} else {
			output, err := c.primary.Classify(ctx, userInput)
			if err == nil && output != nil && output.Intent != nil {
				return output, nil
			}
			if c.fallback == nil {
				if err != nil {
					return nil, err
				}
				return nil, fmt.Errorf("primary classifier returned empty output")
			}
		}
	}
	if c.fallback == nil {
		return nil, fmt.Errorf("classifier fallback is required")
	}
	return c.fallback.Classify(ctx, userInput)
}
