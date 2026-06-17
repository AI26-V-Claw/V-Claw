package traceutil

import "testing"

func TestBuildTraceURL(t *testing.T) {
	t.Setenv("LANGFUSE_HOST", "https://us.cloud.langfuse.com")
	t.Setenv("LANGFUSE_PROJECT_ID", "proj_123")

	got := BuildTraceURL("trace_abc")
	want := "https://us.cloud.langfuse.com/project/proj_123/traces/trace_abc"
	if got != want {
		t.Fatalf("BuildTraceURL() = %q, want %q", got, want)
	}
}

func TestBuildTraceURLReturnsEmptyWithoutProjectID(t *testing.T) {
	t.Setenv("LANGFUSE_HOST", "https://us.cloud.langfuse.com")
	t.Setenv("LANGFUSE_PROJECT_ID", "")

	if got := BuildTraceURL("trace_abc"); got != "" {
		t.Fatalf("BuildTraceURL() = %q, want empty", got)
	}
}
