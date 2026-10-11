package dashboards

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/search"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/Namarusa/PocketbasePlus/internal/internalcols"
	"github.com/Namarusa/PocketbasePlus/internal/labels"
	"github.com/Namarusa/PocketbasePlus/internal/rbac"
)

const (
	queryTimeout   = 10 * time.Second
	maxGroups      = 100  // bar/pie groups
	maxListItems   = 100  // list panel records
	maxSeriesPoint = 2000 // time series buckets
	defaultLimit   = 10
)

// Panel types.
const (
	TypeMetric     = "metric"
	TypeTimeSeries = "time_series"
	TypeBar        = "bar"
	TypePie        = "pie"
	TypeMeter      = "meter"
	TypeList       = "list"
	TypeLabel      = "label"
)

var panelTypes = map[string]bool{
	TypeMetric: true, TypeTimeSeries: true, TypeBar: true, TypePie: true,
	TypeMeter: true, TypeList: true, TypeLabel: true,
}

// Query holds the data options of a panel (the display options are only
// used by the dashboard UI).
type Query struct {
	Collection string   `json:"collection"`
	Filter     string   `json:"filter"`
	Fn         string   `json:"fn"`         // count | count_distinct | sum | avg | min | max
	Field      string   `json:"field"`      // aggregated field (not needed for count)
	GroupField string   `json:"groupField"` // bar / pie
	DateField  string   `json:"dateField"`  // time series
	Precision  string   `json:"precision"`  // hour | day | week | month | year
	Range      string   `json:"range"`      // 24h | 7d | 30d | 90d | 365d | "" (all)
	Limit      int      `json:"limit"`
	Order      string   `json:"order"`      // bar / pie: value (default) | label
	Sort       string   `json:"sort"`       // list: e.g. "-created"
	Fields     []string `json:"fields"`     // list
	SplitField string   `json:"splitField"` // time series / bar: one series per value
	MaxSeries  int      `json:"maxSeries"`
}

// Group is one bar/pie slice.
type Group struct {
	Key   any     `json:"key"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

// Point is one time series bucket.
type Point struct {
	X string  `json:"x"`
	Y float64 `json:"y"`
}

// errNoAccess is returned when the viewer can't list the panel collection.
var errNoAccess = errors.New("you don't have access to this data")

// runner executes panel queries for a viewer.
type runner struct {
	app  core.App
	auth *core.Record // superuser or role user
	ctx  context.Context
	vars map[string]resolvedVar // dashboard variables
	// params are bound in the filter as {:name} (the placeholders of the
	// pages: {{record.x}}, {{query.x}}...)
	params dbx.Params
}

func (r *runner) superuser() bool {
	return r.auth != nil && r.auth.IsSuperuser()
}

// Run returns the data of a panel.
func (r *runner) Run(panelType string, q Query) (any, error) {
	switch panelType {
	case TypeLabel:
		return nil, nil
	case TypeMetric, TypeMeter:
		v, err := r.aggregate(q)
		if err != nil {
			return nil, err
		}
		return map[string]any{"value": v}, nil
	case TypeTimeSeries:
		if q.SplitField != "" {
			return r.timeSeriesSplit(q)
		}
		points, err := r.timeSeries(q)
		if err != nil {
			return nil, err
		}
		return map[string]any{"points": points, "precision": q.Precision}, nil
	case TypeBar, TypePie:
		if q.SplitField != "" && panelType == TypeBar {
			return r.groupsSplit(q)
		}
		groups, err := r.groups(q)
		if err != nil {
			return nil, err
		}
		return map[string]any{"groups": groups}, nil
	case TypeList:
		return r.list(q)
	}
	return nil, fmt.Errorf("unknown panel type %q", panelType)
}

// --- helpers --------------------------------------------------------------------

func (r *runner) collection(name string) (*core.Collection, error) {
	if name == "" {
		return nil, errors.New("select a collection")
	}
	c, err := r.app.FindCachedCollectionByNameOrId(name)
	if err != nil || internalcols.Is(c) {
		return nil, fmt.Errorf("unknown collection %q", name)
	}
	if !r.superuser() && c.ListRule == nil {
		return nil, errNoAccess
	}
	return c, nil
}

// field returns a validated field of the collection (hidden fields are only
// available to superusers, like in the records API).
func (r *runner) field(c *core.Collection, name string, allowed ...string) (core.Field, error) {
	f := c.Fields.GetByName(name)
	if f == nil || f.Type() == core.FieldTypePassword || (f.GetHidden() && !r.superuser()) {
		return nil, fmt.Errorf("unknown field %q", name)
	}
	// the field permissions of the viewer's roles, like the records API
	if !r.superuser() {
		if readable := rbac.ReadableFields(r.app, c, r.auth); readable != nil && !readable[name] {
			return nil, fmt.Errorf("unknown field %q", name)
		}
	}
	if len(allowed) > 0 && !contains(allowed, f.Type()) {
		return nil, fmt.Errorf("the field %q (%s) can't be used here", name, f.Type())
	}
	return f, nil
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

func isMultiple(f core.Field) bool {
	switch v := f.(type) {
	case *core.SelectField:
		return v.IsMultiple()
	case *core.RelationField:
		return v.IsMultiple()
	}
	return false
}

// col quotes a validated field of the "t" source table.
func col(name string) string {
	return "[[t." + name + "]]"
}

// source returns the SQL of the records the viewer can see (list rule) that
// match the panel filter, as a derived table body, plus its params.
func (r *runner) source(c *core.Collection, filter string) (string, dbx.Params, error) {
	query := r.app.RecordQuery(c)
	info := &core.RequestInfo{Auth: r.auth, Context: core.RequestInfoContextDefault, Method: "GET"}
	resolver := core.NewRecordFieldResolver(r.app, c, info, true)

	if !r.superuser() && c.ListRule != nil && *c.ListRule != "" {
		expr, err := search.FilterData(*c.ListRule).BuildExpr(resolver)
		if err != nil {
			return "", nil, err
		}
		query.AndWhere(expr)
	}

	// panel filters follow the records API: hidden fields only for superusers
	resolver.SetAllowHiddenFields(r.superuser())
	if strings.TrimSpace(filter) != "" {
		bound, params, err := bindVariables(filter, r.vars)
		if err != nil {
			return "", nil, err
		}
		for k, v := range r.params {
			params[k] = v
		}
		expr, err := search.FilterData(bound).BuildExpr(resolver, params)
		if err != nil {
			return "", nil, fmt.Errorf("invalid filter: %w", err)
		}
		query.AndWhere(expr)
	}
	if err := resolver.UpdateQuery(query); err != nil {
		return "", nil, err
	}

	built := query.Build()
	return built.SQL(), built.Params(), nil
}

// all runs a SELECT over the source records and scans the rows.
func (r *runner) all(c *core.Collection, filter, selectSQL, rest string, extraParams dbx.Params, dest any) error {
	src, params, err := r.source(c, filter)
	if err != nil {
		return err
	}
	for k, v := range extraParams {
		params[k] = v
	}
	ctx, cancel := context.WithTimeout(r.ctx, queryTimeout)
	defer cancel()
	sql := "SELECT " + selectSQL + " FROM (" + src + ") [[t]] " + rest
	return r.app.ConcurrentDB().NewQuery(sql).Bind(params).WithContext(ctx).All(dest)
}

// aggregateSQL returns the aggregate expression of the query.
func (r *runner) aggregateSQL(c *core.Collection, q Query) (string, error) {
	switch q.Fn {
	case "", "count":
		return "COUNT(*)", nil
	case "count_distinct":
		f, err := r.field(c, q.Field)
		if err != nil {
			return "", err
		}
		return "COUNT(DISTINCT " + col(f.GetName()) + ")", nil
	case "sum", "avg":
		f, err := r.field(c, q.Field, core.FieldTypeNumber)
		if err != nil {
			return "", err
		}
		if q.Fn == "sum" {
			return "COALESCE(SUM(" + col(f.GetName()) + "), 0)", nil
		}
		return "AVG(" + col(f.GetName()) + ")", nil
	case "min", "max":
		f, err := r.field(c, q.Field, core.FieldTypeNumber, core.FieldTypeDate, core.FieldTypeAutodate)
		if err != nil {
			return "", err
		}
		name := col(f.GetName())
		if f.Type() != core.FieldTypeNumber {
			// empty dates are stored as "" and would always be the min
			name = "NULLIF(" + name + ", '')"
		}
		return strings.ToUpper(q.Fn) + "(" + name + ")", nil
	}
	return "", fmt.Errorf("unknown function %q", q.Fn)
}

// --- metric / meter -------------------------------------------------------------

func (r *runner) aggregate(q Query) (any, error) {
	c, err := r.collection(q.Collection)
	if err != nil {
		return nil, err
	}
	agg, err := r.aggregateSQL(c, q)
	if err != nil {
		return nil, err
	}
	var rows []dbx.NullStringMap
	if err := r.all(c, q.Filter, agg+" AS [[v]]", "", nil, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 || !rows[0]["v"].Valid {
		return nil, nil
	}
	return scalar(rows[0]["v"].String), nil
}

// scalar returns numbers as float64 and anything else (dates) as string.
func scalar(s string) any {
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

// --- bar / pie ------------------------------------------------------------------

var groupableTypes = []string{
	core.FieldTypeText, core.FieldTypeEmail, core.FieldTypeURL, core.FieldTypeSelect,
	core.FieldTypeBool, core.FieldTypeNumber, core.FieldTypeRelation,
	core.FieldTypeDate, core.FieldTypeAutodate,
}

func (r *runner) groups(q Query) ([]Group, error) {
	c, err := r.collection(q.Collection)
	if err != nil {
		return nil, err
	}
	f, err := r.field(c, q.GroupField, groupableTypes...)
	if err != nil {
		if q.GroupField == "" {
			return nil, errors.New("select a field to group by")
		}
		return nil, err
	}
	agg, err := r.aggregateSQL(c, q)
	if err != nil {
		return nil, err
	}

	key, from := groupKeySQL(f, "j")

	order := "[[v]] DESC, [[k]]"
	if q.Order == "label" {
		order = "[[k]]"
	}
	limit := clamp(q.Limit, defaultLimit, maxGroups)

	// the FROM clause is added by all(): join the json_each table right after it
	var rows []dbx.NullStringMap
	err = r.all(c, q.Filter, key+" AS [[k]], "+agg+" AS [[v]]",
		from+" GROUP BY [[k]] ORDER BY "+order+fmt.Sprintf(" LIMIT %d", limit), nil, &rows)
	if err != nil {
		return nil, err
	}

	groups := make([]Group, 0, len(rows))
	for _, row := range rows {
		g := Group{Key: row["k"].String}
		if !row["k"].Valid {
			g.Key = nil
		}
		if row["v"].Valid {
			g.Value, _ = strconv.ParseFloat(row["v"].String, 64)
		}
		groups = append(groups, g)
	}
	r.labelGroups(f, groups)
	return groups, nil
}

// groupKeySQL returns the SQL key of a group field and the extra FROM
// clause: multiple select/relation values (JSON arrays) are expanded with
// json_each (one group per value), dates are grouped by day.
func groupKeySQL(f core.Field, alias string) (string, string) {
	key := col(f.GetName())
	switch {
	case isMultiple(f):
		from := ", json_each(CASE WHEN json_valid(" + key + ") AND json_type(" + key + ") = 'array' THEN " + key + " ELSE json_array(" + key + ") END) [[" + alias + "]]"
		return "[[" + alias + ".value]]", from
	case f.Type() == core.FieldTypeDate || f.Type() == core.FieldTypeAutodate:
		return "strftime('%Y-%m-%d', " + key + ")", ""
	}
	return key, ""
}

// labelKeys returns readable labels of group keys (related record names, yes/no...).
func (r *runner) labelKeys(f core.Field, keys []string) map[string]string {
	var related map[string]string
	if rel, ok := f.(*core.RelationField); ok {
		ids := make([]string, 0, len(keys))
		for _, k := range keys {
			if k != "" {
				ids = append(ids, k)
			}
		}
		related = r.relationLabels(rel, ids)
	}
	labels := make(map[string]string, len(keys))
	for _, key := range keys {
		switch {
		case key == "":
			labels[key] = "(empty)"
		case related != nil && related[key] != "":
			labels[key] = related[key]
		case f.Type() == core.FieldTypeBool && key == "1":
			labels[key] = "Yes"
		case f.Type() == core.FieldTypeBool && key == "0":
			labels[key] = "No"
		default:
			labels[key] = key
		}
	}
	return labels
}

// labelGroups sets readable labels (related record names, yes/no...).
func (r *runner) labelGroups(f core.Field, groups []Group) {
	keys := make([]string, 0, len(groups))
	for _, g := range groups {
		k, _ := g.Key.(string)
		keys = append(keys, k)
	}
	labels := r.labelKeys(f, keys)
	for i := range groups {
		k, _ := groups[i].Key.(string)
		groups[i].Label = labels[k]
	}
}

// relationLabels returns readable names of related records (their
// presentable fields, or a usual name field), keyed by id.
func (r *runner) relationLabels(f *core.RelationField, ids []string) map[string]string {
	result := map[string]string{}
	if len(ids) == 0 {
		return result
	}
	relCol, err := r.app.FindCachedCollectionByNameOrId(f.CollectionId)
	if err != nil {
		return result
	}
	names := labels.Fields(relCol, r.superuser())
	if len(names) == 0 {
		return result
	}
	records, err := r.app.FindRecordsByIds(relCol, ids)
	if err != nil {
		return result
	}
	for _, rec := range records {
		result[rec.Id] = labels.Of(rec, names)
	}
	return result
}

// --- time series ----------------------------------------------------------------

var ranges = map[string]time.Duration{
	"24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour,
	"90d": 90 * 24 * time.Hour, "365d": 365 * 24 * time.Hour,
}

// bucketSQL returns the SQLite expression of a date bucket (UTC).
func bucketSQL(field, precision string) (string, error) {
	switch precision {
	case "hour":
		return "strftime('%Y-%m-%d %H:00', " + field + ")", nil
	case "", "day":
		return "strftime('%Y-%m-%d', " + field + ")", nil
	case "week": // weeks start on Monday
		return "date(" + field + ", '-6 days', 'weekday 1')", nil
	case "month":
		return "strftime('%Y-%m', " + field + ")", nil
	case "year":
		return "strftime('%Y', " + field + ")", nil
	}
	return "", fmt.Errorf("unknown precision %q", precision)
}

func (r *runner) timeSeries(q Query) ([]Point, error) {
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

	var rows []dbx.NullStringMap
	err = r.all(c, q.Filter, bucket+" AS [[k]], "+agg+" AS [[v]]",
		where+" GROUP BY [[k]] ORDER BY [[k]]"+fmt.Sprintf(" LIMIT %d", maxSeriesPoint), params, &rows)
	if err != nil {
		return nil, err
	}

	values := map[string]float64{}
	keys := []string{}
	for _, row := range rows {
		if !row["k"].Valid {
			continue
		}
		var v float64
		if row["v"].Valid {
			v, _ = strconv.ParseFloat(row["v"].String, 64)
		}
		values[row["k"].String] = v
		keys = append(keys, row["k"].String)
	}
	return fillBuckets(values, keys, q.Precision, from, to), nil
}

// timeWindow returns the WHERE clause of a time series range: a preset
// ("30d"...), a dashboard date range variable ("$period") or all time.
// from is zero when the series starts at its first value.
func (r *runner) timeWindow(f core.Field, rangeOpt string) (string, dbx.Params, time.Time, time.Time, error) {
	field := col(f.GetName())
	where := "WHERE " + field + " != ''"
	params := dbx.Params{}
	now := time.Now().UTC()
	var from time.Time
	to := now
	switch {
	case strings.HasPrefix(rangeOpt, "$"):
		v, ok := r.vars[strings.TrimPrefix(rangeOpt, "$")]
		if !ok || v.Type != "date_range" {
			return "", nil, from, to, fmt.Errorf("unknown dashboard date range %s", rangeOpt)
		}
		where += " AND " + field + " >= {:__from} AND " + field + " < {:__to}"
		params["__from"], params["__to"] = v.From, v.To
		if t, err := time.Parse(dtLayout, v.From); err == nil && t.Year() > 1 {
			from = t
		}
		if t, err := time.Parse(dtLayout, v.To); err == nil && t.Before(now) {
			to = t.Add(-time.Millisecond)
		}
	case rangeOpt != "":
		d, ok := ranges[rangeOpt]
		if !ok {
			return "", nil, from, to, fmt.Errorf("unknown range %q", rangeOpt)
		}
		from = now.Add(-d)
		where += " AND " + field + " >= {:__from}"
		params["__from"] = types.NowDateTime().Add(-d).String()
	}
	return where, params, from, to, nil
}

// fillBuckets returns one point per bucket between the range start (or the
// first value) and now, so that charts show the empty periods.
func fillBuckets(values map[string]float64, keys []string, precision string, from, to time.Time) []Point {
	format, step := bucketFormat(precision)
	start := from
	if start.IsZero() {
		if len(keys) == 0 {
			return []Point{}
		}
		parsed, err := time.Parse(format, keys[0])
		if err != nil {
			return pointsOf(values, keys)
		}
		start = parsed
	}
	start = truncate(start, precision)

	points := []Point{}
	for t := start; !t.After(to) && len(points) < maxSeriesPoint; t = step(t) {
		key := t.Format(format)
		points = append(points, Point{X: key, Y: values[key]})
	}
	// values outside of the generated buckets (shouldn't happen) are kept
	seen := map[string]bool{}
	for _, p := range points {
		seen[p.X] = true
	}
	for _, k := range keys {
		if !seen[k] {
			points = append(points, Point{X: k, Y: values[k]})
		}
	}
	sort.Slice(points, func(i, j int) bool { return points[i].X < points[j].X })
	return points
}

func pointsOf(values map[string]float64, keys []string) []Point {
	points := make([]Point, 0, len(keys))
	for _, k := range keys {
		points = append(points, Point{X: k, Y: values[k]})
	}
	return points
}

func bucketFormat(precision string) (string, func(time.Time) time.Time) {
	switch precision {
	case "hour":
		return "2006-01-02 15:00", func(t time.Time) time.Time { return t.Add(time.Hour) }
	case "week":
		return "2006-01-02", func(t time.Time) time.Time { return t.AddDate(0, 0, 7) }
	case "month":
		return "2006-01", func(t time.Time) time.Time { return t.AddDate(0, 1, 0) }
	case "year":
		return "2006", func(t time.Time) time.Time { return t.AddDate(1, 0, 0) }
	}
	return "2006-01-02", func(t time.Time) time.Time { return t.AddDate(0, 0, 1) }
}

func truncate(t time.Time, precision string) time.Time {
	t = t.UTC()
	switch precision {
	case "hour":
		return t.Truncate(time.Hour)
	case "week":
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		offset := (int(day.Weekday()) + 6) % 7 // days since Monday
		return day.AddDate(0, 0, -offset)
	case "month":
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	case "year":
		return time.Date(t.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// --- list -----------------------------------------------------------------------

type listField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

func (r *runner) list(q Query) (any, error) {
	c, err := r.collection(q.Collection)
	if err != nil {
		return nil, err
	}

	names := q.Fields
	if len(names) == 0 {
		names = defaultListFields(c, r.superuser())
		if readable := rbac.ReadableFields(r.app, c, r.auth); readable != nil && !r.superuser() {
			names = slices.DeleteFunc(names, func(n string) bool { return !readable[n] })
		}
	}
	fields := make([]core.Field, 0, len(names))
	info := make([]listField, 0, len(names))
	for _, n := range names {
		f, err := r.field(c, n)
		if err != nil {
			return nil, err
		}
		fields = append(fields, f)
		info = append(info, listField{Name: f.GetName(), Type: f.Type()})
	}

	order := "[[t.rowid]] DESC"
	if s := strings.TrimSpace(q.Sort); s != "" {
		desc := strings.HasPrefix(s, "-")
		f, err := r.field(c, strings.TrimLeft(s, "-+"))
		if err != nil {
			return nil, err
		}
		order = col(f.GetName())
		if desc {
			order += " DESC"
		}
	}
	limit := clamp(q.Limit, defaultLimit, maxListItems)

	var ids []dbx.NullStringMap
	if err := r.all(c, q.Filter, "[[t.id]] AS [[id]]", fmt.Sprintf("ORDER BY %s LIMIT %d", order, limit), nil, &ids); err != nil {
		return nil, err
	}
	idList := make([]string, 0, len(ids))
	for _, row := range ids {
		idList = append(idList, row["id"].String)
	}
	records, err := r.app.FindRecordsByIds(c, idList)
	if err != nil {
		return nil, err
	}
	byId := map[string]*core.Record{}
	for _, rec := range records {
		byId[rec.Id] = rec
	}

	// readable relation values
	relLabels := map[string]map[string]string{}
	for _, f := range fields {
		if rel, ok := f.(*core.RelationField); ok {
			var relIds []string
			for _, rec := range records {
				relIds = append(relIds, rec.GetStringSlice(f.GetName())...)
			}
			relLabels[f.GetName()] = r.relationLabels(rel, relIds)
		}
	}

	items := make([]map[string]any, 0, len(idList))
	for _, id := range idList { // keep the query order
		rec := byId[id]
		if rec == nil {
			continue
		}
		item := map[string]any{"id": rec.Id}
		for _, f := range fields {
			name := f.GetName()
			if labels, ok := relLabels[name]; ok {
				vals := []string{}
				for _, relId := range rec.GetStringSlice(name) {
					if l := labels[relId]; l != "" {
						vals = append(vals, l)
					} else {
						vals = append(vals, relId)
					}
				}
				item[name] = strings.Join(vals, ", ")
				continue
			}
			item[name] = rec.Get(name)
		}
		items = append(items, item)
	}
	return map[string]any{"fields": info, "items": items}, nil
}

// defaultListFields returns up to 3 readable fields.
func defaultListFields(c *core.Collection, superuser bool) []string {
	var names []string
	for _, f := range c.Fields {
		if len(names) == 3 {
			break
		}
		switch f.Type() {
		case core.FieldTypePassword, core.FieldTypeJSON, core.FieldTypeEditor, core.FieldTypeFile:
			continue
		}
		if f.GetName() == core.FieldNameId || (f.GetHidden() && !superuser) {
			continue
		}
		names = append(names, f.GetName())
	}
	return names
}

func clamp(v, def, max int) int {
	if v <= 0 {
		return def
	}
	if v > max {
		return max
	}
	return v
}
