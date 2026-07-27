// Package query is a small, self-contained SQL query/pagination helper built on
// gorm. It replaces agent-frame's dependency on igo-pkg xsql/builder: callers
// describe filters as a slice of *Query and receive a gorm-ready WHERE fragment
// plus bind values, and use Paginate/CeilPageNum for paging.
package query

import (
	"fmt"
	"math"
	"reflect"
	"strings"

	"gorm.io/gorm"
)

// Operator enumerates the supported comparison operators. Values intentionally
// match agent-frame's builder.Operator_* ordinals so JSON page requests coming
// from the same frontend deserialize identically.
type Operator int

const (
	OpEq          Operator = 0  // =
	OpNe1         Operator = 1  // !=
	OpNe2         Operator = 2  // <>
	OpIn          Operator = 3  // IN
	OpNotIn       Operator = 4  // NOT IN
	OpGt          Operator = 5  // >
	OpGte         Operator = 6  // >=
	OpLt          Operator = 7  // <
	OpLte         Operator = 8  // <=
	OpLike        Operator = 9  // LIKE (caller supplies %)
	OpNotLike     Operator = 10 // NOT LIKE
	OpBetween     Operator = 11 // BETWEEN a AND b
	OpNotBetween  Operator = 12 // NOT BETWEEN a AND b
	OpNull        Operator = 13 // IS NULL
	OpLikePercent Operator = 92 // LIKE, wraps value with %...%
)

// Query is a single filter condition.
type Query struct {
	Key      string   `json:"key"`
	Value    any      `json:"value"`
	Operator Operator `json:"operator"`
}

// PageData carries pagination request/response fields.
type PageData struct {
	PageNum     int   `json:"page_num"`
	PageSize    int   `json:"page_size"`
	TotalNumber int64 `json:"total_number"`
	TotalPage   int64 `json:"total_page"`
}

// SortData carries an order-by field and direction (asc|desc).
type SortData struct {
	Sort      string `json:"sort"`
	Direction string `json:"direction"`
}

// BuildWhere turns queries into a gorm WHERE fragment ("a = ? AND b IN ?") and
// the matching bind values. An empty slice yields an empty string and nil values.
func BuildWhere(queries []*Query) (string, []any, error) {
	if len(queries) == 0 {
		return "", nil, nil
	}

	var clauses []string
	var values []any
	for _, q := range queries {
		if q == nil {
			continue
		}
		if strings.TrimSpace(q.Key) == "" {
			return "", nil, fmt.Errorf("query: empty key")
		}
		switch q.Operator {
		case OpEq:
			clauses = append(clauses, q.Key+" = ?")
			values = append(values, q.Value)
		case OpNe1, OpNe2:
			clauses = append(clauses, q.Key+" <> ?")
			values = append(values, q.Value)
		case OpGt:
			clauses = append(clauses, q.Key+" > ?")
			values = append(values, q.Value)
		case OpGte:
			clauses = append(clauses, q.Key+" >= ?")
			values = append(values, q.Value)
		case OpLt:
			clauses = append(clauses, q.Key+" < ?")
			values = append(values, q.Value)
		case OpLte:
			clauses = append(clauses, q.Key+" <= ?")
			values = append(values, q.Value)
		case OpLike:
			clauses = append(clauses, q.Key+" LIKE ?")
			values = append(values, q.Value)
		case OpLikePercent:
			clauses = append(clauses, q.Key+" LIKE ?")
			values = append(values, "%"+fmt.Sprintf("%v", q.Value)+"%")
		case OpNotLike:
			clauses = append(clauses, q.Key+" NOT LIKE ?")
			values = append(values, q.Value)
		case OpIn:
			clauses = append(clauses, q.Key+" IN ?")
			values = append(values, q.Value)
		case OpNotIn:
			clauses = append(clauses, q.Key+" NOT IN ?")
			values = append(values, q.Value)
		case OpBetween, OpNotBetween:
			lo, hi, err := betweenPair(q.Value)
			if err != nil {
				return "", nil, err
			}
			kw := "BETWEEN"
			if q.Operator == OpNotBetween {
				kw = "NOT BETWEEN"
			}
			clauses = append(clauses, q.Key+" "+kw+" ? AND ?")
			values = append(values, lo, hi)
		case OpNull:
			clauses = append(clauses, q.Key+" IS NULL")
		default:
			return "", nil, fmt.Errorf("query: unsupported operator %d", q.Operator)
		}
	}

	return strings.Join(clauses, " AND "), values, nil
}

// betweenPair extracts the two bounds for a BETWEEN condition.
func betweenPair(v any) (any, any, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice || rv.Len() != 2 {
		return nil, nil, fmt.Errorf("query: BETWEEN value must be a 2-element slice")
	}
	return rv.Index(0).Interface(), rv.Index(1).Interface(), nil
}

// Paginate returns a gorm scope applying OFFSET/LIMIT with sane guardrails.
func Paginate(page, pageSize int) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if page <= 0 {
			page = 1
		}
		switch {
		case pageSize > 100:
			pageSize = 100
		case pageSize <= 0:
			pageSize = 10
		}
		return db.Offset((page - 1) * pageSize).Limit(pageSize)
	}
}

// CeilPageNum computes the total number of pages.
func CeilPageNum(total int64, pageSize int) int64 {
	if pageSize <= 0 {
		return 0
	}
	return int64(math.Ceil(float64(total) / float64(pageSize)))
}

// SelectFields flattens optional select-column groups into a single slice.
// With no args it returns ["*"] so gorm selects all columns.
func SelectFields(selectArgs ...[]string) []string {
	if len(selectArgs) == 0 {
		return []string{"*"}
	}
	var out []string
	for _, arg := range selectArgs {
		out = append(out, arg...)
	}
	if len(out) == 0 {
		return []string{"*"}
	}
	return out
}

// OrderBy renders "sort direction", defaulting when either part is empty.
func (s *SortData) OrderBy(defaultSort string) string {
	sort := defaultSort
	dir := "desc"
	if s != nil {
		if strings.TrimSpace(s.Sort) != "" {
			sort = s.Sort
		}
		if strings.TrimSpace(s.Direction) != "" {
			dir = s.Direction
		}
	}
	return sort + " " + dir
}
