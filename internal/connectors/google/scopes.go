package google

const (
	ScopeGmailReadonly        = "https://www.googleapis.com/auth/gmail.readonly"
	ScopeGmailCompose         = "https://www.googleapis.com/auth/gmail.compose"
	ScopeGmailSend            = "https://www.googleapis.com/auth/gmail.send"
	ScopeGmailModify          = "https://www.googleapis.com/auth/gmail.modify"
	ScopeCalendarReadonly     = "https://www.googleapis.com/auth/calendar.readonly"
	ScopeCalendarEvents       = "https://www.googleapis.com/auth/calendar.events"
	ScopeChatMessages         = "https://www.googleapis.com/auth/chat.messages"
	ScopeChatMemberships      = "https://www.googleapis.com/auth/chat.memberships"
	ScopeChatSpaces           = "https://www.googleapis.com/auth/chat.spaces"
	ScopeChatSpacesReadonly   = "https://www.googleapis.com/auth/chat.spaces.readonly"
	ScopeChatMessagesCreate   = "https://www.googleapis.com/auth/chat.messages.create"
	ScopeChatMessagesReadonly = "https://www.googleapis.com/auth/chat.messages.readonly"
	ScopeDirectoryReadonly    = "https://www.googleapis.com/auth/directory.readonly"
)

var G1Scopes = []string{
	ScopeGmailReadonly,
	ScopeGmailCompose,
	ScopeGmailSend,
	ScopeGmailModify,
	ScopeCalendarReadonly,
	ScopeCalendarEvents,
	ScopeChatSpacesReadonly,
	ScopeChatMessagesCreate,
	ScopeChatMessagesReadonly,
	ScopeChatMessages,
	ScopeChatMemberships,
	ScopeChatSpaces,
	ScopeDirectoryReadonly,
}
