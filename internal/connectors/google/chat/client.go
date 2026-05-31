package chat

import (
	"context"
	"net/http"

	"google.golang.org/api/chat/v1"
	"google.golang.org/api/option"
)

type Space struct {
	Name        string
	DisplayName string
	Type        string
}

type Message struct {
	Name string
	Text string
}

func ListSpaces(ctx context.Context, client *http.Client, pageSize int64) ([]Space, error) {
	service, err := chat.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, err
	}

	response, err := service.Spaces.List().PageSize(pageSize).Do()
	if err != nil {
		return nil, err
	}

	spaces := make([]Space, 0, len(response.Spaces))
	for _, space := range response.Spaces {
		spaces = append(spaces, Space{
			Name:        space.Name,
			DisplayName: space.DisplayName,
			Type:        space.SpaceType,
		})
	}
	return spaces, nil
}

func CreateTextMessage(ctx context.Context, client *http.Client, parent string, text string) (Message, error) {
	service, err := chat.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return Message{}, err
	}

	response, err := service.Spaces.Messages.Create(parent, &chat.Message{Text: text}).Do()
	if err != nil {
		return Message{}, err
	}

	return Message{
		Name: response.Name,
		Text: response.Text,
	}, nil
}
