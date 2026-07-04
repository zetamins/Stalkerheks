package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/erkexzcx/stalkerhek/db"
	"github.com/erkexzcx/stalkerhek/stalker"
)

func main() {
	profileName := flag.String("profile", "atk97-online", "profile name to test")
	count := flag.Int("count", 5, "number of channels to resolve")
	flag.Parse()

	store, err := db.Open("./stalkerhek.db")
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	c, err := stalker.LoadProfile(store, *profileName)
	if err != nil {
		log.Fatalf("Failed to load profile: %v", err)
	}

	if err := c.Portal.Start(); err != nil {
		log.Fatalf("Handshake failed: %v", err)
	}

	channels, err := c.Portal.RetrieveChannels()
	if err != nil {
		log.Fatalf("RetrieveChannels failed: %v", err)
	}

	// Deterministic ordering so repeated runs pick the same channels.
	names := make([]string, 0, len(channels))
	for name := range channels {
		names = append(names, name)
	}
	sort.Strings(names)

	n := *count
	if n > len(names) {
		n = len(names)
	}

	for i := 0; i < n; i++ {
		ch := channels[names[i]]
		link, err := ch.NewLink(true)
		if err != nil {
			fmt.Fprintf(os.Stdout, "RESULT\t%s\t%s\tLINK_ERROR\t%s\n", *profileName, ch.Title, err)
			continue
		}
		fmt.Fprintf(os.Stdout, "RESULT\t%s\t%s\tOK\t%s\n", *profileName, ch.Title, link)
	}
}
