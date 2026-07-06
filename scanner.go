package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// modelEntry holds the information about a single GORM model struct collected
// during AST scanning.
type modelEntry struct {
	// PkgAlias is the import alias for the package that defines the struct.
	// It is empty when the output package is the same as the model package
	// (same-package mode), so that references are emitted without a qualifier.
	PkgAlias string
	// TypeName is the unqualified struct name, e.g. "User".
	TypeName string
	// deps is the list of struct names that this model BelongsTo, i.e. the
	// referenced tables that must exist before this one can have its foreign
	// key constraints created.
	deps []string
}

// scanConfig holds the parameters that control which structs are collected.
type scanConfig struct {
	// baseEmbeds is the set of anonymous-embed names that qualify a struct for
	// inclusion (e.g. "BaseModel", "gorm.Model").
	baseEmbeds map[string]bool
	// enableMarker is the comment text that opts a struct in explicitly.
	enableMarker string
	// disableMarker is the comment text that opts a struct out explicitly.
	disableMarker string
	// tagKey is the struct-tag key used to locate foreign-key declarations
	// (typically "gorm").
	tagKey string
	// outputPkg is the name of the generated output package, used to detect
	// same-package mode and suppress the self-import.
	outputPkg string
	// modulePath is the Go module path read from go.mod, used to build import
	// paths for discovered packages.
	modulePath string
	// moduleRoot is the on-disk path of the module root directory.
	moduleRoot string
}

// scanDirs walks each directory in dirs, parses every .go file with the Go
// AST parser, and returns the list of model entries together with the import
// map needed by the code generator.
func scanDirs(dirs []string, cfg scanConfig) ([]modelEntry, map[string]string, error) {
	fset := token.NewFileSet()
	imports := make(map[string]string) // alias → import path
	var models []modelEntry

	for _, dir := range dirs {
		var (
			pkgName  string // Go package name of the directory
			pkgAlias string // alias used in the generated file
		)

		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}

			file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				return err
			}

			// Initialise package metadata on the first file seen in this dir.
			if pkgName == "" {
				pkgName = file.Name.Name
				pkgAlias = pkgName

				absDir, _ := filepath.Abs(dir)
				rel, _ := filepath.Rel(cfg.moduleRoot, absDir)

				if cfg.outputPkg != pkgName {
					// Different package: record an import for the alias.
					imports[pkgAlias] = filepath.ToSlash(filepath.Join(cfg.modulePath, rel))
				} else {
					// Same package as output: references need no qualifier.
					pkgAlias = ""
				}
			}

			for _, decl := range file.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.TYPE {
					continue
				}

				for _, spec := range gd.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok {
						continue
					}

					// disable marker takes precedence over everything else.
					if hasMarker(gd.Doc, cfg.disableMarker) || hasMarker(ts.Doc, cfg.disableMarker) {
						continue
					}

					// A struct is included if it embeds one of the base types
					// or carries the explicit enable marker.
					if !embedsBase(st, cfg.baseEmbeds) &&
						!hasMarker(gd.Doc, cfg.enableMarker) &&
						!hasMarker(ts.Doc, cfg.enableMarker) {
						continue
					}

					models = append(models, modelEntry{
						PkgAlias: pkgAlias,
						TypeName: ts.Name.Name,
						deps:     belongsToDeps(ts.Name.Name, st, cfg.tagKey),
					})
				}
			}
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	return models, imports, nil
}

// hasMarker reports whether any comment in cg contains the given marker text.
func hasMarker(cg *ast.CommentGroup, marker string) bool {
	if cg == nil {
		return false
	}
	for _, c := range cg.List {
		if strings.Contains(c.Text, marker) {
			return true
		}
	}
	return false
}

// embedsBase reports whether st has an anonymous (embedded) field whose name
// is in the baseEmbeds set.  Both plain identifiers ("BaseModel") and
// selector expressions ("gorm.Model") are handled.
func embedsBase(st *ast.StructType, bases map[string]bool) bool {
	for _, f := range st.Fields.List {
		if len(f.Names) > 0 {
			continue // named field, not an embed
		}
		name := embeddedName(f.Type)
		if name != "" && bases[name] {
			return true
		}
	}
	return false
}

// embeddedName extracts the base name of an embedded type expression.
// It returns "BaseModel" for *ast.Ident{Name:"BaseModel"} and "gorm.Model"
// for *ast.SelectorExpr{X:"gorm", Sel:"Model"}.  Pointer embeds are also
// supported.
func embeddedName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return embeddedName(t.X)
	case *ast.SelectorExpr:
		if pkg, ok := t.X.(*ast.Ident); ok {
			return pkg.Name + "." + t.Sel.Name
		}
	}
	return ""
}

// belongsToDeps returns the struct names that typeName BelongsTo.
//
// A BelongsTo relationship is detected by a struct field that:
//  1. Has a struct tag with the configured tag key containing "foreignKey".
//  2. The foreignKey value matches the name of a field that actually exists
//     in the current struct (i.e. this struct holds the FK column, not the
//     other side).
//  3. The referenced type is different from typeName itself (no self-reference).
//
// HasMany and HasOne fields are intentionally excluded because they do not
// create a FK column in this table and therefore impose no migration ordering
// constraint here.
func belongsToDeps(typeName string, st *ast.StructType, tagKey string) []string {
	// Build the set of field names defined in this struct.
	ownFields := make(map[string]struct{})
	for _, f := range st.Fields.List {
		for _, id := range f.Names {
			ownFields[id.Name] = struct{}{}
		}
	}

	var deps []string
	for _, f := range st.Fields.List {
		if f.Tag == nil {
			continue
		}
		rawTag := strings.Trim(f.Tag.Value, "`")
		tagVal := reflect.StructTag(rawTag).Get(tagKey)
		if !strings.Contains(tagVal, "foreignKey") {
			continue
		}

		// Extract the foreignKey value, e.g. "UserID" from "foreignKey:UserID;…".
		fkField := tagSetting(tagVal, "foreignKey")
		if fkField == "" {
			continue
		}
		// Only count this as a BelongsTo if the FK field lives in this struct.
		if _, ok := ownFields[fkField]; !ok {
			continue
		}

		refName := fieldTypeName(f.Type)
		if refName == "" || refName == typeName {
			continue
		}
		deps = append(deps, refName)
	}
	return deps
}

// fieldTypeName extracts the unqualified type name from a field type
// expression, following pointers and slices.  It returns "" for types it
// cannot handle (maps, channels, etc.).
func fieldTypeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return fieldTypeName(t.X)
	case *ast.ArrayType:
		return fieldTypeName(t.Elt)
	}
	return ""
}

// tagSetting returns the value associated with key inside a semicolon-separated
// GORM tag string (e.g. tagSetting("foreignKey:UserID;references:ID", "foreignKey")
// returns "UserID").
func tagSetting(tag, key string) string {
	for part := range strings.SplitSeq(tag, ";") {
		part = strings.TrimSpace(part)
		if k, v, ok := strings.Cut(part, ":"); ok && k == key {
			return v
		}
	}
	return ""
}
