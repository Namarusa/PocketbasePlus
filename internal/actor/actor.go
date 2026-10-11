// Package actor carries "who made this change" through contexts, so that the
// record hooks (revisions) know if a change came from a superuser, a user, a
// flow or the system.
package actor

import (
	"context"

	"github.com/pocketbase/pocketbase/core"
)

// Actor types.
const (
	TypeSuperuser = "superuser"
	TypeUser      = "user"
	TypeFlow      = "flow"
	TypeForm      = "form"
	TypeSystem    = "system"
)

// Actor describes the author of a change.
type Actor struct {
	Type  string `json:"type"`
	Id    string `json:"id"`
	Label string `json:"label"`
}

// System is the actor of changes without a known author (hooks, CLI...).
var System = Actor{Type: TypeSystem, Label: "System"}

type key struct{}

// With returns a context carrying the actor.
func With(ctx context.Context, a Actor) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, key{}, a)
}

// From returns the actor of a context.
func From(ctx context.Context) (Actor, bool) {
	if ctx == nil {
		return Actor{}, false
	}
	a, ok := ctx.Value(key{}).(Actor)
	return a, ok
}

// OfAuth returns the actor of an authenticated record.
func OfAuth(auth *core.Record) Actor {
	if auth == nil {
		return System
	}
	label := auth.Email()
	if label == "" {
		label = auth.Id
	}
	if auth.IsSuperuser() {
		return Actor{Type: TypeSuperuser, Id: auth.Id, Label: label}
	}
	return Actor{Type: TypeUser, Id: auth.Collection().Name + "/" + auth.Id, Label: label}
}
