package notifications

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/auth"
	"github.com/dbvault/dbvault/backend/internal/httpx"
	"github.com/dbvault/dbvault/backend/internal/reqctx"
)

func (s *Service) Routes(r chi.Router) {
	r.Get("/notifications", s.handleList)
	r.Post("/notifications", s.handleCreate)
	r.Get("/notifications/events", s.handleEvents)
	r.Get("/notifications/deliveries", s.handleDeliveries)
	r.Patch("/notifications/{id}", s.handleUpdate)
	r.Delete("/notifications/{id}", s.handleDelete)
	r.Post("/notifications/{id}/test", s.handleTest)
}

func idParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "id")
	if !httpx.IsUUID(id) {
		httpx.Error(w, r, apperr.NotFound("Notification channel"))
		return "", false
	}
	return id, true
}

func (s *Service) handleList(w http.ResponseWriter, r *http.Request) {
	m, _ := reqctx.MembershipFrom(r.Context())
	list, err := s.List(r.Context(), m.OrgID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, list)
}

func (s *Service) handleEvents(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{"events": AllEvents, "email_configured": s.EmailConfigured})
}

func (s *Service) handleDeliveries(w http.ResponseWriter, r *http.Request) {
	m, _ := reqctx.MembershipFrom(r.Context())
	list, err := s.Deliveries(r.Context(), m.OrgID, httpx.QueryInt(r, "limit", 50, 1, 200))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, list)
}

func (s *Service) handleCreate(w http.ResponseWriter, r *http.Request) {
	m, err := auth.Require(r.Context(), auth.RoleAdmin)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var in Input
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := in.Validate(true); err != nil {
		httpx.Error(w, r, err)
		return
	}
	p, _ := reqctx.PrincipalFrom(r.Context())
	c, secret, err := s.Create(r.Context(), m.OrgID, p.UserID, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	out := map[string]any{"notification": c}
	if secret != "" {
		out["signing_secret"] = secret
	}
	httpx.JSON(w, http.StatusCreated, out)
}

func (s *Service) handleUpdate(w http.ResponseWriter, r *http.Request) {
	m, err := auth.Require(r.Context(), auth.RoleAdmin)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	var in Input
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := in.Validate(false); err != nil {
		httpx.Error(w, r, err)
		return
	}
	c, err := s.Update(r.Context(), m.OrgID, id, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, c)
}

func (s *Service) handleDelete(w http.ResponseWriter, r *http.Request) {
	m, err := auth.Require(r.Context(), auth.RoleAdmin)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	if err := s.Delete(r.Context(), m.OrgID, id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (s *Service) handleTest(w http.ResponseWriter, r *http.Request) {
	m, err := auth.Require(r.Context(), auth.RoleAdmin)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	deliveryID, err := s.SendTest(r.Context(), m.OrgID, id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"delivery_id": deliveryID, "status": "queued"})
}
