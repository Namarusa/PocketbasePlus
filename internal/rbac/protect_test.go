package rbac_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/Namarusa/PocketbasePlus/internal/rbac"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

const (
	adminRoleID = "roleadmin000001"
	selfUserID  = "userself0000001"
)

func protectApp(t testing.TB) *tests.TestApp {
	app := testutil.NewApp(t)
	rbac.Register(app)
	testutil.Create(t, app, "roles", map[string]any{"id": adminRoleID, "name": "admin"})
	testutil.Create(t, app, "users", map[string]any{"id": selfUserID, "email": "me@test.com", "password": "1234567890"})
	testutil.Create(t, app, core.CollectionNameSuperusers, map[string]any{"email": "root@test.com", "password": "1234567890"})
	return app
}

func TestRolesAreProtected(t *testing.T) {
	cases := []struct {
		name, method, url, body, auth string
		status                        int
	}{
		{"sign up with a role", http.MethodPost, "/api/collections/users/records", `{"email":"x@test.com","password":"1234567890","passwordConfirm":"1234567890","roles":["` + adminRoleID + `"]}`, "", 403},
		{"sign up without roles", http.MethodPost, "/api/collections/users/records", `{"email":"x@test.com","password":"1234567890","passwordConfirm":"1234567890"}`, "", 200},
		{"give oneself a role", http.MethodPatch, "/api/collections/users/records/" + selfUserID, `{"roles+":"` + adminRoleID + `"}`, "me@test.com", 403},
		{"change one's name", http.MethodPatch, "/api/collections/users/records/" + selfUserID, `{"name":"Me"}`, "me@test.com", 200},
		{"superuser sets roles", http.MethodPatch, "/api/collections/users/records/" + selfUserID, `{"roles":["` + adminRoleID + `"]}`, "root@test.com", 200},
	}
	for _, c := range cases {
		headers := map[string]string{"Content-Type": "application/json"}
		(&tests.ApiScenario{
			Name: c.name, Method: c.method, URL: c.url, Body: strings.NewReader(c.body), Headers: headers,
			TestAppFactory: protectApp, ExpectedStatus: c.status, ExpectedContent: []string{`"`},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				if c.auth == "" {
					return
				}
				col := "users"
				if c.auth == "root@test.com" {
					col = core.CollectionNameSuperusers
				}
				u, _ := app.FindAuthRecordByEmail(col, c.auth)
				headers["Authorization"] = testutil.AuthToken(t, u)
			},
		}).Test(t)
	}
}
