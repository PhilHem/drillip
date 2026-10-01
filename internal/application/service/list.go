package service

import (
	"context"

	"github.com/PhilHem/drillip/internal/domain"
)

// List loads a validated page of error groups.
func (s *Errors) List(ctx context.Context, query domain.ListQuery) (domain.ErrorPage, error) {
	if err := ctx.Err(); err != nil {
		return domain.ErrorPage{}, err
	}
	if err := query.Validate(); err != nil {
		return domain.ErrorPage{}, err
	}
	return s.store.List(query)
}
