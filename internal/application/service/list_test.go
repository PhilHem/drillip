package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	outport "github.com/PhilHem/drillip/internal/application/port/out"
	"github.com/PhilHem/drillip/internal/domain"
)

type listRepository struct {
	outport.Repository
	query domain.ListQuery
	page  domain.ErrorPage
	err   error
	reads int
}

func (r *listRepository) List(query domain.ListQuery) (domain.ErrorPage, error) {
	r.query = query
	r.reads++
	return r.page, r.err
}

func TestListPassesQueryAndPageThroughPorts(t *testing.T) {
	query := domain.ListQuery{
		Filter: domain.ListFilter{Level: "warning", TagKey: "host", TagVal: "web-1"},
		Search: "  needle%_", Sort: domain.SortCount, Limit: 10, Offset: 20,
	}
	want := domain.ErrorPage{
		Errors:  []domain.ErrorSummary{{Fingerprint: "abcd123456789012", Count: 42}},
		HasMore: true,
	}
	repo := &listRepository{page: want}
	got, err := New(repo, nil, nil).List(context.Background(), query)
	if err != nil || repo.reads != 1 || repo.query != query || !reflect.DeepEqual(got, want) {
		t.Fatalf("page=%+v err=%v repository=%+v", got, err, repo)
	}

	repo.err = errors.New("storage unavailable")
	if _, err := New(repo, nil, nil).List(context.Background(), query); !errors.Is(err, repo.err) {
		t.Fatalf("storage error = %v", err)
	}
}

func TestListRejectsInvalidQueriesBeforeStorage(t *testing.T) {
	for _, query := range []domain.ListQuery{
		{},
		{Limit: -1},
		{Limit: domain.MaxListLimit + 1},
		{Limit: 1, Offset: -1},
		{Limit: 1, Sort: "unknown"},
	} {
		repo := &listRepository{}
		_, err := New(repo, nil, nil).List(context.Background(), query)
		if !errors.Is(err, domain.ErrInvalidListQuery) || repo.reads != 0 {
			t.Fatalf("query=%+v err=%v reads=%d", query, err, repo.reads)
		}
	}
}

func TestListChecksContextBeforeStorage(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	for _, ctx := range []context.Context{canceled, expired} {
		repo := &listRepository{}
		_, err := New(repo, nil, nil).List(ctx, domain.ListQuery{Limit: 1})
		if !errors.Is(err, ctx.Err()) || repo.reads != 0 {
			t.Fatalf("err=%v reads=%d", err, repo.reads)
		}
	}
}
