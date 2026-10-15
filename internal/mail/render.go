// Package mail implements email templates, a send queue and the automations
// (record events, schedules, delays) that feed it.
package mail

import (
	"bytes"
	htmltemplate "html/template"
	"strings"
	texttemplate "text/template"

	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/formula"
)

// Rendered is a ready to send email content.
type Rendered struct {
	Subject string `json:"subject"`
	HTML    string `json:"html"`
	Text    string `json:"text"`
}

// funcs are the functions of the templates (see internal/formula).
var funcs = map[string]any(formula.Funcs())

// TemplateFuncs returns the helper functions available to templates.
func TemplateFuncs() map[string]any {
	return formula.Funcs()
}

// Validate checks that the template parts parse.
func Validate(subject, html, text string) error {
	_, err := Render(subject, html, text, nil, true)
	return err
}

// Render executes the subject and text parts with text/template and the html
// part with html/template (which escapes all interpolated record values).
//
// When parseOnly is true the templates are only parsed, not executed.
func Render(subject, html, text string, data map[string]any, parseOnly ...bool) (Rendered, error) {
	only := len(parseOnly) > 0 && parseOnly[0]
	var out Rendered
	var err error

	if out.Subject, err = execText("subject", subject, data, only); err != nil {
		return out, err
	}
	// subjects are single line headers
	out.Subject = strings.Join(strings.Fields(out.Subject), " ")

	if out.Text, err = execText("text", text, data, only); err != nil {
		return out, err
	}

	t, err := htmltemplate.New("html").Funcs(funcs).Option("missingkey=zero").Parse(html)
	if err != nil {
		return out, secretError(err)
	}
	if !only {
		var buf bytes.Buffer
		if err := t.Execute(&buf, data); err != nil {
			return out, err
		}
		out.HTML = buf.String()
	}

	return out, nil
}

func execText(name, src string, data map[string]any, parseOnly bool) (string, error) {
	t, err := texttemplate.New(name).Funcs(funcs).Option("missingkey=zero").Parse(src)
	if err != nil || parseOnly {
		return "", secretError(err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return strings.ReplaceAll(buf.String(), "<no value>", ""), nil
}

// secretError explains a secret ({{secret.NAME}}) used in an email: the
// values of the secrets can't be sent.
func secretError(err error) error {
	if err != nil && strings.Contains(err.Error(), `function "secret" not defined`) {
		return formula.ErrSecretHere
	}
	return err
}

// RecordData exports the non hidden fields of a record (plus its email for
// auth records) for use as template data.
func RecordData(record *core.Record) map[string]any {
	if record == nil {
		return map[string]any{}
	}
	data := map[string]any{
		"collectionId":   record.Collection().Id,
		"collectionName": record.Collection().Name,
	}
	for _, f := range record.Collection().Fields {
		name := f.GetName()
		if f.Type() == core.FieldTypePassword || (f.GetHidden() && name != core.FieldNameEmail) {
			continue
		}
		data[name] = record.Get(name)
	}
	return data
}

// TemplateData builds the full data map passed to templates.
func TemplateData(app core.App, record *core.Record, records []*core.Record, recipient string) map[string]any {
	list := make([]map[string]any, 0, len(records))
	for _, r := range records {
		list = append(list, RecordData(r))
	}
	return map[string]any{
		"record":    RecordData(record),
		"records":   list,
		"recipient": map[string]any{"email": recipient},
		"app": map[string]any{
			"name": app.Settings().Meta.AppName,
			"url":  app.Settings().Meta.AppURL,
		},
		"unsubscribe_url": UnsubscribeURL(app, recipient),
	}
}
