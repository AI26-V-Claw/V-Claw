# Testing E2E Use Cases

## 1. Use Case Template

```text
[
  {
    "step": int - số thứ tự của step trong use case
    "user": {
      "message": string - nội dung user gửi vào agent ở step hiện tại, có thể là yêu cầu tự nhiên hoặc "/approve", "/reject"
    },
    "agent": {
      "expectation": string - mô tả ngắn kỳ vọng agent sẽ làm ở step này
      "requires_approval": boolean - true nếu step phải tạo yêu cầu approve, false nếu không được yêu cầu approve
      "expected_tools": list[string] - danh sách tool agent phải dùng trong step này
      "expected_approval_tool": string | null - tool chờ approve trong step hiện tại, dùng null nếu không có
      "response_contains": list[string] - danh sách chuỗi phải xuất hiện trong câu trả lời agent của step, list rỗng thì bỏ qua
    }
  }
]
```

## 2. Sample

```json
[
  {
    "step": 0,
    "user": {
      "message": "Tính chính xác cho mình: 9382472 * 83240 - 23948293"
    },
    "agent": {
      "expectation": "Agent cần sử dụng tool calculator để tính toán và trả về kết quả chính xác là 780973020987.",
      "requires_approval": false,
      "expected_tools": ["calculator"],
      "expected_approval_tool": null,
      "response_contains": ["780973020987"]
    }
  }
]
```

## 3. Seeded Sessions

- Nếu cần test khả năng agent nối ngữ cảnh hoặc dùng bộ nhớ từ lịch sử phiên trước, tạo thư mục seed tại `testing-e2e/sessions/<ten-file-usecase-khong-co-.json>/`.
- Mỗi seed session có thể chứa `memory.json` và `transcript.json`.
- Khi chạy một usecase có thư mục seed trùng tên, harness sẽ tự copy seed đó sang `data/sessions/<sessionId>/` trước khi bắt đầu step 0.
- Cách này phù hợp để test:
  - nhớ sở thích hoặc facts đã lưu trong memory
  - follow-up question dựa trên tool result ở phiên trước
  - tham chiếu lại email / lịch / file vừa xử lý mà không gọi lại tool nếu agent đã có đủ context
  - nhớ nội dung lịch sử chat/transcript của cùng session, ngay cả khi không có tool mới ở turn hiện tại
