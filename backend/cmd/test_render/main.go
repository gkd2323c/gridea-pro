package main

import (
	"fmt"
	"gridea-pro/backend/internal/render"
	"os"
)

func main() {
	appDir := "/Users/eric/Documents/Gridea Pro"
	themeName := "amore-jinja2"

	factory := render.NewRendererFactory(appDir, themeName)
	r, err := factory.CreateRenderer()
	if err != nil {
		fmt.Printf("Engine create err: %v\n", err)
		os.Exit(1)
	}

	_, err = r.Render("partials/global-seo", nil)
	if err != nil {
		fmt.Printf("RENDER ERROR HEAD: %v\n", err)
	}

	_, err = r.Render("partials/scripts", nil)
	if err != nil {
		fmt.Printf("RENDER ERROR INDEX-SEO: %v\n", err)
	}

	html3, err := r.Render("index", nil)
	if err != nil {
		fmt.Printf("RENDER ERROR INDEX: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("SUCCESS, len=", len(html3))
}
