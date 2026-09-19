package storage

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/auth"
	"github.com/dbvault/dbvault/backend/internal/httpx"
	"github.com/dbvault/dbvault/backend/internal/reqctx"
)

func (s *DestinationService) Routes(r chi.Router) {
	r.Get("/storage", s.handleList)
	r.Post("/storage", s.handleCreate)
	r.Get("/storage/builtin", s.handleBuiltin)
	r.Post("/storage/test", s.handleTestUnsaved)
	r.Get("/storage/{id}", s.handleGet)
	r.Patch("/storage/{id}", s.handleUpdate)
	r.Delete("/storage/{id}", s.handleDelete)
	r.Post("/storage/{id}/test", s.handleTestSaved)
}

func idParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "id")
	if !httpx.IsUUID(id) {
		httpx.Error(w, r, apperr.NotFound("Storage destination"))
		return "", false
	}
	return id, true
}

func (s *DestinationService) handleList(w http.ResponseWriter, r *http.Request) {
	m, _ := reqctx.MembershipFrom(r.Context())
	list, err := s.List(r.Context(), m.OrgID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, list)
}

func (s *DestinationService) handleBuiltin(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"available": s.Builtin.Configured()}
	if s.Builtin.Configured() {
		out["type"] = TypeMinIO
		out["endpoint"] = s.Builtin.Endpoint
		out["bucket"] = s.Builtin.Bucket
		out["console_url"] = s.Builtin.PublicEndpoint
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *DestinationService) handleGet(w http.ResponseWriter, r *http.Request) {
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
	httpx.JSON(w, http.StatusOK, d)
}

func (s *DestinationService) handleCreate(w http.ResponseWriter, r *http.Request) {
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
	if err := s.Normalize(&in, true); err != nil {
		httpx.Error(w, r, err)
		return
	}
	p, _ := reqctx.PrincipalFrom(r.Context())
	d, err := s.Create(r.Context(), m.OrgID, p.UserID, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	res, _ := s.TestSaved(r.Context(), m.OrgID, d.ID)
	if refreshed, err := s.Get(r.Context(), m.OrgID, d.ID); err == nil {
		d = refreshed
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"storage": d, "test": res})
}

func (s *DestinationService) handleUpdate(w http.ResponseWriter, r *http.Request) {
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
	if err := s.Normalize(&in, false); err != nil {
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

func (s *DestinationService) handleDelete(w http.ResponseWriter, r *http.Request) {
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

func (s *DestinationService) handleTestSaved(w http.ResponseWriter, r *http.Request) {
	m, err := auth.Require(r.Context(), auth.RoleMember)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	res, err := s.TestSaved(r.Context(), m.OrgID, id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}

func (s *DestinationService) handleTestUnsaved(w http.ResponseWriter, r *http.Request) {
	m, err := auth.Require(r.Context(), auth.RoleAdmin)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req struct {
		Input
		StorageID string `json:"storage_id"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	in := req.Input
	editing := req.StorageID != ""
	if err := s.Normalize(&in, !editing); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if editing && (in.AccessKeyID == nil || *in.AccessKeyID == "") && in.Type != TypeLocal {
		if !httpx.IsUUID(req.StorageID) {
			httpx.Error(w, r, apperr.NotFound("Storage destination"))
			return
		}
		creds, err := s.Credentials(r.Context(), m.OrgID, req.StorageID)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		in.AccessKeyID, in.SecretAccessKey = &creds.AccessKeyID, &creds.SecretAccessKey
	}
	httpx.JSON(w, http.StatusOK, s.TestInput(r.Context(), in))
}
