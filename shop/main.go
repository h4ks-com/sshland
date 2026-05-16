package main

import (
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func main() {
	apiURL := env("SHOP_API_URL", "")
	if apiURL == "" {
		fmt.Fprintln(os.Stderr, "shop is not configured. Tell the h4ks admins.")
		fmt.Fprintln(os.Stderr, "(SHOP_API_URL not set — point at the h4kshop Next.js service)")
		os.Exit(1)
	}
	bearer := os.Getenv("OAUTH_BEARER_TOKEN")
	publicURL := env("SHOP_PUBLIC_URL", apiURL)

	client := NewShopClient(apiURL, bearer)
	p := tea.NewProgram(newModel(client, publicURL), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatalf("shop: %v", err)
	}
}
