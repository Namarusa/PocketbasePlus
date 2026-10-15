package mail

import (
	"net/http"
	"net/mail"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/mailer"
)

const maxManualRecords = 5000

func registerRoutes(e *Engine, se *core.ServeEvent) {
	g := se.Router.Group("/api/emails")
	g.Bind(apis.RequireSuperuserAuth())
	g.POST("/preview", e.preview)
	g.POST("/test", e.sendTest)
	g.POST("/send", e.sendManual)
	g.GET("/accounts", listAccountEmails)
	g.POST("/accounts/customize", e.customizeAccount)
	g.POST("/automations/{id}/run", e.runAutomation)
	g.POST("/messages/{id}/retry", e.retryMessage)
	g.POST("/messages/{id}/cancel", e.cancelMessage)

	// public: signed links of the marketing emails
	se.Router.GET("/email/unsubscribe", unsubscribeRoute)
	se.Router.POST("/email/unsubscribe", unsubscribeRoute)
}

// renderBody is shared by the preview and test endpoints. Subject, Html and
// Text override the stored template so that unsaved editor changes can be previewed.
type renderBody struct {
	TemplateId string         `json:"templateId"`
	Subject    *string        `json:"subject"`
	HTML       *string        `json:"html"`
	Text       *string        `json:"text"`
	Collection string         `json:"collection"`
	RecordId   string         `json:"recordId"`
	Data       map[string]any `json:"data"`    // sample record data
	Account    string         `json:"account"` // the account email the template replaces (sample link, code...)
}

// renderFromBody returns the rendered content, a template error (expected
// while editing) or a request error.
func (e *Engine) renderFromBody(re *core.RequestEvent, body renderBody, to string) (Rendered, error, error) {
	var subject, html, text string
	if body.TemplateId != "" {
		tpl, err := re.App.FindRecordById(templatesCollection, body.TemplateId)
		if err != nil {
			return Rendered{}, nil, re.NotFoundError("Template not found.", err)
		}
		subject, html, text = tpl.GetString("subject"), tpl.GetString("html"), tpl.GetString("text")
		if body.Account == "" {
			body.Account = tpl.GetString("account_email")
		}
	}
	if body.Subject != nil {
		subject = *body.Subject
	}
	if body.HTML != nil {
		html = *body.HTML
	}
	if body.Text != nil {
		text = *body.Text
	}

	var record *core.Record
	if body.Collection != "" {
		var err error
		if body.RecordId != "" {
			record, err = re.App.FindRecordById(body.Collection, body.RecordId)
		} else {
			// latest record as a realistic sample
			record, err = re.App.FindFirstRecordByFilter(body.Collection, "")
		}
		if err != nil && !isNoRows(err) {
			return Rendered{}, nil, re.BadRequestError("Failed to load the sample record.", err)
		}
	}

	data := TemplateData(re.App, record, nil, to)
	if record == nil && body.Data != nil {
		data["record"] = body.Data
	}
	if _, ok := accountEmail(body.Account); ok {
		delete(data, "unsubscribe_url")
		for k, v := range SampleAccountValues(re.App, body.Account) {
			data[k] = v
		}
	}

	out, err := Render(subject, html, text, data)
	return out, err, nil
}

func (e *Engine) preview(re *core.RequestEvent) error {
	var body renderBody
	if err := re.BindBody(&body); err != nil {
		return re.BadRequestError("Invalid request body.", err)
	}
	out, tplErr, reqErr := e.renderFromBody(re, body, re.Auth.Email())
	if reqErr != nil {
		return reqErr
	}
	if tplErr != nil {
		// template errors are expected while typing; return them as data
		return re.JSON(http.StatusOK, map[string]any{"error": tplErr.Error()})
	}
	return re.JSON(http.StatusOK, out)
}

func (e *Engine) sendTest(re *core.RequestEvent) error {
	var body renderBody
	if err := re.BindBody(&body); err != nil {
		return re.BadRequestError("Invalid request body.", err)
	}
	to := re.Auth.Email()
	out, tplErr, reqErr := e.renderFromBody(re, body, to)
	if reqErr != nil {
		return reqErr
	}
	if tplErr != nil {
		return re.BadRequestError("Template error: "+tplErr.Error(), nil)
	}

	meta := re.App.Settings().Meta
	err := re.App.NewMailClient().Send(&mailer.Message{
		From:    mail.Address{Name: meta.SenderName, Address: meta.SenderAddress},
		To:      []mail.Address{{Address: to}},
		Subject: "[TEST] " + out.Subject,
		HTML:    out.HTML,
		Text:    out.Text,
	})
	if err != nil {
		return re.BadRequestError("Failed to send: "+err.Error(), nil)
	}
	return re.JSON(http.StatusOK, map[string]any{"to": to})
}

type sendBody struct {
	TemplateId       string   `json:"templateId"`
	Collection       string   `json:"collection"`
	RecordIds        []string `json:"recordIds"`
	Filter           string   `json:"filter"`
	RecipientField   string   `json:"recipientField"`
	StaticRecipients string   `json:"staticRecipients"`
	SendAt           string   `json:"sendAt"` // optional RFC3339 date for scheduling
	Dedupe           bool     `json:"dedupe"` // one email per address (the first record wins)
	DryRun           bool     `json:"dryRun"` // count the recipients without queueing
}

func (e *Engine) sendManual(re *core.RequestEvent) error {
	var body sendBody
	if err := re.BindBody(&body); err != nil {
		return re.BadRequestError("Invalid request body.", err)
	}
	req := BulkRequest{
		TemplateId:       body.TemplateId,
		Collection:       body.Collection,
		RecordIds:        body.RecordIds,
		Filter:           body.Filter,
		RecipientField:   body.RecipientField,
		StaticRecipients: body.StaticRecipients,
		Dedupe:           body.Dedupe,
		DryRun:           body.DryRun,
	}
	if body.SendAt != "" {
		t, err := time.Parse(time.RFC3339, body.SendAt)
		if err != nil {
			return re.BadRequestError("Invalid sendAt date.", nil)
		}
		req.SendAt = t
	}
	res, err := e.SendBulk(re.App, req)
	if err != nil {
		return re.BadRequestError(err.Error(), nil)
	}
	return re.JSON(http.StatusOK, res)
}

func (e *Engine) runAutomation(re *core.RequestEvent) error {
	a, err := re.App.FindRecordById(automationsCollection, re.Request.PathValue("id"))
	if err != nil {
		return re.NotFoundError("Automation not found.", err)
	}
	if a.GetString("trigger") != TriggerSchedule {
		return re.BadRequestError("Only schedule automations can be run manually.", nil)
	}
	n, err := e.RunSchedule(a.Id)
	if err != nil {
		return re.BadRequestError("Failed to run the automation: "+err.Error(), nil)
	}
	return re.JSON(http.StatusOK, map[string]any{"queued": n})
}

func (e *Engine) retryMessage(re *core.RequestEvent) error {
	msg, err := re.App.FindRecordById(messagesCollection, re.Request.PathValue("id"))
	if err != nil {
		return re.NotFoundError("Message not found.", err)
	}
	if msg.GetString("status") == StatusSent {
		return re.BadRequestError("The message was already sent.", nil)
	}
	msg.Set("status", StatusPending)
	msg.Set("attempts", 0)
	msg.Set("error", "")
	msg.Set("send_at", time.Now().UTC())
	if err := re.App.Save(msg); err != nil {
		return err
	}
	e.Kick()
	return re.NoContent(http.StatusNoContent)
}

func (e *Engine) cancelMessage(re *core.RequestEvent) error {
	msg, err := re.App.FindRecordById(messagesCollection, re.Request.PathValue("id"))
	if err != nil {
		return re.NotFoundError("Message not found.", err)
	}
	if msg.GetString("status") != StatusPending {
		return re.BadRequestError("Only pending messages can be cancelled.", nil)
	}
	msg.Set("status", StatusCancelled)
	if err := re.App.Save(msg); err != nil {
		return err
	}
	return re.NoContent(http.StatusNoContent)
}
