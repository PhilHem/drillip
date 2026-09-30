package httpclient

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/PhilHem/drillip/internal/adapter/httpwire"
	"github.com/PhilHem/drillip/internal/domain"
)

func (c *Client) List(ctx context.Context, query domain.ListQuery) (domain.ErrorPage, error) {
	if err := query.Validate(); err != nil {
		return domain.ErrorPage{}, err
	}
	q := filterQuery(query.Filter)
	q.Set("search", query.Search)
	q.Set("sort", query.Sort)
	q.Set("limit", strconv.Itoa(query.Limit))
	q.Set("offset", strconv.Itoa(query.Offset))
	var data httpwire.ErrorPage
	if err := c.request(ctx, http.MethodGet, "/api/0/list/", q, &data, "errors", "has_more"); err != nil {
		return domain.ErrorPage{}, err
	}
	if len(data.Errors) > query.Limit || (data.HasMore && len(data.Errors) != query.Limit) {
		return domain.ErrorPage{}, fmt.Errorf("invalid list response: inconsistent page size")
	}
	page := domain.ErrorPage{Errors: make([]domain.ErrorSummary, len(data.Errors)), HasMore: data.HasMore}
	for i, d := range data.Errors {
		if err := canonical(d.Fingerprint); err != nil {
			return domain.ErrorPage{}, err
		}
		page.Errors[i] = domain.ErrorSummary{Fingerprint: d.Fingerprint, Count: d.Count, Level: d.Level, Type: d.Type, Value: d.Value, FirstSeen: d.FirstSeen, LastSeen: d.LastSeen, ResolvedAt: d.ResolvedAt, State: d.State}
	}
	return page, nil
}
