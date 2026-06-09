package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"vclaw/internal/agent"
	slackchannel "vclaw/internal/channels/slack"
	"vclaw/internal/channels/telegram"
	"vclaw/internal/config"
)

type App struct {
	logger  *slog.Logger
	runners []channelRunner
}

type channelRunner interface {
	Run(context.Context) error
}

func New() (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if !cfg.TelegramEnabled && !cfg.SlackEnabled {
		return nil, fmt.Errorf("at least one channel must be enabled")
	}
	runtime, err := NewAgentRuntime(context.Background(), AgentRuntimeConfig{
		OpenAIAPIKey:          cfg.OpenAIAPIKey,
		OpenAIModel:           cfg.OpenAIModel,
		OpenAIBaseURL:         cfg.OpenAIBaseURL,
		Logger:                logger,
		MaxIterations:         agent.DefaultMaxIterations,
		EnableGoogleTools:     cfg.GoogleToolsEnabled,
		GoogleCredentialsPath: cfg.GoogleCredentialsPath,
		GoogleTokenPath:       cfg.GoogleTokenPath,
	})
	if err != nil {
		return nil, err
	}
	messenger := agent.NewRuntimeMessenger(runtime)

	runners := make([]channelRunner, 0, 2)
	if cfg.TelegramEnabled {
		runners = append(runners, telegram.New(cfg.TelegramBotToken, cfg.AllowedTelegramUserID, cfg.DataDir, nil, messenger, logger))
	}
	if cfg.SlackEnabled {
		slackBot, err := slackchannel.New(slackchannel.Config{
			BotToken:          cfg.SlackBotToken,
			AppToken:          cfg.SlackAppToken,
			OwnerUserID:       cfg.SlackOwnerUserID,
			AllowedChannelIDs: cfg.SlackAllowedChannelIDs,
			PolicyStore:       nil,
		}, messenger, logger)
		if err != nil {
			return nil, err
		}
		runners = append(runners, slackBot)
	}
	return &App{logger: logger, runners: runners}, nil
}

func (a *App) Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, len(a.runners))
	for _, runner := range a.runners {
		runner := runner
		go func() {
			errCh <- runner.Run(ctx)
		}()
	}

	var firstErr error
	for range a.runners {
		err := <-errCh
		if err != nil && !errors.Is(err, context.Canceled) && firstErr == nil {
			firstErr = err
			stop()
		}
	}
	if firstErr != nil {
		return firstErr
	}
	return nil
}
