# V-Claw N2 E2E harness

Thư mục này chứa harness/scripts/scenarios/artifacts cho real E2E N2. Thư mục được gitignore vì có thể chứa dữ liệu tài khoản test, transcript, run artifacts và selector môi trường.

## Trạng thái hiện tại

Skeleton ban đầu. Chưa claim pass cho scenario nào nếu chưa chạy thật.

## Cấu trúc

```text
testing-e2e/
  README.md
  scenarios/
  scripts/
  artifacts/
```

## Nguyên tắc

- Chỉ status hợp lệ: `pass`, `fail`, `blocked_env`, `pending_verification`.
- Chỉ `pass` được tính readiness.
- Real E2E dùng fixtures `[VCLAW-E2E]` và env selectors rõ ràng.
- Mọi write object phải có `run_id`.
- Hard assertions là chính; LLM judge chỉ phụ.

## Chạy skeleton

```powershell
./testing-e2e/scripts/run_n2_e2e.ps1 -Scenario n2.1-golden-workspace-flow -DryRun
```
