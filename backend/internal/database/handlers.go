package database

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/auth"
	"github.com/dbvault/dbvault/backend/internal/httpx"
	"github.com/dbvault/dbvault/backend/internal/reqctx"
)

func (s *Service) Routes(r chi.Router) {
	r.Get("/databases", s.handleList)
	r.Post("/databases", s.handleCreate)
	r.Post("/databases/test", s.handleTestUnsaved)
	r.Get("/databases/{id}", s.handleGet)
	r.Patch("/databases/{id}", s.handleUpdate)
	r.Delete("/databases/{id}", s.handleDelete)
	r.Post("/databases/{id}/test", s.handleTestSaved)
}

func idParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "id")
	if !httpx.IsUUID(id) {
		httpx.Error(w, r, apperr.NotFound("Database"))
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

func (s *Service) handleGet(w http.ResponseWriter, r *http.Request) {
	m, _ := reqctx.MembershipFrom(r.Context())
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	d, err := s.Get(r.Context(), m.OrgID, id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h, err := s.Health(r.Context(), m.OrgID, d)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"database": d, "health": h})
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
	d, err := s.Create(r.Context(), m.OrgID, p.UserID, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	// Test right away so the list shows connection health immediately.
	res, err := s.TestTarget(r.Context(), in.Target())
	if err == nil {
		s.RecordTest(r.Context(), d.ID, res)
		if refreshed, err := s.Get(r.Context(), m.OrgID, d.ID); err == nil {
			d = refreshed
		}
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"database": d, "test": res})
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
	d, err := s.Update(r.Context(), m.OrgID, id, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
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

func (s *Service) handleTestSaved(w http.ResponseWriter, r *http.Request) {
	m, err := auth.Require(r.Context(), auth.RoleMember)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	if _, err := s.Get(r.Context(), m.OrgID, id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	res, err := s.TestSaved(r.Context(), m.OrgID, id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}

// handleTestUnsaved tests credentials from the "add database" form before
// they are stored. When editing, database_id lets the stored password be
// reused if the form leaves it blank.
func (s *Service) handleTestUnsaved(w http.ResponseWriter, r *http.Request) {
	m, err := auth.Require(r.Context(), auth.RoleAdmin)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req struct {
		Input
		DatabaseID string `json:"database_id"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	in := req.Input
	needPassword := req.DatabaseID == ""
	if err := in.Validate(needPassword); err != nil {
		httpx.Error(w, r, err)
		return
	}
	t := in.Target()
	if t.Password == "" && req.DatabaseID != "" {
		if !httpx.IsUUID(req.DatabaseID) {
			httpx.Error(w, r, apperr.NotFound("Database"))
			return
		}
		stored, err := s.Target(r.Context(), m.OrgID, req.DatabaseID)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		t.Password = stored.Password
		if t.SSLRootCert == "" && in.SSLRootCert == nil {
			t.SSLRootCert = stored.SSLRootCert
		}
	}
	res, err := s.TestTarget(r.Context(), t)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}
