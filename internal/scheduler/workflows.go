package scheduler

type WorkflowDefinition struct {
	ID       string
	Name     string
	CronExpr string
	Prompt   string
}

func GetDefaultWorkflows(env func(string) string) []WorkflowDefinition {
	emailCron := env("CRON_EMAIL_CLASSIFY")
	if emailCron == "" {
		emailCron = "0 8,17 * * *"
	}
	summaryCron := env("CRON_WEEKLY_SUMMARY")
	if summaryCron == "" {
		summaryCron = "0 18 * * 5"
	}
	reminderCron := env("CRON_TASK_REMINDER")
	if reminderCron == "" {
		reminderCron = "0 8 * * *"
	}

	return []WorkflowDefinition{
		{
			ID:       "workflow_email_classify",
			Name:     "Phân loại Email",
			CronExpr: emailCron,
			Prompt:   "Sử dụng công cụ gmail.listEmails để lấy danh sách email chưa đọc trong INBOX. Dựa vào trường Subject và Snippet trong kết quả trả về, bạn hãy tự phân tích và phân loại các email đó thành 3 nhóm: 1. Quan trọng, 2. Quảng cáo/Spam, 3. Khác. Tóm tắt các email quan trọng và gửi kết quả vào khung chat này cho tôi. Tuyệt đối không dùng công cụ gmail.getEmail để mở đọc nội dung chi tiết của bất kỳ email nào.",
		},
		{
			ID:       "workflow_weekly_summary",
			Name:     "Tóm tắt Lịch Tuần",
			CronExpr: summaryCron,
			Prompt:   "Sử dụng công cụ calendar.listEvents để lấy lịch trình của tôi trong tuần tới. Tóm tắt các sự kiện, đặc biệt chú ý các cuộc họp quan trọng và trả lời kết quả lại cho tôi.",
		},
		{
			ID:       "workflow_task_reminder",
			Name:     "Nhắc Việc Hôm Nay",
			CronExpr: reminderCron,
			Prompt:   "Sử dụng calendar.listEvents để lấy lịch trình hôm nay và gmail.listEmails để lấy các email chưa đọc quan trọng. Tổng hợp tất cả thành một danh sách công việc cần làm hôm nay và trả lời lại cho tôi.",
		},
	}
}
