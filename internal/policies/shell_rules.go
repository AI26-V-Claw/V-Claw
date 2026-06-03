package policies

// shellRules is the ordered policy matrix for run_shell requests.
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
// │ Credential files         │ credential_access    │ block            │
// │ System-deep commands     │ high_risk            │ block            │
// │ Network tools            │ external_network     │ block            │
// │ Destructive file ops     │ needs_approval       │ needs_approval   │
// │ Overwrite redirect (>)   │ needs_approval       │ needs_approval   │
// │ Create / new file ops    │ safe_write           │ allow            │
// │ Read-only ops            │ safe_read            │ allow            │
// └──────────────────────────┴─────────────────────┴──────────────────┘
var shellRules = []MatrixEntry{

	// ── Credential access (highest priority, always block) ────────────────

	{".env", RiskCredentialAccess, DecisionBlock,
		"Lệnh tham chiếu đến file credential (.env). Bị chặn mặc định."},
	{"id_rsa", RiskCredentialAccess, DecisionBlock,
		"Lệnh tham chiếu đến SSH private key. Bị chặn mặc định."},
	{"id_ed25519", RiskCredentialAccess, DecisionBlock,
		"Lệnh tham chiếu đến SSH private key. Bị chặn mặc định."},
	{"id_ecdsa", RiskCredentialAccess, DecisionBlock,
		"Lệnh tham chiếu đến SSH private key. Bị chặn mặc định."},
	{"credentials.json", RiskCredentialAccess, DecisionBlock,
		"Lệnh tham chiếu đến file credential Google. Bị chặn mặc định."},
	{"token.json", RiskCredentialAccess, DecisionBlock,
		"Lệnh tham chiếu đến OAuth token. Bị chặn mặc định."},
	{"secrets.json", RiskCredentialAccess, DecisionBlock,
		"Lệnh tham chiếu đến file secrets. Bị chặn mặc định."},
	{"service_account", RiskCredentialAccess, DecisionBlock,
		"Lệnh tham chiếu đến service account credential. Bị chặn mặc định."},
	{".netrc", RiskCredentialAccess, DecisionBlock,
		"Lệnh tham chiếu đến file .netrc chứa credential. Bị chặn mặc định."},
	{".pgpass", RiskCredentialAccess, DecisionBlock,
		"Lệnh tham chiếu đến file PostgreSQL password. Bị chặn mặc định."},
	{"kubeconfig", RiskCredentialAccess, DecisionBlock,
		"Lệnh tham chiếu đến Kubernetes config (chứa token). Bị chặn mặc định."},
	{"/etc/shadow", RiskCredentialAccess, DecisionBlock,
		"Lệnh tham chiếu đến file shadow password. Bị chặn mặc định."},
	{".pem", RiskCredentialAccess, DecisionBlock,
		"Lệnh tham chiếu đến file PEM (certificate/key). Bị chặn mặc định."},
	{".p12", RiskCredentialAccess, DecisionBlock,
		"Lệnh tham chiếu đến file PKCS12 (certificate/key). Bị chặn mặc định."},

	// ── High-risk system commands (always block) ──────────────────────────

	{"shutdown", RiskHighRisk, DecisionBlock,
		"Lệnh tắt máy hệ thống. Bị chặn hoàn toàn."},
	{"reboot", RiskHighRisk, DecisionBlock,
		"Lệnh khởi động lại hệ thống. Bị chặn hoàn toàn."},
	{"halt", RiskHighRisk, DecisionBlock,
		"Lệnh dừng hệ thống. Bị chặn hoàn toàn."},
	{"poweroff", RiskHighRisk, DecisionBlock,
		"Lệnh tắt nguồn hệ thống. Bị chặn hoàn toàn."},
	{"systemctl", RiskHighRisk, DecisionBlock,
		"Lệnh quản lý systemd service. Bị chặn hoàn toàn."},
	{"service ", RiskHighRisk, DecisionBlock,
		"Lệnh quản lý service. Bị chặn hoàn toàn."},
	{"sudo ", RiskHighRisk, DecisionBlock,
		"Lệnh leo thang đặc quyền sudo. Bị chặn hoàn toàn."},
	{"su ", RiskHighRisk, DecisionBlock,
		"Lệnh đổi user (su). Bị chặn hoàn toàn."},
	{"mount ", RiskHighRisk, DecisionBlock,
		"Lệnh mount filesystem. Bị chặn hoàn toàn."},
	{"umount", RiskHighRisk, DecisionBlock,
		"Lệnh unmount filesystem. Bị chặn hoàn toàn."},
	{"fdisk", RiskHighRisk, DecisionBlock,
		"Lệnh phân vùng ổ đĩa. Bị chặn hoàn toàn."},
	{"mkfs", RiskHighRisk, DecisionBlock,
		"Lệnh format filesystem. Bị chặn hoàn toàn."},
	{"dd ", RiskHighRisk, DecisionBlock,
		"Lệnh dd (ghi thẳng vào block device). Bị chặn hoàn toàn."},
	{"crontab", RiskHighRisk, DecisionBlock,
		"Lệnh quản lý cron job. Bị chặn hoàn toàn."},
	{"insmod", RiskHighRisk, DecisionBlock,
		"Lệnh nạp kernel module. Bị chặn hoàn toàn."},
	{"modprobe", RiskHighRisk, DecisionBlock,
		"Lệnh nạp kernel module. Bị chặn hoàn toàn."},
	{"iptables", RiskHighRisk, DecisionBlock,
		"Lệnh cấu hình tường lửa. Bị chặn hoàn toàn."},
	{"nft ", RiskHighRisk, DecisionBlock,
		"Lệnh cấu hình nftables. Bị chặn hoàn toàn."},
	{"killall", RiskHighRisk, DecisionBlock,
		"Lệnh kill tất cả process theo tên. Bị chặn hoàn toàn."},
	{"pkill", RiskHighRisk, DecisionBlock,
		"Lệnh kill process theo pattern. Bị chặn hoàn toàn."},

	// ── External network (needs_approval - network=none trong sandbox) ───────

	{"curl ", RiskExternalNetwork, DecisionNeedsApproval,
		"Lệnh gửi HTTP request ra ngoài. Cần xác nhận của người dùng trước khi thực thi (sandbox hiện tắt mạng)."},
	{"wget ", RiskExternalNetwork, DecisionNeedsApproval,
		"Lệnh tải file từ mạng. Cần xác nhận của người dùng trước khi thực thi (sandbox hiện tắt mạng)."},
	{"nc ", RiskExternalNetwork, DecisionNeedsApproval,
		"Lệnh netcat (kết nối mạng). Cần xác nhận của người dùng trước khi thực thi (sandbox hiện tắt mạng)."},
	{"netcat", RiskExternalNetwork, DecisionNeedsApproval,
		"Lệnh netcat (kết nối mạng). Cần xác nhận của người dùng trước khi thực thi (sandbox hiện tắt mạng)."},
	{"ssh ", RiskExternalNetwork, DecisionNeedsApproval,
		"Lệnh SSH ra ngoài. Cần xác nhận của người dùng trước khi thực thi (sandbox hiện tắt mạng)."},
	{"scp ", RiskExternalNetwork, DecisionNeedsApproval,
		"Lệnh copy qua SSH. Cần xác nhận của người dùng trước khi thực thi (sandbox hiện tắt mạng)."},
	{"sftp ", RiskExternalNetwork, DecisionBlock,
		"Lệnh SFTP. Sandbox không có mạng."},
	{"ftp ", RiskExternalNetwork, DecisionBlock,
		"Lệnh FTP. Sandbox không có mạng."},
	{"rsync ", RiskExternalNetwork, DecisionBlock,
		"Lệnh rsync (có thể kết nối remote). Sandbox không có mạng."},
	{"ping ", RiskExternalNetwork, DecisionBlock,
		"Lệnh ping (network test). Sandbox không có mạng."},
	{"nslookup", RiskExternalNetwork, DecisionBlock,
		"Lệnh DNS lookup. Sandbox không có mạng."},
	{"dig ", RiskExternalNetwork, DecisionBlock,
		"Lệnh DNS query. Sandbox không có mạng."},
	{"telnet", RiskExternalNetwork, DecisionBlock,
		"Lệnh telnet (kết nối mạng). Sandbox không có mạng."},

	// ── Destructive file operations (needs_approval) ──────────────────────

	{"rm ", RiskNeedsApproval, DecisionNeedsApproval,
		"Lệnh xóa file. Cần xác nhận của người dùng trước khi thực thi."},
	{"rm\t", RiskNeedsApproval, DecisionNeedsApproval,
		"Lệnh xóa file. Cần xác nhận của người dùng trước khi thực thi."},
	{"rmdir", RiskNeedsApproval, DecisionNeedsApproval,
		"Lệnh xóa thư mục. Cần xác nhận của người dùng trước khi thực thi."},
	{"shred", RiskNeedsApproval, DecisionNeedsApproval,
		"Lệnh xóa file vĩnh viễn (shred). Cần xác nhận của người dùng."},
	{"truncate", RiskNeedsApproval, DecisionNeedsApproval,
		"Lệnh truncate (xóa nội dung file). Cần xác nhận của người dùng."},
	{"chmod", RiskNeedsApproval, DecisionNeedsApproval,
		"Lệnh thay đổi quyền file. Cần xác nhận của người dùng."},
	{"chown", RiskNeedsApproval, DecisionNeedsApproval,
		"Lệnh thay đổi owner file. Cần xác nhận của người dùng."},

	// Overwrite redirect: `cmd > existing_file` is handled via content analysis
	// in the checker; this entry catches explicit overwrite patterns.
	{" > ", RiskNeedsApproval, DecisionNeedsApproval,
		"Lệnh ghi đè file bằng redirect (>). Cần xác nhận của người dùng."},

	// Append is safer than overwrite but still modifies existing files.
	{" >> ", RiskSafeWrite, DecisionAllow,
		"Lệnh append vào file bằng redirect (>>). Được phép."},

	// mv can overwrite destination if it already exists.
	{"mv ", RiskNeedsApproval, DecisionNeedsApproval,
		"Lệnh di chuyển/đổi tên file (có thể ghi đè đích). Cần xác nhận của người dùng."},

	// ── Safe write (create new, no mutation of existing) ─────────────────

	{"mkdir", RiskSafeWrite, DecisionAllow,
		"Tạo thư mục mới trong workspace. Được phép."},
	{"touch", RiskSafeWrite, DecisionAllow,
		"Tạo file mới (touch). Được phép."},
	{"cp ", RiskSafeWrite, DecisionAllow,
		"Sao chép file. Được phép (cần HITL nếu ghi đè đích)."},
	{"tee ", RiskSafeWrite, DecisionAllow,
		"Ghi output ra file mới (tee). Được phép."},
	{"python ", RiskSafeWrite, DecisionAllow,
		"Chạy Python script. Được phép (sandbox cô lập)."},
	{"python3 ", RiskSafeWrite, DecisionAllow,
		"Chạy Python3 script. Được phép (sandbox cô lập)."},

	// ── Safe read (read-only operations) ──────────────────────────────────

	{"ls", RiskSafeRead, DecisionAllow,
		"Liệt kê file trong workspace. Được phép."},
	{"dir", RiskSafeRead, DecisionAllow,
		"Liệt kê file trong workspace. Được phép."},
	{"cat ", RiskSafeRead, DecisionAllow,
		"Đọc nội dung file. Được phép."},
	{"head ", RiskSafeRead, DecisionAllow,
		"Đọc đầu file. Được phép."},
	{"tail ", RiskSafeRead, DecisionAllow,
		"Đọc cuối file. Được phép."},
	{"grep ", RiskSafeRead, DecisionAllow,
		"Tìm kiếm trong file. Được phép."},
	{"wc ", RiskSafeRead, DecisionAllow,
		"Đếm dòng/từ/byte file. Được phép."},
	{"find ", RiskSafeRead, DecisionAllow,
		"Tìm kiếm file trong workspace. Được phép."},
	{"stat ", RiskSafeRead, DecisionAllow,
		"Xem metadata file. Được phép."},
	{"file ", RiskSafeRead, DecisionAllow,
		"Xác định loại file. Được phép."},
	{"echo ", RiskSafeRead, DecisionAllow,
		"In text ra stdout. Được phép."},
	{"pwd", RiskSafeRead, DecisionAllow,
		"Xem thư mục hiện tại. Được phép."},
	{"whoami", RiskSafeRead, DecisionAllow,
		"Xem user hiện tại. Được phép."},
	{"date", RiskSafeRead, DecisionAllow,
		"Xem ngày giờ hệ thống. Được phép."},
	{"diff ", RiskSafeRead, DecisionAllow,
		"So sánh nội dung file. Được phép."},
	{"sort ", RiskSafeRead, DecisionAllow,
		"Sắp xếp output. Được phép."},
	{"uniq ", RiskSafeRead, DecisionAllow,
		"Loại bỏ dòng trùng. Được phép."},
	{"cut ", RiskSafeRead, DecisionAllow,
		"Cắt cột trong text. Được phép."},
	{"awk ", RiskSafeRead, DecisionAllow,
		"Xử lý text với awk. Được phép."},
	{"sed ", RiskSafeRead, DecisionAllow,
		"Xử lý text với sed. Được phép."},
	{"tr ", RiskSafeRead, DecisionAllow,
		"Thay thế ký tự. Được phép."},
	{"xargs", RiskSafeRead, DecisionAllow,
		"Chuyển output sang argument. Được phép."},
	{"jq ", RiskSafeRead, DecisionAllow,
		"Xử lý JSON. Được phép."},
}

// fileOpsRules maps file_ops operation types to their policy entries.
var fileOpsRules = map[string]MatrixEntry{
	"list": {
		Pattern:   "list",
		RiskLevel: RiskSafeRead,
		Decision:  DecisionAllow,
		ReasonVI:  "Liệt kê file trong workspace. Được phép.",
	},
	"read": {
		Pattern:   "read",
		RiskLevel: RiskSafeRead,
		Decision:  DecisionAllow,
		ReasonVI:  "Đọc file trong workspace. Được phép.",
	},
	"copy": {
		Pattern:   "copy",
		RiskLevel: RiskSafeWrite,
		Decision:  DecisionAllow,
		ReasonVI:  "Sao chép file trong workspace. Được phép.",
	},
	"write": {
		Pattern:   "write",
		RiskLevel: RiskSafeWrite,
		Decision:  DecisionAllow,
		ReasonVI:  "Tạo file mới trong workspace. Được phép.",
	},
	"move": {
		Pattern:   "move",
		RiskLevel: RiskNeedsApproval,
		Decision:  DecisionNeedsApproval,
		ReasonVI:  "Di chuyển file (có thể ghi đè đích). Cần xác nhận của người dùng.",
	},
	"delete": {
		Pattern:   "delete",
		RiskLevel: RiskNeedsApproval,
		Decision:  DecisionNeedsApproval,
		ReasonVI:  "Xóa file trong workspace. Cần xác nhận của người dùng.",
	},
}
