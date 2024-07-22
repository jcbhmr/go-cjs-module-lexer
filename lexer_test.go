package cjsmodulelexer_test

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	cjsmodulelexer "github.com/jcbhmr/go-cjs-module-lexer"
)

func TestParse(t *testing.T) {
	files := []string{
		"testdata/angular.js",
		"testdata/magic-string.js",
	}
	for _, file := range files {
		js, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("os.ReadFile() %s: %v", file, err)
		}
		exports, err := cjsmodulelexer.Parse(string(js), sql.NullString{String: file, Valid: true})
		if err != nil {
			t.Fatalf("cjsmodulelexer.Parse() %s: %v", file, err)
		}
		fmt.Printf("%#+v\n", exports)
	}
}
