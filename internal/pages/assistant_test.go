package pages

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/ai"
	"github.com/Namarusa/PocketbasePlus/internal/assistant"
	"github.com/Namarusa/PocketbasePlus/internal/mail"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

// script is a fake model answering with the next step.
type script struct {
	mu    sync.Mutex
	steps []func(req ai.ChatRequest) ai.Message
}

func (s *script) Chat(ctx context.Context, model string, req ai.ChatRequest) (*ai.ChatResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.steps) == 0 {
		return &ai.ChatResult{Message: ai.Message{Role: "assistant", Content: "done"}}, nil
	}
	step := s.steps[0]
	s.steps = s.steps[1:]
	return &ai.ChatResult{Message: step(req)}, nil
}

func toolCall(id, name string, args any) ai.ToolCall {
	tc := ai.ToolCall{ID: id, Type: "function"}
	tc.Function.Name = name
	b, _ := json.Marshal(args)
	tc.Function.Arguments = string(b)
	return tc
}

func result(req ai.ChatRequest, id string) map[string]any {
	for _, m := range req.Messages {
		if m.Role == "tool" && m.ToolCallID == id {
			var out map[string]any
			_ = json.Unmarshal([]byte(m.Content), &out)
			return out
		}
	}
	return nil
}

type chatResponse struct {
	ID      string           `json:"id"`
	Actions []map[string]any `json:"actions"`
	Error   string           `json:"error"`
}

func TestAssistantPages(t *testing.T) {
	f := setup(t)
	model := &script{}
	a := assistant.Register(f.app, mail.Register(f.app), nil)
	a.SetModel(func(core.App) (*ai.Model, ai.Chatter, error) { return &ai.Model{Name: "Fake"}, model, nil })
	su := testutil.Create(t, f.app, core.CollectionNameSuperusers, map[string]any{"email": "admin@test.com", "password": "1234567890"})

	shop := f.page(t, map[string]any{"title": "Shop", "slug": "shop", "project": map[string]any{"pages": []any{}},
		"html": `<body><h1 id="title">Shop</h1><div data-pb-list="products" data-pb-sort="name"><article data-pb-item><h3 data-pb-field="name"></h3></article></div></body>`})

	r, err := apis.NewRouter(f.app)
	if err != nil {
		t.Fatal(err)
	}
	var mux http.Handler
	if err := f.app.OnServe().Trigger(&core.ServeEvent{App: f.app, Router: r}, func(e *core.ServeEvent) error {
		mux, err = e.Router.BuildMux()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	post := func(path string, body any) chatResponse {
		t.Helper()
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(b)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", testutil.AuthToken(t, su))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		var out chatResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if rec.Code != 200 || out.Error != "" {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
		return out
	}

	model.steps = []func(ai.ChatRequest) ai.Message{
		func(req ai.ChatRequest) ai.Message {
			system := req.Messages[0].Content
			if !strings.Contains(system, "data-pb-formula") || strings.Contains(system, "You can't change pages") {
				t.Errorf("the pages guide is in the prompt and pages are supported")
			}
			return ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{toolCall("g1", "get_page", map[string]any{"page": "shop"})}}
		},
		func(req ai.ChatRequest) ai.Message {
			page := result(req, "g1")
			items := fmt.Sprint(page["outline"])
			if !strings.Contains(items, "ref:#title") || !strings.Contains(items, "list:products") || !strings.Contains(fmt.Sprint(page["html"]), "data-pb-item") {
				t.Errorf("get_page: %v", page)
			}
			return ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{
				// a binding error comes back to the model
				toolCall("p1", "propose_page_change", map[string]any{"page": "shop", "edits": []any{map[string]any{"ref": "1", "action": "after", "html": `<div data-pb-list="nope"><p data-pb-item>x</p></div>`}}}),
				toolCall("p2", "propose_page_change", map[string]any{"page": shop.Id, "change": "prices", "edits": []any{
					map[string]any{"ref": "1.0", "action": "append", "html": `<p data-pb-formula="{{currency &quot;EUR&quot; record.price}}" onclick="alert(1)">x</p><script>alert(2)</script>`},
					map[string]any{"ref": "#title", "action": "replace", "html": `<h1 id="title">Our shop</h1>`},
				}}),
				toolCall("p3", "propose_page", map[string]any{"title": "Product", "slug": "product", "kind": "record", "record_collection": "products",
					"html": `<h1 data-pb-field="name"></h1><p data-pb-formula="{{upper record.name}}"></p>`}),
				toolCall("p4", "propose_page", map[string]any{"title": "Dup", "slug": "shop", "html": "<p>x</p>"}),
			}}
		},
		func(req ai.ChatRequest) ai.Message {
			if r := result(req, "p1"); !strings.Contains(fmt.Sprint(r["error"]), `unknown collection "nope"`) {
				t.Errorf("binding error: %v", r)
			}
			if r := result(req, "p4"); !strings.Contains(fmt.Sprint(r["error"]), "taken") {
				t.Errorf("slug taken: %v", r)
			}
			return ai.Message{Role: "assistant", Content: "Apply the cards."}
		},
	}
	res := post("/api/assistant/chat", map[string]any{"message": "Show the prices on the shop and add a product page", "page": map[string]any{"path": "#/pages/" + shop.Id, "page": shop.Id}})
	if len(res.Actions) != 2 {
		t.Fatalf("actions: %v", res.Actions)
	}
	preview := fmt.Sprint(res.Actions[0]["details"].(map[string]any)["preview"])
	if !strings.Contains(preview, "€12.50") || !strings.Contains(preview, "Our shop") || strings.Contains(preview, "alert") {
		t.Errorf("change preview: %s", preview)
	}
	if p := fmt.Sprint(res.Actions[1]["details"].(map[string]any)["preview"]); !strings.Contains(p, "<h1>Chair</h1>") && !strings.Contains(p, "<h1>Lamp &lt;b&gt;</h1>") {
		t.Errorf("record page preview: %s", p)
	}
	if n, _ := f.app.CountRecords(Collection); n != 1 {
		t.Fatal("nothing changes before Apply")
	}

	for _, act := range res.Actions {
		post("/api/assistant/conversations/"+res.ID+"/actions/"+act["id"].(string)+"/apply", nil)
	}
	shop, _ = f.app.FindRecordById(Collection, shop.Id)
	html := shop.GetString("html")
	if !strings.Contains(html, `data-pb-formula="{{currency &#34;EUR&#34; record.price}}"`) || !strings.Contains(html, "Our shop") || strings.Contains(html, "alert") {
		t.Errorf("applied html: %s", html)
	}
	if shop.GetString("project") != "null" && shop.GetString("project") != "" {
		t.Errorf("the builder project must be reset, got %s", shop.GetString("project"))
	}
	if n, _ := f.app.CountRecords(VersionsCollection); n < 2 {
		t.Errorf("versions before and after, got %d", n)
	}
	product, err := f.app.FindFirstRecordByData(Collection, "slug", "product")
	if err != nil || product.GetBool("published") || product.GetString("record_collection") != f.products.Id {
		t.Fatalf("new page: %v %v", err, product)
	}
	// the published page shows the formula
	out := f.render(t, html, scope{}, nil)
	if !strings.Contains(out, "€12.50") {
		t.Errorf("rendered: %s", out)
	}

	// a page changed since the proposal is refused
	model.steps = []func(ai.ChatRequest) ai.Message{
		func(req ai.ChatRequest) ai.Message {
			return ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{toolCall("u1", "propose_page_change", map[string]any{"page": "shop", "published": false})}}
		},
	}
	res = post("/api/assistant/chat", map[string]any{"conversation": res.ID, "message": "unpublish it"})
	shop.Set("title", "Shop!")
	if err := f.app.Save(shop); err != nil {
		t.Fatal(err)
	}
	last := res.Actions[len(res.Actions)-1]
	b := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/assistant/conversations/"+res.ID+"/actions/"+last["id"].(string)+"/apply", nil)
	req.Header.Set("Authorization", testutil.AuthToken(t, su))
	mux.ServeHTTP(b, req)
	if !strings.Contains(b.Body.String(), "changed since this proposal") {
		t.Errorf("stale proposal: %s", b.Body.String())
	}
}
