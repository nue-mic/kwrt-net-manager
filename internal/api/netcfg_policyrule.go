package api

import (
	"net/http"

	"github.com/nue-mic/kwrt-net-manager/internal/netcfg"
)

// ListPolicyRules GET /api/v1/policy-rules
func (h *NetcfgHandler) ListPolicyRules(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListPolicyRules()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	if items == nil {
		items = []netcfg.PolicyRule{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// CreatePolicyRule POST /api/v1/policy-rules
func (h *NetcfgHandler) CreatePolicyRule(w http.ResponseWriter, r *http.Request) {
	var in netcfg.PolicyRule
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.CreatePolicyRule(in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, out)
}

// UpdatePolicyRule PUT /api/v1/policy-rules/{id}
func (h *NetcfgHandler) UpdatePolicyRule(w http.ResponseWriter, r *http.Request) {
	var in netcfg.PolicyRule
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.UpdatePolicyRule(pathID(r), in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

// DeletePolicyRule DELETE /api/v1/policy-rules/{id}
func (h *NetcfgHandler) DeletePolicyRule(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeletePolicyRule(pathID(r)); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"deleted": pathID(r)})
}

// TogglePolicyRule POST /api/v1/policy-rules/{id}/toggle {enabled}
func (h *NetcfgHandler) TogglePolicyRule(w http.ResponseWriter, r *http.Request) {
	var body toggleReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.svc.SetPolicyRuleEnabled(pathID(r), body.Enabled); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// BatchPolicyRules POST /api/v1/policy-rules/batch {action, ids}
func (h *NetcfgHandler) BatchPolicyRules(w http.ResponseWriter, r *http.Request) {
	var body batchReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.svc.BatchPolicyRules(body.Action, body.IDs); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
