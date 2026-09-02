// Package h is a tiny server-side HTML builder in the spirit of
// Falco.Markup: nodes are built programmatically and rendered to a string,
// with text encoded by default.
package h

import (
	"html"
	"strings"
)

type Attr struct {
	Key    string
	Value  string
	IsFlag bool
	Skip   bool
}

// A is a key="value" attribute.
func A(key, value string) Attr { return Attr{Key: key, Value: value} }

// Flag is a boolean attribute (e.g. required, checked).
func Flag(key string) Attr { return Attr{Key: key, IsFlag: true} }

// If keeps the attribute only when cond holds.
func If(cond bool, a Attr) Attr {
	if !cond {
		a.Skip = true
	}
	return a
}

type Node struct {
	tag      string
	attrs    []Attr
	children []Node
	text     string
	raw      bool
	isText   bool
	fragment bool
	skip     bool
}

// E is an element with attributes and children.
func E(tag string, attrs []Attr, children ...Node) Node {
	return Node{tag: tag, attrs: attrs, children: children}
}

// Text is an encoded text node.
func Text(text string) Node { return Node{isText: true, text: text} }

// Raw is an unencoded text node — for trusted markup only.
func Raw(text string) Node { return Node{isText: true, raw: true, text: text} }

// Frag groups children without a wrapping element.
func Frag(children ...Node) Node { return Node{fragment: true, children: children} }

// Empty renders nothing.
func Empty() Node { return Node{skip: true} }

// IfNode keeps the node only when cond holds.
func IfNode(cond bool, node Node) Node {
	if !cond {
		return Empty()
	}
	return node
}

// voidElements render without a closing tag.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"source": true, "track": true, "wbr": true,
}

func (n Node) write(b *strings.Builder) {
	if n.skip {
		return
	}
	if n.isText {
		if n.raw {
			b.WriteString(n.text)
		} else {
			b.WriteString(html.EscapeString(n.text))
		}
		return
	}
	if n.fragment {
		for _, child := range n.children {
			child.write(b)
		}
		return
	}
	b.WriteByte('<')
	b.WriteString(n.tag)
	for _, attr := range n.attrs {
		if attr.Skip || attr.Key == "" {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(attr.Key)
		if !attr.IsFlag {
			b.WriteString(`="`)
			b.WriteString(html.EscapeString(attr.Value))
			b.WriteByte('"')
		}
	}
	b.WriteByte('>')
	if voidElements[n.tag] {
		return
	}
	for _, child := range n.children {
		child.write(b)
	}
	b.WriteString("</")
	b.WriteString(n.tag)
	b.WriteByte('>')
}

// Render renders a node tree to HTML.
func Render(node Node) string {
	var b strings.Builder
	node.write(&b)
	return b.String()
}

// Document renders a full HTML document with doctype.
func Document(root Node) string {
	return "<!DOCTYPE html>\n" + Render(root)
}
