package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"vclaw/internal/policies"
)

func startPolicyReloadWatcher(ctx context.Context, logger *slog.Logger, store *policies.UserPolicyStore) func() {
	if store == nil {
		return func() {}
	}
	if logger == nil {
		logger = slog.Default()
	}

	hupCh := make(chan os.Signal, 1)
	signal.Notify(hupCh, syscall.SIGHUP)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-hupCh:
				cfg, err := store.Reload()
				if err != nil {
					logger.Error("reload user policy config failed", "path", store.Path(), "error", err)
					continue
				}
				logger.Info("reloaded user policy config",
					"path", store.Path(),
					"auto_allow", cfg.AutoAllow,
					"require_approval", cfg.RequireApproval,
					"always_block", cfg.AlwaysBlock,
				)
			}
		}
	}()

	return func() {
		signal.Stop(hupCh)
	}
}
