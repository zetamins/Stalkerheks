package main

import (
	"flag"
	"log"
	"sync"
	"time"

	"github.com/erkexzcx/stalkerhek/dashboard"
	"github.com/erkexzcx/stalkerhek/db"
	"github.com/erkexzcx/stalkerhek/hls"
	"github.com/erkexzcx/stalkerhek/proxy"
	"github.com/erkexzcx/stalkerhek/stalker"
)

var (
	flagProfile = flag.String("profile", "default", "profile name to load from stalkerhek.db")
	flagDBDir   = flag.String("db", ".", "directory containing stalkerhek.db")
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	flag.Parse()

	// Open profile database
	store, err := db.Open(*flagDBDir + "/stalkerhek.db")
	if err != nil {
		log.Fatalln("Failed to open database:", err)
	}

	// Load profile
	c, err := stalker.LoadProfile(store, *flagProfile)
	if err != nil {
		log.Fatalln("Failed to load profile:", err)
	}

	// Connect to Stalker portal with retry — matches STB's DHCP/NTP wait behavior.
	// The STB waits up to 30s for network; we retry up to 5 times with backoff.
	log.Println("Connecting to Stalker middleware...")
	for attempt := 1; attempt <= 5; attempt++ {
		err = c.Portal.Start()
		if err == nil {
			break
		}
		if attempt < 5 {
			wait := time.Duration(attempt) * 3 * time.Second
			log.Printf("Connection attempt %d failed: %v — retrying in %v", attempt, err, wait)
			time.Sleep(wait)
		} else {
			log.Fatalln("Failed to connect after 5 attempts:", err)
		}
	}

	// Retrieve channels list
	log.Println("Retrieving channels list from Stalker middleware...")
	channels, err := c.Portal.RetrieveChannels()
	if err != nil {
		log.Fatalln(err)
	}
	if len(channels) == 0 {
		log.Fatalln("no IPTV channels retrieved from Stalker middleware. quitting...")
	}

	// Real STBs dispatch get_all_channels (and other loads) before their
	// first watchdog send, so start the watchdog only after RetrieveChannels
	// above, not as part of Portal.Start().
	if c.HLS.Enabled {
		c.Portal.IsPlayingFunc = hls.IsPlaying
	}
	if err := c.Portal.StartWatchdog(); err != nil {
		log.Fatalln("Failed to start watchdog:", err)
	}

	var wg sync.WaitGroup

	if c.HLS.Enabled {
		hls.SetUserAgent(c.Portal.Model)
		hls.SetDeviceHeaders(c.Portal.MAC, c.Portal.Model, c.Portal.SerialNumber)
		hls.SetChannels(channels)
		wg.Add(1)
		go func() {
			log.Println("Starting HLS service...")
			hls.Serve(c.HLS.Bind)
			wg.Done()
		}()
	}

	if c.Proxy.Enabled {
		proxy.SetChannels(channels)
		wg.Add(1)
		go func() {
			log.Println("Starting proxy service...")
			proxy.Serve(c, c.Proxy.Bind)
			wg.Done()
		}()
	}

	if c.Dashboard.Enabled {
		// Register this binary's own profile as running with a real stop
		// function (stop watchdog/HLS/proxy) rather than a fake
		// self-referencing process handle — stopping/deleting this profile
		// from the dashboard UI must not Kill() this process's own PID.
		dashboard.MarkRunning(*flagProfile, func() {
			c.Portal.StopWatchdog()
			if c.HLS.Enabled {
				hls.Stop()
			}
			if c.Proxy.Enabled {
				proxy.Stop()
			}
		})

		wg.Add(1)
		go func() {
			log.Println("Starting dashboard...")
			dashboard.Start(*flagDBDir, c.Dashboard.Bind, store)
			wg.Done()
		}()
	}

	wg.Wait()
}
