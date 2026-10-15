package pages

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	xhtml "golang.org/x/net/html"

	"github.com/Namarusa/PocketbasePlus/internal/formula"
)

// Conditions of an element (data-pb-conditions, JSON), like Budibase's:
//
//	[{"action": "hide", "when": [{"field": "record.image", "op": "empty"}]},
//	 {"action": "set", "prop": "data-pb-style-color", "value": "gold",
//	  "match": "any", "when": [{"field": "record.rarity", "op": "eq", "value": "Rare"},
//	                          {"formula": "{{gt record.hp 100}}"}]}]
//
// show: the element is only shown when one of its show rules matches; hide:
// removed when one matches (the visitor never gets it); set: a dynamic value
// (data-pb-attr-…, data-pb-style-…, data-pb-formula for the text) takes the
// rule's value, a text or a formula (the last matching rule wins). A rule
// matches when all its conditions do ("match": "any": one of them); a rule
// without conditions always matches. A condition compares a field of the
// record, the visitor or the address (formula.Check operators), or is a
// formula that gives true.
const attrConditions = "data-pb-conditions"

const (
	maxRules      = 30
	maxConditions = 10
)

type condRule struct {
	Action string      `json:"action"` // show | hide | set
	Prop   string      `json:"prop,omitempty"`
	Value  string      `json:"value,omitempty"`
	Match  string      `json:"match,omitempty"` // all (default) | any
	When   []condition `json:"when"`
}

type condition struct {
	Field   string `json:"field,omitempty"` // record.x, auth.x, query.x (through relations: record.brand.name)
	Op      string `json:"op,omitempty"`
	Value   string `json:"value,omitempty"` // a text or a formula
	Formula string `json:"formula,omitempty"`
}

var conditionFieldRegex = regexp.MustCompile(`^(record|auth|query)(\.[A-Za-z0-9_]+){1,4}$`)

func parseConditions(src string) ([]condRule, error) {
	var rules []condRule
	if err := json.Unmarshal([]byte(src), &rules); err != nil {
		return nil, formulaError("invalid conditions")
	}
	if len(rules) > maxRules {
		return nil, formulaError("too many conditions")
	}
	for _, r := range rules {
		switch r.Action {
		case "show", "hide":
		case "set":
			if !settableProp(r.Prop) {
				return nil, formulaError("conditions: unknown property " + r.Prop)
			}
		default:
			return nil, formulaError("conditions: unknown action " + r.Action)
		}
		if len(r.When) > maxConditions {
			return nil, formulaError("too many conditions")
		}
	}
	return rules, nil
}

// settableProp reports whether a "set" rule may change the property.
func settableProp(p string) bool {
	if p == attrFormula {
		return true
	}
	if a, ok := strings.CutPrefix(p, attrDynamic); ok {
		return dynamicAttrs[a]
	}
	if st, ok := strings.CutPrefix(p, styleDynamic); ok {
		return dynamicStyles[st]
	}
	return false
}

// condResult is the outcome of the conditions of an element.
type condResult struct {
	Visible bool         `json:"visible"`
	Sets    []propValue  `json:"sets"`
	Rules   []ruleResult `json:"rules"`
}

type propValue struct {
	Prop  string `json:"prop"`
	Value string `json:"value"`
}

type ruleResult struct {
	Matched bool   `json:"matched"`
	Error   string `json:"error,omitempty"`
}

func (r *renderer) evalConditions(s scope, rules []condRule) condResult {
	res := condResult{Rules: make([]ruleResult, len(rules)), Sets: []propValue{}}
	hasShow, shown, hidden := false, false, false
	set := map[string]int{}
	for i, rule := range rules {
		ok, err := r.evalRule(s, rule)
		res.Rules[i] = ruleResult{Matched: ok}
		if err != nil {
			res.Rules[i].Error = err.Error()
		}
		switch rule.Action {
		case "show":
			hasShow = true
			shown = shown || ok
		case "hide":
			hidden = hidden || ok
		case "set":
			if !ok {
				continue
			}
			if j, seen := set[rule.Prop]; seen {
				res.Sets[j].Value = rule.Value // the last one wins
			} else {
				set[rule.Prop] = len(res.Sets)
				res.Sets = append(res.Sets, propValue{rule.Prop, rule.Value})
			}
		}
	}
	res.Visible = (!hasShow || shown) && !hidden
	return res
}

func (r *renderer) evalRule(s scope, rule condRule) (bool, error) {
	if len(rule.When) == 0 {
		return true, nil
	}
	anyOf := rule.Match == "any"
	var firstErr error
	for _, c := range rule.When {
		ok, err := r.evalCondition(s, c)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if anyOf && ok {
			return true, nil
		}
		if !anyOf && !ok {
			return false, firstErr
		}
	}
	return !anyOf, firstErr
}

func (r *renderer) evalCondition(s scope, c condition) (bool, error) {
	if c.Formula != "" || c.Op == "formula" {
		out, err := r.evalFormula(s, c.Formula)
		if err != nil {
			return false, err
		}
		return formula.Check(formula.OpTrue, strings.TrimSpace(out), ""), nil
	}
	if !conditionFieldRegex.MatchString(c.Field) {
		return false, errors.New("conditions: choose a field")
	}
	if !formula.IsOp(c.Op) {
		return false, errors.New("conditions: unknown comparison " + c.Op)
	}
	value := pathLookup(r.formulaData(s, "{{"+c.Field+"}}"), c.Field)
	right := c.Value
	if strings.Contains(right, "{{") {
		out, err := r.evalFormula(s, right)
		if err != nil {
			return false, err
		}
		right = out
	}
	return formula.Check(c.Op, value, right), nil
}

// pathLookup returns the value at a path of the formula data; through a
// list (multiple relations) it's the list of the items' values
// (record.tags.name: the names of the tags).
func pathLookup(data any, path string) any {
	cur := data
	for _, part := range strings.Split(path, ".") {
		if l, ok := cur.(formula.List); ok {
			var out formula.List
			for _, item := range l {
				if v := formula.Lookup(item, part); v != nil {
					out = append(out, v)
				}
			}
			cur = out
			continue
		}
		cur = formula.Lookup(cur, part)
		if cur == nil {
			return nil
		}
	}
	return cur
}

// conditions applies the conditions of the element: false when it's hidden,
// else the matching "set" rules become its dynamic values.
func (r *renderer) conditions(n *xhtml.Node, s scope) bool {
	src := getAttr(n, attrConditions)
	if src == "" {
		return true
	}
	rules, err := parseConditions(src)
	if err != nil {
		r.app.Logger().Warn("Page binding error", "error", err.Error())
		return true
	}
	res := r.evalConditions(s, rules)
	for _, rr := range res.Rules {
		if rr.Error != "" {
			r.app.Logger().Warn("Page binding error", "error", rr.Error)
			if r.preview {
				setAttr(n, "title", "⚠ "+rr.Error)
			}
		}
	}
	if !res.Visible {
		return false
	}
	for _, p := range res.Sets {
		setAttr(n, p.Prop, p.Value)
	}
	return true
}

// conditionsPreview runs the conditions of the builder on a sample record
// (POST /api/pages/{id}/conditions {conditions, collection, record, as}).
func conditionsPreview(e *core.RequestEvent) error {
	var body struct {
		Conditions json.RawMessage `json:"conditions"`
		previewBody
	}
	r, s, err := previewRenderer(e, &body, &body.previewBody)
	if err != nil {
		return err
	}
	rules, err := parseConditions(string(body.Conditions))
	if err != nil {
		return e.JSON(200, map[string]string{"error": err.Error()})
	}
	return e.JSON(200, r.evalConditions(s, rules))
}
