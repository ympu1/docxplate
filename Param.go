package docxplate

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

// ParamPattern - regex pattern to identify params
// const ParamPattern = `{{(#|)[\w\.]+?(| .| )+?}}`
const ParamPattern = `{{(#|)([\w\.]+?)(| .| +)(|(:[a-z]+)+)+?}}`

// Param ..
type Param struct {
	Key   string
	Value string

	IsSlice bool // mark param created from slice
	Params  ParamList

	parent *Param

	AbsoluteKey string // Users.1.Name
	CompactKey  string // Users.Name

	Trigger *ParamTrigger
}

// NewParam ..
func NewParam(key interface{}) *Param {
	p := &Param{
		Key: fmt.Sprintf("%v", key),
	}
	p.AbsoluteKey = p.Key
	p.CompactKey = p.Key
	return p
}

// NewParamFromRaw ..
func NewParamFromRaw(raw []byte) *Param {
	// extract from raw contents
	re := regexp.MustCompile(ParamPattern)
	matches := re.FindAllSubmatch(raw, -1)
	if matches == nil || matches[0] == nil {
		return nil
	}

	p := NewParam(string(matches[0][2]))
	p.Trigger = NewParamTrigger(matches[0][4])
	return p
}

// SetValue - any value to string
func (p *Param) SetValue(val interface{}) {
	switch v := val.(type) {
	case string:
		p.Value = v
	default:
		p.Value = fmt.Sprintf("%v", val)
	}

}

// Placeholder .. {{Key}}
func (p *Param) Placeholder() string {
	return "{{" + p.AbsoluteKey + "}}"
}

// PlaceholderKey .. {{#Key}}
func (p *Param) PlaceholderKey() string {
	return "{{#" + p.AbsoluteKey + "}}"
}

// PlaceholderInline .. {{Key ,}}
func (p *Param) PlaceholderInline() string {
	return "{{" + p.AbsoluteKey + " " // "{{Key " - space suffix
}

// PlaceholderKeyInline .. {{#Key ,}}
func (p *Param) PlaceholderKeyInline() string {
	return "{{#" + p.AbsoluteKey + " " // "{{#Key " - space suffix
}

// PlaceholderPrefix .. {{Key.
func (p *Param) PlaceholderPrefix() string {
	return "{{" + p.AbsoluteKey + "." // "{{Key."
}

// PlaceholderKeyPrefix .. {{Key.
func (p *Param) PlaceholderKeyPrefix() string {
	return "{{#" + p.AbsoluteKey + "." // "{{#Key."
}

// PlaceholderWithTrigger .. {{Key :empty:remove:list}}
func (p *Param) PlaceholderWithTrigger() string {
	return "{{" + p.AbsoluteKey + " " + p.Trigger.String() + "}}"
}

// PlaceholderKeyWithTrigger .. {{#Key :empty:remove:list}}
func (p *Param) PlaceholderKeyWithTrigger() string {
	return "{{#" + p.AbsoluteKey + " " + p.Trigger.String() + "}}"
}

// ToCompact - convert AbsoluteKey placeholder to ComplexKey placeholder
// {{Users.0.Name}} --> {{Users.Name}}
func (p *Param) ToCompact(placeholder string) string {
	return strings.Replace(placeholder, p.AbsoluteKey, p.CompactKey, 1)
}

// Walk down
func (p *Param) Walk(fn func(*Param)) {
	for _, p2 := range p.Params {
		if p2 == nil {
			continue
		}

		// Assign parent
		p2.parent = p

		// Absolute key with slice indexes
		p2.AbsoluteKey = p.AbsoluteKey + "." + p2.Key
		if p.AbsoluteKey == "" {
			p2.AbsoluteKey = p.Key + "." + p2.Key

		}

		// Complex key with no slice indexes
		if p2.parent.IsSlice {
			p2.CompactKey = p.Key
		} else {
			p2.CompactKey = p.CompactKey + "." + p2.Key
		}

		fn(p2)

		p2.Walk(fn)
	}
}

// Depth - how many levels param have of child nodes
// {{Users.1.Name}} --> 3
func (p *Param) Depth() int {
	return strings.Count(p.AbsoluteKey, ".") + 1
}

// Try to extract trigger from raw contents specific to this param
func (p *Param) extractTriggerFrom(buf []byte) *ParamTrigger {
	prefixes := []string{
		p.PlaceholderInline(),
		p.PlaceholderKeyInline(),
	}
	for _, pref := range prefixes {
		bpref := []byte(pref)
		if !bytes.Contains(buf, bpref) {
			continue
		}

		// Get part where trigger is (remove plaheolder prefix)
		buf := bytes.SplitN(buf, bpref, 2)[1]

		// Remove placeholder suffix and only raw trigger part left
		buf = bytes.SplitN(buf, []byte("}}"), 2)[0]

		p.Trigger = NewParamTrigger(buf)
		return p.Trigger
	}

	return nil
}

// RunTrigger - execute trigger
func (p *Param) RunTrigger(xnode *xmlNode) {

	if p.Trigger.On == TriggerOnEmpty && p.Value != "" {
		return
	}

	// 1. Scope - find affected node
	var ntypes = NodeSingleTypes
	switch p.Trigger.Scope {
	case TriggerScopeCell:
		ntypes = NodeCellTypes
	case TriggerScopeRow:
		ntypes = NodeRowTypes
	case TriggerScopeList:
		ntypes = []string{"w-p"} // list items have w-p > w-pPr > w-numPr item
	case TriggerScopeTable:
		ntypes = []string{"w-tbl"}
	case TriggerScopeSection:
		ntypes = NodeSectionTypes
	}

	n := xnode.closestUp(ntypes)
	if n == nil || n.isDeleted {
		// color.Red("EMPTY parent of %v", xnode.Tag())
		return
	}

	isListItem, listID := n.IsListItem()

	// Whole lists: special case
	isListRemove := p.Trigger.Scope == TriggerScopeList                                   // :list
	isListRemove = isListRemove || (isListItem && p.Trigger.Scope == TriggerScopeSection) // :section
	if isListRemove && isListItem {
		// find all list items as this
		for _, wpNode := range n.parent.Nodes {
			isitem, listid := wpNode.IsListItem()
			if !isitem || listid != listID {
				// color.Red("--- %s [%s]", wpNode, wpNode.AllContents())
				continue
			}
			if p.Trigger.Command == TriggerCommandRemove {
				wpNode.Nodes = nil
				wpNode.delete()
			}
		}
		return
	}

	// Simple cases
	if p.Trigger.Command == TriggerCommandRemove {
		// n.printTree("TRIGGER: " + p.Trigger.String() + " " + p.Trigger.Command)
		n.Nodes = nil
		n.delete()
		return
	}

	if p.Trigger.Command == TriggerCommandClear {
		n.Content = nil
		n.Walk(func(n2 *xmlNode) {
			n2.Content = nil
		})
		return
	}

}

// String - compact debug information as string
func (p *Param) String() string {
	s := fmt.Sprintf("%34s=%-20s", p.AbsoluteKey, p.Value)
	s += fmt.Sprintf("\tTrigger[%s]", p.Trigger)
	return s
}
