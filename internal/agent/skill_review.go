package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"time"

	"vclaw/internal/providers"
)

const (
	skillReviewMaxTokens = 1000
	skillReviewTimeout   = 30 * time.Second
	skillCacheDirDefault = "cache/skills"
)

// skillReviewDecision is the JSON response from the review LLM call.
type skillReviewDecision struct {
	Action    string `json:"action"`     // "create" | "patch" | "skip"
	SkillName string `json:"skill_name"` // e.g. "skill.daily_briefing"
	Reason    string `json:"reason"`
	Content   string `json:"content"` // SKILL.md content (only for create/patch)
}

// skillReviewPrompt is injected into the forked LLM call.
// It instructs the model to analyze the conversation and decide on skill actions.
const skillReviewPrompt = `You are a skill management agent. Review the conversation transcript below and decide whether to create or patch a skill.

SIGNALS that warrant creating or patching a skill:
- User repeatedly corrects the agent's approach ("stop doing X", "always do Y first")
- A multi-step workflow appeared that could be templated (sequence of tool calls in specific order)
- A complex task was solved successfully and could benefit others or future sessions
- User preferences about output format or process were stated explicitly

DO NOT create a skill when:
- The error is environmental (missing credentials, network failure, permission denied)
- The task is clearly one-off and not repeatable
- The task is trivial and the LLM can handle it without a workflow (summarize, translate, reformat)
- A transient error occurred that resolved itself

Respond ONLY with a valid JSON object, no markdown, no explanation:
{
  "action": "create" | "patch" | "skip",
  "skill_name": "skill.snake_case_name",
  "reason": "one sentence why",
  "content": "full SKILL.md content here (only for create or patch, empty string for skip)"
}

SKILL.md content format when creating/patching:
---
name: skill.name
description: "When to use this skill. Trigger keywords. What it does."
version: 1.0.0
tags: [tag1, tag2]
---

# Skill Title

## Workflow
1. Step one
2. Step two
3. Step three

## Edge cases
- Edge case handling

Conversation transcript to analyze:
`

// maybeSpawnSkillReview checks if enough iterations have passed and spawns
// a background goroutine to review the conversation for skill opportunities.
// This is called at the end of Run() and never blocks the main response.
func (r *Runtime) maybeSpawnSkillReview(sessionID string, iterationCount int) {
	if r.skillNudgeInterval <= 0 {
		return
	}
	r.skillReviewMu.Lock()
	r.itersSinceSkillReview += iterationCount
	shouldReview := r.itersSinceSkillReview >= r.skillNudgeInterval
	if shouldReview {
		r.itersSinceSkillReview = 0
	}
	r.skillReviewMu.Unlock()

	if !shouldReview {
		return
	}

	// Load transcript snapshot before spawning goroutine (read-only, safe)
	snapshot, err := r.loadTranscriptSnapshot(sessionID)
	if err != nil || strings.TrimSpace(snapshot) == "" {
		return
	}

	go r.runSkillReview(snapshot)
}

// loadTranscriptSnapshot loads the recent conversation as a plain text snapshot.
func (r *Runtime) loadTranscriptSnapshot(sessionID string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	transcript, err := r.sessionStore.LoadTranscript(ctx, sessionID)
	if err != nil {
		return "", err
	}

	var lines []string
	for _, msg := range transcript {
		role := string(msg.Role)
		content := strings.TrimSpace(msg.Content)
		if content == "" || role == "tool" {
			continue
		}
		lines = append(lines, role+": "+content)
		if len(lines) >= 30 { // cap at last 30 messages
			break
		}
	}
	return strings.Join(lines, "\n"), nil
}

// runSkillReview calls the LLM with the review prompt and applies the decision.
func (r *Runtime) runSkillReview(snapshot string) {
	ctx, cancel := context.WithTimeout(context.Background(), skillReviewTimeout)
	defer cancel()

	prompt := skillReviewPrompt + snapshot

	resp, err := r.provider.Generate(ctx, &providers.GenerateRequest{
		UserPrompt: prompt,
		Model:      r.model,
		MaxTokens:  skillReviewMaxTokens,
		Timeout:    skillReviewTimeout,
	})
	if err != nil {
		r.logger.Warn("skill auto-review: LLM call failed", "error", err)
		return
	}

	text := strings.TrimSpace(resp.Text)
	if text == "" {
		return
	}

	var decision skillReviewDecision
	if err := json.Unmarshal([]byte(text), &decision); err != nil {
		// Try stripping markdown fences if present
		text = strings.TrimPrefix(text, "```json")
		text = strings.TrimPrefix(text, "```")
		text = strings.TrimSuffix(text, "```")
		if err2 := json.Unmarshal([]byte(strings.TrimSpace(text)), &decision); err2 != nil {
			r.logger.Warn("skill auto-review: failed to parse LLM response", "error", err2)
			return
		}
	}

	switch strings.TrimSpace(decision.Action) {
	case "skip", "":
		r.logger.Info("skill auto-review: no skill warranted", "reason", decision.Reason)
	case "create":
		r.applySkillCreate(decision)
	case "patch":
		r.applySkillPatch(decision)
	default:
		r.logger.Warn("skill auto-review: unknown action", "action", decision.Action)
	}
}

// applySkillCreate writes a new skill to cache/skills/.
func (r *Runtime) applySkillCreate(d skillReviewDecision) {
	if err := r.writeSkillFile(d.SkillName, d.Content); err != nil {
		r.logger.Error("skill auto-review: failed to create skill",
			"skill", d.SkillName, "error", err)
		return
	}
	r.logger.Info("skill auto-learn: Skill created",
		"skill", d.SkillName, "reason", d.Reason)
}

// applySkillPatch updates an existing skill in cache/skills/.
func (r *Runtime) applySkillPatch(d skillReviewDecision) {
	skillDir := r.skillCacheDir + "/" + d.SkillName
	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		// Skill does not exist yet, treat as create
		r.applySkillCreate(d)
		return
	}
	if err := r.writeSkillFile(d.SkillName, d.Content); err != nil {
		r.logger.Error("skill auto-review: failed to patch skill",
			"skill", d.SkillName, "error", err)
		return
	}
	r.logger.Info("skill auto-learn: Skill patched",
		"skill", d.SkillName, "reason", d.Reason)
}

// writeSkillFile writes SKILL.md and updates manifest.json in cache/skills/.
func (r *Runtime) writeSkillFile(skillName, content string) error {
	if strings.TrimSpace(skillName) == "" || strings.TrimSpace(content) == "" {
		return nil
	}
	skillDir := r.skillCacheDir + "/" + skillName
	if err := os.MkdirAll(skillDir, 0750); err != nil {
		return err
	}
	skillPath := skillDir + "/SKILL.md"
	if err := os.WriteFile(skillPath, []byte(content), 0640); err != nil {
		return err
	}
	return r.updateSkillManifest(skillName, content)
}

// updateSkillManifest appends or updates the entry in cache/skills/manifest.json.
func (r *Runtime) updateSkillManifest(skillName, content string) error {
	manifestPath := r.skillCacheDir + "/manifest.json"

	type record struct {
		Name      string `json:"name"`
		Version   string `json:"version"`
		Content   string `json:"content"`
		UpdatedAt string `json:"updated_at"`
	}

	var records []record
	if data, err := os.ReadFile(manifestPath); err == nil {
		_ = json.Unmarshal(data, &records)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	found := false
	for i, rec := range records {
		if rec.Name == skillName {
			records[i].Content = content
			records[i].UpdatedAt = now
			found = true
			break
		}
	}
	if !found {
		records = append(records, record{
			Name:      skillName,
			Version:   "1.0.0",
			Content:   content,
			UpdatedAt: now,
		})
	}

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(r.skillCacheDir, 0750); err != nil {
		return err
	}
	return os.WriteFile(manifestPath, data, 0640)
}

// logSkillReviewDisabled logs once at startup if skill review is disabled.
func logSkillReviewDisabled(logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("skill auto-learn disabled (VCLAW_SKILL_NUDGE_INTERVAL=0)")
}

// defaultSkillCacheDir returns the default cache directory for auto-learned skills.
func defaultSkillCacheDir() string {
	return skillCacheDirDefault
}
