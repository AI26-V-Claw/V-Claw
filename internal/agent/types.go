package agent

type InboundMessage struct {
	UpdateID int64
	ChatID   int64
	UserID   int64
	Text     string
	Source   string
}

type OutboundMessage struct {
	ChatID int64
	Text   string
}
