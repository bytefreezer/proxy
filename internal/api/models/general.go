package models

type EmptyResponse struct{}
type EmptyRequest struct{}
type HealthResponse struct {
	Status string `json:"status"`
}
