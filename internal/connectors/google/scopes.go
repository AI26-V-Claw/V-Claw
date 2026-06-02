package google

const (
	ScopeGmailReadonly        = "https://www.googleapis.com/auth/gmail.readonly"
	ScopeCalendarReadonly     = "https://www.googleapis.com/auth/calendar.readonly"
	ScopeChatMessages         = "https://www.googleapis.com/auth/chat.messages"
	ScopeChatMemberships      = "https://www.googleapis.com/auth/chat.memberships"
	ScopeChatSpaces           = "https://www.googleapis.com/auth/chat.spaces"
	ScopeChatSpacesReadonly   = "https://www.googleapis.com/auth/chat.spaces.readonly"
	ScopeChatMessagesCreate   = "https://www.googleapis.com/auth/chat.messages.create"
	ScopeChatMessagesReadonly = "https://www.googleapis.com/auth/chat.messages.readonly"
)

var G1Scopes = []string{
	ScopeGmailReadonly,
	ScopeCalendarReadonly,
	ScopeChatSpacesReadonly,
	ScopeChatMessagesCreate,
	ScopeChatMessagesReadonly,
	ScopeChatMessages,
	ScopeChatMemberships,
	ScopeChatSpaces,
}
