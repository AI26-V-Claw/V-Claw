package policies

// pythonRules is the ordered policy matrix for sandbox.runPython requests.
//
// The checker scans the Python source code (or script path) for patterns that
// indicate dangerous behaviour. Like shellRules, matching is first-match from
// the top; more dangerous patterns appear first.
//
// Limitations: this is static pattern matching on raw source text, not AST
// analysis. It catches obvious cases; the Docker sandbox provides the primary
// containment for anything that slips through.
//
// Policy matrix (summary):
//
// ┌──────────────────────────┬──────────────────────┬──────────────────┐
// │ Pattern / Category       │ Risk Level            │ Decision         │
// ├──────────────────────────┼──────────────────────┼──────────────────┤
// │ Credential file access   │ credential_access     │ block            │
// │ System execution         │ high_risk             │ block            │
// │ Network imports/calls    │ external_network      │ block            │
// │ Dynamic execution        │ needs_approval        │ requires_approval│
// │ File delete / overwrite  │ needs_approval        │ requires_approval│
// │ File create / write      │ safe_write            │ allow            │
// │ Read-only / stdlib       │ safe_read             │ allow            │
// └──────────────────────────┴──────────────────────┴──────────────────┘
var pythonRules = []MatrixEntry{

	// ── Credential access (always block) ──────────────────────────────────

	{".env", RiskCredentialAccess, DecisionBlock,
		"Code tham chiếu đến file .env (credential). Bị chặn."},
	{"id_rsa", RiskCredentialAccess, DecisionBlock,
		"Code tham chiếu đến SSH private key. Bị chặn."},
	{"credentials.json", RiskCredentialAccess, DecisionBlock,
		"Code tham chiếu đến Google credentials. Bị chặn."},
	{"token.json", RiskCredentialAccess, DecisionBlock,
		"Code tham chiếu đến OAuth token file. Bị chặn."},
	{"secrets.json", RiskCredentialAccess, DecisionBlock,
		"Code tham chiếu đến secrets file. Bị chặn."},
	{"service_account", RiskCredentialAccess, DecisionBlock,
		"Code tham chiếu đến service account credential. Bị chặn."},
	{".pem", RiskCredentialAccess, DecisionBlock,
		"Code tham chiếu đến PEM certificate/key. Bị chặn."},
	{"/etc/shadow", RiskCredentialAccess, DecisionBlock,
		"Code tham chiếu đến shadow password file. Bị chặn."},
	{"/etc/passwd", RiskCredentialAccess, DecisionBlock,
		"Code tham chiếu đến passwd file. Bị chặn."},

	// ── System execution (always block) ───────────────────────────────────

	{"import subprocess", RiskHighRisk, DecisionBlock,
		"Code dùng subprocess (chạy lệnh hệ thống). Bị chặn trong sandbox."},
	{"from subprocess", RiskHighRisk, DecisionBlock,
		"Code dùng subprocess (chạy lệnh hệ thống). Bị chặn trong sandbox."},
	{"os.system(", RiskHighRisk, DecisionBlock,
		"Code dùng os.system() (chạy lệnh shell). Bị chặn trong sandbox."},
	{"os.popen(", RiskHighRisk, DecisionBlock,
		"Code dùng os.popen() (chạy lệnh shell). Bị chặn trong sandbox."},
	{"os.execv(", RiskHighRisk, DecisionBlock,
		"Code dùng os.execv() (thay thế process). Bị chặn trong sandbox."},
	{"os.execve(", RiskHighRisk, DecisionBlock,
		"Code dùng os.execve() (thay thế process). Bị chặn trong sandbox."},
	{"os.spawn", RiskHighRisk, DecisionBlock,
		"Code dùng os.spawn*() (tạo process con). Bị chặn trong sandbox."},
	{"ctypes", RiskHighRisk, DecisionBlock,
		"Code dùng ctypes (gọi thư viện C). Bị chặn trong sandbox."},
	{"cffi", RiskHighRisk, DecisionBlock,
		"Code dùng cffi (gọi C). Bị chặn trong sandbox."},
	{"import winreg", RiskHighRisk, DecisionBlock,
		"Code truy cap Windows Registry qua winreg. Bi chan trong sandbox."},
	{"winreg.", RiskHighRisk, DecisionBlock,
		"Code truy cap Windows Registry qua winreg. Bi chan trong sandbox."},
	{"hkey_local_machine", RiskHighRisk, DecisionBlock,
		"Code tham chieu HKEY_LOCAL_MACHINE. Bi chan trong sandbox."},
	{"hkey_current_user", RiskHighRisk, DecisionBlock,
		"Code tham chieu HKEY_CURRENT_USER. Bi chan trong sandbox."},
	{"__import__(", RiskHighRisk, DecisionBlock,
		"Code dùng __import__() động. Bị chặn vì có thể bypass import rules."},

	// ── External network (block - container has no network) ───────────────

	{"import socket", RiskExternalNetwork, DecisionBlock,
		"Code import socket (kết nối mạng). Sandbox không có mạng."},
	{"import urllib", RiskExternalNetwork, DecisionBlock,
		"Code import urllib (HTTP request). Sandbox không có mạng."},
	{"from urllib", RiskExternalNetwork, DecisionBlock,
		"Code import urllib (HTTP request). Sandbox không có mạng."},
	{"import requests", RiskExternalNetwork, DecisionBlock,
		"Code import requests (HTTP client). Sandbox không có mạng."},
	{"import httpx", RiskExternalNetwork, DecisionBlock,
		"Code import httpx (HTTP client). Sandbox không có mạng."},
	{"import aiohttp", RiskExternalNetwork, DecisionBlock,
		"Code import aiohttp (async HTTP). Sandbox không có mạng."},
	{"import paramiko", RiskExternalNetwork, DecisionBlock,
		"Code import paramiko (SSH). Sandbox không có mạng."},
	{"import ftplib", RiskExternalNetwork, DecisionBlock,
		"Code import ftplib (FTP). Sandbox không có mạng."},
	{"import smtplib", RiskExternalNetwork, DecisionBlock,
		"Code import smtplib (email). Sandbox không có mạng."},
	{"import imaplib", RiskExternalNetwork, DecisionBlock,
		"Code import imaplib (email). Sandbox không có mạng."},
	{"import http.client", RiskExternalNetwork, DecisionBlock,
		"Code import http.client (HTTP). Sandbox không có mạng."},

	// ── Dynamic execution (requires_approval) ────────────────────────────────

	{"eval(", RiskNeedsApproval, DecisionRequiresApproval,
		"Code dùng eval() (thực thi code động). Cần xác nhận của người dùng."},
	{"exec(", RiskNeedsApproval, DecisionRequiresApproval,
		"Code dùng exec() (thực thi code động). Cần xác nhận của người dùng."},
	{"compile(", RiskNeedsApproval, DecisionRequiresApproval,
		"Code dùng compile() (biên dịch code động). Cần xác nhận của người dùng."},
	{"importlib", RiskNeedsApproval, DecisionRequiresApproval,
		"Code dùng importlib (import động). Cần xác nhận của người dùng."},

	// ── File delete / overwrite (requires_approval) ──────────────────────────

	{"os.remove(", RiskNeedsApproval, DecisionRequiresApproval,
		"Code xóa file (os.remove). Cần xác nhận của người dùng."},
	{"os.unlink(", RiskNeedsApproval, DecisionRequiresApproval,
		"Code xóa file (os.unlink). Cần xác nhận của người dùng."},
	{"shutil.rmtree(", RiskNeedsApproval, DecisionRequiresApproval,
		"Code xóa cả thư mục (shutil.rmtree). Cần xác nhận của người dùng."},
	{"os.rmdir(", RiskNeedsApproval, DecisionRequiresApproval,
		"Code xóa thư mục (os.rmdir). Cần xác nhận của người dùng."},
	{"shutil.move(", RiskNeedsApproval, DecisionRequiresApproval,
		"Code di chuyển file (shutil.move, có thể ghi đè). Cần xác nhận."},
	{"os.rename(", RiskNeedsApproval, DecisionRequiresApproval,
		"Code đổi tên file (os.rename, có thể ghi đè). Cần xác nhận."},
	{".unlink(", RiskNeedsApproval, DecisionRequiresApproval,
		"Code xóa file (pathlib.Path.unlink). Cần xác nhận của người dùng."},
	{".rmdir(", RiskNeedsApproval, DecisionRequiresApproval,
		"Code xóa thư mục (pathlib.Path.rmdir). Cần xác nhận của người dùng."},
	{".rename(", RiskNeedsApproval, DecisionRequiresApproval,
		"Code đổi tên/di chuyển file (pathlib.Path.rename, có thể ghi đè). Cần xác nhận."},
	{".replace(", RiskNeedsApproval, DecisionRequiresApproval,
		"Code replace file (pathlib.Path.replace, có thể ghi đè). Cần xác nhận."},

	// ── Safe write (create new files) ─────────────────────────────────────

	{"import csv", RiskSafeRead, DecisionAllow,
		"Code dung csv module de doc du lieu. Duoc phep."},
	{"open(", RiskSafeWrite, DecisionAllow,
		"Code mở file để đọc/ghi. Được phép trong workspace."},
	{"os.makedirs(", RiskSafeWrite, DecisionAllow,
		"Code tạo thư mục. Được phép trong workspace."},
	{"os.mkdir(", RiskSafeWrite, DecisionAllow,
		"Code tạo thư mục. Được phép trong workspace."},
	{"shutil.copy(", RiskSafeWrite, DecisionAllow,
		"Code sao chép file (shutil.copy). Được phép trong workspace."},

	// ── Office libraries (safe_write) ─────────────────────────────────────

	{"import pandas", RiskSafeWrite, DecisionAllow,
		"Code dùng pandas để xử lý dữ liệu. Được phép."},
	{"import openpyxl", RiskSafeWrite, DecisionAllow,
		"Code dùng openpyxl để đọc/ghi Excel. Được phép."},
	{"import docx", RiskSafeWrite, DecisionAllow,
		"Code dùng python-docx để tạo Word. Được phép."},
	{"from docx", RiskSafeWrite, DecisionAllow,
		"Code dùng python-docx để tạo Word. Được phép."},
	{"import xlrd", RiskSafeWrite, DecisionAllow,
		"Code dùng xlrd để đọc Excel cũ. Được phép."},
	{"import yaml", RiskSafeRead, DecisionAllow,
		"Code dùng PyYAML để đọc config. Được phép."},
	{"import csv", RiskSafeRead, DecisionAllow,
		"Code dùng csv module. Được phép."},
	{"import json", RiskSafeRead, DecisionAllow,
		"Code dùng json module. Được phép."},

	// ── Safe read / stdlib (allow) ────────────────────────────────────────

	{"import os", RiskSafeRead, DecisionAllow,
		"Code import os (filesystem operations). Được phép trong workspace."},
	{"import sys", RiskSafeRead, DecisionAllow,
		"Code import sys. Được phép."},
	{"import re", RiskSafeRead, DecisionAllow,
		"Code import re (regex). Được phép."},
	{"import math", RiskSafeRead, DecisionAllow,
		"Code import math. Được phép."},
	{"import datetime", RiskSafeRead, DecisionAllow,
		"Code import datetime. Được phép."},
	{"import time", RiskSafeRead, DecisionAllow,
		"Code import time. Được phép."},
	{"import collections", RiskSafeRead, DecisionAllow,
		"Code import collections. Được phép."},
	{"import itertools", RiskSafeRead, DecisionAllow,
		"Code import itertools. Được phép."},
	{"import functools", RiskSafeRead, DecisionAllow,
		"Code import functools. Được phép."},
	{"import hashlib", RiskSafeRead, DecisionAllow,
		"Code import hashlib (hashing). Được phép."},
	{"import pathlib", RiskSafeRead, DecisionAllow,
		"Code import pathlib (filesystem). Được phép."},
	{"import glob", RiskSafeRead, DecisionAllow,
		"Code import glob. Được phép."},
	{"import shutil", RiskSafeRead, DecisionAllow,
		"Code import shutil (file operations). Được phép với giám sát."},
	{"import numpy", RiskSafeRead, DecisionAllow,
		"Code dùng numpy. Được phép."},
	{"import chardet", RiskSafeRead, DecisionAllow,
		"Code dùng chardet (encoding detection). Được phép."},
	{"print(", RiskSafeRead, DecisionAllow,
		"Code print. Được phép."},
}
