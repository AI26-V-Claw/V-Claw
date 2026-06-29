package control

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	controlDirName = "control"
	manifestGlob   = "*.json"
	tokenHeader    = "X-VClaw-Control-Token"
)

type Canceller interface {
	CancelSession(sessionID string) bool
}

type Server struct {
	server       *http.Server
	listener     net.Listener
	manifestPath string
	token        string
	address      string
}

type Manifest struct {
	Address   string    `json:"address"`
	Token     string    `json:"token"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

type cancelRequest struct {
	SessionID string `json:"session_id"`
}

type CancelResponse struct {
	Status    string `json:"status"`
	SessionID string `json:"session_id"`
	Message   string `json:"message,omitempty"`
}

func Enabled() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("VCLAW_CONTROL_IPC_ENABLED")))
	return value == "" || value == "1" || value == "true" || value == "yes"
}

func Start(ctx context.Context, dataDir string, canceller Canceller) (*Server, error) {
	if !Enabled() {
		return nil, nil
	}
	if canceller == nil {
		return nil, fmt.Errorf("control canceller is required")
	}
	manifestDir := manifestDirectory(dataDir)
	if err := os.MkdirAll(manifestDir, 0o700); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	token, err := randomToken()
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	address := "http://" + listener.Addr().String()
	s := &Server{
		listener:     listener,
		manifestPath: filepath.Join(manifestDir, fmt.Sprintf("%d-%s.json", os.Getpid(), token[:12])),
		token:        token,
		address:      address,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/cancel", s.handleCancel(canceller))
	s.server = &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second}
	manifest := Manifest{Address: address, Token: token, PID: os.Getpid(), StartedAt: time.Now().UTC()}
	if err := writeManifest(s.manifestPath, manifest); err != nil {
		_ = listener.Close()
		return nil, err
	}
	go func() {
		<-ctx.Done()
		_ = s.Close(context.Background())
	}()
	go func() {
		if err := s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			_ = s.removeOwnManifest()
		}
	}()
	return s, nil
}

func (s *Server) Close(ctx context.Context) error {
	if s == nil || s.server == nil {
		return nil
	}
	_ = s.removeOwnManifest()
	return s.server.Shutdown(ctx)
}

func (s *Server) removeOwnManifest() error {
	if s == nil || strings.TrimSpace(s.manifestPath) == "" {
		return nil
	}
	manifest, err := readManifestFile(s.manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if manifest.Token != s.token || manifest.Address != s.address {
		return nil
	}
	return os.Remove(s.manifestPath)
}

func (s *Server) handleCancel(canceller Canceller) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get(tokenHeader)), []byte(s.token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		defer r.Body.Close()
		var req cancelRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		sessionID := strings.TrimSpace(req.SessionID)
		if sessionID == "" {
			http.Error(w, "session_id is required", http.StatusBadRequest)
			return
		}
		status := "no_active_run"
		message := "no active run found"
		if canceller.CancelSession(sessionID) {
			status = "cancelled"
			message = "cancel requested"
		}
		writeJSON(w, CancelResponse{Status: status, SessionID: sessionID, Message: message})
	}
}

func Cancel(ctx context.Context, dataDir string, sessionID string) (CancelResponse, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return CancelResponse{}, fmt.Errorf("session id is required")
	}
	manifests, err := ReadManifests(dataDir)
	if err != nil {
		return CancelResponse{}, err
	}
	var sawLiveServer bool
	for _, manifest := range manifests {
		response, err := cancelManifest(ctx, manifest, sessionID)
		if err != nil {
			continue
		}
		sawLiveServer = true
		if response.Status == "cancelled" {
			return response, nil
		}
	}
	if sawLiveServer {
		return CancelResponse{Status: "no_active_run", SessionID: sessionID, Message: "no active run found"}, nil
	}
	return CancelResponse{}, fmt.Errorf("agent control server is not running")
}

func cancelManifest(ctx context.Context, manifest Manifest, sessionID string) (CancelResponse, error) {
	payload, err := json.Marshal(cancelRequest{SessionID: sessionID})
	if err != nil {
		return CancelResponse{}, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, strings.TrimRight(manifest.Address, "/")+"/cancel", bytes.NewReader(payload))
	if err != nil {
		return CancelResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(tokenHeader, manifest.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return CancelResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return CancelResponse{}, fmt.Errorf("control server returned %s", resp.Status)
	}
	var out CancelResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return CancelResponse{}, err
	}
	return out, nil
}

func ReadManifest(dataDir string) (Manifest, error) {
	manifests, err := ReadManifests(dataDir)
	if err != nil {
		return Manifest{}, err
	}
	return manifests[0], nil
}

func ReadManifests(dataDir string) ([]Manifest, error) {
	paths, err := filepath.Glob(filepath.Join(manifestDirectory(dataDir), manifestGlob))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("agent control server is not running")
	}
	manifests := make([]Manifest, 0, len(paths))
	for _, path := range paths {
		manifest, err := readManifestFile(path)
		if err != nil || strings.TrimSpace(manifest.Address) == "" || strings.TrimSpace(manifest.Token) == "" {
			continue
		}
		manifests = append(manifests, manifest)
	}
	if len(manifests) == 0 {
		return nil, fmt.Errorf("agent control manifest is invalid")
	}
	return manifests, nil
}

func readManifestFile(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func manifestDirectory(dataDir string) string {
	if strings.TrimSpace(dataDir) == "" {
		dataDir = "./data"
	}
	return filepath.Join(dataDir, controlDirName)
}

func writeManifest(path string, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func randomToken() (string, error) {
	var data [32]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}
