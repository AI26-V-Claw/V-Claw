# Usecase Diagram

## Actors

| Actor | Loại | Mô tả |
|---|---|---|
| **Người dùng** | Human | Tương tác qua Telegram / Slack. Không giao tiếp trực tiếp với Google Workspace hay Sandbox. |
| **V-Claw Agent** | AI System | Thực thi tác vụ: phân loại intent, gọi tool, gọi Google Workspace API, chạy Sandbox, điều phối HITL. |

---

## 1. Nhận & Phân loại yêu cầu

```mermaid
%%{init: {"theme": "base", "themeVariables": {"background": "#ffffff", "mainBkg": "#ffffff", "darkMode": false, "lineColor": "#374151"}}}%%
flowchart LR
    user(["👤 Người dùng"])
    agent(["🤖 V-Claw Agent"])
 
    subgraph INPUT ["Nhận & Phân loại yêu cầu"]
        direction TB
        UC_SEND["Gửi tin nhắn\n(Telegram / Slack)"]
        UC_RECV["Nhận phản hồi"]
        UC_CANCEL["Hủy yêu cầu đang chạy"]
 
        UC_CLASSIFY["Phân loại intent"]
        UC_INJECT["Kiểm tra prompt injection"]
        UC_GREET["Nhận diện chào hỏi"]
        UC_READ["Nhận diện yêu cầu đọc thông tin"]
        UC_DANGER["Nhận diện thao tác nhạy cảm"]
        UC_ASK["Hỏi lại khi thiếu thông tin"]
 
        UC_CLASSIFY -.->|"«include»"| UC_INJECT
        UC_GREET    -.->|"«extend»"| UC_CLASSIFY
        UC_READ     -.->|"«extend»"| UC_CLASSIFY
        UC_DANGER   -.->|"«extend»"| UC_CLASSIFY
        UC_ASK      -.->|"«extend»"| UC_CLASSIFY
    end
 
    user  --> UC_SEND
    user  --> UC_RECV
    user  --> UC_CANCEL
    agent --> UC_CLASSIFY
```

---

## 2. Chạy Tool & Multi-step Planning

```mermaid
%%{init: {"theme": "base", "themeVariables": {"background": "#ffffff", "mainBkg": "#ffffff", "darkMode": false, "lineColor": "#374151"}}}%%
flowchart LR
    agent(["🤖 V-Claw Agent"])

    subgraph TOOL ["Chạy Tool & Planning"]
        direction TB
        T_PLAN["Lập kế hoạch đa bước"]
        T_SELECT["Chọn tool phù hợp"]
        T_PERM["Kiểm tra quyền\ndùng tool"]
        T_RUN["Chạy một tool"]
        T_PARALLEL["Chạy nhiều tool\nsong song"]
        T_ERROR["Xử lý lỗi tool"]
        T_RESULT["Tổng hợp &\ntrả kết quả"]

        T_PLAN -.->|"«include»"| T_SELECT
        T_SELECT -.->|"«include»"| T_PERM
        T_RUN -.->|"«include»"| T_PERM
        T_ERROR -.->|"«extend»"| T_RUN
        T_PARALLEL -.->|"«extend»"| T_RUN
        T_PLAN -.->|"«include»"| T_RESULT
    end

    agent --> T_PLAN
    agent --> T_RUN
    agent --> T_PARALLEL
```

---

## 3. Tương tác Gmail

```mermaid
%%{init: {"theme": "base", "themeVariables": {"background": "#ffffff", "mainBkg": "#ffffff", "darkMode": false, "lineColor": "#374151"}}}%%
flowchart LR
    agent(["🤖 V-Claw Agent"])

    subgraph GMAIL ["Tương tác Gmail"]
        direction TB
        subgraph READ ["🔵 Đọc — an toàn"]
            G_READ["Đọc email"]
            G_FILTER["Tìm & lọc email"]
            G_SUM["Tóm tắt email"]
            G_READ -.->|"«include»"| G_FILTER
            G_SUM -.->|"«include»"| G_FILTER
        end

        subgraph WRITE ["🔴 Ghi — kích hoạt HITL"]
            G_DRAFT["Soạn email"]
            G_SEND["Gửi email"]
            G_REPLY["Trả lời email"]
            G_FWD["Chuyển tiếp email"]
            G_ATTACH["Đính kèm tệp"]
            G_APPROVE["Yêu cầu xác nhận\nngười dùng"]
            G_SEND -.->|"«include»"| G_DRAFT
            G_REPLY -.->|"«extend»"| G_SEND
            G_FWD -.->|"«extend»"| G_SEND
            G_ATTACH -.->|"«extend»"| G_SEND
            G_SEND -.->|"«include»"| G_APPROVE
        end

        subgraph MANAGE ["🟡 Quản lý"]
            G_LABEL["Quản lý nhãn"]
            G_MARK["Đánh dấu đã đọc /\nchưa đọc"]
        end
    end

    agent --> READ
    agent --> WRITE
    agent --> MANAGE
```

---

## 4. Tương tác Calendar

```mermaid
%%{init: {"theme": "base", "themeVariables": {"background": "#ffffff", "mainBkg": "#ffffff", "darkMode": false, "lineColor": "#374151"}}}%%
flowchart LR
    agent(["🤖 V-Claw Agent"])

    subgraph CALENDAR ["Tương tác Calendar"]
        subgraph CAL_READ ["🔵 Đọc — an toàn"]
            C_FREE["Tìm thời gian trống"]
            C_CONFLICT["Kiểm tra trùng lịch"]
            C_VIEW["Xem lịch"]
        end

        subgraph CAL_WRITE ["🔴 Ghi — kích hoạt HITL"]
            C_CREATE["Tạo sự kiện"]
            C_UPDATE["Cập nhật sự kiện"]
            C_CANCEL["Hủy sự kiện"]
            C_REMOVE["Xóa người tham dự"]
            C_APPROVE["Yêu cầu xác nhận\nngười dùng"]
            C_INVITE["Gửi lời mời"]

            C_CREATE -.->|"«include»"| C_FREE
            C_CREATE -.->|"«include»"| C_CONFLICT
            C_INVITE -.->|"«extend»"| C_CREATE

            C_CREATE -.->|"«include»"| C_APPROVE
            C_UPDATE -.->|"«include»"| C_APPROVE
            C_CANCEL -.->|"«include»"| C_APPROVE
            C_REMOVE -.->|"«include»"| C_APPROVE
        end

        C_REMIND["Nhắc lịch tự động"]
    end

    agent --> CAL_READ
    agent --> CAL_WRITE
    agent --> C_REMIND
```

---

## 5. Tương tác Google Chat

```mermaid
%%{init: {"theme": "base", "themeVariables": {"background": "#ffffff", "mainBkg": "#ffffff", "darkMode": false, "lineColor": "#374151"}}}%%
flowchart LR
    agent(["🤖 V-Claw Agent"])

    subgraph CHAT ["Tương tác Google Chat"]
        direction TB
        subgraph CHAT_READ ["🔵 Đọc — an toàn"]
            CH_FETCH["Lấy nội dung hội thoại"]
            CH_SUM["Tóm tắt hội thoại"]
            CH_REPLY["Trả lời tin nhắn"]
            CH_REPLY -.->|"«include»"| CH_FETCH
            CH_SUM -.->|"«include»"| CH_FETCH
        end

        subgraph CHAT_WRITE ["🔴 Ghi — kích hoạt HITL"]
            CH_SEND["Gửi tin nhắn"]
            CH_FILE["Gửi tệp đính kèm"]
            CH_APPROVE["Yêu cầu xác nhận\nngười dùng"]
            CH_FILE -.->|"«extend»"| CH_SEND
            CH_APPROVE -.->|"«extend»"| CH_SEND
        end
    end

    agent --> CHAT_READ
    agent --> CHAT_WRITE
```

---

## 6. Sandbox — Chạy Code Python / Shell

```mermaid
%%{init: {"theme": "base", "themeVariables": {"background": "#ffffff", "mainBkg": "#ffffff", "darkMode": false, "lineColor": "#374151"}}}%%
flowchart LR
    agent(["🤖 V-Claw Agent"])

    subgraph SANDBOX ["Sandbox — Python / Shell"]
        direction TB
        subgraph SB_SETUP ["🟡 Chuẩn bị môi trường"]
            S_INIT["Khởi tạo sandbox"]
            S_LIMIT["Giới hạn workspace\n& tài nguyên"]
            S_LIB["Cài thư viện\ntheo allowlist"]
            S_INIT -.->|"«include»"| S_LIMIT
        end

        subgraph SB_RUN ["🔴 Thực thi — kích hoạt HITL"]
            S_PY["Chạy code Python"]
            S_SH["Chạy lệnh Shell"]
            S_TIMEOUT["Xử lý timeout"]
            S_OUTPUT["Giới hạn output"]
            S_APPROVE["Yêu cầu xác nhận\nngười dùng"]
            S_TIMEOUT -.->|"«extend»"| S_PY
            S_OUTPUT -.->|"«extend»"| S_PY
            S_TIMEOUT -.->|"«extend»"| S_SH
            S_APPROVE -.->|"«extend»"| S_LIB
            S_APPROVE -.->|"«extend»"| S_PY
            S_APPROVE -.->|"«extend»"| S_SH
        end

        subgraph SB_RESULT ["🔵 Kết quả"]
            S_RETURN["Trả kết quả\nvề Agent"]
            S_LOG["Ghi log\nlệnh đã chạy"]
        end
    end

    agent --> SB_SETUP
    agent --> SB_RUN
    agent --> SB_RESULT
```

---

## 7. HITL — Human-in-the-Loop

```mermaid
%%{init: {"theme": "base", "themeVariables": {"background": "#ffffff", "mainBkg": "#ffffff", "darkMode": false, "lineColor": "#374151"}}}%%
flowchart LR
    user(["👤 Người dùng"])
    agent(["🤖 V-Claw Agent"])

    subgraph HITL ["HITL — Xác nhận hành động nhạy cảm"]
        direction TB

        H_TRIGGER["Phát hiện action nhạy cảm"]
        H_REQUEST["Yêu cầu xác nhận người dùng"]
        H_VIEW["Xem mô tả action sắp thực thi"]
        H_APPROVE["Xác nhận thực thi"]
        H_REJECT["Từ chối thực thi"]
        H_EXEC["Thực thi action"]
        H_ABORT["Hủy action"]

        H_TRIGGER -.->|"«include»"| H_REQUEST
        H_APPROVE -.->|"«include»"| H_VIEW
        H_REJECT -.->|"«include»"| H_VIEW
        H_APPROVE -.->|"«include»"| H_EXEC
        H_REJECT -.->|"«include»"| H_ABORT
    end

    agent --> H_TRIGGER
    user --> H_APPROVE
    user --> H_REJECT
```

---

## 8. Bộ nhớ

```mermaid
%%{init: {"theme": "base", "themeVariables": {"background": "#ffffff", "mainBkg": "#ffffff", "darkMode": false, "lineColor": "#374151"}}}%%
flowchart LR
    agent(["🤖 V-Claw Agent"])

    subgraph MEM ["Bộ nhớ Agent"]
        direction TB
        subgraph MEM_SHORT ["🟢 Bộ nhớ ngắn hạn"]
            M1["Lưu lịch sử\nhội thoại trong session"]
            M2["Tóm tắt nội dung\nđã trao đổi"]
            M3["Truy xuất context\nhội thoại cũ"]
            M2 -.->|"«include»"| M1
            M3 -.->|"«include»"| M1
        end

        subgraph MEM_LONG ["🔵 Bộ nhớ dài hạn"]
            L1["Ghi nhớ thói quen\nlàm việc"]
            L2["Ghi nhớ danh sách\nngười quen"]
            L3["Áp dụng context\ndài hạn vào xử lý"]
            L3 -.->|"«include»"| L1
            L3 -.->|"«include»"| L2
        end
    end

    agent --> MEM_SHORT
    agent --> MEM_LONG
```

---

## 9. Observability & Log

```mermaid
%%{init: {"theme": "base", "themeVariables": {"background": "#ffffff", "mainBkg": "#ffffff", "darkMode": false, "lineColor": "#374151"}}}%%
flowchart LR
    agent(["🤖 V-Claw Agent"])
    direction TB
    O1["Ghi log action đã thực thi"]
    O2["Ghi log kết quả HITL"]
    O3["Theo dõi hiệu năng phản hồi"]

    agent --> O1
    agent --> O2
    agent --> O3
```

---
