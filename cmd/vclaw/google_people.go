package main

import (
	"context"
	"fmt"
	"strings"

	"vclaw/internal/connectors/google"
	googleoauth "vclaw/internal/connectors/google/oauth"
	peopleconnector "vclaw/internal/connectors/google/people"
	peopletool "vclaw/internal/tools/office/people"
)

func runGooglePeople(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printGooglePeopleUsage()
		return nil
	}

	switch args[0] {
	case "search-directory":
		fs := newGoogleFlagSet("people search-directory")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		query := fs.String("query", "", "name or email prefix to search in the Workspace directory")
		maxResults := fs.Int64("max-results", 10, "number of people to return (1-50)")
		pageToken := fs.String("page-token", "", "optional People API page token")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}

		httpClient, err := googleoauth.Client(ctx, googleoauth.Config{
			CredentialsPath: *credentialsPath,
			TokenPath:       *tokenPath,
			Scopes:          google.G1Scopes,
		})
		if err != nil {
			return err
		}

		service := peopletool.NewService(peopleconnector.NewClient(httpClient))
		output, toolErr := service.SearchDirectory(ctx, peopletool.SearchDirectoryInput{
			Query:      *query,
			MaxResults: *maxResults,
			PageToken:  *pageToken,
		})
		if toolErr != nil {
			return fmt.Errorf("%s: %s", toolErr.Code, toolErr.Message)
		}

		if len(output.People) == 0 {
			fmt.Println("No directory people found.")
		}
		for _, person := range output.People {
			fmt.Printf("- %s | %s\n", emptyForCLI(person.ResourceName, "(no resource name)"), emptyForCLI(person.DisplayName, "(no display name)"))
			fmt.Printf("  Emails: %s\n", emptyListForCLI(person.EmailAddresses))
			fmt.Printf("  Candidate Chat users: %s\n", emptyListForCLI(person.CandidateUserNames))
			fmt.Printf("  Source IDs: %s\n", emptyListForCLI(person.SourceIDs))
			fmt.Printf("  Source types: %s\n", emptyListForCLI(person.SourceTypes))
		}
		if strings.TrimSpace(output.NextPageToken) != "" {
			fmt.Printf("Next page token: %s\n", output.NextPageToken)
		}
		return nil

	case "help", "-h", "--help":
		printGooglePeopleUsage()
		return nil
	default:
		return fmt.Errorf("unknown google people command %q", args[0])
	}
}

func emptyListForCLI(values []string) string {
	if len(values) == 0 {
		return "(none)"
	}
	return strings.Join(values, ",")
}

func printGooglePeopleUsage() {
	fmt.Println(`Google People commands:
  vclaw google people search-directory -query "Bao" [-max-results 10] [-page-token token]
      Search Google Workspace directory people by name or email.`)
}
