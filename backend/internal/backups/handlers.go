package backups

import (
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/audit"
	"github.com/dbvault/dbvault/backend/internal/auth"
	"github.com/dbvault/dbvault/backend/internal/compress"
	"github.com/dbvault/dbvault/backend/internal/httpx"
	"github.com/dbvault/dbvault/backend/internal/jobs"
	"github.com/dbvault/dbvault/backend/internal/logging"
	"github.com/dbvault/dbvault/backend/internal/reqctx"
)

func (s *Service) Routes(r chi.Router) {
	r.Get("/dashboard", s.handleDashboard)
	r.Get("/backups", s.handleList)
	r.Post("/backups", s.handleCreate)
	r.Get("/backups/{id}", s.handleGet)
	r.Get("/backups/{id}/logs", s.handleLogs)
	r.Post("/backups/{id}/verify", s.handleVerify)
	r.Delete("/backups/{id}", s.handleDelete)
	r.Get("/backups/{id}/download", s.handleDownload)
}

func idParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "id")
	if !httpx.IsUUID(id) {
		httpx.Error(w, r, apperr.NotFound("Backup"))
		return "", false
	}
	return id, true
}

func (s *Service) handleDashboard(w http.ResponseWriter, r *http.Request) {
	m, _ := reqctx.MembershipFrom(r.Context())
	stats, err := s.Stats(r.Context(), m.OrgID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	recent, err := s.List(r.Context(), m.OrgID, ListFilter{Limit: 8})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"stats": stats, "recent_backups": recent})
}

func (s *Service) handleList(w http.ResponseWriter, r *http.Request) {
	m, _ := reqctx.MembershipFrom(r.Context())
	q := r.URL.Query()
	f := ListFilter{
		DatabaseID: q.Get("database_id"),
		ScheduleID: q.Get("schedule_id"),
		Status:     q.Get("status"),
		Trigger:    q.Get("trigger"),
		Limit:      httpx.QueryInt(r, "limit", 50, 1, 200),
	}
	if (f.DatabaseID != "" && !httpx.IsUUID(f.DatabaseID)) || (f.ScheduleID != "" && !httpx.IsUUID(f.ScheduleID)) {
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
	m, err := auth.Require(r.Context(), auth.RoleMember)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var in CreateInput
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if !httpx.IsUUID(in.DatabaseID) {
		// The CLI passes names; resolve them.
		d, err := s.Databases.FindByName(r.Context(), m.OrgID, in.DatabaseID)
		if err != nil {
			httpx.Error(w, r, apperr.Validation(map[string]string{"database_id": "Unknown database."}))
			return
		}
		in.DatabaseID = d.ID
	}
	if in.StorageDestinationID != "" && !httpx.IsUUID(in.StorageDestinationID) {
		httpx.Error(w, r, apperr.Validation(map[string]string{"storage_destination_id": "Invalid storage destination."}))
		return
	}
	p, _ := reqctx.PrincipalFrom(r.Context())
	q, err := s.CreateManual(r.Context(), m.OrgID, p.UserID, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, q)
}

func (s *Service) handleGet(w http.ResponseWriter, r *http.Request) {
	m, _ := reqctx.MembershipFrom(r.Context())
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	b, err := s.Get(r.Context(), m.OrgID, id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	out := map[string]any{"backup": b}
	if b.JobID != nil {
		if j, err := s.Queue.Get(r.Context(), m.OrgID, *b.JobID); err == nil {
			out["job"] = j
			logs, err := jobs.Logs(r.Context(), s.Pool, j.ID, 0)
			if err != nil {
				httpx.Error(w, r, err)
				return
			}
			out["logs"] = logs
		}
	}
	// Latest verification job (if any) so the UI can show its progress/logs.
	var verifyJobID string
	if err := s.Pool.QueryRow(r.Context(), `SELECT id FROM jobs WHERE organization_id = $1 AND type = 'verification' AND payload->>'backup_id' = $2
		ORDER BY created_at DESC LIMIT 1`, m.OrgID, id).Scan(&verifyJobID); err == nil {
		if j, err := s.Queue.Get(r.Context(), m.OrgID, verifyJobID); err == nil {
			out["verification_job"] = j
			if logs, err := jobs.Logs(r.Context(), s.Pool, j.ID, 0); err == nil {
				out["verification_logs"] = logs
			}
		}
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Service) handleLogs(w http.ResponseWriter, r *http.Request) {
	m, _ := reqctx.MembershipFrom(r.Context())
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	b, err := s.Get(r.Context(), m.OrgID, id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if b.JobID == nil {
		httpx.JSON(w, http.StatusOK, []jobs.LogEntry{})
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	logs, err := jobs.Logs(r.Context(), s.Pool, *b.JobID, after)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, logs)
}

func (s *Service) handleVerify(w http.ResponseWriter, r *http.Request) {
	m, err := auth.Require(r.Context(), auth.RoleMember)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	p, _ := reqctx.PrincipalFrom(r.Context())
	q, err := s.RequestVerification(r.Context(), m.OrgID, p.UserID, id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, q)
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
	if err := s.Delete(r.Context(), m.OrgID, id, "manual"); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

// handleDownload streams a backup to the client without buffering it.
//
//	format=raw  (default) the artifact exactly as stored (encrypted/compressed); member+
//	format=dump decrypted and decompressed native dump (e.g. for pg_restore);  admin+
func (s *Service) handleDownload(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "raw"
	}
	minRole := auth.RoleMember
	if format == "dump" {
		minRole = auth.RoleAdmin
	} else if format != "raw" {
		httpx.Error(w, r, apperr.BadRequest("format must be raw or dump."))
		return
	}
	m, err := auth.Require(r.Context(), minRole)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	b, err := s.Get(r.Context(), m.OrgID, id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if b.Status != "completed" || b.StorageKey == nil {
		httpx.Error(w, r, apperr.Conflict("Only completed backups can be downloaded."))
		return
	}
	dest, err := s.Destinations.GetIncludingDeleted(r.Context(), m.OrgID, b.StorageDestinationID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	st, err := s.Destinations.Open(r.Context(), dest)
	if err != nil {
		httpx.Error(w, r, apperr.Unprocessable("storage_error", "Could not open storage: "+err.Error()))
		return
	}
	rc, err := st.Download(r.Context(), *b.StorageKey)
	if err != nil {
		httpx.Error(w, r, apperr.Unprocessable("storage_error", "Could not read the backup from storage: "+err.Error()))
		return
	}
	defer rc.Close()

	name := path.Base(*b.StorageKey)
	var body io.Reader = rc
	if format == "dump" {
		identity := ""
		if b.Encrypted {
			identity, err = s.Keys.Identity(r.Context(), m.OrgID, *b.EncryptionKeyID)
			if err != nil {
				httpx.Error(w, r, err)
				return
			}
		}
		archive, err := OpenArchive(rc, b.Compression, identity)
		if err != nil {
			httpx.Error(w, r, apperr.Unprocessable("decrypt_failed", err.Error()))
			return
		}
		defer archive.Close()
		body = archive
		// backup_...<ext>.zst.age -> backup_...<ext>
		name = strings.TrimSuffix(strings.TrimSuffix(name, ".age"), compress.Extension(b.Compression))
	} else if b.SizeBytes != nil {
		w.Header().Set("Content-Length", strconv.FormatInt(*b.SizeBytes, 10))
	}
	audit.MustRecord(r.Context(), s.Pool, audit.Entry{OrgID: m.OrgID, Action: audit.BackupDownloaded, ResourceType: "backup", ResourceID: b.ID,
		Metadata: map[string]any{"format": format, "database": b.DatabaseName}})

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.Header().Set("Cache-Control", "no-store")
	if b.Checksum != nil && format == "raw" {
		w.Header().Set("X-DBVault-Checksum-SHA256", *b.Checksum)
	}
	if _, err := io.Copy(w, body); err != nil {
		// Headers are already sent; abort so the client sees a failed download
		// instead of a silently truncated file.
		logging.FromContext(r.Context()).Error("backup download interrupted", "backup_id", b.ID, "error", err.Error())
		panic(http.ErrAbortHandler)
	}
}
