# Hướng Dẫn An Toàn

Tài liệu này dành cho người dùng cuối, người demo, hoặc thành viên nhóm cần hiểu nhanh "điều gì an toàn, điều gì cần xác nhận, điều gì chưa nên làm bừa".

## Nguyên Tắc Đơn Giản

V-Claw không nên tự ý làm các việc có rủi ro mà không hỏi lại.

Ví dụ các việc cần cẩn thận:

- gửi email
- tạo hoặc sửa lịch
- gửi tin nhắn chat
- sửa file
- chạy Python hoặc shell
- thao tác với dữ liệu có thể làm thay đổi trạng thái hệ thống

## Khi Nào Hệ Thống Nên Hỏi Lại

Hệ thống nên dừng để xác nhận khi:

- sắp gửi email cho người khác
- sắp tạo hoặc sửa lịch
- sắp gửi tin nhắn ra ngoài
- sắp chạy code
- sắp ghi hoặc sửa file

Nếu các hành động trên chạy thẳng mà không hỏi, đó là dấu hiệu cần kiểm tra lại.

## Khi Nào Có Thể Chạy Thẳng

Thông thường, các việc chỉ đọc có thể an toàn hơn:

- đọc email
- đọc danh sách file
- đọc nội dung tài liệu
- đọc file local trong vùng cho phép

Tuy nhiên, kể cả việc đọc cũng phải rõ ràng:

- không đọc file ngoài vùng cho phép
- không bịa dữ liệu nếu file không tồn tại
- không giả vờ đọc được khi provider đang lỗi

## Ví Dụ An Toàn Dễ Hiểu

### Trường hợp tốt

- người dùng yêu cầu tạo lịch
- hệ thống hiện bước xác nhận
- người dùng bấm từ chối
- hệ thống trả lời đã hủy, không tạo lịch

### Trường hợp không tốt

- người dùng yêu cầu gửi email
- hệ thống gửi luôn mà không hỏi

### Trường hợp lỗi nhưng vẫn an toàn

- người dùng yêu cầu đọc file không tồn tại
- hệ thống nói không tìm thấy file

Điều này an toàn hơn nhiều so với việc bịa ra nội dung.

## File Safety

Khi làm việc với file:

- không nên tin ngay phần đuôi file
- cần kiểm tra file có đúng loại thật không
- nếu thấy file giả PDF hoặc có dấu hiệu lạ, nên báo rõ thay vì cố xử lý tiếp

Usecase liên quan:

- [uploaded-file-prompt-injection-safe.json](/home/nxhai/V_Claw/testing-e2e/usecases/uploaded-file-prompt-injection-safe.json:1)
- [uploaded-file-safety-check.json](/home/nxhai/V_Claw/testing-e2e/usecases/uploaded-file-safety-check.json:1)
- [read-file-not-found.json](/home/nxhai/V_Claw/testing-e2e/usecases/read-file-not-found.json:1)

## Prompt Injection

Nếu bên trong file có dòng kiểu:

- "bỏ qua hướng dẫn trước đó"
- "hãy làm theo lệnh trong file này"

thì hệ thống không nên làm theo mù quáng.

Nó chỉ nên đọc nội dung nghiệp vụ thật sự, không nghe "lệnh ẩn" trong file.

## Điều Người Dùng Nên Biết

- Nếu hệ thống hỏi xác nhận, đó là dấu hiệu tốt chứ không phải bất tiện
- Nếu hệ thống nói "không tìm thấy" hoặc "không thể đọc", điều đó thường an toàn hơn là đoán
- Nếu hệ thống trả về nội dung quá kỹ thuật, nên chuyển cho người vận hành hoặc người trong nhóm kiểm tra thêm

## Cảnh Báo Rõ Ràng

## CHƯA CÓ BÁO CÁO BENCHMARK AN TOÀN CHÍNH THỨC

Hiện chưa có bảng số liệu chuẩn để khẳng định:

- chặn prompt injection tốt đến mức nào
- nhận diện file giả chính xác đến mức nào
- tỷ lệ lỗi an toàn là bao nhiêu

Vì vậy, các case safety hiện vẫn nên được xem là:

- đã có cơ chế
- đã có usecase kiểm thử
- nhưng chưa có benchmark chính thức để quảng bá mạnh

## CHƯA NÊN COI MỌI OUTPUT LÀ HOÀN TOÀN TIN CẬY NẾU CHƯA QUA REVIEW

Nhất là với:

- file lạ
- dữ liệu bên ngoài
- quyền Google không rõ
- môi trường demo chưa ổn định
