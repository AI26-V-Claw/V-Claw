package people

import (
	"context"
	"errors"
	"testing"

	peopleconnector "vclaw/internal/connectors/google/people"
	"vclaw/internal/tools"
)

type fakeConnector struct {
	output peopleconnector.SearchDirectoryOutput
	err    error
}

func (f fakeConnector) SearchDirectoryPeople(context.Context, string, int64, string) (peopleconnector.SearchDirectoryOutput, error) {
	if f.err != nil {
		return peopleconnector.SearchDirectoryOutput{}, f.err
	}
	return f.output, nil
}

func TestSearchDirectoryRequiresQuery(t *testing.T) {
	service := NewService(fakeConnector{})

	_, errShape := service.SearchDirectory(context.Background(), SearchDirectoryInput{})
	if errShape == nil {
		t.Fatal("expected validation error")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Fatalf("expected INVALID_INPUT, got %q", errShape.Code)
	}
}

func TestSearchDirectoryReturnsPeople(t *testing.T) {
	service := NewService(fakeConnector{
		output: peopleconnector.SearchDirectoryOutput{
			People: []peopleconnector.DirectoryPerson{
				{
					ResourceName:       "people/123",
					DisplayName:        "Bao",
					EmailAddresses:     []string{"bao@example.com"},
					CandidateUserNames: []string{"users/123"},
				},
			},
		},
	})

	output, errShape := service.SearchDirectory(context.Background(), SearchDirectoryInput{Query: "Bao"})
	if errShape != nil {
		t.Fatalf("unexpected error: %s", errShape.Message)
	}
	if len(output.People) != 1 {
		t.Fatalf("expected 1 person, got %d", len(output.People))
	}
	if output.People[0].CandidateUserNames[0] != "users/123" {
		t.Fatalf("unexpected candidate usernames: %#v", output.People[0].CandidateUserNames)
	}
}

func TestSearchDirectoryMapsConnectorError(t *testing.T) {
	service := NewService(fakeConnector{err: errors.New("boom")})

	_, errShape := service.SearchDirectory(context.Background(), SearchDirectoryInput{Query: "Bao"})
	if errShape == nil {
		t.Fatal("expected connector error")
	}
	if errShape.Code != "INTERNAL_ERROR" {
		t.Fatalf("expected INTERNAL_ERROR, got %q", errShape.Code)
	}
}

func TestSearchDirectoryToolRiskMetadata(t *testing.T) {
	tool := NewSearchDirectoryTool(NewService(fakeConnector{}))

	if tool.Capability() != tools.CapabilityReadOnly {
		t.Fatalf("expected read-only capability")
	}
	if tool.RiskLevel() != tools.RiskLevelSafeRead {
		t.Fatalf("expected safe_read risk")
	}
}
