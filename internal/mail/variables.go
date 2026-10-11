package mail

import (
	"slices"
	"strings"
	"text/template/parse"
)

// builtinRoots are template data roots filled automatically when sending.
var builtinRoots = map[string]bool{"app": true, "recipient": true, "unsubscribe_url": true}

// TemplateVariables returns the data paths referenced by the template parts
// (e.g. "username", "record.title"), excluding the automatic ones (.app, .recipient)
// and the fields used inside {{range}}/{{with}} blocks (relative to their item).
//
// It is used to show one input per variable when a template is used in a flow.
func TemplateVariables(parts ...string) ([]string, error) {
	seen := map[string]bool{}
	var result []string

	add := func(ident []string) {
		if len(ident) == 0 || builtinRoots[ident[0]] {
			return
		}
		path := strings.Join(ident, ".")
		if !seen[path] {
			seen[path] = true
			result = append(result, path)
		}
	}

	var walk func(n parse.Node)
	walkPipe := func(p *parse.PipeNode) {
		if p == nil {
			return
		}
		for _, cmd := range p.Cmds {
			for _, arg := range cmd.Args {
				walk(arg)
			}
		}
	}
	walk = func(n parse.Node) {
		switch n := n.(type) {
		case *parse.ListNode:
			if n == nil {
				return
			}
			for _, c := range n.Nodes {
				walk(c)
			}
		case *parse.ActionNode:
			walkPipe(n.Pipe)
		case *parse.PipeNode:
			walkPipe(n)
		case *parse.FieldNode:
			add(n.Ident)
		case *parse.ChainNode:
			if f, ok := n.Node.(*parse.FieldNode); ok {
				add(append(slices.Clone(f.Ident), n.Field...))
			}
		case *parse.IfNode:
			walkPipe(n.Pipe)
			walk(n.List)
			walk(n.ElseList)
		case *parse.RangeNode:
			// the body is relative to each item: only the ranged value is a variable
			walkPipe(n.Pipe)
			walk(n.ElseList)
		case *parse.WithNode:
			walkPipe(n.Pipe)
			walk(n.ElseList)
		case *parse.TemplateNode:
			walkPipe(n.Pipe)
		}
	}

	for _, src := range parts {
		trees, err := parse.Parse("t", src, "", "", builtinFuncNames())
		if err != nil {
			return nil, err
		}
		for _, tree := range trees {
			walk(tree.Root)
		}
	}

	// drop parents of more specific paths (e.g. "record" when "record.title" is used)
	filtered := result[:0]
	for _, p := range result {
		isParent := false
		for _, other := range result {
			if strings.HasPrefix(other, p+".") {
				isParent = true
				break
			}
		}
		if !isParent {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
}

// builtinFuncNames lists the functions available to templates, so that the
// parser accepts them (text/template builtins are always known).
func builtinFuncNames() map[string]any {
	names := map[string]any{}
	for k, v := range TemplateFuncs() {
		names[k] = v
	}
	return names
}
