package mail

import (
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// BulkRequest sends a template to the records of a collection (selected by
// ids or a filter, to the addresses at RecipientField) and/or to static
// addresses.
type BulkRequest struct {
	TemplateId       string
	Collection       string
	RecordIds        []string
	Filter           string
	RecipientField   string
	StaticRecipients string
	SendAt           time.Time
	Dedupe           bool // one email per address (the first record wins)
	DryRun           bool // count without queueing
}

// BulkResult counts the emails of a bulk send.
type BulkResult struct {
	Queued        int      `json:"queued"`        // emails queued (or that would be, in a dry run)
	Records       int      `json:"records"`       // matching records
	Unsubscribed  int      `json:"unsubscribed"`  // marketing emails not sent to opted-out addresses
	Duplicates    int      `json:"duplicates"`    // addresses skipped by dedupe
	NoAddress     int      `json:"noAddress"`     // records without a valid address
	Sample        []string `json:"sample"`        // the first recipients
	SampleRecords []string `json:"sampleRecords"` // their records (ids, when sent to records)
	Marketing     bool     `json:"marketing"`
	DryRun        bool     `json:"dryRun"`
}

const bulkSampleSize = 10

// SendBulk queues (or counts) the emails of a bulk send.
func (e *Engine) SendBulk(app core.App, req BulkRequest) (*BulkResult, error) {
	tpl, err := app.FindRecordById(templatesCollection, req.TemplateId)
	if err != nil {
		return nil, errors.New("template not found")
	}
	marketing := tpl.GetBool("marketing")
	static := ParseAddresses(req.StaticRecipients)

	var records []*core.Record
	if req.Collection != "" {
		collection, err := app.FindCollectionByNameOrId(req.Collection)
		if err != nil || isInternal(collection) {
			return nil, errors.New("unknown or internal collection")
		}
		if req.RecipientField != "" {
			if err := validateRecipientPath(app, collection, req.RecipientField); err != nil {
				return nil, errors.New("invalid recipient field: " + err.Error())
			}
		}
		if len(req.RecordIds) > 0 {
			if len(req.RecordIds) > maxManualRecords {
				return nil, errors.New("too many records, narrow the selection")
			}
			records, err = app.FindRecordsByIds(collection, req.RecordIds)
		} else {
			records, err = app.FindRecordsByFilter(collection, req.Filter, "", maxManualRecords+1, 0)
		}
		if err != nil {
			return nil, errors.New("failed to load the records: " + err.Error())
		}
		if len(records) > maxManualRecords {
			return nil, errors.New("too many records, narrow the filter")
		}
	} else if len(static) == 0 {
		return nil, errors.New("select a collection or set static recipients")
	}

	res := &BulkResult{Records: len(records), Marketing: marketing, DryRun: req.DryRun, Sample: []string{}, SampleRecords: []string{}}
	type job struct {
		record *core.Record
		to     string
	}
	var jobs []job
	seen := map[string]bool{}
	add := func(record *core.Record, to string) {
		key := strings.ToLower(to)
		if req.Dedupe && seen[key] {
			res.Duplicates++
			return
		}
		seen[key] = true
		if marketing && IsUnsubscribed(app, to) {
			res.Unsubscribed++
			return
		}
		jobs = append(jobs, job{record, to})
		if len(res.Sample) < bulkSampleSize {
			res.Sample = append(res.Sample, to)
			if record != nil {
				res.SampleRecords = append(res.SampleRecords, record.Id)
			}
		}
	}
	if req.Collection == "" {
		for _, to := range static {
			add(nil, to)
		}
	}
	for _, r := range records {
		addresses := mergeAddresses(ResolveRecipients(app, r, req.RecipientField), static)
		if len(addresses) == 0 {
			res.NoAddress++
		}
		for _, to := range addresses {
			add(r, to)
		}
	}
	res.Queued = len(jobs)
	if req.DryRun || len(jobs) == 0 {
		return res, nil
	}

	err = app.RunInTransaction(func(txApp core.App) error {
		for _, j := range jobs {
			if _, err := Enqueue(txApp, Job{Template: tpl, Record: j.record, To: j.to, SendAt: req.SendAt}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, errors.New("failed to queue the emails: " + err.Error())
	}
	e.Kick()
	return res, nil
}
