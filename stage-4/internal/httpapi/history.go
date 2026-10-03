package httpapi

import (
	"net/http"

	"tablekeeper/internal/store"
)

type historyChangeResponse struct {
	Field string      `json:"field"`
	From  interface{} `json:"from"`
	To    interface{} `json:"to"`
}

type historyEntryResponse struct {
	Seq           int                     `json:"seq"`
	At            string                  `json:"at"`
	Event         string                  `json:"event"`
	Changes       []historyChangeResponse `json:"changes"`
	Revision      int                     `json:"revision"`
	AcceptedTerms store.AcceptedTerms     `json:"accepted_terms"`
	PlanID        string                  `json:"plan_id,omitempty"`
}

type historyResponse struct {
	Reference string                  `json:"reference"`
	Entries   []historyEntryResponse `json:"entries"`
}

func toHistoryResponse(r *store.Reservation) historyResponse {
	out := historyResponse{Reference: r.Reference}
	for _, e := range r.History {
		changes := make([]historyChangeResponse, 0, len(e.Changes))
		for _, c := range e.Changes {
			changes = append(changes, historyChangeResponse{Field: c.Field, From: c.From, To: c.To})
		}
		out.Entries = append(out.Entries, historyEntryResponse{
			Seq:           e.Seq,
			At:            formatRFC3339(e.At),
			Event:         e.Event,
			Changes:       changes,
			Revision:      e.Revision,
			AcceptedTerms: e.AcceptedTerms,
			PlanID:        e.PlanID,
		})
	}
	return out
}

// handleReservationHistory serves GET /reservations/{reference}/history. Per spec, this is a
// deliberate exception to the general "no token -> 401" rule: an unauthenticated or
// non-owning caller gets the same 404 as an unknown reference, never 401 or 403, so the
// existence of someone else's reservation is never leaked.
func (a *API) handleReservationHistory(w http.ResponseWriter, r *http.Request) {
	user, aerr := a.authenticate(r)
	if aerr != nil {
		writeError(w, errNotFound)
		return
	}
	ref := r.PathValue("reference")
	a.Store.Lock()
	defer a.Store.Unlock()

	resv, ok := a.Store.ReservationByReference(ref)
	if !ok || resv.UserID != user.ID {
		writeError(w, errNotFound)
		return
	}
	writeJSON(w, http.StatusOK, toHistoryResponse(resv))
}

type decisionResponse struct {
	Reference     string              `json:"reference"`
	Revision      int                 `json:"revision"`
	AcceptedTerms store.AcceptedTerms `json:"accepted_terms"`
}

// handleReservationDecision serves GET /reservations/{reference}/decision, with the same
// owner-only, 404-even-unauthenticated rule as history.
func (a *API) handleReservationDecision(w http.ResponseWriter, r *http.Request) {
	user, aerr := a.authenticate(r)
	if aerr != nil {
		writeError(w, errNotFound)
		return
	}
	ref := r.PathValue("reference")
	a.Store.Lock()
	defer a.Store.Unlock()

	resv, ok := a.Store.ReservationByReference(ref)
	if !ok || resv.UserID != user.ID {
		writeError(w, errNotFound)
		return
	}
	writeJSON(w, http.StatusOK, decisionResponse{
		Reference:     resv.Reference,
		Revision:      resv.Revision,
		AcceptedTerms: resv.AcceptedTerms,
	})
}
