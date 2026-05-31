package gmail

import (
	"context"
	"net/http"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

type Label struct {
	ID   string
	Name string
}

func ListLabels(ctx context.Context, client *http.Client, userID string) ([]Label, error) {
	service, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, err
	}

	response, err := service.Users.Labels.List(userID).Do()
	if err != nil {
		return nil, err
	}

	labels := make([]Label, 0, len(response.Labels))
	for _, label := range response.Labels {
		labels = append(labels, Label{
			ID:   label.Id,
			Name: label.Name,
		})
	}
	return labels, nil
}
