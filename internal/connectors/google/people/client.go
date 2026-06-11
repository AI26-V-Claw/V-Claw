package people

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/api/option"
	peopleapi "google.golang.org/api/people/v1"
)

const (
	defaultReadMask = "names,emailAddresses,metadata"
)

type Client struct {
	httpClient *http.Client
}

func NewClient(httpClient *http.Client) *Client {
	return &Client{httpClient: httpClient}
}

type DirectoryPerson struct {
	ResourceName       string
	DisplayName        string
	GivenName          string
	FamilyName         string
	EmailAddresses     []string
	SourceIDs          []string
	SourceTypes        []string
	CandidateUserNames []string
}

type SearchDirectoryOutput struct {
	People        []DirectoryPerson
	NextPageToken string
	TotalSize     int64
}

func (c *Client) SearchDirectoryPeople(ctx context.Context, query string, pageSize int64, pageToken string) (SearchDirectoryOutput, error) {
	return SearchDirectoryPeople(ctx, c.httpClient, query, pageSize, pageToken)
}

func (c *Client) GetPerson(ctx context.Context, resourceName string) (DirectoryPerson, error) {
	return GetPerson(ctx, c.httpClient, resourceName)
}

// GetPerson fetches a single person by People API resource name (e.g. "people/123456789").
// Use this to resolve a Chat member's numeric ID to an email address:
// convert "users/123456789" → "people/123456789" before calling.
func GetPerson(ctx context.Context, client *http.Client, resourceName string) (DirectoryPerson, error) {
	resourceName = strings.TrimSpace(resourceName)
	if resourceName == "" {
		return DirectoryPerson{}, errors.New("resourceName is required")
	}
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return DirectoryPerson{}, err
	}
	person, err := service.People.Get(resourceName).
		PersonFields(defaultReadMask).
		Context(ctx).
		Do()
	if err != nil {
		return DirectoryPerson{}, err
	}
	return personFromAPI(person), nil
}

func SearchDirectoryPeople(ctx context.Context, client *http.Client, query string, pageSize int64, pageToken string) (SearchDirectoryOutput, error) {
	if strings.TrimSpace(query) == "" {
		return SearchDirectoryOutput{}, errors.New("query is required")
	}

	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return SearchDirectoryOutput{}, err
	}

	call := service.People.SearchDirectoryPeople().
		Query(strings.TrimSpace(query)).
		ReadMask(defaultReadMask).
		Sources("DIRECTORY_SOURCE_TYPE_DOMAIN_PROFILE", "DIRECTORY_SOURCE_TYPE_DOMAIN_CONTACT").
		PageSize(pageSize).
		Context(ctx)
	if strings.TrimSpace(pageToken) != "" {
		call = call.PageToken(pageToken)
	}

	response, err := call.Do()
	if err != nil {
		return SearchDirectoryOutput{}, err
	}

	people := make([]DirectoryPerson, 0, len(response.People))
	for _, person := range response.People {
		people = append(people, personFromAPI(person))
	}
	return SearchDirectoryOutput{
		People:        people,
		NextPageToken: response.NextPageToken,
		TotalSize:     response.TotalSize,
	}, nil
}

func serviceFromClient(ctx context.Context, client *http.Client) (*peopleapi.Service, error) {
	if client == nil {
		return nil, errors.New("http client is required")
	}

	service, err := peopleapi.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("create people service: %w", err)
	}
	return service, nil
}

func personFromAPI(person *peopleapi.Person) DirectoryPerson {
	if person == nil {
		return DirectoryPerson{}
	}

	out := DirectoryPerson{
		ResourceName: person.ResourceName,
	}
	if len(person.Names) > 0 && person.Names[0] != nil {
		name := person.Names[0]
		out.DisplayName = name.DisplayName
		out.GivenName = name.GivenName
		out.FamilyName = name.FamilyName
	}
	for _, email := range person.EmailAddresses {
		if email == nil || strings.TrimSpace(email.Value) == "" {
			continue
		}
		out.EmailAddresses = append(out.EmailAddresses, strings.TrimSpace(email.Value))
	}
	if person.Metadata != nil {
		for _, source := range person.Metadata.Sources {
			if source == nil {
				continue
			}
			if strings.TrimSpace(source.Id) != "" {
				out.SourceIDs = append(out.SourceIDs, source.Id)
				out.CandidateUserNames = append(out.CandidateUserNames, "users/"+source.Id)
			}
			if strings.TrimSpace(source.Type) != "" {
				out.SourceTypes = append(out.SourceTypes, source.Type)
			}
		}
	}

	if id := strings.TrimPrefix(person.ResourceName, "people/"); id != person.ResourceName && strings.TrimSpace(id) != "" {
		out.CandidateUserNames = append(out.CandidateUserNames, "users/"+id)
	}
	out.CandidateUserNames = uniqueStrings(out.CandidateUserNames)
	return out
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
