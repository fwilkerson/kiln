package kiln

import (
	"bytes"
	"encoding/json"
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// writeFiles is -write, which writes each file a test holds equal to the code, in place of the test.
var writeFiles = flag.Bool("write", false, "write the defaults' schemas and help.txt")

// TestSchema fails while a schema among the defaults differs from the JSON Schema of its type. It builds each from
// the type's fields and their docs. An editor reads kiln.schema.json for kiln.json, and monitor.schema.json for each
// monitor.json. Each doc becomes the description an editor shows on hover, with its key in place of its Go name.
func TestSchema(t *testing.T) {
	docs := typeDocs(t)
	for _, s := range []struct {
		file, title string
		typ         reflect.Type
	}{
		{"defaults/generated/kiln.schema.json", "kiln.json", reflect.TypeFor[config]()},
		{"defaults/generated/monitor.schema.json", "monitor.json", reflect.TypeFor[settings]()},
	} {
		root := schemaOf(t, s.typ, docs)
		root.property("workflow").Enum = workflows
		root.Schema, root.Title = "http://json-schema.org/draft-07/schema#", s.title
		root.Description = hover(root.Description, s.typ.Name(), s.title)
		raw, err := plainJSON(root)
		if err != nil {
			t.Fatal(err)
		}

		var got bytes.Buffer
		if err := json.Indent(&got, raw, "", "  "); err != nil {
			t.Fatal(err)
		}

		got.WriteByte('\n')
		if *writeFiles {
			if err := os.WriteFile(s.file, got.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}

			continue
		}

		if want, err := defaults.ReadFile(s.file); err != nil || !bytes.Equal(got.Bytes(), want) {
			t.Errorf("%s differs from %s, %v: run go test ./internal/kiln -run TestSchema -write, then cp "+
				"internal/kiln/%[1]s .kiln/generated/", s.file, s.typ, err)
		}
	}
}

// hover is doc, which starts with the Go name goName, as an editor shows it for key: starting with key.
func hover(doc, goName, key string) string {
	rest, ok := strings.CutPrefix(doc, goName)
	if !ok || strings.IndexAny(rest, " ,") != 0 {
		return doc
	}

	return key + rest
}

// schemaNode is a JSON Schema, of the keywords config needs.
type schemaNode struct {
	Schema               string        `json:"$schema,omitempty"`
	Title                string        `json:"title,omitempty"`
	Description          string        `json:"description,omitempty"`
	Type                 string        `json:"type,omitempty"`
	Enum                 []workflow    `json:"enum,omitempty"`
	AnyOf                []*schemaNode `json:"anyOf,omitempty"`
	Items                *schemaNode   `json:"items,omitempty"`
	Properties           properties    `json:"properties,omitempty"`
	AdditionalProperties any           `json:"additionalProperties,omitempty"`
}

// property is the schema of n's property name, or nil when n has none of that name.
func (n *schemaNode) property(name string) *schemaNode {
	i := slices.IndexFunc(n.Properties, func(p property) bool { return p.name == name })
	if i < 0 {
		return nil
	}

	return n.Properties[i].schema
}

// properties are an object's properties, in the order of its struct's fields.
type properties []property

type property struct {
	name   string
	schema *schemaNode
}

func (p properties) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, prop := range p {
		name, _ := plainJSON(prop.name)
		value, err := plainJSON(prop.schema)
		if err != nil {
			return nil, err
		}

		if i > 0 {
			b.WriteByte(',')
		}

		b.Write(name)
		b.WriteByte(':')
		b.Write(value)
	}

	b.WriteByte('}')
	return b.Bytes(), nil
}

// plainJSON is v as compact JSON, with no character escaped for HTML, so a doc reads as written.
func plainJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	err := enc.Encode(v)
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), err
}

// schemaOf is the schema of a value of type typ, as kiln.json holds it. docs holds each type's doc and each field's.
// A struct refuses a key it lacks, as parseConfig does.
func schemaOf(t *testing.T, typ reflect.Type, docs map[string]string) *schemaNode {
	switch typ {
	case reflect.TypeFor[paths]():
		return &schemaNode{AnyOf: []*schemaNode{{Type: "string"}, {Type: "array", Items: &schemaNode{Type: "string"}}}}
	case reflect.TypeFor[json.RawMessage]():
		return &schemaNode{}
	case reflect.TypeFor[timeout]():
		return &schemaNode{Type: "string"}
	}

	switch typ.Kind() {
	case reflect.Pointer:
		return schemaOf(t, typ.Elem(), docs)
	case reflect.String:
		return &schemaNode{Type: "string"}
	case reflect.Bool:
		return &schemaNode{Type: "boolean"}
	case reflect.Int, reflect.Uint16:
		return &schemaNode{Type: "integer"}
	case reflect.Float64:
		return &schemaNode{Type: "number"}
	case reflect.Slice:
		return &schemaNode{Type: "array", Items: schemaOf(t, typ.Elem(), docs)}
	case reflect.Map:
		return &schemaNode{Type: "object", AdditionalProperties: schemaOf(t, typ.Elem(), docs)}
	case reflect.Struct:
		return &schemaNode{Description: docs[typ.Name()], Type: "object", Properties: fieldsOf(t, typ, docs),
			AdditionalProperties: false}
	}

	t.Fatalf("kiln.json holds a %s, which schemaOf has no schema for", typ)
	return nil
}

// fieldsOf are the properties of struct typ, as encoding/json reads them. An embedded struct's fields are its own.
func fieldsOf(t *testing.T, typ reflect.Type, docs map[string]string) properties {
	var props properties
	for f := range typ.Fields() {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if f.Anonymous && name == "" {
			props = append(props, fieldsOf(t, f.Type, docs)...)
			continue
		}

		s := schemaOf(t, f.Type, docs)
		s.Description = hover(docs[typ.Name()+"."+f.Name], f.Name, name)
		props = append(props, property{name: name, schema: s})
	}

	return props
}

// typeDocs reads the doc of each type of the package, by its name, and of each field, by "type.Field".
func typeDocs(t *testing.T) map[string]string {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	docs := map[string]string{}
	fset := token.NewFileSet()
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}

		for _, decl := range f.Decls {
			if d, ok := decl.(*ast.GenDecl); ok && d.Tok == token.TYPE {
				readDocs(d, docs)
			}
		}
	}

	return docs
}

// readDocs adds to docs the doc of each type d declares, and of each of its fields.
func readDocs(d *ast.GenDecl, docs map[string]string) {
	for _, spec := range d.Specs {
		ts := spec.(*ast.TypeSpec)
		docs[ts.Name.Name] = docText(cmpOr(ts.Doc, d.Doc))
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			continue
		}

		for _, f := range st.Fields.List {
			for _, name := range f.Names {
				docs[ts.Name.Name+"."+name.Name] = docText(f.Doc)
			}
		}
	}
}

// docText is a doc comment's text on one line, or "" for none.
func docText(doc *ast.CommentGroup) string {
	return strings.Join(strings.Fields(doc.Text()), " ")
}
