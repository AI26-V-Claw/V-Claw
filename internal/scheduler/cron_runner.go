package scheduler

import (
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"
)

type Notifier interface {
	TriggerWorkflow(prompt string)
}

type CronRunner struct {
	cron     *cron.Cron
	notifier Notifier
	logger   *slog.Logger
	env      func(string) string
}

func NewCronRunner(notifier Notifier, logger *slog.Logger, env func(string) string) *CronRunner {
	if logger == nil {
		logger = slog.Default()
	}
	c := cron.New(cron.WithLocation(time.Local))
	return &CronRunner{
		cron:     c,
		notifier: notifier,
		logger:   logger,
		env:      env,
	}
}

func (r *CronRunner) Start() {
	workflows := GetDefaultWorkflows(r.env)
	for _, wf := range workflows {
		wf := wf // capture
		if wf.CronExpr == "" {
			continue
		}
		_, err := r.cron.AddFunc(wf.CronExpr, func() {
			r.logger.Info("triggering periodic workflow", "id", wf.ID, "name", wf.Name)
			r.notifier.TriggerWorkflow(wf.Prompt)
		})
		if err != nil {
			r.logger.Error("failed to schedule workflow", "id", wf.ID, "error", err)
			continue
		}
		r.logger.Info("scheduled workflow", "id", wf.ID, "cron", wf.CronExpr)
	}
	r.cron.Start()
}

func (r *CronRunner) Stop() {
	if r.cron != nil {
		r.cron.Stop()
	}
}
