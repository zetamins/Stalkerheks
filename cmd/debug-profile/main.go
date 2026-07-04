package main

import (
	"flag"
	"log"
	"time"

	"github.com/erkexzcx/stalkerhek/db"
	"github.com/erkexzcx/stalkerhek/stalker"
)

func main() {
	profileName := flag.String("profile", "atk97-online", "profile name to test")
	flag.Parse()

	log.Printf("Loading profile: %s", *profileName)
	store, err := db.Open("./stalkerhek.db")
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	c, err := stalker.LoadProfile(store, *profileName)
	if err != nil {
		log.Fatalf("Failed to load profile: %v", err)
	}

	log.Printf("Connecting to portal URL: %s", c.Portal.Location)
	start := time.Now()
	err = c.Portal.Start()
	if err != nil {
		log.Fatalf("Handshake failed after %v: %v", time.Since(start), err)
	}
	log.Printf("Handshake succeeded in %v", time.Since(start))

	log.Println("Retrieving channels list...")
	start = time.Now()
	channels, err := c.Portal.RetrieveChannels()
	if err != nil {
		log.Fatalf("RetrieveChannels failed after %v: %v", time.Since(start), err)
	}
	log.Printf("Successfully retrieved %d channels in %v", len(channels), time.Since(start))

	// Print first 5 channels as sample
	count := 0
	for name, ch := range channels {
		if count >= 5 {
			break
		}
		log.Printf("Channel Sample: Name=%q, CMD=%q, GenreID=%q", name, ch.CMD, ch.GenreID)
		count++
	}
}
