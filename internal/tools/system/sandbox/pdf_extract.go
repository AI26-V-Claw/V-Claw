package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vclaw/internal/tools"
	pytool "vclaw/internal/tools/os/python"
)

const ToolNameExtractPDF = "sandbox.extractPDF"

type ExtractPDFTool struct {
	cfg Config
}

func NewExtractPDFTool(cfg Config) ExtractPDFTool {
	return ExtractPDFTool{cfg: normalizeConfig(cfg)}
}

func (ExtractPDFTool) Name() string { return ToolNameExtractPDF }

func (ExtractPDFTool) Description() string {
	return "Extract a local workspace PDF into structured Markdown using deterministic PyMuPDF table and column detection. It removes repeated page chrome, zero-width characters, and line-break hyphenation while preserving headings and tables. Use this instead of sandbox.runPython when content will be saved to Google Docs. The tool writes one UTF-8 .md file in the workspace and requires approval because it creates a local file."
}

func (ExtractPDFTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"localPath":  map[string]any{"type": "string", "description": "Exact absolute host path of a PDF inside the sandbox workspace."},
			"outputFile": map[string]any{"type": "string", "description": "Optional Markdown filename written at the workspace root. Must end in .md."},
		},
		"required":             []string{"localPath"},
		"additionalProperties": false,
	}
}

func (ExtractPDFTool) Capability() tools.Capability { return tools.CapabilityMutating }
func (ExtractPDFTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelLocalWrite }

func (t ExtractPDFTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	if t.cfg.Runner == nil {
		return sandboxNotConfigured(call)
	}
	sessionID := stringArgumentOr(call.Arguments, defaultIfEmpty(t.cfg.DefaultSessionID, defaultSessionID), "session_id", "sessionId")
	workspaceDir, err := resolveWorkspaceDir(call.Arguments, t.cfg, sessionID)
	if err != nil {
		return sandboxInputError(call, err)
	}
	inputHostPath, inputContainerPath, err := resolvePDFInputPath(workspaceDir, stringArgument(call.Arguments, "localPath"))
	if err != nil {
		return sandboxInputError(call, err)
	}
	outputName, err := markdownOutputName(inputHostPath, stringArgument(call.Arguments, "outputFile"))
	if err != nil {
		return sandboxInputError(call, err)
	}
	outputHostPath := filepath.Join(workspaceDir, outputName)
	outputContainerPath := "/workspace/" + filepath.ToSlash(outputName)

	output, runErr := pytool.RunPython(ctx, pytool.Input{
		RequestID:      requestID(call),
		SessionID:      sessionID,
		WorkspaceDir:   workspaceDir,
		Code:           structuredPDFExtractionCode(inputContainerPath, outputContainerPath),
		TimeoutSeconds: 90,
		UserIntent:     "extract a PDF into structured Markdown",
	}, t.cfg.Runner)
	result := pythonToolResult(call, output, runErr)
	if !result.Success {
		return result
	}
	info, err := os.Stat(outputHostPath)
	if err != nil || !info.Mode().IsRegular() {
		return sandboxInputError(call, fmt.Errorf("structured PDF extraction did not produce %s", outputName))
	}
	stats := map[string]any{}
	_ = json.Unmarshal([]byte(strings.TrimSpace(output.Stdout)), &stats)
	stats["size_bytes"] = info.Size()
	stats["format"] = "markdown"
	result.ContentForLLM = fmt.Sprintf("Structured PDF Markdown created at host path %s. Use this exact path as localPath for docs.appendMarkdown. Extraction stats: %s", outputHostPath, strings.TrimSpace(output.Stdout))
	result.ContentForUser = fmt.Sprintf("Đã trích xuất PDF có cấu trúc vào %s", outputName)
	result.ArtifactRef = &tools.ToolArtifactRef{Kind: "file", Label: outputName, URI: outputHostPath}
	result.Metadata = stats
	return result
}

func resolvePDFInputPath(workspaceDir, localPath string) (string, string, error) {
	localPath = strings.TrimSpace(localPath)
	if localPath == "" {
		return "", "", fmt.Errorf("localPath is required")
	}
	if strings.HasPrefix(filepath.ToSlash(localPath), "/workspace/") {
		localPath = filepath.Join(workspaceDir, filepath.FromSlash(strings.TrimPrefix(filepath.ToSlash(localPath), "/workspace/")))
	} else if !filepath.IsAbs(localPath) {
		localPath = filepath.Join(workspaceDir, localPath)
	}
	absPath, err := filepath.Abs(localPath)
	if err != nil {
		return "", "", fmt.Errorf("resolve localPath: %w", err)
	}
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", "", fmt.Errorf("PDF file is unavailable: %w", err)
	}
	realWorkspace, err := filepath.EvalSymlinks(workspaceDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve workspace: %w", err)
	}
	rel, err := filepath.Rel(realWorkspace, realPath)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("localPath is outside the sandbox workspace")
	}
	if !strings.EqualFold(filepath.Ext(realPath), ".pdf") {
		return "", "", fmt.Errorf("localPath must reference a .pdf file")
	}
	info, err := os.Stat(realPath)
	if err != nil || !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("localPath must reference a regular PDF file")
	}
	return realPath, "/workspace/" + filepath.ToSlash(rel), nil
}

func markdownOutputName(inputPath, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
		return base + "_structured.md", nil
	}
	if filepath.Base(requested) != requested || !strings.EqualFold(filepath.Ext(requested), ".md") {
		return "", fmt.Errorf("outputFile must be a .md filename without directories")
	}
	return requested, nil
}

func structuredPDFExtractionCode(inputPath, outputPath string) string {
	inputJSON, _ := json.Marshal(inputPath)
	outputJSON, _ := json.Marshal(outputPath)
	code := strings.ReplaceAll(structuredPDFExtractionScript, "__INPUT_PATH__", string(inputJSON))
	return strings.ReplaceAll(code, "__OUTPUT_PATH__", string(outputJSON))
}

const structuredPDFExtractionScript = `import fitz
import json
import re
from pathlib import Path

INPUT_PATH = __INPUT_PATH__
OUTPUT_PATH = __OUTPUT_PATH__

ZERO_WIDTH = dict.fromkeys(map(ord, "\u200b\u200c\u200d\u2060\ufeff"), None)

def clean(value):
    if value is None:
        return ""
    value = str(value).translate(ZERO_WIDTH).replace("\u00ad", "")
    value = re.sub(r"[-\u2010\u2011]\s*\n\s*", "", value)
    value = value.replace("\u2010", "-").replace("\u2011", "-").replace("\u00a0", " ")
    value = re.sub(r"\s*\n\s*", " ", value)
    return re.sub(r"[ \t]+", " ", value).strip()

def md_cell(value):
    return clean(value).replace("\\", "\\\\").replace("|", "\\|")

def is_subheading(value):
    return bool(re.fullmatch(r"[A-Za-z][A-Za-z /()&+-]{0,38}", value)) and len(value.split()) <= 5

def geometric_rows(page, table):
    raw_rows = table.extract()
    candidates = []
    for row in table.rows:
        cells = sorted([cell for cell in row.cells if cell], key=lambda cell: cell[0])
        if len(cells) > len(candidates):
            candidates = cells
    if len(candidates) < 2:
        return raw_rows
    edges = [cell[0] for cell in candidates] + [candidates[-1][2]]
    rows = []
    for row_index, row in enumerate(table.rows):
        raw_values = [clean(cell) for cell in raw_rows[row_index] if clean(cell)] if row_index < len(raw_rows) else []
        if len(raw_values) == 1 and (is_subheading(raw_values[0]) or raw_values[0].startswith("#include")):
            rows.append([raw_values[0]])
            continue
        cells = [cell for cell in row.cells if cell]
        if not cells:
            rows.append([])
            continue
        y0 = min(cell[1] for cell in cells)
        y1 = max(cell[3] for cell in cells)
        words = page.get_text("words", clip=(edges[0], y0, edges[-1], y1), sort=True)
        columns = [[] for _ in range(len(edges) - 1)]
        for word in words:
            center = (word[0] + word[2]) / 2
            column = len(edges) - 2
            for candidate in range(len(edges) - 1):
                if center < edges[candidate + 1]:
                    column = candidate
                    break
            columns[column].append(word)
        values = []
        for words_in_column in columns:
            line_groups = []
            current_key = None
            current_words = []
            for word in words_in_column:
                key = (word[5], word[6])
                if current_key is not None and key != current_key:
                    line_groups.append(" ".join(current_words))
                    current_words = []
                current_key = key
                current_words.append(word[4])
            if current_words:
                line_groups.append(" ".join(current_words))
            values.append(clean("\n".join(line_groups)))
        while values and not values[-1]:
            values.pop()
        rows.append(values)
    return rows

def table_lines(rows):
    lines = []
    pending = []

    def flush():
        nonlocal pending
        if not pending:
            return
        width = max(len(row) for row in pending)
        labels = ["Entry", "Description", "Value", "Details", "Notes"][:width]
        if width > len(labels):
            labels.extend([f"Column {i}" for i in range(len(labels) + 1, width + 1)])
        lines.append("| " + " | ".join(labels) + " |")
        lines.append("| " + " | ".join(["---"] * width) + " |")
        for row in pending:
            row = row + [""] * (width - len(row))
            lines.append("| " + " | ".join(row) + " |")
        lines.append("")
        pending = []

    for raw_row in rows:
        row = [md_cell(cell) for cell in raw_row]
        while row and not row[-1]:
            row.pop()
        values = [cell for cell in row if cell]
        if not values:
            continue
        if len(values) == 1:
            flush()
            value = values[0]
            if is_subheading(value):
                lines.extend(["### " + value, ""])
            elif value.startswith("#include"):
                lines.extend(["~~~c", value, "~~~", ""])
            else:
                lines.extend(["_" + value + "_", ""])
            continue
        pending.append(row)
    flush()
    return lines

doc = fitz.open(INPUT_PATH)
out = []
table_count = 0

if len(doc):
    top = doc[0].get_text("text", clip=(0, 0, doc[0].rect.width, 82))
    top_lines = [clean(line) for line in top.splitlines() if clean(line)]
    title = next((line for line in top_lines if "Cheat Sheet" in line), Path(INPUT_PATH).stem)
    out.extend(["# " + title, ""])
    for line in top_lines:
        if line != title and "Cheatography" not in line:
            out.extend(["_" + line + "_", ""])

for page_number, page in enumerate(doc, 1):
    tables = list(page.find_tables().tables)
    tables.sort(key=lambda table: (0 if table.bbox[0] < page.rect.width / 2 else 1, table.bbox[1]))
    if page_number > 1:
        out.extend([f"## Page {page_number}", ""])
    for table in tables:
        raw_rows = table.extract()
        rows = geometric_rows(page, table)
        if not rows:
            continue
        title_cells = [clean(cell) for cell in raw_rows[0] if clean(cell)] if raw_rows else []
        if title_cells:
            out.extend(["## " + " - ".join(dict.fromkeys(title_cells)), ""])
        out.extend(table_lines(rows[1:]))
        table_count += 1

markdown = "\n".join(out).strip() + "\n"
Path(OUTPUT_PATH).write_text(markdown, encoding="utf-8")
print(json.dumps({"pages": len(doc), "tables": table_count, "characters": len(markdown), "output": OUTPUT_PATH}))
`
