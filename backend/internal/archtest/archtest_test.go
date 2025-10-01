// Package archtest checks the clean-architecture dependency rule for every
// feature under internal/:
//
//	http → usecase → domain ← postgres
//
// domain may use only the standard library and the uuid type; usecase may not
// touch the database or HTTP; postgres may not reach up into usecase or http.
package archtest

import (
	"go/build"
	"path/filepath"
	"strings"
	"testing"
)

const module = "github.com/Kalki767/AddisHiwot/backend"

// layerRules lists, per layer, import paths (or prefixes) that the layer must not use.
var layerRules = map[string][]string{
	"domain": {
		"github.com/jackc/", module + "/internal/db", module + "/internal/platform",
		"net/http", "database/sql", "/postgres", "/http", "/usecase",
	},
	"usecase": {
		"github.com/jackc/", module + "/internal/db", "net/http", "database/sql", "/postgres", "/http",
	},
	"postgres": {"/usecase", "/http", "net/http"},
}

// allowedOutsideStdlib are the only non-standard imports a domain package may use.
var allowedOutsideStdlib = []string{"github.com/google/uuid"}

func TestDependencyRule(t *testing.T) {
	checked := 0
	for layer, forbidden := range layerRules {
		dirs, err := filepath.Glob(filepath.Join("..", "*", layer))
		if err != nil {
			t.Fatal(err)
		}
		for _, dir := range dirs {
			pkg, err := build.ImportDir(dir, 0) // production files only, not tests
			if err != nil {
				t.Fatalf("%s: %v", dir, err)
			}
			checked++
			for _, imp := range pkg.Imports {
				for _, bad := range forbidden {
					if imp == bad || strings.HasPrefix(imp, bad) || (strings.HasPrefix(bad, "/") && strings.HasSuffix(imp, bad)) {
						t.Errorf("%s (%s layer) imports %s, which breaks the dependency rule", dir, layer, imp)
					}
				}
				if layer == "domain" && strings.Contains(strings.SplitN(imp, "/", 2)[0], ".") && !allowed(imp) {
					t.Errorf("%s (domain layer) imports %s; domain may use only the standard library and %v", dir, imp, allowedOutsideStdlib)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no domain, usecase or postgres packages found: the check would pass without checking anything")
	}
}

func allowed(imp string) bool {
	for _, a := range allowedOutsideStdlib {
		if imp == a {
			return true
		}
	}
	return false
}
