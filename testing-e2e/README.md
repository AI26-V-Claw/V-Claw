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
