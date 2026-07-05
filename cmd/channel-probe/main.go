package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"time"

	"github.com/erkexzcx/stalkerhek/db"
	"github.com/erkexzcx/stalkerhek/stalker"
)

// probePacingDelay is the pause between successive channels' create_link
// calls. The underlying stream gateway rate-limits token issuance per-MAC to
// ~1-2 tokens before a multi-minute cooldown (fix1.md §4.2/§5.5) — looping
// over channels back-to-back with no delay reliably trips it, turning every
// channel after the first couple into a spurious LINK_ERROR. This delay is
// a best-effort mitigation, not a guarantee: it keeps a small probe run
// comfortably under the observed rate, but it can't out-pace the limit
// indefinitely for a large --count.
const probePacingDelay = 2 * time.Second

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
		if i > 0 {
			time.Sleep(probePacingDelay)
		}
		ch := channels[names[i]]
		link, err := ch.NewLink(true)
		if err != nil {
			fmt.Fprintf(os.Stdout, "RESULT\t%s\t%s\tLINK_ERROR\t%s\n", *profileName, ch.Title, err)
			continue
		}
		fmt.Fprintf(os.Stdout, "RESULT\t%s\t%s\tOK\t%s\n", *profileName, ch.Title, link)
	}
}
