package dashboard

import "github.com/good-fish-man/agent-runtime-client/infra/data"

// Handler serves dashboard summary endpoints used by the frontend.
type Handler struct {
	data *data.Data
}

func NewHandler(d *data.Data) *Handler {
	return &Handler{data: d}
}
