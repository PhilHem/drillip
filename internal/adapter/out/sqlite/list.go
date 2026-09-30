package sqlite

import (
	"strings"

	"github.com/PhilHem/drillip/internal/domain"
)

// List returns a page ordered by last activity or occurrence count, with a
// fingerprint tie-breaker so unchanged data has stable page boundaries.
func (s *Store) List(q domain.ListQuery) (domain.ErrorPage, error) {
	if err := q.Validate(); err != nil {
		return domain.ErrorPage{}, err
	}

	query := `SELECT fingerprint, count, type, value, level, last_seen, first_seen, COALESCE(resolved_at, '') FROM errors`
	var args []any
	var conditions []string
	if q.Filter.Level != "" {
		conditions = append(conditions, `level = ?`)
		args = append(args, q.Filter.Level)
	}
	if q.Filter.TagKey != "" {
		conditions = append(conditions, `json_extract(tags, '$.'||?) = ?`)
		args = append(args, q.Filter.TagKey, q.Filter.TagVal)
	}
	if q.Search != "" {
		conditions = append(conditions, `(instr(lower(type), lower(?)) > 0 OR instr(lower(value), lower(?)) > 0)`)
		args = append(args, q.Search, q.Search)
	}
	if len(conditions) > 0 {
		query += ` WHERE ` + strings.Join(conditions, ` AND `)
	}
	if q.Sort == domain.SortCount {
		query += ` ORDER BY count DESC, last_seen DESC, fingerprint ASC`
	} else {
		query += ` ORDER BY last_seen DESC, fingerprint ASC`
	}
	query += ` LIMIT ? OFFSET ?`
	args = append(args, q.Limit+1, q.Offset)

	errors, err := s.queryErrorSummaries(query, args)
	if err != nil {
		return domain.ErrorPage{}, err
	}
	page := domain.ErrorPage{Errors: errors, HasMore: len(errors) > q.Limit}
	if page.HasMore {
		page.Errors = page.Errors[:q.Limit]
	}
	return page, nil
}
