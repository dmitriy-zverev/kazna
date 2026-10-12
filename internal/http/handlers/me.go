package handlers

import (
	"net/http"

	"github.com/dmitriy-zverev/kazna/internal/auth"
	"github.com/dmitriy-zverev/kazna/internal/http/dto"
	"github.com/dmitriy-zverev/kazna/internal/http/respond"
)

// Me reports which application the presented API key belongs to. It exists so
// application isolation is verifiable end to end before the payment API lands.
func Me(w http.ResponseWriter, r *http.Request) {
	authenticated, ok := auth.From(r.Context())
	if !ok {
		respond.Error(w, r, http.StatusUnauthorized, respond.CodeUnauthorized, "not authenticated", false)
		return
	}

	app := authenticated.Application
	respond.JSON(w, http.StatusOK, dto.Me{
		ApplicationID: app.ID.String(),
		Name:          app.Name,
		Slug:          app.Slug,
	})
}
