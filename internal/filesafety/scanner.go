package filesafety

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type Decision string

const (
	DecisionAllow            Decision = "allow"
	DecisionRequiresApproval Decision = "requires_approval"
	DecisionBlock            Decision = "block"
)

const (
	FlagExternalOrigin             = "external_origin"
	FlagTypeMatch                  = "type_match"
	FlagTypeMismatch               = "type_mismatch"
	FlagDangerousType              = "dangerous_type"
	FlagUnsupportedType            = "unsupported_type"
	FlagTooLarge                   = "too_large"
	FlagTooSmall                   = "too_small"
	FlagCorruptOrUnreadable        = "corrupt_or_unreadable"
	FlagArchiveRisk                = "archive_risk"
	FlagActiveContent              = "active_content"
	FlagPromptInjectionSuspected   = "prompt_injection_suspected"
	FlagArchiveContainer           = "archive_container"
	FlagExecutableOrScriptShortcut = "executable_script_shortcut"
)

const (
	ScannerVersion = "file-safety-scanner/1"
	PolicyVersion  = "file-safety-policy/1"

	DefaultMaxSizeBytes       = int64(25 * 1024 * 1024)
	DefaultArchiveMaxFiles    = 1000
	DefaultArchiveMaxExpanded = int64(100 * 1024 * 1024)
)

type Input struct {
	Filename     string
	ClaimedMIME  string
	Origin       string
	SourceTool   string
	MaxSizeBytes int64
}

type Result struct {
	Decision       Decision `json:"decision"`
	Flags          []string `json:"flags"`
	ReasonUser     string   `json:"reason_user"`
	ReasonAudit    string   `json:"reason_audit"`
	Filename       string   `json:"filename"`
	Extension      string   `json:"extension"`
	ClaimedMIME    string   `json:"claimed_mime,omitempty"`
	DetectedType   string   `json:"detected_type"`
	SizeBytes      int64    `json:"size_bytes"`
	SHA256         string   `json:"sha256"`
	Origin         string   `json:"origin,omitempty"`
	SourceTool     string   `json:"source_tool,omitempty"`
	ScannerVersion string   `json:"scanner_version"`
	PolicyVersion  string   `json:"policy_version"`
}

func (r Result) Metadata() map[string]any {
	return map[string]any{
		"decision":        string(r.Decision),
		"flags":           append([]string(nil), r.Flags...),
		"reason_user":     r.ReasonUser,
		"reason_audit":    r.ReasonAudit,
		"filename":        r.Filename,
		"extension":       r.Extension,
		"claimed_mime":    r.ClaimedMIME,
		"detected_type":   r.DetectedType,
		"size_bytes":      r.SizeBytes,
		"sha256":          r.SHA256,
		"origin":          r.Origin,
		"source_tool":     r.SourceTool,
		"scanner_version": r.ScannerVersion,
		"policy_version":  r.PolicyVersion,
	}
}

func (r Result) Allowed() bool {
	return r.Decision == DecisionAllow
}

func ScanPath(path string, input Input) (Result, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Result{}, err
	}
	if info.IsDir() {
		return Result{}, fmt.Errorf("path is a directory")
	}
	maxSize := input.MaxSizeBytes
	if maxSize <= 0 {
		maxSize = DefaultMaxSizeBytes
	}
	if info.Size() > maxSize {
		return blocked(input, info.Size(), "", "unknown", []string{FlagTooLarge}, "File is too large to process safely.", "size exceeds configured limit"), nil
	}
	file, err := os.Open(path)
	if err != nil {
		return Result{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		return Result{}, err
	}
	if int64(len(data)) > maxSize {
		return blocked(input, int64(len(data)), "", "unknown", []string{FlagTooLarge}, "File is too large to process safely.", "size exceeds configured limit"), nil
	}
	if input.Filename == "" {
		input.Filename = filepath.Base(path)
	}
	return ScanBytes(data, input), nil
}

func ScanBytes(data []byte, input Input) Result {
	input.Filename = strings.TrimSpace(input.Filename)
	input.ClaimedMIME = strings.TrimSpace(input.ClaimedMIME)
	input.Origin = strings.TrimSpace(input.Origin)
	input.SourceTool = strings.TrimSpace(input.SourceTool)
	maxSize := input.MaxSizeBytes
	if maxSize <= 0 {
		maxSize = DefaultMaxSizeBytes
	}

	ext := strings.ToLower(filepath.Ext(input.Filename))
	size := int64(len(data))
	sum := sha256.Sum256(data)
	detected := detectType(data, ext)
	flags := []string{}
	if input.Origin != "" && input.Origin != "local_workspace" {
		flags = append(flags, FlagExternalOrigin)
	}
	if size == 0 {
		return result(input, ext, detected, size, sum[:], DecisionBlock, append(flags, FlagTooSmall), "File is empty.", "empty file")
	}
	if size > maxSize {
		return result(input, ext, detected, size, sum[:], DecisionBlock, append(flags, FlagTooLarge), "File is too large to process safely.", "size exceeds configured limit")
	}
	if dangerousExtension(ext) || detected == "windows_executable" || detected == "ole_compound_document" {
		return result(input, ext, detected, size, sum[:], DecisionBlock, append(flags, FlagDangerousType, FlagExecutableOrScriptShortcut), "File type can execute code or hide active content.", "dangerous executable, script, shortcut, or OLE type")
	}
	if hasTypeMismatch(ext, detected) {
		flags = append(flags, FlagTypeMismatch)
	} else if ext != "" {
		flags = append(flags, FlagTypeMatch)
	}

	if active := activeContentFlags(data, detected, ext); len(active) > 0 {
		return result(input, ext, detected, size, sum[:], DecisionBlock, append(flags, active...), "File contains active content that is blocked by policy.", "active content detected")
	}
	if isArchiveType(detected, ext) {
		archiveFlags, err := scanArchive(data)
		if err != nil {
			return result(input, ext, detected, size, sum[:], DecisionBlock, append(flags, FlagCorruptOrUnreadable), "File appears corrupt or unreadable.", "archive parse failed: "+err.Error())
		}
		flags = append(flags, FlagArchiveContainer)
		if len(archiveFlags) > 0 {
			return result(input, ext, detected, size, sum[:], DecisionBlock, append(flags, archiveFlags...), "Archive contains unsafe paths, links, nested archives, or too much content.", "archive guard failed")
		}
	}
	if promptInjectionSuspected(data, detected, ext) {
		return result(input, ext, detected, size, sum[:], DecisionRequiresApproval, append(flags, FlagPromptInjectionSuspected), "File text looks like it may contain prompt-injection instructions.", "prompt injection pattern detected")
	}
	if !supportedType(detected, ext) {
		return result(input, ext, detected, size, sum[:], DecisionRequiresApproval, append(flags, FlagUnsupportedType), "File type is unknown or unsupported and needs approval.", "unsupported or unknown type")
	}
	if hasFlag(flags, FlagTypeMismatch) {
		return result(input, ext, detected, size, sum[:], DecisionRequiresApproval, flags, "File extension does not match detected content type.", "extension/content type mismatch")
	}
	return result(input, ext, detected, size, sum[:], DecisionAllow, flags, "File passed safety checks.", "allowed")
}

func blocked(input Input, size int64, hash string, detected string, flags []string, user string, audit string) Result {
	if hash == "" {
		sum := sha256.Sum256(nil)
		hash = hex.EncodeToString(sum[:])
	}
	return Result{
		Decision:       DecisionBlock,
		Flags:          uniqueStrings(flags),
		ReasonUser:     user,
		ReasonAudit:    audit,
		Filename:       input.Filename,
		Extension:      strings.ToLower(filepath.Ext(input.Filename)),
		ClaimedMIME:    input.ClaimedMIME,
		DetectedType:   detected,
		SizeBytes:      size,
		SHA256:         hash,
		Origin:         input.Origin,
		SourceTool:     input.SourceTool,
		ScannerVersion: ScannerVersion,
		PolicyVersion:  PolicyVersion,
	}
}

func result(input Input, ext string, detected string, size int64, hash []byte, decision Decision, flags []string, user string, audit string) Result {
	return Result{
		Decision:       decision,
		Flags:          uniqueStrings(flags),
		ReasonUser:     user,
		ReasonAudit:    audit,
		Filename:       input.Filename,
		Extension:      ext,
		ClaimedMIME:    input.ClaimedMIME,
		DetectedType:   detected,
		SizeBytes:      size,
		SHA256:         hex.EncodeToString(hash),
		Origin:         input.Origin,
		SourceTool:     input.SourceTool,
		ScannerVersion: ScannerVersion,
		PolicyVersion:  PolicyVersion,
	}
}

func detectType(data []byte, ext string) string {
	switch {
	case bytes.HasPrefix(data, []byte("%PDF-")):
		return "pdf"
	case bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return "png"
	case bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}):
		return "jpeg"
	case bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a")):
		return "gif"
	case len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return "webp"
	case bytes.HasPrefix(data, []byte("MZ")):
		return "windows_executable"
	case bytes.HasPrefix(data, []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}):
		return "ole_compound_document"
	case bytes.HasPrefix(data, []byte("PK\x03\x04")) || bytes.HasPrefix(data, []byte("PK\x05\x06")) || bytes.HasPrefix(data, []byte("PK\x07\x08")):
		if officeType := detectOfficeZip(data); officeType != "" {
			return officeType
		}
		return "zip"
	}
	trimmed := bytes.TrimSpace(data)
	lower := strings.ToLower(string(firstBytes(trimmed, 512)))
	if strings.HasPrefix(lower, "<!doctype html") || strings.HasPrefix(lower, "<html") {
		return "html"
	}
	if strings.HasPrefix(lower, "<svg") || strings.Contains(lower, "<svg ") {
		return "svg"
	}
	if utf8.Valid(data) && !bytes.Contains(data, []byte{0}) {
		if ext == ".csv" || strings.Contains(string(firstBytes(data, 4096)), ",") {
			return "text"
		}
		return "text"
	}
	if sniffed := http.DetectContentType(firstBytes(data, 512)); strings.HasPrefix(sniffed, "text/") {
		return "text"
	}
	return "unknown"
}

func detectOfficeZip(data []byte) string {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ""
	}
	hasContentTypes := false
	hasWord, hasXL, hasPPT := false, false, false
	for _, f := range reader.File {
		name := strings.ToLower(filepath.ToSlash(f.Name))
		switch {
		case name == "[content_types].xml":
			hasContentTypes = true
		case strings.HasPrefix(name, "word/"):
			hasWord = true
		case strings.HasPrefix(name, "xl/"):
			hasXL = true
		case strings.HasPrefix(name, "ppt/"):
			hasPPT = true
		}
	}
	if !hasContentTypes {
		return ""
	}
	switch {
	case hasWord:
		return "docx"
	case hasXL:
		return "xlsx"
	case hasPPT:
		return "pptx"
	default:
		return ""
	}
}

func dangerousExtension(ext string) bool {
	switch ext {
	case ".exe", ".dll", ".scr", ".bat", ".cmd", ".ps1", ".lnk", ".url", ".com", ".msi", ".vbs", ".js", ".jar", ".docm", ".xlsm", ".pptm":
		return true
	default:
		return false
	}
}

func hasTypeMismatch(ext, detected string) bool {
	if ext == "" || detected == "unknown" {
		return false
	}
	switch ext {
	case ".pdf":
		return detected != "pdf"
	case ".png":
		return detected != "png"
	case ".jpg", ".jpeg":
		return detected != "jpeg"
	case ".gif":
		return detected != "gif"
	case ".webp":
		return detected != "webp"
	case ".zip":
		return detected != "zip"
	case ".docx":
		return detected != "docx"
	case ".xlsx":
		return detected != "xlsx"
	case ".pptx":
		return detected != "pptx"
	case ".txt", ".md", ".json", ".csv", ".go", ".py", ".yaml", ".yml", ".xml", ".log":
		return detected != "text" && detected != "html" && detected != "svg"
	case ".html", ".htm":
		return detected != "html" && detected != "text"
	case ".svg":
		return detected != "svg" && detected != "text"
	default:
		return false
	}
}

func activeContentFlags(data []byte, detected, ext string) []string {
	lower := strings.ToLower(string(firstBytes(data, 2*1024*1024)))
	if detected == "pdf" {
		structural := stripPDFStreams(lower)
		for _, token := range []string{"/javascript", "/launch", "/embeddedfile"} {
			if strings.Contains(structural, token) {
				return []string{FlagActiveContent}
			}
		}
	}
	if detected == "html" || detected == "svg" || ext == ".html" || ext == ".htm" || ext == ".svg" {
		if strings.Contains(lower, "<script") || strings.Contains(lower, "javascript:") || containsEventHandler(lower) {
			return []string{FlagActiveContent}
		}
	}
	if detected == "docx" || detected == "xlsx" || detected == "pptx" || detected == "zip" {
		if strings.Contains(lower, "vbaproject.bin") || strings.Contains(lower, "activeX/") {
			return []string{FlagActiveContent}
		}
	}
	return nil
}

func stripPDFStreams(text string) string {
	var builder strings.Builder
	for {
		start := strings.Index(text, "stream")
		if start < 0 {
			builder.WriteString(text)
			return builder.String()
		}
		builder.WriteString(text[:start])
		rest := text[start+len("stream"):]
		end := strings.Index(rest, "endstream")
		if end < 0 {
			return builder.String()
		}
		text = rest[end+len("endstream"):]
	}
}

func containsEventHandler(text string) bool {
	for _, token := range []string{" onclick=", " onload=", " onerror=", " onmouseover=", " onfocus=", " onsubmit="} {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func isArchiveType(detected, ext string) bool {
	return detected == "zip" || detected == "docx" || detected == "xlsx" || detected == "pptx" || ext == ".zip" || ext == ".docx" || ext == ".xlsx" || ext == ".pptx"
}

func scanArchive(data []byte) ([]string, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	flags := []string{}
	total := int64(0)
	if len(reader.File) > DefaultArchiveMaxFiles {
		flags = append(flags, FlagArchiveRisk)
	}
	for _, f := range reader.File {
		name := filepath.ToSlash(f.Name)
		clean := filepath.Clean(name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(name) || strings.Contains(name, "../") {
			flags = append(flags, FlagArchiveRisk)
		}
		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			flags = append(flags, FlagArchiveRisk)
		}
		total += int64(f.UncompressedSize64)
		if total > DefaultArchiveMaxExpanded {
			flags = append(flags, FlagArchiveRisk)
		}
		entryExt := strings.ToLower(filepath.Ext(name))
		if entryExt == ".zip" || entryExt == ".rar" || entryExt == ".7z" || entryExt == ".tar" || entryExt == ".gz" {
			flags = append(flags, FlagArchiveRisk)
		}
	}
	return uniqueStrings(flags), nil
}

func promptInjectionSuspected(data []byte, detected, ext string) bool {
	if detected != "text" && detected != "html" && detected != "svg" && ext != ".txt" && ext != ".md" && ext != ".html" && ext != ".svg" {
		return false
	}
	lower := strings.ToLower(string(firstBytes(data, 1024*1024)))
	for _, token := range []string{
		"ignore previous instructions",
		"ignore all previous instructions",
		"system prompt",
		"developer message",
		"you are now",
		"do not follow",
	} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func supportedType(detected, ext string) bool {
	switch detected {
	case "pdf", "png", "jpeg", "gif", "webp", "text", "docx", "xlsx", "pptx", "zip":
		return true
	case "html", "svg":
		return ext == ".html" || ext == ".htm" || ext == ".svg"
	default:
		return false
	}
}

func firstBytes(data []byte, n int) []byte {
	if len(data) <= n {
		return data
	}
	return data[:n]
}

func hasFlag(flags []string, flag string) bool {
	for _, f := range flags {
		if f == flag {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
