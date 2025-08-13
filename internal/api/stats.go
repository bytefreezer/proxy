package api

import (
	"context"
	"strings"

	"github.com/n0needt0/goodies/bytefreezer-proxy/internal/api/models"
	"github.com/swaggest/usecase"
	"github.com/swaggest/usecase/status"
)

const (
	SUCCESS  = "success"
	HEALTHY  = "healthy"
	UNHEALTY = "unhealthy"
)

func (api *API) HealthCheck() usecase.IOInteractorOf[models.EmptyRequest, models.HealthResponse] {

	u := usecase.NewInteractor(func(ctx context.Context, req models.EmptyRequest, resp *models.HealthResponse) error {

		status := "success" //TODO: replace with actual health check

		if strings.ToLower(status) == SUCCESS {
			resp.Status = HEALTHY
		} else {
			resp.Status = UNHEALTY
		}

		return nil
	})
	u.SetTags("Internal")
	u.SetExpectedErrors(status.Internal)
	u.SetDescription("Check status of the service.")
	return u
}
