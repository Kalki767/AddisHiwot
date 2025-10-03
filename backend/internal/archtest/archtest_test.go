// Package archtest checks the clean-architecture dependency rule for every
// feature under internal/:
//
//	http → usecase → domain ← postgres
//
// domain may use only the standard library, uuid and the pure page and validate
// packages; usecase may reach our other packages only through their domain;
// postgres may not reach up into usecase or http; http may not touch the database.
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
		"github.com/jackc/", module + "/internal/db", module + "/internal/platform/httpx",
		module + "/internal/platform/database", module + "/internal/platform/config",
		module + "/internal/platform/health", module + "/internal/platform/testdb",
		"net/http", "database/sql", "/postgres", "/http", "/usecase",
	},
	"usecase": {
		"github.com/jackc/", module + "/internal/db", "net/http", "database/sql", "/postgres", "/http",
	},
	"postgres": {"/usecase", "/http", "net/http"},
	"http":     {"github.com/jackc/", module + "/internal/db", "/postgres", "database/sql"},
}

// allowedOutsideStdlib are the only non-standard imports a domain package may
// use: the uuid type and our two pure packages (no I/O, no HTTP, no database).
var allowedOutsideStdlib = []string{
	"github.com/google/uuid",
	module + "/internal/platform/page",
	module + "/internal/platform/validate",
}

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
				// A use case may reach other packages of ours only through their domain.
				if layer == "usecase" && strings.HasPrefix(imp, module+"/internal/") && !strings.HasSuffix(imp, "/domain") && !allowed(imp) {
					t.Errorf("%s (usecase layer) imports %s; use cases may use only domain packages and %v", dir, imp, allowedOutsideStdlib[1:])
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
