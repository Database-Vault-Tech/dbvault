package restore

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/auth"
	"github.com/dbvault/dbvault/backend/internal/httpx"
	"github.com/dbvault/dbvault/backend/internal/jobs"
	"github.com/dbvault/dbvault/backend/internal/reqctx"
)

func (s *Service) Routes(r chi.Router) {
	r.Get("/restores", s.handleList)
	r.Post("/restores", s.handleCreate)
	r.Get("/restores/{id}", s.handleGet)
}

func (s *Service) handleList(w http.ResponseWriter, r *http.Request) {
	m, _ := reqctx.MembershipFrom(r.Context())
	q := r.URL.Query()
	f := ListFilter{
		TargetDatabaseID: q.Get("target_database_id"),
		Status:           q.Get("status"),
		Mode:             q.Get("mode"),
		Limit:            httpx.QueryInt(r, "limit", 50, 1, 200),
	}
	if f.TargetDatabaseID != "" && !httpx.IsUUID(f.TargetDatabaseID) {
		httpx.Error(w, r, apperr.BadRequest("Invalid filter id."))
		return
	}
	if b := q.Get("before"); b != "" {
		t, err := time.Parse(time.RFC3339Nano, b)
		if err != nil {
			httpx.Error(w, r, apperr.BadRequest("before must be an RFC 3339 timestamp."))
			return
		}
		f.Before = &t
	}
	list, err := s.List(r.Context(), m.OrgID, f)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	meta := map[string]any{"limit": f.Limit}
	if len(list) == f.Limit {
		meta["next_before"] = list[len(list)-1].CreatedAt.Format(time.RFC3339Nano)
	}
	httpx.List(w, list, meta)
}

func (s *Service) handleCreate(w http.ResponseWriter, r *http.Request) {
	m, err := auth.Require(r.Context(), auth.RoleAdmin)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var in CreateInput
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	p, _ := reqctx.PrincipalFrom(r.Context())
	j, err := s.Create(r.Context(), m.OrgID, p.UserID, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, j)
}

func (s *Service) handleGet(w http.ResponseWriter, r *http.Request) {
	m, _ := reqctx.MembershipFrom(r.Context())
	id := chi.URLParam(r, "id")
	if !httpx.IsUUID(id) {
		httpx.Error(w, r, apperr.NotFound("Restore"))
		return
	}
	rj, err := s.Get(r.Context(), m.OrgID, id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	out := map[string]any{"restore": rj}
	if rj.JobID != nil {
		if j, err := s.Queue.Get(r.Context(), m.OrgID, *rj.JobID); err == nil {
			out["job"] = j
		}
		logs, err := jobs.Logs(r.Context(), s.Pool, *rj.JobID, 0)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		out["logs"] = logs
	}
	httpx.JSON(w, http.StatusOK, out)
}
