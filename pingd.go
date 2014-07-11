package main

import (
	"container/ring"
	"flag"
	"github.com/ant0ine/go-json-rest/rest"
	"github.com/marpaia/graphite-golang"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const DefGraph = "127.0.0.1:2003"
const DefApi = ":8081"
const DefRTT = 3        // Seconds
const DefCircleLen = 10 // length circular buffer for ping duration results
const DefMainMetric = "pingd"

var graph *graphite.Graphite

func main() {
	var apiVar string
	var graphVar string

	flag.StringVar(&apiVar, "api", DefApi, "api host:port")
	flag.StringVar(&graphVar, "graphite", DefGraph, "graphite export host port")
	flag.Parse()

	graphHost, graphPort, err := net.SplitHostPort(graphVar)
	if err != nil {
		log.Fatal("graphite host:port ", err)
	}
	graphPortInt, err := strconv.Atoi(graphPort)
	if err != nil {
		log.Fatal("graphite port ", err)
	}

	graphite, err := graphite.NewGraphite(graphHost, graphPortInt)
	if err != nil {
		log.Fatal(err)
	}
	graph = graphite

	handler := rest.ResourceHandler{
		EnableRelaxedContentType: true,
	}

	err = handler.SetRoutes(
		&rest.Route{"GET", "/hosts", GetAll},
		&rest.Route{"POST", "/hosts", Post},
		&rest.Route{"GET", "/hosts/:id", Get},
		&rest.Route{"DELETE", "/hosts/:id", Delete},
	)
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(http.ListenAndServe(apiVar, &handler))
}

type Host struct {
	Id      string
	Address string
}

type HostStore struct {
	Host
	LastCheck time.Time
	Avg       float64
	Min       float64
	Max       float64
	Last      float64
	Loss      int

	values *ring.Ring
	q      chan bool
}

type CheckValue struct {
	Time     time.Time
	Duration time.Duration
}

func (s *HostStore) Stop() {
	s.q <- true
}

func (s *HostStore) Insert(rtt time.Duration) {
	now := time.Now()
	s.LastCheck = now
	s.values.Value = &CheckValue{now, rtt}
	s.values = s.values.Next()
	var n int
	n = 0
	s.Loss = 0

	s.values.Do(func(x interface{}) {
		v, ok := x.(*CheckValue)
		if ok {
			if n == 0 && v.Duration != 0 {
				s.Avg = v.Duration.Seconds()
				s.Max = v.Duration.Seconds()
				s.Min = v.Duration.Seconds()
				s.Last = v.Duration.Seconds()
			}
			if n != 0 && v.Duration != 0 {
				s.Avg += v.Duration.Seconds()
				if v.Duration.Seconds() > s.Max {
					s.Max = v.Duration.Seconds()
				}
				if v.Duration.Seconds() < s.Min {
					s.Min = v.Duration.Seconds()
				}
				s.Last = v.Duration.Seconds()
			}
			if v.Duration == 0 {
				s.Loss++
			}
			n++
		}
	})

	s.Avg = s.Avg / float64(n)

	metric := DefMainMetric + "." + s.Id + "."

	go graph.SendMetric(graphite.Metric{metric + "rtt", strconv.FormatFloat(s.Last, 'g', 12, 64), now.Unix()})
	go graph.SendMetric(graphite.Metric{metric + "avg", strconv.FormatFloat(s.Avg, 'g', 12, 64), now.Unix()})
	go graph.SendMetric(graphite.Metric{metric + "max", strconv.FormatFloat(s.Max, 'g', 12, 64), now.Unix()})
	go graph.SendMetric(graphite.Metric{metric + "min", strconv.FormatFloat(s.Min, 'g', 12, 64), now.Unix()})
	go graph.SendMetric(graphite.Metric{metric + "loss", strconv.FormatInt(int64(s.Loss), 10), now.Unix()})
}

var store = map[string]*HostStore{}

var lock = sync.RWMutex{}

func Get(w rest.ResponseWriter, r *rest.Request) {
	id := r.PathParam("id")
	lock.RLock()
	var host *HostStore
	if store[id] != nil {
		host = &HostStore{}
		*host = *store[id]
	}
	lock.RUnlock()
	if host == nil {
		rest.NotFound(w, r)
		return
	}
	w.WriteJson(host)
}

func GetAll(w rest.ResponseWriter, r *rest.Request) {
	lock.RLock()
	hosts := make([]HostStore, len(store))
	i := 0
	for _, host := range store {
		hosts[i] = *host
		i++
	}
	lock.RUnlock()
	w.WriteJson(&hosts)
}

func Post(w rest.ResponseWriter, r *rest.Request) {
	host := Host{}
	err := r.DecodeJsonPayload(&host)
	if err != nil {
		rest.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if host.Id == "" {
		rest.Error(w, "id required", 400)
		return
	}
	if host.Address == "" {
		rest.Error(w, "address required", 400)
		return
	}
	ra, err := net.ResolveIPAddr("ip4:icmp", host.Address)
	if err != nil {
		rest.Error(w, err.Error(), 400)
		return
	}
	lock.Lock()
	q := make(chan bool, 1) // chan for stop ping
	store[host.Id] = &HostStore{
		host,
		time.Now(),
		0.0, 0.0, 0.0, 0.0,
		0,
		ring.New(DefCircleLen),
		q}
	go ping(host.Id, ra, time.Second*DefRTT, q, &lock, store)
	lock.Unlock()
	w.WriteJson(&host)
}

func Delete(w rest.ResponseWriter, r *rest.Request) {
	id := r.PathParam("id")
	lock.Lock()
	host := store[id]
	if host != nil {
		host.Stop()
		delete(store, id)
		w.WriteHeader(http.StatusOK)
	} else {
		rest.NotFound(w, r)
	}
	lock.Unlock()
}
