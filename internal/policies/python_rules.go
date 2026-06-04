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
// │ Credential file access   │ sensitive_read     │ block            │
// │ System execution         │ destructive             │ block            │
// │ Network imports/calls    │ external_write      │ block            │
// │ Dynamic execution        │ destructive        │ requires_approval│
// │ File delete / overwrite  │ destructive        │ requires_approval│
// │ File create / write      │ local_write            │ allow            │
// │ Read-only / stdlib       │ safe_read             │ allow            │
// └──────────────────────────┴──────────────────────┴──────────────────┘
var pythonRules = []MatrixEntry{

	// ── Credential access (always block) ──────────────────────────────────

	{".env", RiskSensitiveRead, DecisionBlock,
		"Code tham chiếu đến file .env (credential). Bị chặn."},
	{"id_rsa", RiskSensitiveRead, DecisionBlock,
		"Code tham chiếu đến SSH private key. Bị chặn."},
	{"credentials.json", RiskSensitiveRead, DecisionBlock,
		"Code tham chiếu đến Google credentials. Bị chặn."},
	{"token.json", RiskSensitiveRead, DecisionBlock,
		"Code tham chiếu đến OAuth token file. Bị chặn."},
	{"secrets.json", RiskSensitiveRead, DecisionBlock,
		"Code tham chiếu đến secrets file. Bị chặn."},
	{"service_account", RiskSensitiveRead, DecisionBlock,
		"Code tham chiếu đến service account credential. Bị chặn."},
	{".pem", RiskSensitiveRead, DecisionBlock,
		"Code tham chiếu đến PEM certificate/key. Bị chặn."},
	{"/etc/shadow", RiskSensitiveRead, DecisionBlock,
		"Code tham chiếu đến shadow password file. Bị chặn."},
	{"/etc/passwd", RiskSensitiveRead, DecisionBlock,
		"Code tham chiếu đến passwd file. Bị chặn."},

	// ── System execution (always block) ───────────────────────────────────

	{"import subprocess", RiskDestructive, DecisionBlock,
		"Code dùng subprocess (chạy lệnh hệ thống). Bị chặn trong sandbox."},
	{"from subprocess", RiskDestructive, DecisionBlock,
		"Code dùng subprocess (chạy lệnh hệ thống). Bị chặn trong sandbox."},
	{"os.system(", RiskDestructive, DecisionBlock,
		"Code dùng os.system() (chạy lệnh shell). Bị chặn trong sandbox."},
	{"os.popen(", RiskDestructive, DecisionBlock,
		"Code dùng os.popen() (chạy lệnh shell). Bị chặn trong sandbox."},
	{"os.execv(", RiskDestructive, DecisionBlock,
		"Code dùng os.execv() (thay thế process). Bị chặn trong sandbox."},
	{"os.execve(", RiskDestructive, DecisionBlock,
		"Code dùng os.execve() (thay thế process). Bị chặn trong sandbox."},
	{"os.spawn", RiskDestructive, DecisionBlock,
		"Code dùng os.spawn*() (tạo process con). Bị chặn trong sandbox."},
	{"ctypes", RiskDestructive, DecisionBlock,
		"Code dùng ctypes (gọi thư viện C). Bị chặn trong sandbox."},
	{"cffi", RiskDestructive, DecisionBlock,
		"Code dùng cffi (gọi C). Bị chặn trong sandbox."},
	{"import winreg", RiskDestructive, DecisionBlock,
		"Code truy cap Windows Registry qua winreg. Bi chan trong sandbox."},
	{"winreg.", RiskDestructive, DecisionBlock,
		"Code truy cap Windows Registry qua winreg. Bi chan trong sandbox."},
	{"hkey_local_machine", RiskDestructive, DecisionBlock,
		"Code tham chieu HKEY_LOCAL_MACHINE. Bi chan trong sandbox."},
	{"hkey_current_user", RiskDestructive, DecisionBlock,
		"Code tham chieu HKEY_CURRENT_USER. Bi chan trong sandbox."},
	{"__import__(", RiskDestructive, DecisionBlock,
		"Code dùng __import__() động. Bị chặn vì có thể bypass import rules."},

	// ── External network (block - container has no network) ───────────────

	{"import socket", RiskExternalWrite, DecisionBlock,
		"Code import socket (kết nối mạng). Sandbox không có mạng."},
	{"import urllib", RiskExternalWrite, DecisionBlock,
		"Code import urllib (HTTP request). Sandbox không có mạng."},
	{"from urllib", RiskExternalWrite, DecisionBlock,
		"Code import urllib (HTTP request). Sandbox không có mạng."},
	{"import requests", RiskExternalWrite, DecisionBlock,
		"Code import requests (HTTP client). Sandbox không có mạng."},
	{"import httpx", RiskExternalWrite, DecisionBlock,
		"Code import httpx (HTTP client). Sandbox không có mạng."},
	{"import aiohttp", RiskExternalWrite, DecisionBlock,
		"Code import aiohttp (async HTTP). Sandbox không có mạng."},
	{"import paramiko", RiskExternalWrite, DecisionBlock,
		"Code import paramiko (SSH). Sandbox không có mạng."},
	{"import ftplib", RiskExternalWrite, DecisionBlock,
		"Code import ftplib (FTP). Sandbox không có mạng."},
	{"import smtplib", RiskExternalWrite, DecisionBlock,
		"Code import smtplib (email). Sandbox không có mạng."},
	{"import imaplib", RiskExternalWrite, DecisionBlock,
		"Code import imaplib (email). Sandbox không có mạng."},
	{"import http.client", RiskExternalWrite, DecisionBlock,
		"Code import http.client (HTTP). Sandbox không có mạng."},

	// ── Dynamic execution (requires_approval) ────────────────────────────────

	{"eval(", RiskDestructive, DecisionRequiresApproval,
		"Code dùng eval() (thực thi code động). Cần xác nhận của người dùng."},
	{"exec(", RiskDestructive, DecisionRequiresApproval,
		"Code dùng exec() (thực thi code động). Cần xác nhận của người dùng."},
	{"compile(", RiskDestructive, DecisionRequiresApproval,
		"Code dùng compile() (biên dịch code động). Cần xác nhận của người dùng."},
	{"importlib", RiskDestructive, DecisionRequiresApproval,
		"Code dùng importlib (import động). Cần xác nhận của người dùng."},

	// ── File delete / overwrite (requires_approval) ──────────────────────────

	{"os.remove(", RiskDestructive, DecisionRequiresApproval,
		"Code xóa file (os.remove). Cần xác nhận của người dùng."},
	{"os.unlink(", RiskDestructive, DecisionRequiresApproval,
		"Code xóa file (os.unlink). Cần xác nhận của người dùng."},
	{"shutil.rmtree(", RiskDestructive, DecisionRequiresApproval,
		"Code xóa cả thư mục (shutil.rmtree). Cần xác nhận của người dùng."},
	{"os.rmdir(", RiskDestructive, DecisionRequiresApproval,
		"Code xóa thư mục (os.rmdir). Cần xác nhận của người dùng."},
	{"shutil.move(", RiskDestructive, DecisionRequiresApproval,
		"Code di chuyển file (shutil.move, có thể ghi đè). Cần xác nhận."},
	{"os.rename(", RiskDestructive, DecisionRequiresApproval,
		"Code đổi tên file (os.rename, có thể ghi đè). Cần xác nhận."},
	{".unlink(", RiskDestructive, DecisionRequiresApproval,
		"Code xóa file (pathlib.Path.unlink). Cần xác nhận của người dùng."},
	{".rmdir(", RiskDestructive, DecisionRequiresApproval,
		"Code xóa thư mục (pathlib.Path.rmdir). Cần xác nhận của người dùng."},
	{".rename(", RiskDestructive, DecisionRequiresApproval,
		"Code đổi tên/di chuyển file (pathlib.Path.rename, có thể ghi đè). Cần xác nhận."},
	{".replace(", RiskDestructive, DecisionRequiresApproval,
		"Code replace file (pathlib.Path.replace, có thể ghi đè). Cần xác nhận."},

	// ── Safe write (create new files) ─────────────────────────────────────

	{"import csv", RiskSafeRead, DecisionAllow,
		"Code dung csv module de doc du lieu. Duoc phep."},
	{"open(", RiskLocalWrite, DecisionAllow,
		"Code mở file để đọc/ghi. Được phép trong workspace."},
	{"os.makedirs(", RiskLocalWrite, DecisionAllow,
		"Code tạo thư mục. Được phép trong workspace."},
	{"os.mkdir(", RiskLocalWrite, DecisionAllow,
		"Code tạo thư mục. Được phép trong workspace."},
	{"shutil.copy(", RiskLocalWrite, DecisionAllow,
		"Code sao chép file (shutil.copy). Được phép trong workspace."},

	// ── Office libraries (local_write) ─────────────────────────────────────

	{"import pandas", RiskLocalWrite, DecisionAllow,
		"Code dùng pandas để xử lý dữ liệu. Được phép."},
	{"import openpyxl", RiskLocalWrite, DecisionAllow,
		"Code dùng openpyxl để đọc/ghi Excel. Được phép."},
	{"import docx", RiskLocalWrite, DecisionAllow,
		"Code dùng python-docx để tạo Word. Được phép."},
	{"from docx", RiskLocalWrite, DecisionAllow,
		"Code dùng python-docx để tạo Word. Được phép."},
	{"import xlrd", RiskLocalWrite, DecisionAllow,
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
