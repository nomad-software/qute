package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Class options live in spec/classes/<module>.yaml; ownership overlays live in
// spec/overlays/<module>.yaml. Overlays have the shape
//
//	QLayout:
//	  addWidget:  { args: { w: transfer-to-cpp } }
//	  takeAt:     { return: transfer-to-go }
//
// A method key is either a bare name (all overloads) or a signature such as
// "QPushButton(const QString&, QWidget*)" or "resize(const QSize&)".

type classOpts struct {
	// Kind: object (default for QObjects and polymorphic classes), value
	// (copyable Qt value class held in an owned handle) or event.
	Kind string `yaml:"kind"`
	// Subclass generates a shim with Go-overridable virtuals and a Base type.
	Subclass bool `yaml:"subclass"`
	// NotifyDtor generates a shim whose destructor tells Go the object died
	// (for non-QObject classes with a virtual destructor).
	NotifyDtor bool `yaml:"notify-dtor"`
	// NoConstruct hides constructors.
	NoConstruct bool `yaml:"no-construct"`
	// SkipVirtuals lists virtuals that may not be overridden from Go.
	SkipVirtuals []string `yaml:"skip-virtuals"`
	// Reviewed marks every default rule on the class as reviewed.
	Reviewed bool `yaml:"reviewed"`
	// RawPointers passes every pointer to a number as unsafe.Pointer instead of
	// an out-parameter or slice (OpenGL function tables, where GL decides the size).
	RawPointers bool `yaml:"raw-pointers"`
	// EnumsOnly emits the class's enums but no wrapper (QChar is a rune in Go).
	EnumsOnly bool `yaml:"enums-only"`
}

type classSpec struct {
	Module  string                `yaml:"module"`
	Package string                `yaml:"package"`
	Classes map[string]*classOpts `yaml:"classes"`
}

type overlay struct {
	Args     map[string]string `yaml:"args"`
	Return   string            `yaml:"return"`
	Name     string            `yaml:"name"`
	Full     string            `yaml:"full"`
	KeepDefs bool              `yaml:"keep-defaults"`
	Skip     any               `yaml:"skip"`
	Error    bool              `yaml:"error"`
	ErrorMsg string            `yaml:"error-string"`
	Cpp      string            `yaml:"cpp"`     // C++ call expression: $self, $0, $1 ... for the arguments
	Created  bool              `yaml:"created"` // the result is built by the binding, so its death is tracked
	Reviewed bool              `yaml:"reviewed"`
	Test     string            `yaml:"test"`
	Doc      string            `yaml:"doc"`
}

func (o *overlay) skipped() bool {
	if o == nil || o.Skip == nil {
		return false
	}
	switch v := o.Skip.(type) {
	case bool:
		return v
	case string:
		return v != ""
	}
	return true
}

type overlayFile map[string]map[string]*overlay

func loadYAML(path string, into any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// normSig normalises a C++ type list for matching overlay signatures.
func normType(s string) string {
	s = strings.ReplaceAll(s, " ", "")
	return s
}

// overlayFor returns the merged overlay for a method: bare-name entries first,
// then a matching signature entry on top.
func (f overlayFile) overlayFor(class string, m *irMethod) (*overlay, []string) {
	entries := f[class]
	if entries == nil {
		return nil, nil
	}
	var used []string
	var out *overlay
	merge := func(o *overlay) {
		if o == nil {
			return
		}
		if out == nil {
			c := *o
			c.Args = map[string]string{}
			for k, v := range o.Args {
				c.Args[k] = v
			}
			out = &c
			return
		}
		for k, v := range o.Args {
			out.Args[k] = v
		}
		if o.Return != "" {
			out.Return = o.Return
		}
		if o.Name != "" {
			out.Name = o.Name
		}
		if o.Full != "" {
			out.Full = o.Full
		}
		if o.KeepDefs {
			out.KeepDefs = true
		}
		if o.Skip != nil {
			out.Skip = o.Skip
		}
		if o.Error {
			out.Error = true
		}
		if o.ErrorMsg != "" {
			out.ErrorMsg = o.ErrorMsg
		}
		if o.Cpp != "" {
			out.Cpp = o.Cpp
		}
		if o.Created {
			out.Created = true
		}
		if o.Reviewed {
			out.Reviewed = true
		}
		if o.Test != "" {
			out.Test = o.Test
		}
		if o.Doc != "" {
			out.Doc = o.Doc
		}
	}
	if o, ok := entries[m.Name]; ok {
		merge(o)
		used = append(used, m.Name)
	}
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !strings.Contains(k, "(") {
			continue
		}
		name := k[:strings.Index(k, "(")]
		if name != m.Name {
			continue
		}
		if sigMatches(k[len(name):], m) {
			merge(entries[k])
			used = append(used, k)
		}
	}
	return out, used
}

func sigMatches(sig string, m *irMethod) bool {
	sig = strings.TrimSpace(sig)
	constM := strings.HasSuffix(sig, "const")
	sig = strings.TrimSuffix(sig, "const")
	sig = strings.TrimSpace(sig)
	if !strings.HasPrefix(sig, "(") || !strings.HasSuffix(sig, ")") {
		return false
	}
	inner := sig[1 : len(sig)-1]
	var parts []string
	if strings.TrimSpace(inner) != "" {
		parts = splitTopLevel(inner)
	}
	if len(parts) != len(m.Params) {
		return false
	}
	if constM && !m.Const {
		return false
	}
	for i, p := range parts {
		if !typeMatches(p, m.Params[i].Type) {
			return false
		}
	}
	return true
}

// typeMatches compares an overlay type with an IR type, accepting the
// spelling, the canonical form, or a class-qualified spelling.
func typeMatches(want string, t irType) bool {
	wb, wc, wp, wr, _ := parseCpp(want)
	for _, have := range []string{t.Spelling, t.Canonical} {
		hb, hc, hp, hr, _ := parseCpp(have)
		if wc != hc || wp != hp || wr != hr {
			continue
		}
		wb, hb := normType(wb), normType(hb)
		if wb == hb || strings.HasSuffix(wb, "::"+hb) {
			return true
		}
	}
	return false
}

func splitTopLevel(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, c := range s {
		switch c {
		case '<', '(':
			depth++
		case '>', ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}
