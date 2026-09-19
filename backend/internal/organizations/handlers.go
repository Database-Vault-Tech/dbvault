package organizations

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/audit"
	"github.com/dbvault/dbvault/backend/internal/auth"
	"github.com/dbvault/dbvault/backend/internal/db"
	"github.com/dbvault/dbvault/backend/internal/httpx"
	"github.com/dbvault/dbvault/backend/internal/reqctx"
	"github.com/dbvault/dbvault/backend/internal/validate"
)

// UserRoutes are authenticated but not organization-scoped.
func (s *Service) UserRoutes(r chi.Router) {
	r.Get("/organizations", s.handleList)
	r.Post("/organizations", s.handleCreate)
	r.Post("/invitations/accept", s.handleAccept)
}

// OrgRoutes are scoped to the selected organization.
func (s *Service) OrgRoutes(r chi.Router) {
	r.Get("/organization", s.handleCurrent)
	r.Patch("/organization", s.handleUpdate)
	r.Post("/organization/recovery-key", s.handleRecoveryKey)
	r.Get("/team/members", s.handleMembers)
	r.Patch("/team/members/{userID}", s.handleChangeRole)
	r.Delete("/team/members/{userID}", s.handleRemove)
	r.Get("/team/invitations", s.handleInvitations)
	r.Post("/team/invitations", s.handleInvite)
	r.Delete("/team/invitations/{id}", s.handleRevokeInvitation)
}

func (s *Service) handleList(w http.ResponseWriter, r *http.Request) {
	p, _ := reqctx.PrincipalFrom(r.Context())
	orgs, err := s.ListForUser(r.Context(), p.UserID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, orgs)
}

func (s *Service) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	v := validate.New()
	v.Required("name", req.Name)
	v.MaxLen("name", req.Name, 80)
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}
	p, _ := reqctx.PrincipalFrom(r.Context())
	var org Organization
	err := db.WithTx(r.Context(), s.Pool, func(tx pgx.Tx) error {
		var err error
		org, err = s.Create(r.Context(), tx, p.UserID, req.Name)
		return err
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, org)
}

func (s *Service) handleAccept(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	p, _ := reqctx.PrincipalFrom(r.Context())
	org, err := s.AcceptInvitation(r.Context(), p, strings.TrimSpace(req.Token))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, org)
}

func (s *Service) handleCurrent(w http.ResponseWriter, r *http.Request) {
	m, _ := reqctx.MembershipFrom(r.Context())
	var members int
	_ = s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM organization_members WHERE organization_id = $1`, m.OrgID).Scan(&members)
	httpx.JSON(w, http.StatusOK, map[string]any{"id": m.OrgID, "name": m.OrgName, "slug": m.OrgSlug, "role": m.Role, "member_count": members})
}

func (s *Service) handleUpdate(w http.ResponseWriter, r *http.Request) {
	m, err := auth.Require(r.Context(), auth.RoleAdmin)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	v := validate.New()
	v.Required("name", req.Name)
	v.MaxLen("name", req.Name, 80)
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if _, err := s.Pool.Exec(r.Context(), `UPDATE organizations SET name = $2 WHERE id = $1`, m.OrgID, req.Name); err != nil {
		httpx.Error(w, r, err)
		return
	}
	audit.MustRecord(r.Context(), s.Pool, audit.Entry{OrgID: m.OrgID, Action: audit.OrgUpdated, ResourceType: "organization", ResourceID: m.OrgID, Metadata: map[string]any{"name": req.Name}})
	httpx.JSON(w, http.StatusOK, map[string]any{"id": m.OrgID, "name": req.Name, "slug": m.OrgSlug, "role": m.Role})
}

func (s *Service) handleRecoveryKey(w http.ResponseWriter, r *http.Request) {
	m, err := auth.Require(r.Context(), auth.RoleOwner)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	p, _ := reqctx.PrincipalFrom(r.Context())
	key, err := s.ExportRecoveryKey(r.Context(), m, p, req.Password)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, key)
}

func (s *Service) handleMembers(w http.ResponseWriter, r *http.Request) {
	m, _ := reqctx.MembershipFrom(r.Context())
	members, err := s.Members(r.Context(), m.OrgID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, members)
}

func (s *Service) handleChangeRole(w http.ResponseWriter, r *http.Request) {
	m, err := auth.Require(r.Context(), auth.RoleAdmin)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	userID := chi.URLParam(r, "userID")
	if !httpx.IsUUID(userID) {
		httpx.Error(w, r, apperr.NotFound("Member"))
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if !auth.ValidRole(req.Role) {
		httpx.Error(w, r, apperr.Validation(map[string]string{"role": "Must be one of: owner, admin, member, viewer."}))
		return
	}
	if err := s.ChangeRole(r.Context(), m, userID, req.Role); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (s *Service) handleRemove(w http.ResponseWriter, r *http.Request) {
	m, _ := reqctx.MembershipFrom(r.Context())
	p, _ := reqctx.PrincipalFrom(r.Context())
	userID := chi.URLParam(r, "userID")
	if !httpx.IsUUID(userID) {
		httpx.Error(w, r, apperr.NotFound("Member"))
		return
	}
	if err := s.RemoveMember(r.Context(), m, p.UserID, userID); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (s *Service) handleInvitations(w http.ResponseWriter, r *http.Request) {
	m, err := auth.Require(r.Context(), auth.RoleAdmin)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	invs, err := s.Invitations(r.Context(), m.OrgID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, invs)
}

func (s *Service) handleInvite(w http.ResponseWriter, r *http.Request) {
	m, err := auth.Require(r.Context(), auth.RoleAdmin)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	v := validate.New()
	v.Email("email", req.Email)
	v.OneOf("role", req.Role, auth.RoleAdmin, auth.RoleMember, auth.RoleViewer)
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}
	p, _ := reqctx.PrincipalFrom(r.Context())
	inv, err := s.Invite(r.Context(), m, p, req.Email, req.Role)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"invitation": inv, "email_sent": s.Mailer != nil && s.Mailer.Configured()})
}

func (s *Service) handleRevokeInvitation(w http.ResponseWriter, r *http.Request) {
	m, err := auth.Require(r.Context(), auth.RoleAdmin)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	id := chi.URLParam(r, "id")
	if !httpx.IsUUID(id) {
		httpx.Error(w, r, apperr.NotFound("Invitation"))
		return
	}
	if err := s.RevokeInvitation(r.Context(), m.OrgID, id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}
