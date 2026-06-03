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
	"vclaw/internal/intent"
	"vclaw/internal/memory"
	"vclaw/internal/providers"
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

	llmClient, err := providers.NewClient(providers.Config{
		Provider: chooseLLMProvider(cfg),
		APIKey:   chooseLLMAPIKey(cfg),
		BaseURL:  chooseLLMBaseURL(cfg),
		Model:    chooseLLMModel(cfg),
	})
	if err != nil {
		return nil, err
	}
	if llmClient == nil {
		return nil, fmt.Errorf("LLM provider is required for intent classification")
	}

	memoryStore := memory.NewStore()
	intentClassifier := intent.NewClassifier(llmClient)
	orchestrator := agent.NewOrchestrator(memoryStore, intentClassifier, llmClient)
	runners := make([]channelRunner, 0, 2)
	if cfg.TelegramEnabled {
		runners = append(runners, telegram.New(cfg.TelegramBotToken, cfg.AllowedTelegramUserID, cfg.DataDir, orchestrator, logger))
	}
	if cfg.SlackEnabled {
		slackBot, err := slackchannel.New(slackchannel.Config{
			BotToken:          cfg.SlackBotToken,
			AppToken:          cfg.SlackAppToken,
			AllowedChannelIDs: cfg.SlackAllowedChannelIDs,
			AllowedUserIDs:    cfg.SlackAllowedUserIDs,
		}, orchestrator, logger)
		if err != nil {
			return nil, err
		}
		runners = append(runners, slackBot)
	}
	if len(runners) == 0 {
		return nil, fmt.Errorf("at least one channel must be enabled")
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

func chooseLLMProvider(cfg config.Config) string {
	if cfg.LLMProvider != "" {
		return cfg.LLMProvider
	}
	if cfg.LLMAPIKey != "" && cfg.LLMModel != "" {
		return "openai-compatible"
	}
	if cfg.AnthropicAPIKey != "" && cfg.AnthropicResponseModel != "" {
		return "anthropic"
	}
	return ""
}

func chooseLLMAPIKey(cfg config.Config) string {
	if cfg.LLMAPIKey != "" {
		return cfg.LLMAPIKey
	}
	return cfg.AnthropicAPIKey
}

func chooseLLMBaseURL(cfg config.Config) string {
	if cfg.LLMBaseURL != "" {
		return cfg.LLMBaseURL
	}
	if cfg.LLMProvider == "anthropic" || (cfg.LLMProvider == "" && cfg.AnthropicAPIKey != "" && cfg.AnthropicResponseModel != "") {
		return "https://api.anthropic.com"
	}
	return "https://api.openai.com/v1"
}

func chooseLLMModel(cfg config.Config) string {
	if cfg.LLMModel != "" {
		return cfg.LLMModel
	}
	return cfg.AnthropicResponseModel
}
