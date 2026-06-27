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

	// Tee logs to <db>/<profile>.log so the dashboard's "View Logs" works even
	// when the engine is launched directly (not spawned by the dashboard).
	dashboard.SetupProfileLogging(*flagDBDir, *flagProfile)

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

	// Connect to Stalker portal with retry
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

	// Retrieve radio channels (non-fatal)
	radioChannels, err := c.Portal.RetrieveRadioChannels()
	if err != nil {
		log.Println("Radio channels not available (continuing):", err)
	}

	// Create per-profile HLS and proxy instances for multi-profile isolation.
	hlsInst := hls.NewInstance()
	proxyInst := proxy.NewInstance(c)

	if c.HLS.Enabled {
		hlsInst.SetUserAgent(c.Portal.Model)
		hlsInst.SetDeviceHeaders(c.Portal.MAC, c.Portal.Model, c.Portal.SerialNumber)
		hlsInst.SetChannels(channels)
		c.Portal.IsPlayingFunc = hlsInst.IsPlaying
	}

	if c.Proxy.Enabled {
		proxyInst.SetChannels(channels)
		if radioChannels != nil {
			proxyInst.SetRadioChannels(radioChannels)
		}
	}

	// Real STBs dispatch get_all_channels (and other loads) before their
	// first watchdog send.
	if c.HLS.Enabled {
		c.Portal.IsPlayingFunc = hlsInst.IsPlaying
	}
	if err := c.Portal.StartWatchdog(); err != nil {
		log.Fatalln("Failed to start watchdog:", err)
	}

	var wg sync.WaitGroup

	if c.HLS.Enabled {
		wg.Add(1)
		go func() {
			log.Println("Starting HLS service...")
			hlsInst.Serve(c.HLS.Bind)
			wg.Done()
		}()
	}

	if c.Proxy.Enabled {
		wg.Add(1)
		go func() {
			log.Println("Starting proxy service...")
			proxyInst.Serve(c.Proxy.Bind)
			wg.Done()
		}()
	}

	if c.Dashboard.Enabled {
		// Register this binary's own profile as running with a real stop
		// function rather than a fake self-referencing process handle.
		dashboard.MarkRunning(*flagProfile, func() {
			c.Portal.StopWatchdog()
			if c.HLS.Enabled {
				hlsInst.Stop()
			}
			if c.Proxy.Enabled {
				proxyInst.Stop()
			}
		})

		// Also set the backward-compat defaults so the dashboard's
		// package-level API (used by startProfileInProcess) works.
		hls.SetChannels(channels)
		proxy.SetChannels(channels)
		if radioChannels != nil {
			proxy.SetRadioChannels(radioChannels)
		}

		wg.Add(1)
		go func() {
			log.Println("Starting dashboard...")
			dashboard.Start(*flagDBDir, c.Dashboard.Bind, store)
			wg.Done()
		}()
	}

	wg.Wait()
}
