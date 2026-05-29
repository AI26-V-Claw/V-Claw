package oauth

import "testing"

func TestExtractAuthCode(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "raw code",
			input: "abc123\n",
			want:  "abc123",
		},
		{
			name:  "redirect url",
			input: "http://localhost/?state=vclaw-google-oauth&code=abc123&scope=email",
			want:  "abc123",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := extractAuthCode(test.input)
			if err != nil {
				t.Fatalf("extractAuthCode() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("extractAuthCode() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestExtractAuthCodeRejectsEmptyInput(t *testing.T) {
	if _, err := extractAuthCode(" \n"); err == nil {
		t.Fatal("extractAuthCode() error = nil, want non-nil")
	}
}
