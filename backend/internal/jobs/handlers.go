package jobs

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/audit"
	"github.com/dbvault/dbvault/backend/internal/auth"
	"github.com/dbvault/dbvault/backend/internal/db"
	"github.com/dbvault/dbvault/backend/internal/httpx"
	"github.com/dbvault/dbvault/backend/internal/reqctx"
)

func (qu *Queue) Routes(r chi.Router) {
	r.Get("/jobs", qu.handleList)
	r.Get("/jobs/{id}", qu.handleGet)
	r.Get("/jobs/{id}/logs", qu.handleLogs)
	r.Post("/jobs/{id}/cancel", qu.handleCancel)
}

func jobID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "id")
	if !httpx.IsUUID(id) {
		httpx.Error(w, r, apperr.NotFound("Job"))
		return "", false
	}
	return id, true
}

func (qu *Queue) handleList(w http.ResponseWriter, r *http.Request) {
	m, _ := reqctx.MembershipFrom(r.Context())
	list, err := qu.List(r.Context(), m.OrgID, ListFilter{
		Type: r.URL.Query().Get("type"), Status: r.URL.Query().Get("status"), Limit: httpx.QueryInt(r, "limit", 50, 1, 200),
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, list)
}

func (qu *Queue) handleGet(w http.ResponseWriter, r *http.Request) {
	m, _ := reqctx.MembershipFrom(r.Context())
	id, ok := jobID(w, r)
	if !ok {
		return
	}
	j, err := qu.Get(r.Context(), m.OrgID, id)
	if db.IsNotFound(err) {
		httpx.Error(w, r, apperr.NotFound("Job"))
		return
	}
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	logs, err := Logs(r.Context(), qu.pool, id, 0)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"job": j, "logs": logs})
}

func (qu *Queue) handleLogs(w http.ResponseWriter, r *http.Request) {
	m, _ := reqctx.MembershipFrom(r.Context())
	id, ok := jobID(w, r)
	if !ok {
		return
	}
	if _, err := qu.Get(r.Context(), m.OrgID, id); err != nil {
		httpx.Error(w, r, apperr.NotFound("Job"))
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	logs, err := Logs(r.Context(), qu.pool, id, after)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, logs)
}

func (qu *Queue) handleCancel(w http.ResponseWriter, r *http.Request) {
	m, err := auth.Require(r.Context(), auth.RoleMember)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	id, ok := jobID(w, r)
	if !ok {
		return
	}
	j, err := qu.RequestCancel(r.Context(), m.OrgID, id)
	switch {
	case db.IsNotFound(err):
		httpx.Error(w, r, apperr.NotFound("Job"))
		return
	case errors.Is(err, ErrNotCancellable):
		httpx.Error(w, r, apperr.Conflict("This job has already finished."))
		return
	case err != nil:
		httpx.Error(w, r, err)
		return
	}
	audit.MustRecord(r.Context(), qu.pool, audit.Entry{OrgID: m.OrgID, Action: audit.JobCancelled, ResourceType: "job", ResourceID: id, Metadata: map[string]any{"type": j.Type}})
	httpx.JSON(w, http.StatusAccepted, j)
}
