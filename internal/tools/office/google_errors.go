package office

import "strings"

const (
	ErrorAuthExpired           = "AUTH_EXPIRED"
	ErrorAuthMissingScope      = "AUTH_MISSING_SCOPE"
	ErrorActionBlockedByPolicy = "ACTION_BLOCKED_BY_POLICY"
	ErrorProviderTimeout       = "PROVIDER_TIMEOUT"
	ErrorProviderUnavailable   = "PROVIDER_UNAVAILABLE"
	ErrorRateLimited           = "RATE_LIMITED"
	ErrorResourceNotFound      = "RESOURCE_NOT_FOUND"
)

// FriendlyGoogleToolError returns a user-facing message for Google Workspace
// tool failures. The machine-readable error code is still carried separately
// by each tool's ErrorShape.
func FriendlyGoogleToolError(code string, product string, raw string) string {
	product = strings.TrimSpace(product)
	if product == "" {
		product = "Google Workspace"
	}
	raw = strings.TrimSpace(raw)

	switch strings.TrimSpace(code) {
	case ErrorAuthMissingScope:
		return product + " chưa được cấp đủ quyền cho thao tác này. Vui lòng chạy lại `vclaw google auth` với tài khoản đúng, rồi thử lại."
	case ErrorAuthExpired:
		return "Phiên đăng nhập " + product + " đã hết hạn hoặc không còn hợp lệ. Vui lòng chạy lại `vclaw google auth`, rồi thử lại."
	case ErrorActionBlockedByPolicy:
		return "Google từ chối thao tác này vì tài khoản hiện tại không có quyền trên tài nguyên hoặc chính sách Workspace đang chặn thao tác."
	case ErrorResourceNotFound:
		return "Không tìm thấy tài nguyên " + product + " được yêu cầu, hoặc tài khoản hiện tại chưa có quyền truy cập tài nguyên đó."
	case ErrorRateLimited:
		return product + " đang giới hạn tần suất gọi API. Vui lòng đợi một lúc rồi thử lại."
	case ErrorProviderTimeout:
		return "Kết nối tới " + product + " bị quá thời gian chờ. Vui lòng thử lại."
	case ErrorProviderUnavailable:
		return product + " tạm thời không sẵn sàng. Vui lòng thử lại sau."
	default:
		if raw != "" {
			return product + " trả về lỗi: " + raw
		}
		return product + " trả về lỗi không xác định."
	}
}
