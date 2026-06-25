package meet

// Space represents a Google Meet meeting space returned by the Meet API.
type Space struct {
	Name        string
	MeetingURI  string
	MeetingCode string
}
