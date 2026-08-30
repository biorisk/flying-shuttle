package web_test

import (
	"strings"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/web"
	"github.com/biorisk/flying-shuttle/internal/web/components"
)

func TestBaseRenders(t *testing.T) {
	html := web.RenderString(components.Base("Flying Shuttle"))
	for _, want := range []string{"<!doctype html>", "<title>Flying Shuttle</title>", web.DatastarScriptPath, "/static/app.css"} {
		if !strings.Contains(strings.ToLower(html), strings.ToLower(want)) {
			t.Fatalf("Base() output missing %q\n%s", want, html)
		}
	}
}

func TestStaticFSHasDatastar(t *testing.T) {
	f, err := web.StaticFS().Open("vendor/datastar-v1.0.3.js")
	if err != nil {
		t.Fatalf("datastar runtime not embedded: %v", err)
	}
	f.Close()
}
