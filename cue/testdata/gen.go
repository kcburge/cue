// Copyright 2020 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/constant"
	"go/format"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue"
	cueast "cuelang.org/go/cue/ast"
	cueformat "cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal"
	"cuelang.org/go/pkg/encoding/yaml"
	"cuelang.org/go/tools/fix"
)

//go:generate go run gen.go
//go:generate go test ../internal/compile --update
//go:generate go test ../internal/eval --update

func main() {
	flag.Parse()
	log.SetFlags(log.Lshortfile)

	cfg := &packages.Config{
		Mode: packages.NeedSyntax | packages.NeedTypesInfo,

		Tests: true,
	}
	a, err := packages.Load(cfg, "..")
	if err != nil {
		log.Fatal(err)
	}

	for _, p := range a {
		e := extractor{p: p}
		e.extractFromPackage()
	}
}

type extractor struct {
	p *packages.Package

	dir    string
	name   string
	count  int
	a      *txtar.Archive
	header *bytes.Buffer

	tags []string
}

func (e *extractor) fatalf(format string, args ...interface{}) {
	prefix := fmt.Sprintf("%s/%d[%s]: ", e.dir, e.count, e.name)
	log.Fatalf(prefix+format, args...)
}

func (e *extractor) warnf(format string, args ...interface{}) {
	prefix := fmt.Sprintf("%s/%d[%s]: ", e.dir, e.count, e.name)
	log.Printf(prefix+format, args...)
}

func (e *extractor) extractFromPackage() {
	for _, file := range e.p.Syntax {
		for _, d := range file.Decls {
			e.processTestFunc(d)
		}
	}
}

func (e *extractor) processTestFunc(d ast.Decl) {
	p := e.p
	f, ok := d.(*ast.FuncDecl)
	if !ok {
		return
	}

	if !strings.HasPrefix(f.Name.Name, "Test") {
		return
	}
	e.dir = strings.ToLower(f.Name.Name[len("Test"):])
	e.count = 0
	e.tags = nil
	if e.dir == "x" { // TestX
		return
	}

	if len(f.Type.Params.List) != 1 {
		return
	}

	if p.TypesInfo.TypeOf(f.Type.Params.List[0].Type).String() != "*testing.T" {
		return
	}
	e.extractFromTestFunc(f)
}

func (e *extractor) isConstant(x ast.Expr) bool {
	return constant.Val(e.p.TypesInfo.Types[x].Value) != nil
}

func (e *extractor) stringConst(x ast.Expr) string {
	v := e.p.TypesInfo.Types[x].Value
	if v.Kind() != constant.String {
		return v.String()
	}
	return constant.StringVal(v)
}

func (e *extractor) boolConst(x ast.Expr) bool {
	v := e.p.TypesInfo.Types[x].Value
	return constant.BoolVal(v)
}

func (e *extractor) exprString(x ast.Expr) string {
	w := &bytes.Buffer{}
	_ = format.Node(w, e.p.Fset, x)
	return w.String()
}

func (e *extractor) extractFromTestFunc(f *ast.FuncDecl) {
	defer func() {
		if err := recover(); err != nil {
			e.warnf("PANIC: %v", err)
			panic(err)
		}
	}()
	// Extract meta data.
	for _, stmt := range f.Body.List {
		es, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		if call, ok := es.X.(*ast.CallExpr); ok {
			if e.exprString(call.Fun) != "rewriteHelper" {
				continue
			}
			e.tags = append(e.tags, e.exprString(call.Args[2]))
		}
	}

	// Extract test data.
	for _, stmt := range f.Body.List {
		ast.Inspect(stmt, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CompositeLit:
				t := e.p.TypesInfo.TypeOf(x.Type)

				switch t.String() {
				case "[]cuelang.org/go/cue.testCase",
					"[]cuelang.org/go/cue.exportTest":
					// TODO: "[]cuelang.org/go/cue.subsumeTest",
				default:
					return false
				}

				for _, elt := range x.Elts {
					if kv, ok := elt.(*ast.KeyValueExpr); ok {
						elt = kv.Value
					}
					e.extractTest(elt.(*ast.CompositeLit))
					e.count++
				}

				return false
			}
			return true
		})
	}
}

func (e *extractor) extractTest(x *ast.CompositeLit) {
	e.name = ""
	e.header = &bytes.Buffer{}
	e.a = &txtar.Archive{}

	e.header.WriteString(`# DO NOT EDIT; generated by go run testdata/gen.go
#
`)

	for _, elmt := range x.Elts {
		f, ok := elmt.(*ast.KeyValueExpr)
		if !ok {
			e.fatalf("Invalid slice element: %T", elmt)
			continue
		}

		switch key := e.exprString(f.Key); key {
		case "name", "desc":
			e.name = e.stringConst(f.Value)
			fmt.Fprintf(e.header, "#name: %v\n", e.stringConst(f.Value))

		case "in":
			src := []byte(e.stringConst(f.Value))
			src, err := cueformat.Source(src)

			if f, err := parser.ParseFile("in.cue", src, parser.ParseComments); err == nil {
				f = fix.File(f)
				b, err := cueformat.Node(f)
				if err == nil {
					src = b
				}
			}

			if err != nil {
				fmt.Fprintln(e.header, "#skip")
				e.warnf("Skipped: %v", err)
				continue
			}
			e.a.Files = append(e.a.Files, txtar.File{
				Name: "in.cue",
				Data: src,
			})

			e.populate(src)

		case "out":
			if !e.isConstant(f.Value) {
				e.warnf("Could not determine value for 'out' field")
				continue
			}
			e.a.Files = append(e.a.Files, txtar.File{
				Name: "out/legacy-debug",
				Data: []byte(e.stringConst(f.Value)),
			})
		default:
			fmt.Fprintf(e.header, "%s: %v\n", key, e.exprString(f.Value))
		}
	}

	for _, t := range e.tags {
		fmt.Fprintf(e.header, "#%s\n", t)
	}

	e.a.Comment = e.header.Bytes()

	_ = os.Mkdir(e.dir, 0755)

	name := fmt.Sprintf("%03d", e.count)
	if e.name != "" {
		name += "_" + e.name
	}
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, ":", "_")
	filename := filepath.Join(e.dir, name+".txtar")
	err := ioutil.WriteFile(filename, txtar.Format(e.a), 0644)
	if err != nil {
		e.fatalf("Could not write file: %v", err)
	}
}

// populate sets the golden tests based on the old compiler, evaluator,
// and exporter.
func (e *extractor) populate(src []byte) {
	r := cue.Runtime{}
	inst, err := r.Compile("in.cue", src)
	if err != nil {
		e.fatalf("Failed to parse: %v", err)
	}

	v := inst.Value()

	e.addFile(e.a, "out/def", v.Syntax(
		cue.Docs(true),
		cue.Attributes(true),
		cue.Optional(true),
		cue.Definitions(true)))

	if v.Validate(cue.Concrete(true)) == nil {
		e.addFile(e.a, "out/export", v.Syntax(
			cue.Concrete(true),
			cue.Final(),
			cue.Docs(true),
			cue.Attributes(true)))

		s, err := yaml.Marshal(v)
		if err != nil {
			fmt.Fprintln(e.header, "#bug: true")
			e.warnf("Could not encode as YAML: %v", err)
		}
		e.a.Files = append(e.a.Files,
			txtar.File{Name: "out/yaml", Data: []byte(s)})

		b, err := json.Marshal(v)
		if err != nil {
			fmt.Fprintln(e.header, "#bug: true")
			e.warnf("Could not encode as JSON: %v", err)
		}
		e.a.Files = append(e.a.Files,
			txtar.File{Name: "out/json", Data: b})
	}
}

func (e *extractor) addFile(a *txtar.Archive, name string, n cueast.Node) {
	b, err := cueformat.Node(internal.ToFile(n))
	if err != nil {
		e.fatalf("Failed to format %s: %v\n", name, err)
	}
	a.Files = append(a.Files, txtar.File{Name: name, Data: b})
}
