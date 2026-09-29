package service

import (
	"errors"
	"testing"
	"time"

	outport "github.com/PhilHem/drillip/internal/application/port/out"
	"github.com/PhilHem/drillip/internal/domain"
)

type queryRepository struct {
	outport.Repository
	reference          string
	lookupErr, readErr error
	reads              int
}

func (r *queryRepository) FindByPrefix(ref string) (string, error) {
	r.reference = ref
	return "abcd123456789012", r.lookupErr
}
func (r *queryRepository) read(fp string) error {
	if fp != "abcd123456789012" {
		panic("read without canonical fingerprint")
	}
	r.reads++
	return r.readErr
}
func (r *queryRepository) GetDetail(fp string) (*domain.ErrorDetail, error) {
	return &domain.ErrorDetail{Fingerprint: fp}, r.read(fp)
}
func (r *queryRepository) GetTrend(fp string, _ time.Time) ([]domain.TrendBucket, error) {
	return nil, r.read(fp)
}
func (r *queryRepository) GetReleases(fp string) ([]domain.ReleaseStats, error) {
	return nil, r.read(fp)
}

func TestInvestigationOperationsOwnReferenceResolution(t *testing.T) {
	operations := map[string]func(*Errors, string) (string, error){
		"detail": func(s *Errors, ref string) (string, error) {
			result, err := s.GetDetail(ref)
			if err != nil {
				return "", err
			}
			return result.Fingerprint, nil
		},
		"trend": func(s *Errors, ref string) (string, error) {
			result, err := s.GetTrend(ref, time.Now())
			return result.Fingerprint, err
		},
		"releases": func(s *Errors, ref string) (string, error) {
			result, err := s.GetReleases(ref)
			return result.Fingerprint, err
		},
	}
	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			for _, ref := range []string{"abcd", "abcd123456789012"} {
				repo := &queryRepository{}
				fp, err := operation(New(repo, nil, nil), ref)
				if err != nil || fp != "abcd123456789012" || repo.reference != ref || repo.reads != 1 {
					t.Fatalf("result=%q err=%v repository=%+v", fp, err, repo)
				}
			}
			for _, lookupErr := range []error{domain.ErrInvalidFingerprint, domain.ErrErrorNotFound, domain.ErrAmbiguousFingerprint, errors.New("database unavailable")} {
				repo := &queryRepository{lookupErr: lookupErr}
				_, err := operation(New(repo, nil, nil), "abcd")
				if !errors.Is(err, lookupErr) || repo.reads != 0 {
					t.Fatalf("err=%v reads=%d", err, repo.reads)
				}
			}
			readErr := errors.New("read failed")
			repo := &queryRepository{readErr: readErr}
			if _, err := operation(New(repo, nil, nil), "abcd"); !errors.Is(err, readErr) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
