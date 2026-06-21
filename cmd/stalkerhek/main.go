package main

import (
	"flag"
	"log"
	"sync"

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

	// Connect to Stalker portal
	log.Println("Connecting to Stalker middleware...")
	if err = c.Portal.Start(); err != nil {
		log.Fatalln(err)
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

	var wg sync.WaitGroup

	if c.HLS.Enabled {
		wg.Add(1)
		go func() {
			log.Println("Starting HLS service...")
			hls.SetUserAgent(c.Portal.Model)
			hls.SetDeviceHeaders(c.Portal.MAC, c.Portal.Model, c.Portal.SerialNumber)
			hls.Start(channels, c.HLS.Bind)
			wg.Done()
		}()
	}

	if c.Proxy.Enabled {
		wg.Add(1)
		go func() {
			log.Println("Starting proxy service...")
			proxy.Start(c, channels)
			wg.Done()
		}()
	}

	if c.Dashboard.Enabled {
		wg.Add(1)
		go func() {
			log.Println("Starting dashboard...")
			dashboard.Start(*flagDBDir, c.Dashboard.Bind, store, *flagProfile)
			wg.Done()
		}()
	}

	wg.Wait()
}
