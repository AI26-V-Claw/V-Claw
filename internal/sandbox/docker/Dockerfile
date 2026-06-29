# V-Claw Sandbox - Python Runtime Image
#
# Yêu cầu bảo mật:
#   - Không chạy bằng quyền root (user: sandbox, uid: 1000)
#   - Working directory cố định tại /workspace
#   - Không có shell tools nguy hiểm (curl, wget bị loại bỏ)
#   - Tất cả output qua stdout/stderr để Go runner thu thập
#
# Build: docker build -t vclaw-sandbox:latest .
# Run:   docker run --rm --read-only --tmpfs /tmp:size=64m \
#               --memory=256m --cpus=0.5 \
#               -v <host-workspace>:/workspace:rw \
#               vclaw-sandbox:latest python -c "print('hello')"

FROM python:3.12-slim

LABEL maintainer="vclaw"
LABEL description="V-Claw Python sandbox - isolated, non-root, workspace-only"
LABEL version="1.0.0"

# ─── Hệ thống ──────────────────────────────────────────────────────────────────
# Cài thêm gói cần thiết, xóa apt cache để giảm image size
RUN apt-get update && apt-get install -y --no-install-recommends \
        # libgomp cần cho một số thư viện numpy
        libgomp1 \
        # tzone cho pandas datetime
        tzdata \
    && rm -rf /var/lib/apt/lists/*

# Timezone mặc định UTC
ENV TZ=UTC
ENV PYTHONUNBUFFERED=1
ENV PYTHONDONTWRITEBYTECODE=1
# Giới hạn recursion depth để tránh stack overflow trong code user
ENV PYTHONMAXRECURSION=500
# Không có home riêng, không cache pip ngoài /tmp
ENV PIP_NO_CACHE_DIR=1
ENV PIP_DISABLE_PIP_VERSION_CHECK=1

# ─── Tạo user sandbox (non-root) ──────────────────────────────────────────────
RUN groupadd --gid 1000 sandbox \
    && useradd --uid 1000 --gid 1000 \
               --no-create-home \
               --shell /bin/false \
               sandbox

# ─── Cài Python dependencies ──────────────────────────────────────────────────
COPY requirements.txt /tmp/requirements.txt
RUN pip install --no-cache-dir --default-timeout=120 --retries=10 -r /tmp/requirements.txt \
    && rm /tmp/requirements.txt

# ─── Workspace ────────────────────────────────────────────────────────────────
# /workspace là thư mục duy nhất user sandbox có quyền ghi
# Go runner sẽ mount volume session-specific vào đây
RUN mkdir -p /workspace && chown sandbox:sandbox /workspace
VOLUME ["/workspace"]
WORKDIR /workspace

# ─── Entrypoint ───────────────────────────────────────────────────────────────
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
# Convert CRLF -> LF (Windows host có thể tạo file với line endings CRLF)
RUN sed -i 's/\r//' /usr/local/bin/entrypoint.sh \
    && chmod +x /usr/local/bin/entrypoint.sh \
    && chown root:root /usr/local/bin/entrypoint.sh

# Chạy với user sandbox từ đây trở đi
USER sandbox

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
# Default command nếu không truyền gì: in phiên bản Python
CMD ["python", "--version"]
