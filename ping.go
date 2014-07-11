package main

import (
	"github.com/tatsushid/go-fastping"
	"log"
	"net"
	"sync"
	"time"
)

type response struct {
	addr *net.IPAddr
	rtt  time.Duration
}

func ping(host string, ra *net.IPAddr, rtt time.Duration, c chan bool, lock *sync.RWMutex, store map[string]*HostStore) {
	p := fastping.NewPinger()

	results := make(map[string]*response)
	results[ra.String()] = nil
	p.AddIPAddr(ra)

	onRecv, onIdle := make(chan *response), make(chan bool)
	p.AddHandler("receive", func(addr *net.IPAddr, t time.Duration) {
		onRecv <- &response{addr: addr, rtt: t}
	})
	p.AddHandler("idle", func() {
		onIdle <- true
	})

	p.MaxRTT = rtt
	quit, errch := p.RunLoop()

	wait := make(chan bool)

loop:
	for {
		select {
		case <-c:
			log.Printf("get interrupted %v", ra)
			quit <- wait
		case res := <-onRecv:
			if _, ok := results[res.addr.String()]; ok {
				results[res.addr.String()] = res
			}
		case <-onIdle:
			for v, r := range results {
				lock.Lock()
				if r == nil {
					store[host].Insert(0)
				} else {
					store[host].Insert(r.rtt)
				}
				lock.Unlock()
				results[v] = nil
			}
		case err := <-errch:
			log.Println("%v failed: %v", ra, err)
			c <- true
		case <-wait:
			break loop
		}
	}
	log.Printf("exit %v", ra)
}
