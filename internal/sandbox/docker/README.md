# V-Claw Python Sandbox Docker Image

Docker image cho Python sandbox của V-Claw. Image này được thiết kế để chạy code Python do AI Agent sinh ra trong môi trường cô lập, an toàn.

## Thông Số Kỹ Thuật

| Thuộc tính | Giá trị |
|---|---|
| Base image | `python:3.12-slim` |
| User | `sandbox` (uid: 1000, non-root) |
| Working directory | `/workspace` |
| Network | Tắt hoàn toàn (`--network none`) |
| CPU limit | 0.5 core |
| RAM limit | 256 MB |
| File size limit (ulimit) | 50 MB |
| File descriptor limit (ulimit) | 256 |
| Process limit (ulimit) | 64 |
| Filesystem | Read-only (trừ `/workspace` và `/tmp`) |

## Thư Viện Python

| Thư viện | Mục đích |
|---|---|
| `pandas` | Xử lý dữ liệu, đọc CSV/Excel |
| `numpy` | Tính toán số |
| `openpyxl` | Đọc/ghi file `.xlsx` |
| `xlrd` | Đọc file `.xls` cũ |
| `python-docx` | Tạo/sửa file `.docx` |
| `chardet` | Detect encoding text |
| `python-dateutil` | Parse ngày tháng |
| `PyYAML` | Đọc file YAML config |

## Build Image

```sh
cd internal/sandbox/docker
docker build -t vclaw-sandbox:latest .
```

## Chạy Thủ Công (Dev/Test)

```sh
# Chạy lệnh Python đơn giản
docker run --rm \
  --network none \
  --memory=256m --cpus=0.5 \
  --read-only \
  --tmpfs /tmp:size=64m \
  --security-opt no-new-privileges:true \
  --cap-drop ALL \
  -v "$(pwd)/workspace:/workspace:rw" \
  vclaw-sandbox:latest \
  python -c "print('hello from sandbox')"

# Chạy file Python
docker run --rm \
  --network none \
  --memory=256m --cpus=0.5 \
  --read-only \
  --tmpfs /tmp:size=64m \
  --security-opt no-new-privileges:true \
  --cap-drop ALL \
  -v "/path/to/session-workspace:/workspace:rw" \
  vclaw-sandbox:latest \
  python /workspace/script.py

# Dùng docker compose (dev only)
docker compose run --rm sandbox python -c "import pandas; print(pandas.__version__)"
```

Go runner (`internal/sandbox/runtime`) tạo container bằng cách gọi `docker run` qua `os/exec` với các tham số tương đương:
```go
// Tham khảo - xem internal/sandbox/runtime để biết implementation
containerConfig := &container.Config{
    Image:      "vclaw-sandbox:latest",
    Cmd:        []string{"python", "/workspace/job_abc123.py"},
    WorkingDir: "/workspace",
    User:       "1000:1000",
}

hostConfig := &container.HostConfig{
    NetworkMode: "none",
    ReadonlyRootfs: true,
    Resources: container.Resources{
        Memory:   256 * 1024 * 1024, // 256 MB
        NanoCPUs: 500_000_000,        // 0.5 CPU
    },
    Tmpfs: map[string]string{
        "/tmp": "size=67108864",
    },
    Binds: []string{"/host/session/workspace:/workspace:rw"},
    SecurityOpt: []string{"no-new-privileges:true"},
    CapDrop: strslice.StrSlice{"ALL"},
}
```

## Cấu Trúc Workspace

Mỗi job nhận một workspace riêng, được mount vào `/workspace`:

```
/workspace/
├── input/          # File đầu vào (read-only nếu cần)
├── output/         # File đầu ra của job
└── job_<id>.py     # Script Python được inject bởi runner
```

## Bảo Mật

- **Non-root**: Container chạy với uid 1000 (`sandbox`), không có sudo.
- **No new privileges**: Không thể leo thang đặc quyền.
- **Cap drop ALL**: Không có Linux capabilities nào.
- **Network none**: Không kết nối được mạng trong hay ngoài.
- **Read-only rootfs**: Toàn bộ filesystem read-only ngoài `/workspace` và `/tmp`.
- **ulimit**: Giới hạn file, process, file size ở cấp process.
- **Timeout**: Go runner set context timeout trước khi chạy container.

## Checklist Acceptance Criteria S1-T2

- [x] Python runtime (`python:3.12-slim`) có trong image
- [x] pip và các thư viện cần thiết được cài
- [x] Container chạy bằng non-root user (`sandbox`, uid 1000)
- [x] Thư mục `/workspace` được tạo và sở hữu bởi user sandbox
- [x] `--network none` ngắt hoàn toàn kết nối mạng
- [x] Resource limits (CPU/RAM) được định nghĩa trong docker-compose.yml
- [x] Filesystem read-only với tmpfs cho /tmp
- [x] `entrypoint.sh` kiểm tra user và working directory trước khi exec
- [x] ulimit giới hạn file, process, file size ở cấp shell
- [x] `cap_drop: ALL` xóa toàn bộ Linux capabilities
