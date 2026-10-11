package dashboards

import (
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	defaultSeries = 5
	maxSeries     = 12
	otherKey      = "__other__"
)

// Series is one line/bar series of a split chart.
type Series struct {
	Key    string    `json:"key"`
	Label  string    `json:"label"`
	Points []Point   `json:"points,omitempty"` // time series
	Values []float64 `json:"values,omitempty"` // bar (aligned to the categories)
}

// Category is one x value of a split bar chart.
type Category struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// splitField returns the field of the series (single values only: a record
// belongs to one series) and its SQL key.
func (r *runner) splitField(c *core.Collection, q Query) (core.Field, string, error) {
	f, err := r.field(c, q.SplitField, groupableTypes...)
	if err != nil {
		return nil, "", err
	}
	if isMultiple(f) {
		return nil, "", fmt.Errorf("the field %q has multiple values and can't split the series", f.GetName())
	}
	key, _ := groupKeySQL(f, "s")
	return f, key, nil
}

// additive reports whether the values of several series can be summed ("Other").
func additive(fn string) bool {
	return fn == "" || fn == "count" || fn == "sum"
}

type splitRow struct {
	k, s string
	v    float64
}

func scanSplitRows(rows []dbx.NullStringMap) []splitRow {
	out := make([]splitRow, 0, len(rows))
	for _, row := range rows {
		r := splitRow{k: row["k"].String, s: row["s"].String}
		if row["v"].Valid {
			r.v, _ = strconv.ParseFloat(row["v"].String, 64)
		}
		out = append(out, r)
	}
	return out
}

// topSeries returns the keys of the biggest series and whether the others
// are merged into "Other".
func topSeries(rows []splitRow, limit int, fn string) ([]string, bool) {
	totals := map[string]float64{}
	for _, r := range rows {
		totals[r.s] += r.v
	}
	keys := make([]string, 0, len(totals))
	for k := range totals {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if totals[keys[i]] != totals[keys[j]] {
			return totals[keys[i]] > totals[keys[j]]
		}
		return keys[i] < keys[j]
	})
	if len(keys) <= limit {
		return keys, false
	}
	return keys[:limit], additive(fn)
}

func (r *runner) timeSeriesSplit(q Query) (any, error) {
	c, err := r.collection(q.Collection)
	if err != nil {
		return nil, err
	}
	f, err := r.field(c, q.DateField, core.FieldTypeDate, core.FieldTypeAutodate)
	if err != nil {
		if q.DateField == "" {
			return nil, errors.New("select a date field")
		}
		return nil, err
	}
	sf, splitKey, err := r.splitField(c, q)
	if err != nil {
		return nil, err
	}
	agg, err := r.aggregateSQL(c, q)
	if err != nil {
		return nil, err
	}
	bucket, err := bucketSQL(col(f.GetName()), q.Precision)
	if err != nil {
		return nil, err
	}
	where, params, from, to, err := r.timeWindow(f, q.Range)
	if err != nil {
		return nil, err
	}

	var raw []dbx.NullStringMap
	err = r.all(c, q.Filter, bucket+" AS [[k]], "+splitKey+" AS [[s]], "+agg+" AS [[v]]",
		where+" GROUP BY [[k]], [[s]] ORDER BY [[k]]"+fmt.Sprintf(" LIMIT %d", maxSeriesPoint*maxSeries), params, &raw)
	if err != nil {
		return nil, err
	}
	rows := scanSplitRows(raw)
	keys, withOther := topSeries(rows, clamp(q.MaxSeries, defaultSeries, maxSeries), q.Fn)
	inTop := map[string]bool{}
	for _, k := range keys {
		inTop[k] = true
	}

	values := map[string]map[string]float64{} // series -> bucket -> value
	buckets := []string{}
	seenBucket := map[string]bool{}
	for _, row := range rows {
		s := row.s
		if !inTop[s] {
			if !withOther {
				continue
			}
			s = otherKey
		}
		if values[s] == nil {
			values[s] = map[string]float64{}
		}
		values[s][row.k] += row.v
		if !seenBucket[row.k] {
			seenBucket[row.k] = true
			buckets = append(buckets, row.k)
		}
	}
	sort.Strings(buckets)

	labels := r.labelKeys(sf, keys)
	if withOther {
		keys = append(keys, otherKey)
		labels[otherKey] = "Other"
	}
	series := make([]Series, 0, len(keys))
	for _, k := range keys {
		series = append(series, Series{Key: k, Label: labels[k], Points: fillBuckets(values[k], buckets, q.Precision, from, to)})
	}
	return map[string]any{"series": series, "precision": q.Precision}, nil
}

func (r *runner) groupsSplit(q Query) (any, error) {
	c, err := r.collection(q.Collection)
	if err != nil {
		return nil, err
	}
	gf, err := r.field(c, q.GroupField, groupableTypes...)
	if err != nil {
		if q.GroupField == "" {
			return nil, errors.New("select a field to group by")
		}
		return nil, err
	}
	sf, splitKey, err := r.splitField(c, q)
	if err != nil {
		return nil, err
	}
	agg, err := r.aggregateSQL(c, q)
	if err != nil {
		return nil, err
	}
	key, from := groupKeySQL(gf, "j")

	var raw []dbx.NullStringMap
	err = r.all(c, q.Filter, key+" AS [[k]], "+splitKey+" AS [[s]], "+agg+" AS [[v]]",
		from+" GROUP BY [[k]], [[s]]"+fmt.Sprintf(" LIMIT %d", maxGroups*maxSeries*10), nil, &raw)
	if err != nil {
		return nil, err
	}
	rows := scanSplitRows(raw)

	// categories: the biggest totals (or alphabetical)
	totals := map[string]float64{}
	for _, row := range rows {
		totals[row.k] += row.v
	}
	cats := make([]string, 0, len(totals))
	for k := range totals {
		cats = append(cats, k)
	}
	sort.Slice(cats, func(i, j int) bool {
		if q.Order != "label" && totals[cats[i]] != totals[cats[j]] {
			return totals[cats[i]] > totals[cats[j]]
		}
		return cats[i] < cats[j]
	})
	if limit := clamp(q.Limit, defaultLimit, maxGroups); len(cats) > limit {
		cats = cats[:limit]
	}
	catIndex := map[string]int{}
	for i, k := range cats {
		catIndex[k] = i
	}

	keys, withOther := topSeries(rows, clamp(q.MaxSeries, defaultSeries, maxSeries), q.Fn)
	if withOther {
		keys = append(keys, otherKey)
	}
	index := map[string]int{}
	for i, k := range keys {
		index[k] = i
	}
	series := make([]Series, len(keys))
	for i := range series {
		series[i].Values = make([]float64, len(cats))
	}
	for _, row := range rows {
		ci, ok := catIndex[row.k]
		if !ok {
			continue
		}
		si, ok := index[row.s]
		if !ok {
			if !withOther {
				continue
			}
			si = index[otherKey]
		}
		series[si].Values[ci] += row.v
	}

	splitLabels := r.labelKeys(sf, keys)
	for i, k := range keys {
		series[i].Key = k
		series[i].Label = splitLabels[k]
		if k == otherKey {
			series[i].Label = "Other"
		}
	}
	catLabels := r.labelKeys(gf, cats)
	categories := make([]Category, len(cats))
	for i, k := range cats {
		categories[i] = Category{Key: k, Label: catLabels[k]}
	}
	return map[string]any{"categories": categories, "series": series}, nil
}
