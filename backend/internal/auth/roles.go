package auth

import (
	"context"

	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/reqctx"
)

// Organization roles, from least to most privileged.
const (
	RoleViewer = "viewer"
	RoleMember = "member"
	RoleAdmin  = "admin"
	RoleOwner  = "owner"
)

var roleRank = map[string]int{RoleViewer: 1, RoleMember: 2, RoleAdmin: 3, RoleOwner: 4}

// ValidRole reports whether r is a known role.
func ValidRole(r string) bool { _, ok := roleRank[r]; return ok }

// RoleAtLeast reports whether role grants at least min.
func RoleAtLeast(role, min string) bool {
	return roleRank[role] >= roleRank[min] && roleRank[min] > 0
}

// Permission matrix (see docs/security.md):
//
//	viewer  read-only access to everything except secrets
//	member  + run backups, verify backups, test connections, download encrypted artifacts
//	admin   + manage databases, storage, schedules, notifications; restore; delete backups; invite members
//	owner   + manage member roles, export the recovery key, delete the organization

// Require returns an error unless the caller's membership role is at least min.
func Require(ctx context.Context, min string) (reqctx.Membership, error) {
	m, ok := reqctx.MembershipFrom(ctx)
	if !ok {
		return m, apperr.Forbidden("No organization selected.")
	}
	if !RoleAtLeast(m.Role, min) {
		return m, apperr.Forbidden("Your role (" + m.Role + ") does not allow this action. Requires " + min + " or higher.")
	}
	return m, nil
}
