package longmem

import "testing"

// ---------------------------------------------------------------------------
// Helper: run a table-driven sub-test
// ---------------------------------------------------------------------------

type validateCase struct {
	name    string
	content string
	wantErr bool
}

func runValidateCases(t *testing.T, cases []validateCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMemoryContent(tc.content)
			if tc.wantErr && err == nil {
				t.Errorf("expected rejection but got nil error for %q", tc.content)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error %v for %q", err, tc.content)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Rejection tests — one group per regex in secretPatterns
// ---------------------------------------------------------------------------

func TestValidateMemoryContent_KeyValueSecrets(t *testing.T) {
	runValidateCases(t, []validateCase{
		// Pattern 1: key=value style (api_key, secret, password, etc.)
		{"OPENAI_API_KEY=sk-test-secret-value", "OPENAI_API_KEY=sk-test-secret-value", true},
		{"access_token: abc123def456", "access_token: abc123def456", true},
		{"refresh_token=eyJhbGciOiJIUzI1NiJ9", "refresh_token=eyJhbGciOiJIUzI1NiJ9", true},
		{"my-secret: hush", "my-secret: hush", true},
		{"password=myPassword123", "password=myPassword123", true},
		{"passwd: hunter2", "passwd: hunter2", true},
		{"GOOGLE_CLIENT_SECRET: GOCSPX-abcdef", "GOOGLE_CLIENT_SECRET: GOCSPX-abcdef", true},
		{"PRIVATE_KEY: -----BEGIN RSA PRIVATE KEY-----", "PRIVATE_KEY: -----BEGIN RSA PRIVATE KEY-----", true},
		// case insensitivity
		{"Api_Key = test123", "Api_Key = test123", true},
		{"SECRET: abc", "SECRET: abc", true},
		{"Access_Token = tok123", "Access_Token = tok123", true},
	})
}

func TestValidateMemoryContent_BearerEquals(t *testing.T) {
	runValidateCases(t, []validateCase{
		{"bearer=eyJhbGciOiJIUzI1NiJ9", "bearer=eyJhbGciOiJIUzI1NiJ9", true},
		{"bearer: abcdefghijklmnop", "bearer: abcdefghijklmnop", true},
	})
}

func TestValidateMemoryContent_OpenAIKey(t *testing.T) {
	runValidateCases(t, []validateCase{
		{"sk-proj1234abcdEFGHijklMNOPqrstuvwxyz", "sk-proj1234abcdEFGHijklMNOPqrstuvwxyz", true},
		{"my key is sk-abcdefghijklmno1234", "my key is sk-abcdefghijklmno1234", true},
	})
}

func TestValidateMemoryContent_GoogleAIzaKey(t *testing.T) {
	runValidateCases(t, []validateCase{
		{"AIzaSyD-examplekey1234567890abcde", "AIzaSyD-examplekey1234567890abcde", true},
	})
}

func TestValidateMemoryContent_GitHubPAT(t *testing.T) {
	runValidateCases(t, []validateCase{
		{"ghp_abcdefghijklmnopqrstuvwxyz1234", "ghp_abcdefghijklmnopqrstuvwxyz1234", true},
		{"gho_abcdefghijklmnopqrstuvwxyz1234", "gho_abcdefghijklmnopqrstuvwxyz1234", true},
		{"ghu_abcdefghijklmnopqrstuvwxyz1234", "ghu_abcdefghijklmnopqrstuvwxyz1234", true},
		{"ghs_abcdefghijklmnopqrstuvwxyz1234", "ghs_abcdefghijklmnopqrstuvwxyz1234", true},
		{"ghr_abcdefghijklmnopqrstuvwxyz1234", "ghr_abcdefghijklmnopqrstuvwxyz1234", true},
	})
}

func TestValidateMemoryContent_BearerToken(t *testing.T) {
	runValidateCases(t, []validateCase{
		{"Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9", "Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9", true},
		{"Authorization: bearer abcdefghijklmnop1234", "Authorization: bearer abcdefghijklmnop1234", true},
	})
}

func TestValidateMemoryContent_PrivateKeyPEM(t *testing.T) {
	runValidateCases(t, []validateCase{
		{"-----BEGIN RSA PRIVATE KEY-----", "-----BEGIN RSA PRIVATE KEY-----", true},
		{"-----BEGIN EC PRIVATE KEY-----", "-----BEGIN EC PRIVATE KEY-----", true},
		{"-----BEGIN PRIVATE KEY-----", "-----BEGIN PRIVATE KEY-----", true},
		{"-----BEGIN ENCRYPTED PRIVATE KEY-----", "-----BEGIN ENCRYPTED PRIVATE KEY-----", true},
	})
}

// ---------------------------------------------------------------------------
// False positive tests — should NOT be rejected
// ---------------------------------------------------------------------------

func TestValidateMemoryContent_AllowsPlainFact(t *testing.T) {
	runValidateCases(t, []validateCase{
		{"Agent prefers concise replies", "Agent prefers concise replies", false},
		{"Tên tôi là Bảo", "Tên tôi là Bảo", false},
		{"Email: baolnc@vclaw.site", "Email: baolnc@vclaw.site", false},
		{"Thích làm việc bằng tiếng Việt", "Thích làm việc bằng tiếng Việt", false},
		{"Người quen thuộc: quanghtd@vclaw.site", "Người quen thuộc: quanghtd@vclaw.site", false},
	})
}

func TestValidateMemoryContent_AllowsDescriptiveApiKeyReference(t *testing.T) {
	// The regex requires [:=] after the keyword, so descriptions that mention
	// "api_key" or "secret" without an assignment should pass.
	runValidateCases(t, []validateCase{
		{"The api_key field is documented here", "The api_key field is documented here", false},
		{"User keeps password in a vault", "User keeps password in a vault", false},
		{"See the secret section of the wiki", "See the secret section of the wiki", false},
	})
}

func TestValidateMemoryContent_AllowsShortBearerLikeText(t *testing.T) {
	// "bearer" without [:=] or without a 16+ char token should pass.
	runValidateCases(t, []validateCase{
		{"Bearer is used for OAuth auth", "Bearer is used for OAuth auth", false},
	})
}

func TestValidateMemoryContent_AllowsShortSkPrefix(t *testing.T) {
	// sk- prefix followed by fewer than 16 chars should not match.
	runValidateCases(t, []validateCase{
		{"sk-abc123 is too short", "sk-abc123 is too short", false},
	})
}

func TestValidateMemoryContent_AllowsShortGitHubPrefix(t *testing.T) {
	// ghp_ prefix followed by fewer than 20 chars should not match.
	runValidateCases(t, []validateCase{
		{"ghp_short is not a PAT", "ghp_short is not a PAT", false},
	})
}

func TestValidateMemoryContent_EmptyAndWhitespace(t *testing.T) {
	runValidateCases(t, []validateCase{
		{"empty string", "", false},
		{"whitespace only", "   \t\n  ", false},
	})
}
