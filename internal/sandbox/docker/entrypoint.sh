#!/bin/sh
# V-Claw Sandbox Entrypoint
#
# Mục đích:
#   - Xác nhận môi trường chạy đúng (non-root, đúng thư mục)
#   - Thiết lập giới hạn tài nguyên cấp process (ulimit)
#   - Chuyển sang command được truyền vào
#
# Go runner truyền command dạng:
#   entrypoint.sh python /workspace/job_<id>.py
#   entrypoint.sh python -c "print('hello')"
#   entrypoint.sh sh -c "ls /workspace"

set -e

# ─── Kiểm tra user ────────────────────────────────────────────────────────────
if [ "$(id -u)" -eq 0 ]; then
    echo "[SANDBOX ERROR] Không được chạy sandbox bằng root." >&2
    exit 1
fi

# ─── Kiểm tra working directory ───────────────────────────────────────────────
if [ "$PWD" != "/workspace" ]; then
    echo "[SANDBOX ERROR] Thư mục làm việc phải là /workspace, hiện tại: $PWD" >&2
    exit 1
fi

# ─── Giới hạn tài nguyên cấp process ────────────────────────────────────────
# Giới hạn số file descriptor (tránh file handle leak)
ulimit -n 256
# Giới hạn core dump size = 0 (không ghi core dump)
ulimit -c 0
# Giới hạn file size tối đa có thể ghi = 50MB
ulimit -f 51200
# Lưu ý: giới hạn số process (fork bomb) được xử lý ở cấp Docker
# bằng flag --pids-limit khi Go runner tạo container.

# ─── Exec command ─────────────────────────────────────────────────────────────
# Nếu không có argument thì chạy python --version để kiểm tra
if [ $# -eq 0 ]; then
    exec python --version
fi

exec "$@"
