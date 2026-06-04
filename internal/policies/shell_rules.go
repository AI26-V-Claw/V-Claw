package policies

// shellRules is the ordered policy matrix for sandbox.runShell requests.
//
// Matching is first-match: the checker iterates from the top and returns the
// first entry whose Pattern is found in the normalised (lowercase) command.
// More specific / higher-risk rules are placed before more general ones.
//
// Policy matrix (summary):
//
// ┌──────────────────────────┬─────────────────────┬──────────────────┐
// │ Pattern / Category       │ Risk Level           │ Decision         │
// ├──────────────────────────┼─────────────────────┼──────────────────┤
// │ Credential files         │ sensitive_read    │ block            │
// │ System-deep commands     │ destructive            │ block            │
// │ Network tools            │ external_write     │ block            │
// │ Destructive file ops     │ destructive       │ requires_approval│
// │ Overwrite redirect (>)   │ destructive       │ requires_approval│
// │ Create / new file ops    │ local_write       │ requires_approval*│
// │ Read-only ops            │ safe_read         │ requires_approval*│
// └──────────────────────────┴─────────────────────┴──────────────────┘
//
// * Some low-risk entries are represented as DecisionAllow in this matrix so
// their risk can be classified precisely. RuleBasedChecker then applies the
// sandbox contract invariant: sandbox.runShell is code_execution and must be
// approved before execution.
var shellRules = []MatrixEntry{
	// Windows service control and registry commands (always block).
	{"sc.exe", RiskDestructive, DecisionBlock,
		"Lenh sc.exe quan ly Windows service. Bi chan hoan toan."},
	{"sc stop", RiskDestructive, DecisionBlock,
		"Lenh dung Windows service. Bi chan hoan toan."},
	{"sc start", RiskDestructive, DecisionBlock,
		"Lenh khoi dong Windows service. Bi chan hoan toan."},
	{"sc create", RiskDestructive, DecisionBlock,
		"Lenh tao Windows service. Bi chan hoan toan."},
	{"sc delete", RiskDestructive, DecisionBlock,
		"Lenh xoa Windows service. Bi chan hoan toan."},
	{"net stop", RiskDestructive, DecisionBlock,
		"Lenh dung Windows service bang net stop. Bi chan hoan toan."},
	{"net start", RiskDestructive, DecisionBlock,
		"Lenh khoi dong Windows service bang net start. Bi chan hoan toan."},
	{"reg.exe", RiskDestructive, DecisionBlock,
		"Lenh reg.exe truy cap Windows Registry. Bi chan hoan toan."},
	{"reg add", RiskDestructive, DecisionBlock,
		"Lenh them Windows Registry key. Bi chan hoan toan."},
	{"reg delete", RiskDestructive, DecisionBlock,
		"Lenh xoa Windows Registry key. Bi chan hoan toan."},
	{"reg query", RiskDestructive, DecisionBlock,
		"Lenh doc Windows Registry. Bi chan hoan toan."},
	{"regedit", RiskDestructive, DecisionBlock,
		"Lenh mo Registry Editor. Bi chan hoan toan."},
	{"hklm\\", RiskDestructive, DecisionBlock,
		"Lenh tham chieu HKEY_LOCAL_MACHINE. Bi chan hoan toan."},
	{"hkcu\\", RiskDestructive, DecisionBlock,
		"Lenh tham chieu HKEY_CURRENT_USER. Bi chan hoan toan."},
	{"hkey_local_machine", RiskDestructive, DecisionBlock,
		"Lenh tham chieu HKEY_LOCAL_MACHINE. Bi chan hoan toan."},
	{"hkey_current_user", RiskDestructive, DecisionBlock,
		"Lenh tham chieu HKEY_CURRENT_USER. Bi chan hoan toan."},

	// ── Credential access (highest priority, always block) ────────────────

	{".env", RiskSensitiveRead, DecisionBlock,
		"Lệnh tham chiếu đến file credential (.env). Bị chặn mặc định."},
	{"id_rsa", RiskSensitiveRead, DecisionBlock,
		"Lệnh tham chiếu đến SSH private key. Bị chặn mặc định."},
	{"id_ed25519", RiskSensitiveRead, DecisionBlock,
		"Lệnh tham chiếu đến SSH private key. Bị chặn mặc định."},
	{"id_ecdsa", RiskSensitiveRead, DecisionBlock,
		"Lệnh tham chiếu đến SSH private key. Bị chặn mặc định."},
	{"credentials.json", RiskSensitiveRead, DecisionBlock,
		"Lệnh tham chiếu đến file credential Google. Bị chặn mặc định."},
	{"token.json", RiskSensitiveRead, DecisionBlock,
		"Lệnh tham chiếu đến OAuth token. Bị chặn mặc định."},
	{"secrets.json", RiskSensitiveRead, DecisionBlock,
		"Lệnh tham chiếu đến file secrets. Bị chặn mặc định."},
	{"service_account", RiskSensitiveRead, DecisionBlock,
		"Lệnh tham chiếu đến service account credential. Bị chặn mặc định."},
	{".netrc", RiskSensitiveRead, DecisionBlock,
		"Lệnh tham chiếu đến file .netrc chứa credential. Bị chặn mặc định."},
	{".pgpass", RiskSensitiveRead, DecisionBlock,
		"Lệnh tham chiếu đến file PostgreSQL password. Bị chặn mặc định."},
	{"kubeconfig", RiskSensitiveRead, DecisionBlock,
		"Lệnh tham chiếu đến Kubernetes config (chứa token). Bị chặn mặc định."},
	{"/etc/shadow", RiskSensitiveRead, DecisionBlock,
		"Lệnh tham chiếu đến file shadow password. Bị chặn mặc định."},
	{".pem", RiskSensitiveRead, DecisionBlock,
		"Lệnh tham chiếu đến file PEM (certificate/key). Bị chặn mặc định."},
	{".p12", RiskSensitiveRead, DecisionBlock,
		"Lệnh tham chiếu đến file PKCS12 (certificate/key). Bị chặn mặc định."},

	// ── High-risk system commands (always block) ──────────────────────────

	{"shutdown", RiskDestructive, DecisionBlock,
		"Lệnh tắt máy hệ thống. Bị chặn hoàn toàn."},
	{"reboot", RiskDestructive, DecisionBlock,
		"Lệnh khởi động lại hệ thống. Bị chặn hoàn toàn."},
	{"halt", RiskDestructive, DecisionBlock,
		"Lệnh dừng hệ thống. Bị chặn hoàn toàn."},
	{"poweroff", RiskDestructive, DecisionBlock,
		"Lệnh tắt nguồn hệ thống. Bị chặn hoàn toàn."},
	{"systemctl", RiskDestructive, DecisionBlock,
		"Lệnh quản lý systemd service. Bị chặn hoàn toàn."},
	{"service ", RiskDestructive, DecisionBlock,
		"Lệnh quản lý service. Bị chặn hoàn toàn."},
	{"sudo ", RiskDestructive, DecisionBlock,
		"Lệnh leo thang đặc quyền sudo. Bị chặn hoàn toàn."},
	{"su ", RiskDestructive, DecisionBlock,
		"Lệnh đổi user (su). Bị chặn hoàn toàn."},
	{"mount ", RiskDestructive, DecisionBlock,
		"Lệnh mount filesystem. Bị chặn hoàn toàn."},
	{"umount", RiskDestructive, DecisionBlock,
		"Lệnh unmount filesystem. Bị chặn hoàn toàn."},
	{"fdisk", RiskDestructive, DecisionBlock,
		"Lệnh phân vùng ổ đĩa. Bị chặn hoàn toàn."},
	{"mkfs", RiskDestructive, DecisionBlock,
		"Lệnh format filesystem. Bị chặn hoàn toàn."},
	{"dd ", RiskDestructive, DecisionBlock,
		"Lệnh dd (ghi thẳng vào block device). Bị chặn hoàn toàn."},
	{"crontab", RiskDestructive, DecisionBlock,
		"Lệnh quản lý cron job. Bị chặn hoàn toàn."},
	{"insmod", RiskDestructive, DecisionBlock,
		"Lệnh nạp kernel module. Bị chặn hoàn toàn."},
	{"modprobe", RiskDestructive, DecisionBlock,
		"Lệnh nạp kernel module. Bị chặn hoàn toàn."},
	{"iptables", RiskDestructive, DecisionBlock,
		"Lệnh cấu hình tường lửa. Bị chặn hoàn toàn."},
	{"nft ", RiskDestructive, DecisionBlock,
		"Lệnh cấu hình nftables. Bị chặn hoàn toàn."},
	{"killall", RiskDestructive, DecisionBlock,
		"Lệnh kill tất cả process theo tên. Bị chặn hoàn toàn."},
	{"pkill", RiskDestructive, DecisionBlock,
		"Lệnh kill process theo pattern. Bị chặn hoàn toàn."},

	// ── External network (requires_approval - network=none trong sandbox) ───────

	{"curl ", RiskExternalWrite, DecisionRequiresApproval,
		"Lệnh gửi HTTP request ra ngoài. Cần xác nhận của người dùng trước khi thực thi (sandbox hiện tắt mạng)."},
	{"wget ", RiskExternalWrite, DecisionRequiresApproval,
		"Lệnh tải file từ mạng. Cần xác nhận của người dùng trước khi thực thi (sandbox hiện tắt mạng)."},
	{"nc ", RiskExternalWrite, DecisionRequiresApproval,
		"Lệnh netcat (kết nối mạng). Cần xác nhận của người dùng trước khi thực thi (sandbox hiện tắt mạng)."},
	{"netcat", RiskExternalWrite, DecisionRequiresApproval,
		"Lệnh netcat (kết nối mạng). Cần xác nhận của người dùng trước khi thực thi (sandbox hiện tắt mạng)."},
	{"ssh ", RiskExternalWrite, DecisionRequiresApproval,
		"Lệnh SSH ra ngoài. Cần xác nhận của người dùng trước khi thực thi (sandbox hiện tắt mạng)."},
	{"scp ", RiskExternalWrite, DecisionRequiresApproval,
		"Lệnh copy qua SSH. Cần xác nhận của người dùng trước khi thực thi (sandbox hiện tắt mạng)."},
	{"sftp ", RiskExternalWrite, DecisionBlock,
		"Lệnh SFTP. Sandbox không có mạng."},
	{"ftp ", RiskExternalWrite, DecisionBlock,
		"Lệnh FTP. Sandbox không có mạng."},
	{"rsync ", RiskExternalWrite, DecisionBlock,
		"Lệnh rsync (có thể kết nối remote). Sandbox không có mạng."},
	{"ping ", RiskExternalWrite, DecisionBlock,
		"Lệnh ping (network test). Sandbox không có mạng."},
	{"nslookup", RiskExternalWrite, DecisionBlock,
		"Lệnh DNS lookup. Sandbox không có mạng."},
	{"dig ", RiskExternalWrite, DecisionBlock,
		"Lệnh DNS query. Sandbox không có mạng."},
	{"telnet", RiskExternalWrite, DecisionBlock,
		"Lệnh telnet (kết nối mạng). Sandbox không có mạng."},

	// ── Destructive file operations (requires_approval) ──────────────────────

	{"rm ", RiskDestructive, DecisionRequiresApproval,
		"Lệnh xóa file. Cần xác nhận của người dùng trước khi thực thi."},
	{"rm\t", RiskDestructive, DecisionRequiresApproval,
		"Lệnh xóa file. Cần xác nhận của người dùng trước khi thực thi."},
	{"rmdir", RiskDestructive, DecisionRequiresApproval,
		"Lệnh xóa thư mục. Cần xác nhận của người dùng trước khi thực thi."},
	{"shred", RiskDestructive, DecisionRequiresApproval,
		"Lệnh xóa file vĩnh viễn (shred). Cần xác nhận của người dùng."},
	{"truncate", RiskDestructive, DecisionRequiresApproval,
		"Lệnh truncate (xóa nội dung file). Cần xác nhận của người dùng."},
	{"chmod", RiskDestructive, DecisionRequiresApproval,
		"Lệnh thay đổi quyền file. Cần xác nhận của người dùng."},
	{"chown", RiskDestructive, DecisionRequiresApproval,
		"Lệnh thay đổi owner file. Cần xác nhận của người dùng."},

	// Overwrite redirect: `cmd > existing_file` is handled via content analysis
	// in the checker; this entry catches explicit overwrite patterns.
	{" > ", RiskDestructive, DecisionRequiresApproval,
		"Lệnh ghi đè file bằng redirect (>). Cần xác nhận của người dùng."},

	// Append is safer than overwrite but still modifies existing files.
	{" >> ", RiskLocalWrite, DecisionAllow,
		"Lệnh append vào file bằng redirect (>>). Phân loại local_write; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},

	// mv can overwrite destination if it already exists.
	{"mv ", RiskDestructive, DecisionRequiresApproval,
		"Lệnh di chuyển/đổi tên file (có thể ghi đè đích). Cần xác nhận của người dùng."},

	// ── Local write (create new, no mutation of existing) ────────────────

	{"mkdir", RiskLocalWrite, DecisionAllow,
		"Tạo thư mục mới trong workspace. Phân loại local_write; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"touch", RiskLocalWrite, DecisionAllow,
		"Tạo file mới (touch). Phân loại local_write; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"cp ", RiskLocalWrite, DecisionAllow,
		"Sao chép file. Phân loại local_write; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"tee ", RiskLocalWrite, DecisionAllow,
		"Ghi output ra file mới (tee). Phân loại local_write; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"python ", RiskLocalWrite, DecisionAllow,
		"Chạy Python script. Phân loại local_write/code execution; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"python3 ", RiskLocalWrite, DecisionAllow,
		"Chạy Python3 script. Phân loại local_write/code execution; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},

	// ── Read-only operations ──────────────────────────────────────────────

	{"ls", RiskSafeRead, DecisionAllow,
		"Liệt kê file trong workspace. Phân loại safe_read; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"dir", RiskSafeRead, DecisionAllow,
		"Liệt kê file trong workspace. Phân loại safe_read; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"cat ", RiskSafeRead, DecisionAllow,
		"Đọc nội dung file. Phân loại safe_read; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"head ", RiskSafeRead, DecisionAllow,
		"Đọc đầu file. Phân loại safe_read; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"tail ", RiskSafeRead, DecisionAllow,
		"Đọc cuối file. Phân loại safe_read; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"grep ", RiskSafeRead, DecisionAllow,
		"Tìm kiếm trong file. Phân loại safe_read; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"wc ", RiskSafeRead, DecisionAllow,
		"Đếm dòng/từ/byte file. Phân loại safe_read; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"find ", RiskSafeRead, DecisionAllow,
		"Tìm kiếm file trong workspace. Phân loại safe_read; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"stat ", RiskSafeRead, DecisionAllow,
		"Xem metadata file. Phân loại safe_read; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"file ", RiskSafeRead, DecisionAllow,
		"Xác định loại file. Phân loại safe_read; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"echo ", RiskSafeRead, DecisionAllow,
		"In text ra stdout. Phân loại safe_read; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"pwd", RiskSafeRead, DecisionAllow,
		"Xem thư mục hiện tại. Phân loại safe_read; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"whoami", RiskSafeRead, DecisionAllow,
		"Xem user hiện tại. Phân loại safe_read; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"date", RiskSafeRead, DecisionAllow,
		"Xem ngày giờ hệ thống. Phân loại safe_read; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"diff ", RiskSafeRead, DecisionAllow,
		"So sánh nội dung file. Phân loại safe_read; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"sort ", RiskSafeRead, DecisionAllow,
		"Sắp xếp output. Phân loại safe_read; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"uniq ", RiskSafeRead, DecisionAllow,
		"Loại bỏ dòng trùng. Phân loại safe_read; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"cut ", RiskSafeRead, DecisionAllow,
		"Cắt cột trong text. Phân loại safe_read; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"awk ", RiskSafeRead, DecisionAllow,
		"Xử lý text với awk. Phân loại safe_read; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"sed ", RiskSafeRead, DecisionAllow,
		"Xử lý text với sed. Phân loại safe_read; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"tr ", RiskSafeRead, DecisionAllow,
		"Thay thế ký tự. Phân loại safe_read; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"xargs", RiskSafeRead, DecisionAllow,
		"Chuyển output sang argument. Phân loại safe_read; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	{"jq ", RiskSafeRead, DecisionAllow,
		"Xử lý JSON. Phân loại safe_read; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
}
