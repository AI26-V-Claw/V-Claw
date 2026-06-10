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

	// Inject a base transport with IdleConnTimeout shorter than Google's server-side 90s
	// idle timeout. This prevents the "connection forcibly closed" error that occurs when
	// a keepalive connection is reused after the server has already closed it.
	baseTransport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, &http.Client{Transport: baseTransport})
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
		fmt.Fprint(w, oauthSuccessPage)
	})
	return mux
}

const oauthSuccessPage = `<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<title>V-Claw OAuth Complete</title>
	<style>
		:root {
			color-scheme: light;
			--bg: #f6f8fb;
			--card: #ffffff;
			--ink: #111827;
			--muted: #5f6b7a;
			--line: #e6eaf0;
			--accent: #2563eb;
			--accent-soft: #dbeafe;
			--success: #16a34a;
			--success-soft: #dcfce7;
		}

		* {
			box-sizing: border-box;
		}

		body {
			margin: 0;
			min-height: 100vh;
			display: grid;
			place-items: center;
			background:
				radial-gradient(circle at top left, rgba(37, 99, 235, 0.12), transparent 32rem),
				linear-gradient(135deg, var(--bg), #eef4ff);
			color: var(--ink);
			font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
			padding: 2rem;
		}

		main {
			width: min(100%, 32rem);
			background: var(--card);
			border: 1px solid var(--line);
			border-radius: 1.25rem;
			box-shadow: 0 1.5rem 4rem rgba(15, 23, 42, 0.12);
			padding: 2rem;
		}

		.brand {
			display: flex;
			align-items: center;
			gap: 0.75rem;
			margin-bottom: 1.5rem;
			font-weight: 700;
			letter-spacing: 0.02em;
		}

		.logo {
			width: 2.5rem;
			height: 2.5rem;
			display: grid;
			place-items: center;
			border-radius: 0.85rem;
			background: var(--accent-soft);
			color: var(--accent);
			font-weight: 800;
		}

		.status {
			width: 4rem;
			height: 4rem;
			display: grid;
			place-items: center;
			border-radius: 999px;
			background: var(--success-soft);
			color: var(--success);
			font-size: 2rem;
			margin-bottom: 1.25rem;
		}

		h1 {
			margin: 0;
			font-size: clamp(2rem, 5vw, 2.75rem);
			line-height: 1;
			letter-spacing: -0.03em;
		}

		p {
			margin: 1rem 0 0;
			color: var(--muted);
			font-size: 1rem;
			line-height: 1.65;
		}

		.next {
			margin-top: 1.5rem;
			padding: 1rem;
			border-radius: 0.9rem;
			background: #f8fafc;
			border: 1px solid var(--line);
		}

		.next strong {
			display: block;
			margin-bottom: 0.25rem;
			color: var(--ink);
		}
	</style>
</head>
<body>
	<main>
		<div class="brand" aria-label="V-Claw">
			<div class="logo">V</div>
			<span>V-Claw</span>
		</div>
		<div class="status" aria-hidden="true">✓</div>
		<h1>OAuth complete</h1>
		<p>Your Google Workspace account is connected and the local token has been received by V-Claw.</p>
		<div class="next">
			<strong>You can close this tab.</strong>
			<p>Return to the terminal to continue running the command.</p>
		</div>
	</main>
</body>
</html>`

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
