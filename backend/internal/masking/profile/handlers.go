package profile

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/auth"
	"github.com/dbvault/dbvault/backend/internal/engine"
	"github.com/dbvault/dbvault/backend/internal/httpx"
	"github.com/dbvault/dbvault/backend/internal/masking"
	"github.com/dbvault/dbvault/backend/internal/reqctx"
)

// DatabaseLookup resolves a database's engine within an organization.
type DatabaseLookup func(r *http.Request, orgID, id string) (engineName string, err error)

type Handlers struct {
	Svc      *Service
	Database DatabaseLookup
}

func (h *Handlers) Routes(r chi.Router) {
	r.Get("/databases/{id}/masking", h.editor)
	r.Put("/databases/{id}/masking/profiles/{name}", h.put)
	r.Delete("/databases/{id}/masking/profiles/{name}", h.delete)
}

// ColumnView is a catalog column annotated for the editor.
type ColumnView struct {
	engine.CatalogColumn
	// Personal is true when the column looks like it holds personal data;
	// Suggested is the rule DBVault proposes for it.
	Personal  bool   `json:"personal"`
	Suggested string `json:"suggested,omitempty"`
}

type TableView struct {
	Schema          string       `json:"schema,omitempty"`
	Name            string       `json:"name"`
	Key             string       `json:"key"`
	Columns         []ColumnView `json:"columns"`
	References      []string     `json:"references,omitempty"`
	SuggestTruncate bool         `json:"suggest_truncate"`
}

// editor returns everything the profile editor needs: the schema of the
// latest restored backup, suggestions, saved profiles and rule examples.
// It never includes data from the database.
func (h *Handlers) editor(w http.ResponseWriter, r *http.Request) {
	m, _ := reqctx.MembershipFrom(r.Context())
	id := chi.URLParam(r, "id")
	if !httpx.IsUUID(id) {
		httpx.Error(w, r, apperr.NotFound("Database"))
		return
	}
	engineName, err := h.Database(r, m.OrgID, id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	supported, reason := h.Svc.Supported(engineName)
	profiles, err := h.Svc.List(r.Context(), m.OrgID, id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	cat, src, err := h.Svc.LatestCatalog(r.Context(), m.OrgID, id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	out := map[string]any{
		"supported": supported, "unsupported_reason": reason, "profiles": profiles,
		"schema": nil, "schema_source": src, "suggested": nil, "problems": map[string][]string{},
		"examples": masking.Examples("example-key"),
	}
	if cat != nil {
		var tables []TableView
		for _, t := range cat.Tables {
			tv := TableView{Schema: t.Schema, Name: t.Name, Key: tableKey(t), References: t.References, SuggestTruncate: masking.SuggestTable(t)}
			for _, c := range t.Columns {
				s := masking.SuggestColumn(c)
				tv.Columns = append(tv.Columns, ColumnView{CatalogColumn: c, Personal: s != "", Suggested: s})
			}
			tables = append(tables, tv)
		}
		out["schema"] = map[string]any{"tables": tables}
		out["suggested"] = masking.Suggest(*cat)
		problems := map[string][]string{}
		for _, p := range profiles {
			problems[p.Name] = Problems(p.Rules, cat)
		}
		out["problems"] = problems
	}
	httpx.JSON(w, http.StatusOK, out)
}

func tableKey(t engine.CatalogTable) string {
	if t.Schema == "" || t.Schema == "public" {
		return t.Name
	}
	return t.Schema + "." + t.Name
}

func (h *Handlers) put(w http.ResponseWriter, r *http.Request) {
	m, err := auth.Require(r.Context(), auth.RoleAdmin)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	p, _ := reqctx.PrincipalFrom(r.Context())
	id := chi.URLParam(r, "id")
	if !httpx.IsUUID(id) {
		httpx.Error(w, r, apperr.NotFound("Database"))
		return
	}
	engineName, err := h.Database(r, m.OrgID, id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if ok, reason := h.Svc.Supported(engineName); !ok {
		httpx.Error(w, r, apperr.Unprocessable("masking_unsupported", reason))
		return
	}
	var req struct {
		Rules json.RawMessage `json:"rules"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	// Parsed separately so rule errors ("table x: use truncate or…") reach the user.
	var rules masking.Rules
	if len(req.Rules) == 0 {
		rules.Tables = map[string]masking.TableRule{}
	} else if err := json.Unmarshal(req.Rules, &rules); err != nil {
		httpx.Error(w, r, apperr.Validation(map[string]string{"rules": err.Error()}))
		return
	}
	prof, err := h.Svc.Put(r.Context(), m.OrgID, p.UserID, id, chi.URLParam(r, "name"), rules)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	cat, _, err := h.Svc.LatestCatalog(r.Context(), m.OrgID, id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	// Saving never fails on schema problems (the schema may be about to
	// change); they're returned so the editor can show them.
	httpx.JSON(w, http.StatusOK, map[string]any{"profile": prof, "problems": Problems(prof.Rules, cat)})
}

func (h *Handlers) delete(w http.ResponseWriter, r *http.Request) {
	m, err := auth.Require(r.Context(), auth.RoleAdmin)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	id := chi.URLParam(r, "id")
	if !httpx.IsUUID(id) {
		httpx.Error(w, r, apperr.NotFound("Database"))
		return
	}
	if err := h.Svc.Delete(r.Context(), m.OrgID, id, chi.URLParam(r, "name")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}
