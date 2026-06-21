// Package mobile provides gomobile bindings for stalkerhek.
// Compile with: gomobile bind -target=android -androidapi 21 ./mobile
package mobile

import (
	"log"
	"os"
	"path/filepath"

	"github.com/erkexzcx/stalkerhek/dashboard"
	"github.com/erkexzcx/stalkerhek/db"
	"github.com/erkexzcx/stalkerhek/hls"
	"github.com/erkexzcx/stalkerhek/proxy"
	"github.com/erkexzcx/stalkerhek/stalker"
)

var (
	store    *db.Store
	channels map[string]*stalker.Channel
	genres   map[string]string
)

// StartServer starts the stalkerhek proxy, HLS, and dashboard services.
// dbDir is the directory for stalkerhek.db (usually app's internal storage).
// profileName is the profile to load from the database.
// Returns the dashboard port as a string.
func StartServer(dbDir string, profileName string) string {
	log.Println("stalkerhek: starting server...")

	// Open database
	var err error
	store, err = db.Open(filepath.Join(dbDir, "stalkerhek.db"))
	if err != nil {
		log.Printf("Failed to open DB: %v", err)
		return "error: " + err.Error()
	}

	// Load profile
	c, err := stalker.LoadProfile(store, profileName)
	if err != nil {
		log.Printf("Failed to load profile: %v", err)
		return "error: " + err.Error()
	}

	// Connect to portal
	log.Println("Connecting to Stalker middleware...")
	if err = c.Portal.Start(); err != nil {
		// Portal might be unreachable — start services anyway for dashboard
		log.Printf("Portal connection failed (services will still start): %v", err)
	}

	// Try to retrieve channels
	channels, err = c.Portal.RetrieveChannels()
	if err != nil {
		log.Printf("Channel retrieval failed: %v", err)
		channels = make(map[string]*stalker.Channel)
	}
	for _, ch := range channels {
		if ch.Genres != nil {
			genres = *ch.Genres
			break
		}
	}

	// Start HLS service
	go func() {
		log.Println("Starting HLS service...")
		hls.SetUserAgent(c.Portal.Model)
		hls.SetDeviceHeaders(c.Portal.MAC, c.Portal.Model, c.Portal.SerialNumber)
		hls.Start(channels, c.HLS.Bind)
	}()

	// Start proxy service
	go func() {
		log.Println("Starting proxy service...")
		proxy.Start(c, channels)
	}()

	// Start dashboard
	if c.Dashboard.Bind == "" {
		c.Dashboard.Bind = "0.0.0.0:8080"
	}
	go func() {
		log.Println("Starting dashboard...")
		dashboard.Start(dbDir, c.Dashboard.Bind, store, profileName, channels, genres)
	}()

	// Extract port from bind address
	port := "8080"
	if c.Dashboard.Bind != "" {
		parts := filepath.SplitList(c.Dashboard.Bind)
		if len(parts) > 0 {
			_, p, _ := os.Hostname()
			_ = p
		}
		// Parse bind like "0.0.0.0:8080"
		for i := len(c.Dashboard.Bind) - 1; i >= 0; i-- {
			if c.Dashboard.Bind[i] == ':' {
				port = c.Dashboard.Bind[i+1:]
				break
			}
		}
	}

	log.Printf("stalkerhek: services started on port %s", port)
	return port
}
