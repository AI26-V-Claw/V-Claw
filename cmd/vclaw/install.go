package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const installSubdir = "Programs\\V-Claw\\bin"

func runInstall() error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("install is currently supported on Windows only")
	}
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate current executable: %w", err)
	}
	installDir := filepath.Join(os.Getenv("LOCALAPPDATA"), installSubdir)
	if strings.TrimSpace(os.Getenv("LOCALAPPDATA")) == "" {
		return fmt.Errorf("LOCALAPPDATA is not set")
	}
	if err := os.MkdirAll(installDir, 0755); err != nil {
		return fmt.Errorf("create install directory: %w", err)
	}
	installedExe := filepath.Join(installDir, "vclaw.exe")
	if err := copyFile(exePath, installedExe); err != nil {
		return fmt.Errorf("copy vclaw.exe: %w", err)
	}
	if err := addUserPath(installDir); err != nil {
		return err
	}
	fmt.Println("V-Claw installed.")
	fmt.Printf("Installed: %s\n", installedExe)
	fmt.Println("Open a new terminal, then run:")
	fmt.Println("  vclaw setup")
	fmt.Println("  vclaw doctor")
	fmt.Println("  vclaw start")
	return nil
}

func addUserPath(dir string) error {
	current, err := readUserPath()
	if err != nil {
		return err
	}
	parts := strings.Split(current, ";")
	for _, part := range parts {
		if strings.EqualFold(strings.TrimSpace(part), dir) {
			return nil
		}
	}
	updated := dir
	if strings.TrimSpace(current) != "" {
		updated = current + ";" + dir
	}
	cmd := setUserPathCommand(updated)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("update user PATH: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func setUserPathCommand(updated string) *exec.Cmd {
	cmd := exec.Command("powershell", "-NoProfile", "-Command", "$pathValue = [Environment]::GetEnvironmentVariable('VCLAW_UPDATED_USER_PATH', 'Process'); [Environment]::SetEnvironmentVariable('Path', $pathValue, 'User')")
	cmd.Env = append(os.Environ(), "VCLAW_UPDATED_USER_PATH="+updated)
	return cmd
}

func readUserPath() (string, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-Command", "[Environment]::GetEnvironmentVariable('Path', 'User')")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("read user PATH: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}
