package oauth

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type Config struct {
	CredentialsPath string
	TokenPath       string
	Scopes          []string
}

func Client(ctx context.Context, cfg Config) (*http.Client, error) {
	oauthConfig, err := oauthConfig(cfg)
	if err != nil {
		return nil, err
	}

	token, err := tokenFromFile(cfg.TokenPath)
	if err != nil {
		token, err = tokenFromWeb(ctx, oauthConfig)
		if err != nil {
			return nil, err
		}
		if err := saveToken(cfg.TokenPath, token); err != nil {
			return nil, err
		}
	}

	return oauthConfig.Client(ctx, token), nil
}

func oauthConfig(cfg Config) (*oauth2.Config, error) {
	if cfg.CredentialsPath == "" {
		return nil, errors.New("credentials path is required")
	}
	if cfg.TokenPath == "" {
		return nil, errors.New("token path is required")
	}
	if len(cfg.Scopes) == 0 {
		return nil, errors.New("at least one OAuth scope is required")
	}

	credentials, err := os.ReadFile(cfg.CredentialsPath)
	if err != nil {
		return nil, fmt.Errorf("read credentials: %w", err)
	}

	config, err := google.ConfigFromJSON(credentials, cfg.Scopes...)
	if err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}
	return config, nil
}

func tokenFromFile(path string) (*oauth2.Token, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	token := &oauth2.Token{}
	if err := json.NewDecoder(file).Decode(token); err != nil {
		return nil, err
	}
	return token, nil
}

func tokenFromWeb(ctx context.Context, config *oauth2.Config) (*oauth2.Token, error) {
	token, err := tokenFromLocalCallback(ctx, config)
	if err == nil {
		return token, nil
	}

	fmt.Printf("Could not complete OAuth through the local callback: %v\n", err)
	fmt.Println("Falling back to manual authorization code entry.")
	return tokenFromManualCode(ctx, config)
}

func tokenFromLocalCallback(ctx context.Context, config *oauth2.Config) (*oauth2.Token, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start local callback listener: %w", err)
	}
	defer listener.Close()

	localConfig := *config
	localConfig.RedirectURL = fmt.Sprintf("http://%s/oauth2callback", listener.Addr().String())

	state, err := randomState()
	if err != nil {
		return nil, err
	}

	resultCh := make(chan authResult, 1)
	server := &http.Server{
		Handler: oauthCallbackHandler(state, resultCh),
	}
	defer server.Shutdown(context.Background())

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			select {
			case resultCh <- authResult{err: fmt.Errorf("serve local callback: %w", err)}:
			default:
			}
		}
	}()

	authURL := localConfig.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	fmt.Println("Open this URL in your browser and approve V-Claw:")
	fmt.Println(authURL)
	if err := openBrowser(authURL); err != nil {
		fmt.Printf("Could not open browser automatically: %v\n", err)
	}
	fmt.Println("Waiting for Google OAuth callback in the browser...")

	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	select {
	case result := <-resultCh:
		if result.err != nil {
			return nil, result.err
		}
		token, err := localConfig.Exchange(ctx, result.code)
		if err != nil {
			return nil, fmt.Errorf("exchange authorization code: %w", err)
		}
		return token, nil
	case <-waitCtx.Done():
		return nil, fmt.Errorf("wait for OAuth callback: %w", waitCtx.Err())
	}
}

type authResult struct {
	code string
	err  error
}

func oauthCallbackHandler(state string, resultCh chan<- authResult) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if authError := query.Get("error"); authError != "" {
			sendAuthResult(resultCh, authResult{err: fmt.Errorf("OAuth error: %s", authError)})
			http.Error(w, "V-Claw OAuth failed. You can close this tab and return to the terminal.", http.StatusBadRequest)
			return
		}
		if gotState := query.Get("state"); gotState != state {
			sendAuthResult(resultCh, authResult{err: errors.New("OAuth state mismatch")})
			http.Error(w, "V-Claw OAuth failed. You can close this tab and return to the terminal.", http.StatusBadRequest)
			return
		}
		code := query.Get("code")
		if code == "" {
			http.Error(w, "Missing OAuth authorization code.", http.StatusBadRequest)
			return
		}

		sendAuthResult(resultCh, authResult{code: code})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<!doctype html><title>V-Claw OAuth</title><h1>V-Claw OAuth complete</h1><p>You can close this tab and return to the terminal.</p>")
	})
	return mux
}

func sendAuthResult(resultCh chan<- authResult, result authResult) {
	select {
	case resultCh <- result:
	default:
	}
}

func randomState() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate OAuth state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func tokenFromManualCode(ctx context.Context, config *oauth2.Config) (*oauth2.Token, error) {
	authURL := config.AuthCodeURL("vclaw-google-oauth", oauth2.AccessTypeOffline, oauth2.ApprovalForce)

	fmt.Println("Open this URL in your browser and approve V-Claw:")
	fmt.Println(authURL)
	if err := openBrowser(authURL); err != nil {
		fmt.Printf("Could not open browser automatically: %v\n", err)
	}
	fmt.Print("Paste the authorization code or redirected URL here: ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("read authorization code: %w", err)
	}

	code, err := extractAuthCode(input)
	if err != nil {
		return nil, err
	}

	token, err := config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchange authorization code: %w", err)
	}
	return token, nil
}

func extractAuthCode(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", errors.New("authorization code is empty")
	}

	parsedURL, err := url.Parse(value)
	if err == nil && parsedURL.Query().Get("code") != "" {
		return parsedURL.Query().Get("code"), nil
	}

	return value, nil
}

func saveToken(path string, token *oauth2.Token) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create token directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("open token file: %w", err)
	}
	defer file.Close()

	if err := json.NewEncoder(file).Encode(token); err != nil {
		return fmt.Errorf("write token file: %w", err)
	}
	return nil
}

func openBrowser(rawURL string) error {
	var command string
	var args []string

	switch runtime.GOOS {
	case "windows":
		command = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", rawURL}
	case "darwin":
		command = "open"
		args = []string{rawURL}
	default:
		command = "xdg-open"
		args = []string{rawURL}
	}

	return exec.Command(command, args...).Start()
}
