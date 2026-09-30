package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/PhilHem/drillip/internal/adapter/httpwire"
	"github.com/PhilHem/drillip/internal/domain"
)

func (h *Handler) HandleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	params := r.URL.Query()
	query := domain.ListQuery{
		Filter: domain.ListFilter{Level: params.Get("level")},
		Search: params.Get("search"),
		Sort:   params.Get("sort"),
		Limit:  domain.DefaultListLimit,
	}
	if params.Has("tag") {
		key, value, ok := domain.ParseTag(params.Get("tag"))
		if !ok {
			writeError(w, http.StatusBadRequest, "tag must be key=value")
			return
		}
		query.Filter.TagKey, query.Filter.TagVal = key, value
	}
	for name, dest := range map[string]*int{"limit": &query.Limit, "offset": &query.Offset} {
		if params.Has(name) {
			n, err := strconv.Atoi(params.Get(name))
			if err != nil {
				writeError(w, http.StatusBadRequest, name+" must be an integer")
				return
			}
			*dest = n
		}
	}
	if err := query.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	page, err := h.Errors.List(r.Context(), query)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidListQuery) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		slog.Error("HandleList", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	result := httpwire.ErrorPage{Errors: make([]httpwire.Error, len(page.Errors)), HasMore: page.HasMore}
	for i, summary := range page.Errors {
		result.Errors[i] = httpwire.FromSummary(summary)
	}
	writeJSON(w, result)
}
