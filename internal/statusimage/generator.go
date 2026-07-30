package statusimage

import (
	"context"

	"qq-status-bot/internal/statusapi"
)

type Generator struct {
	Client   statusapi.Client
	Renderer Renderer
}

func (g Generator) Generate(ctx context.Context, baseURL, pageID, period string) ([]byte, error) {
	page, err := g.Client.Fetch(ctx, baseURL, pageID, period)
	if err != nil {
		return nil, err
	}
	return g.Renderer.Render(page)
}
