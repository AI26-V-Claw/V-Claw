package google

import "testing"

func TestG1ScopesIncludesWorkspaceScopes(t *testing.T) {
	for _, scope := range []string{
		ScopeDriveReadonly,
		ScopeDrive,
		ScopeDocumentsReadonly,
		ScopeDocuments,
		ScopeSpreadsheetsReadonly,
		ScopeSpreadsheets,
		ScopeMeetingsSpaceCreated,
	} {
		if !hasScope(G1Scopes, scope) {
			t.Fatalf("G1Scopes missing %s", scope)
		}
	}
}

func hasScope(scopes []string, want string) bool {
	for _, scope := range scopes {
		if scope == want {
			return true
		}
	}
	return false
}
