package safety

// ─── Shell danger rules ───────────────────────────────────────────────────────
//
// shellDangerRules is the master rule list for S1-T7.
// Rules are grouped by ThreatCategory and ordered from highest severity first.
// All patterns are matched case-insensitively against the full command string.
//
// Covered categories (S1-T7 requirements):
//   1. file_deletion      — rm, rmdir, shred, del /f, rd /s
//   2. system_shutdown    — shutdown, reboot, halt, poweroff, init 0/6
//   3. registry_access    — reg.exe, regedit, HKLM, HKCU, HKEY_
//   4. service_control    — systemctl, service, sc.exe, net start/stop
//   5. credential_access  — .env, id_rsa, token.json, /etc/shadow …
//
// Additional categories detected for completeness:
//   - data_overwrite       — overwrite redirect >, truncate
//   - network_access       — curl, wget, nc, ssh, scp
//   - privilege_escalation — sudo, su, runas, chmod/chown deep
//   - code_execution       — eval, base64 | bash, pipe to shell
//   - process_kill         — kill, killall, pkill, taskkill

var shellDangerRules = []DangerRule{

	// ══════════════════════════════════════════════════════════════════════
	// 1. CREDENTIAL ACCESS — block default
	// ══════════════════════════════════════════════════════════════════════

	{".env", ThreatCredentialAccess, SeverityHigh,
		"Lệnh tham chiếu đến file .env chứa biến môi trường nhạy cảm."},
	{"id_rsa", ThreatCredentialAccess, SeverityHigh,
		"Lệnh tham chiếu đến SSH private key id_rsa."},
	{"id_ed25519", ThreatCredentialAccess, SeverityHigh,
		"Lệnh tham chiếu đến SSH private key id_ed25519."},
	{"id_ecdsa", ThreatCredentialAccess, SeverityHigh,
		"Lệnh tham chiếu đến SSH private key id_ecdsa."},
	{"id_dsa", ThreatCredentialAccess, SeverityHigh,
		"Lệnh tham chiếu đến SSH private key id_dsa."},
	{"credentials.json", ThreatCredentialAccess, SeverityHigh,
		"Lệnh tham chiếu đến Google OAuth credentials file."},
	{"token.json", ThreatCredentialAccess, SeverityHigh,
		"Lệnh tham chiếu đến OAuth access token file."},
	{"secrets.json", ThreatCredentialAccess, SeverityHigh,
		"Lệnh tham chiếu đến secrets configuration file."},
	{"service_account.json", ThreatCredentialAccess, SeverityHigh,
		"Lệnh tham chiếu đến Google service account key."},
	{".netrc", ThreatCredentialAccess, SeverityHigh,
		"Lệnh tham chiếu đến .netrc chứa username/password."},
	{".pgpass", ThreatCredentialAccess, SeverityHigh,
		"Lệnh tham chiếu đến PostgreSQL password file."},
	{"kubeconfig", ThreatCredentialAccess, SeverityHigh,
		"Lệnh tham chiếu đến Kubernetes config (chứa bearer token)."},
	{"/etc/shadow", ThreatCredentialAccess, SeverityHigh,
		"Lệnh tham chiếu đến /etc/shadow (Unix password hashes)."},
	{"/etc/sudoers", ThreatCredentialAccess, SeverityHigh,
		"Lệnh tham chiếu đến /etc/sudoers (sudo privilege config)."},
	{".ssh/", ThreatCredentialAccess, SeverityHigh,
		"Lệnh tham chiếu đến thư mục .ssh/ (chứa SSH keys)."},
	{"known_hosts", ThreatCredentialAccess, SeverityMedium,
		"Lệnh tham chiếu đến SSH known_hosts file."},
	{".aws/credentials", ThreatCredentialAccess, SeverityHigh,
		"Lệnh tham chiếu đến AWS credentials file."},
	{".azure/", ThreatCredentialAccess, SeverityHigh,
		"Lệnh tham chiếu đến Azure credentials directory."},
	{"gcloud/", ThreatCredentialAccess, SeverityHigh,
		"Lệnh tham chiếu đến GCloud credentials directory."},
	{".docker/config.json", ThreatCredentialAccess, SeverityHigh,
		"Lệnh tham chiếu đến Docker registry credentials."},

	// ══════════════════════════════════════════════════════════════════════
	// 2. SYSTEM SHUTDOWN — always block
	// ══════════════════════════════════════════════════════════════════════

	{"shutdown", ThreatSystemShutdown, SeverityHigh,
		"Lệnh tắt máy hệ thống (shutdown). Có thể gây mất dữ liệu toàn bộ."},
	{"reboot", ThreatSystemShutdown, SeverityHigh,
		"Lệnh khởi động lại hệ thống (reboot). Có thể ngắt tất cả tiến trình."},
	{"halt", ThreatSystemShutdown, SeverityHigh,
		"Lệnh dừng hệ thống (halt). Tương đương tắt máy ngay lập tức."},
	{"poweroff", ThreatSystemShutdown, SeverityHigh,
		"Lệnh tắt nguồn hệ thống (poweroff)."},
	{"init 0", ThreatSystemShutdown, SeverityHigh,
		"Lệnh chuyển sang runlevel 0 (tắt máy)."},
	{"init 6", ThreatSystemShutdown, SeverityHigh,
		"Lệnh chuyển sang runlevel 6 (khởi động lại)."},
	{"telinit 0", ThreatSystemShutdown, SeverityHigh,
		"Lệnh telinit 0 (tắt máy qua init)."},

	// Windows equivalents
	{"shutdown /s", ThreatSystemShutdown, SeverityHigh,
		"Lệnh tắt máy Windows (shutdown /s)."},
	{"shutdown /r", ThreatSystemShutdown, SeverityHigh,
		"Lệnh khởi động lại Windows (shutdown /r)."},
	{"stop-computer", ThreatSystemShutdown, SeverityHigh,
		"Lệnh tắt máy PowerShell (Stop-Computer)."},
	{"restart-computer", ThreatSystemShutdown, SeverityHigh,
		"Lệnh khởi động lại PowerShell (Restart-Computer)."},

	// ══════════════════════════════════════════════════════════════════════
	// 3. REGISTRY ACCESS (Windows) — block
	// ══════════════════════════════════════════════════════════════════════

	{"reg.exe", ThreatRegistryAccess, SeverityHigh,
		"Lệnh reg.exe truy cập Windows Registry từ command line."},
	{"regedit", ThreatRegistryAccess, SeverityHigh,
		"Lệnh mở Registry Editor (regedit). Có thể sửa cấu hình hệ thống."},
	{"regedit.exe", ThreatRegistryAccess, SeverityHigh,
		"Lệnh mở Registry Editor (regedit.exe)."},
	{"reg add", ThreatRegistryAccess, SeverityHigh,
		"Lệnh thêm registry key (reg add). Sửa cấu hình hệ thống."},
	{"reg delete", ThreatRegistryAccess, SeverityHigh,
		"Lệnh xóa registry key (reg delete). Có thể làm hỏng hệ thống."},
	{"reg query", ThreatRegistryAccess, SeverityMedium,
		"Lệnh đọc registry key (reg query). Có thể đọc thông tin nhạy cảm."},
	{"reg export", ThreatRegistryAccess, SeverityMedium,
		"Lệnh export registry (reg export). Có thể lấy thông tin nhạy cảm."},
	{"reg import", ThreatRegistryAccess, SeverityHigh,
		"Lệnh import registry (reg import). Có thể ghi đè cấu hình hệ thống."},
	{"hklm\\", ThreatRegistryAccess, SeverityHigh,
		"Lệnh tham chiếu đến HKEY_LOCAL_MACHINE (registry hệ thống)."},
	{"hkcu\\", ThreatRegistryAccess, SeverityMedium,
		"Lệnh tham chiếu đến HKEY_CURRENT_USER (registry người dùng)."},
	{"hkey_local_machine", ThreatRegistryAccess, SeverityHigh,
		"Lệnh tham chiếu đến HKEY_LOCAL_MACHINE registry hive."},
	{"hkey_current_user", ThreatRegistryAccess, SeverityMedium,
		"Lệnh tham chiếu đến HKEY_CURRENT_USER registry hive."},
	{"hkey_classes_root", ThreatRegistryAccess, SeverityHigh,
		"Lệnh tham chiếu đến HKEY_CLASSES_ROOT registry hive."},
	{"set-itemproperty", ThreatRegistryAccess, SeverityHigh,
		"PowerShell Set-ItemProperty có thể ghi registry."},
	{"get-itemproperty", ThreatRegistryAccess, SeverityMedium,
		"PowerShell Get-ItemProperty có thể đọc registry."},
	{"new-item hk", ThreatRegistryAccess, SeverityHigh,
		"PowerShell New-Item trên registry hive."},

	// ══════════════════════════════════════════════════════════════════════
	// 4. SERVICE CONTROL — block
	// ══════════════════════════════════════════════════════════════════════

	{"systemctl", ThreatServiceControl, SeverityHigh,
		"Lệnh systemctl quản lý systemd service. Có thể dừng/khởi động service hệ thống."},
	{"service ", ThreatServiceControl, SeverityHigh,
		"Lệnh service quản lý init.d service."},
	{"sc.exe", ThreatServiceControl, SeverityHigh,
		"Lệnh sc.exe (Windows Service Control). Quản lý Windows services."},
	{"sc start", ThreatServiceControl, SeverityHigh,
		"Lệnh khởi động Windows service (sc start)."},
	{"sc stop", ThreatServiceControl, SeverityHigh,
		"Lệnh dừng Windows service (sc stop)."},
	{"sc create", ThreatServiceControl, SeverityHigh,
		"Lệnh tạo Windows service mới (sc create)."},
	{"sc delete", ThreatServiceControl, SeverityHigh,
		"Lệnh xóa Windows service (sc delete)."},
	{"sc config", ThreatServiceControl, SeverityHigh,
		"Lệnh cấu hình Windows service (sc config)."},
	{"net start", ThreatServiceControl, SeverityHigh,
		"Lệnh khởi động Windows service (net start)."},
	{"net stop", ThreatServiceControl, SeverityHigh,
		"Lệnh dừng Windows service (net stop)."},
	{"invoke-service", ThreatServiceControl, SeverityHigh,
		"PowerShell gọi service (Invoke-Service)."},
	{"start-service", ThreatServiceControl, SeverityHigh,
		"PowerShell khởi động service (Start-Service)."},
	{"stop-service", ThreatServiceControl, SeverityHigh,
		"PowerShell dừng service (Stop-Service)."},
	{"restart-service", ThreatServiceControl, SeverityHigh,
		"PowerShell khởi động lại service (Restart-Service)."},
	{"chkconfig", ThreatServiceControl, SeverityHigh,
		"Lệnh chkconfig quản lý service khởi động cùng hệ thống."},
	{"update-rc.d", ThreatServiceControl, SeverityHigh,
		"Lệnh update-rc.d quản lý init.d startup."},

	// ══════════════════════════════════════════════════════════════════════
	// 5. FILE DELETION — needs_approval
	// ══════════════════════════════════════════════════════════════════════

	{"rm ", ThreatFileDeletion, SeverityMedium,
		"Lệnh rm xóa file. Hành động không thể hoàn tác."},
	{"rm\t", ThreatFileDeletion, SeverityMedium,
		"Lệnh rm xóa file. Hành động không thể hoàn tác."},
	{"rmdir", ThreatFileDeletion, SeverityMedium,
		"Lệnh rmdir xóa thư mục. Hành động không thể hoàn tác."},
	{"rm -rf", ThreatFileDeletion, SeverityHigh,
		"Lệnh rm -rf xóa đệ quy không hỏi. Cực kỳ nguy hiểm nếu dùng sai đường dẫn."},
	{"rm -r", ThreatFileDeletion, SeverityHigh,
		"Lệnh rm -r xóa đệ quy thư mục. Có thể xóa nhiều file."},
	{"shred", ThreatFileDeletion, SeverityHigh,
		"Lệnh shred xóa file vĩnh viễn (không thể khôi phục)."},
	{"wipe", ThreatFileDeletion, SeverityHigh,
		"Lệnh wipe xóa dữ liệu vĩnh viễn."},
	{"del /f", ThreatFileDeletion, SeverityMedium,
		"Lệnh del /f (Windows) xóa file bắt buộc."},
	{"del /s", ThreatFileDeletion, SeverityHigh,
		"Lệnh del /s (Windows) xóa file đệ quy."},
	{"rd /s", ThreatFileDeletion, SeverityHigh,
		"Lệnh rd /s (Windows) xóa thư mục đệ quy."},
	{"remove-item", ThreatFileDeletion, SeverityMedium,
		"PowerShell Remove-Item xóa file hoặc thư mục."},
	{"clear-content", ThreatFileDeletion, SeverityMedium,
		"PowerShell Clear-Content xóa nội dung file."},

	// ══════════════════════════════════════════════════════════════════════
	// DATA OVERWRITE — needs_approval
	// ══════════════════════════════════════════════════════════════════════

	{" > ", ThreatDataOverwrite, SeverityMedium,
		"Redirect (>) ghi đè nội dung file hiện có. Dữ liệu cũ bị mất."},
	{"truncate", ThreatDataOverwrite, SeverityMedium,
		"Lệnh truncate làm rỗng nội dung file."},
	{"tee --", ThreatDataOverwrite, SeverityMedium,
		"Lệnh tee với flag có thể ghi đè file."},

	// ══════════════════════════════════════════════════════════════════════
	// PRIVILEGE ESCALATION — block
	// ══════════════════════════════════════════════════════════════════════

	{"sudo ", ThreatPrivilegeEscalation, SeverityHigh,
		"Lệnh sudo thực thi với quyền root. Nguy hiểm trong sandbox."},
	{"su ", ThreatPrivilegeEscalation, SeverityHigh,
		"Lệnh su chuyển sang user khác (thường là root)."},
	{"runas ", ThreatPrivilegeEscalation, SeverityHigh,
		"Lệnh runas (Windows) chạy với quyền user khác."},
	{"chmod 777", ThreatPrivilegeEscalation, SeverityMedium,
		"Lệnh chmod 777 mở quyền truy cập toàn bộ cho file."},
	{"chmod +s", ThreatPrivilegeEscalation, SeverityHigh,
		"Lệnh chmod +s thiết lập SUID bit (leo thang đặc quyền)."},
	{"chown root", ThreatPrivilegeEscalation, SeverityHigh,
		"Lệnh chown root chuyển owner về root."},
	{"newgrp", ThreatPrivilegeEscalation, SeverityHigh,
		"Lệnh newgrp đổi group ID."},
	{"usermod", ThreatPrivilegeEscalation, SeverityHigh,
		"Lệnh usermod sửa thông tin user hệ thống."},
	{"passwd", ThreatPrivilegeEscalation, SeverityHigh,
		"Lệnh passwd thay đổi mật khẩu user."},

	// ══════════════════════════════════════════════════════════════════════
	// NETWORK ACCESS — block (sandbox has --network none)
	// ══════════════════════════════════════════════════════════════════════

	{"curl ", ThreatNetworkAccess, SeverityHigh,
		"Lệnh curl gửi HTTP request. Sandbox không có kết nối mạng."},
	{"wget ", ThreatNetworkAccess, SeverityHigh,
		"Lệnh wget tải file từ mạng. Sandbox không có kết nối mạng."},
	{"nc ", ThreatNetworkAccess, SeverityHigh,
		"Lệnh netcat (nc) tạo kết nối mạng. Sandbox không có kết nối mạng."},
	{"ncat ", ThreatNetworkAccess, SeverityHigh,
		"Lệnh ncat tạo kết nối mạng. Sandbox không có kết nối mạng."},
	{"ssh ", ThreatNetworkAccess, SeverityHigh,
		"Lệnh SSH kết nối máy chủ từ xa. Sandbox không có kết nối mạng."},
	{"scp ", ThreatNetworkAccess, SeverityHigh,
		"Lệnh scp copy file qua SSH. Sandbox không có kết nối mạng."},
	{"sftp ", ThreatNetworkAccess, SeverityHigh,
		"Lệnh SFTP truyền file. Sandbox không có kết nối mạng."},
	{"ftp ", ThreatNetworkAccess, SeverityHigh,
		"Lệnh FTP truyền file. Sandbox không có kết nối mạng."},
	{"rsync ", ThreatNetworkAccess, SeverityHigh,
		"Lệnh rsync đồng bộ file (có thể remote). Sandbox không có kết nối mạng."},
	{"ping ", ThreatNetworkAccess, SeverityMedium,
		"Lệnh ping kiểm tra kết nối mạng. Sandbox không có kết nối mạng."},
	{"nslookup", ThreatNetworkAccess, SeverityMedium,
		"Lệnh nslookup tra cứu DNS. Sandbox không có kết nối mạng."},
	{"dig ", ThreatNetworkAccess, SeverityMedium,
		"Lệnh dig tra cứu DNS. Sandbox không có kết nối mạng."},
	{"telnet", ThreatNetworkAccess, SeverityHigh,
		"Lệnh telnet kết nối mạng. Sandbox không có kết nối mạng."},
	{"invoke-webrequest", ThreatNetworkAccess, SeverityHigh,
		"PowerShell Invoke-WebRequest gửi HTTP. Sandbox không có kết nối mạng."},
	{"invoke-restmethod", ThreatNetworkAccess, SeverityHigh,
		"PowerShell Invoke-RestMethod gọi REST API. Sandbox không có kết nối mạng."},

	// ══════════════════════════════════════════════════════════════════════
	// PROCESS KILL — block
	// ══════════════════════════════════════════════════════════════════════

	{"killall", ThreatProcessKill, SeverityHigh,
		"Lệnh killall kết thúc tất cả process có tên đó."},
	{"pkill", ThreatProcessKill, SeverityHigh,
		"Lệnh pkill kết thúc process theo pattern."},
	{"kill -9", ThreatProcessKill, SeverityHigh,
		"Lệnh kill -9 (SIGKILL) buộc kết thúc process ngay lập tức."},
	{"taskkill", ThreatProcessKill, SeverityHigh,
		"Lệnh taskkill (Windows) kết thúc process."},
	{"stop-process", ThreatProcessKill, SeverityHigh,
		"PowerShell Stop-Process kết thúc process."},

	// ══════════════════════════════════════════════════════════════════════
	// CODE EXECUTION (shell-level eval/decode) — block
	// ══════════════════════════════════════════════════════════════════════

	{"eval ", ThreatCodeExecution, SeverityHigh,
		"Lệnh eval thực thi code động từ string. Dễ bị injection."},
	{"base64 -d", ThreatCodeExecution, SeverityHigh,
		"Decode base64 thường được dùng để obfuscate payload."},
	{"| bash", ThreatCodeExecution, SeverityHigh,
		"Pipe vào bash — thực thi lệnh từ stdin. Pattern phổ biến của malware."},
	{"| sh", ThreatCodeExecution, SeverityHigh,
		"Pipe vào sh — thực thi lệnh từ stdin."},
	{"xargs sh", ThreatCodeExecution, SeverityHigh,
		"xargs sh thực thi lệnh được xây dựng động."},
	{"iex(", ThreatCodeExecution, SeverityHigh,
		"PowerShell IEX (Invoke-Expression) thực thi code động. Pattern phổ biến của malware."},
	{"invoke-expression", ThreatCodeExecution, SeverityHigh,
		"PowerShell Invoke-Expression thực thi code động."},
}

// ─── Python danger rules ──────────────────────────────────────────────────────

// pythonDangerRules scans Python source code for dangerous patterns.
// These are used by the DangerDetector, complementing the policy matrix in
// internal/policies/python_rules.go (which is first-match only).
var pythonDangerRules = []DangerRule{

	// Credential access
	{".env", ThreatCredentialAccess, SeverityHigh,
		"Code Python tham chiếu đến .env file."},
	{"id_rsa", ThreatCredentialAccess, SeverityHigh,
		"Code Python tham chiếu đến SSH private key."},
	{"credentials.json", ThreatCredentialAccess, SeverityHigh,
		"Code Python tham chiếu đến Google credentials file."},
	{"token.json", ThreatCredentialAccess, SeverityHigh,
		"Code Python tham chiếu đến OAuth token file."},
	{"secrets.json", ThreatCredentialAccess, SeverityHigh,
		"Code Python tham chiếu đến secrets file."},
	{"/etc/shadow", ThreatCredentialAccess, SeverityHigh,
		"Code Python tham chiếu đến /etc/shadow password file."},
	{"/etc/passwd", ThreatCredentialAccess, SeverityHigh,
		"Code Python tham chiếu đến /etc/passwd."},
	{".pem", ThreatCredentialAccess, SeverityHigh,
		"Code Python tham chiếu đến PEM certificate/key."},
	{".aws/credentials", ThreatCredentialAccess, SeverityHigh,
		"Code Python tham chiếu đến AWS credentials."},

	// System shutdown commands embedded in Python strings.
	{"shutdown /s", ThreatSystemShutdown, SeverityHigh,
		"Code Python chua lenh shutdown Windows."},
	{"shutdown", ThreatSystemShutdown, SeverityHigh,
		"Code Python chua lenh shutdown he thong."},
	{"reboot", ThreatSystemShutdown, SeverityHigh,
		"Code Python chua lenh reboot he thong."},
	{"poweroff", ThreatSystemShutdown, SeverityHigh,
		"Code Python chua lenh poweroff he thong."},

	// Windows registry access.
	{"import winreg", ThreatRegistryAccess, SeverityHigh,
		"Code Python import winreg de truy cap Windows Registry."},
	{"winreg.", ThreatRegistryAccess, SeverityHigh,
		"Code Python dung winreg de truy cap Windows Registry."},
	{"hkey_local_machine", ThreatRegistryAccess, SeverityHigh,
		"Code Python tham chieu HKEY_LOCAL_MACHINE."},
	{"hkey_current_user", ThreatRegistryAccess, SeverityMedium,
		"Code Python tham chieu HKEY_CURRENT_USER."},

	// System execution
	{"import subprocess", ThreatCodeExecution, SeverityHigh,
		"Code Python import subprocess — có thể chạy lệnh hệ thống."},
	{"from subprocess", ThreatCodeExecution, SeverityHigh,
		"Code Python import từ subprocess module."},
	{"os.system(", ThreatCodeExecution, SeverityHigh,
		"os.system() thực thi shell command. Nguy hiểm trong sandbox."},
	{"os.popen(", ThreatCodeExecution, SeverityHigh,
		"os.popen() mở shell pipeline. Nguy hiểm trong sandbox."},
	{"os.execv(", ThreatCodeExecution, SeverityHigh,
		"os.execv() thay thế process hiện tại. Cực kỳ nguy hiểm."},
	{"os.execve(", ThreatCodeExecution, SeverityHigh,
		"os.execve() thay thế process. Cực kỳ nguy hiểm."},
	{"os.spawn", ThreatCodeExecution, SeverityHigh,
		"os.spawn*() tạo process con."},
	{"ctypes", ThreatCodeExecution, SeverityHigh,
		"ctypes gọi thư viện C native. Có thể bypass sandbox."},
	{"__import__(", ThreatCodeExecution, SeverityHigh,
		"__import__() dynamic import. Có thể import module nguy hiểm."},
	{"eval(", ThreatCodeExecution, SeverityMedium,
		"eval() thực thi code động từ string."},
	{"exec(", ThreatCodeExecution, SeverityMedium,
		"exec() thực thi code động."},
	{"compile(", ThreatCodeExecution, SeverityMedium,
		"compile() biên dịch code động."},

	// Network access
	{"import socket", ThreatNetworkAccess, SeverityHigh,
		"Code Python import socket — tạo kết nối mạng."},
	{"import urllib", ThreatNetworkAccess, SeverityHigh,
		"Code Python import urllib — gửi HTTP request."},
	{"from urllib", ThreatNetworkAccess, SeverityHigh,
		"Code Python import từ urllib."},
	{"import requests", ThreatNetworkAccess, SeverityHigh,
		"Code Python import requests HTTP library."},
	{"import httpx", ThreatNetworkAccess, SeverityHigh,
		"Code Python import httpx HTTP client."},
	{"import aiohttp", ThreatNetworkAccess, SeverityHigh,
		"Code Python import aiohttp async HTTP."},
	{"import paramiko", ThreatNetworkAccess, SeverityHigh,
		"Code Python import paramiko SSH library."},
	{"import smtplib", ThreatNetworkAccess, SeverityHigh,
		"Code Python import smtplib (email)."},

	// File deletion
	{"os.remove(", ThreatFileDeletion, SeverityMedium,
		"os.remove() xóa file. Cần xác nhận."},
	{"os.unlink(", ThreatFileDeletion, SeverityMedium,
		"os.unlink() xóa file. Cần xác nhận."},
	{"shutil.rmtree(", ThreatFileDeletion, SeverityHigh,
		"shutil.rmtree() xóa đệ quy cả thư mục. Nguy hiểm."},
	{"os.rmdir(", ThreatFileDeletion, SeverityMedium,
		"os.rmdir() xóa thư mục rỗng."},
	{"pathlib", ThreatFileDeletion, SeverityLow,
		"pathlib có thể dùng để xóa file (Path.unlink, rmdir)."},
}
