package main

import (
    "github.com/ant0ine/go-json-rest/rest"
    "log"
    "net/http"
    "sync"
    "time"
    "net"
	"container/ring"
)

func main() {
    handler := rest.ResourceHandler{
        EnableRelaxedContentType: true,
    }

    err := handler.SetRoutes(
        &rest.Route{"GET", "/hosts", GetAll},
        &rest.Route{"POST", "/hosts", Post},
		&rest.Route{"GET", "/hosts/*address", Get},
        &rest.Route{"DELETE", "/hosts/*address", Delete},
    )
    if err != nil {
        log.Fatal(err)
    }
    log.Fatal(http.ListenAndServe(":8080", &handler))
}

type Host struct {
    Address string
}

type HostStore struct {
    Host
    LastCheck time.Time
	Values *ring.Ring

    q chan bool
}

type HostExport struct {
	Host
	LastCheck time.Time
	Avg float64
	Min float64
	Max float64
	Loss int
}

type CheckValue struct {
	Time time.Time
	Duration time.Duration
}

func (s *HostStore) Stop() {
	s.q <- true
}

func (s *HostStore) Calc() *HostExport {
	ret := &HostExport{}
	ret.Address = s.Address
	ret.LastCheck = s.LastCheck

	var values []CheckValue

	s.Values.Do(func(x interface{}) {
		val, ok := x.(*CheckValue)
		if ok {
			values = append(values, *val)
		}
	})

	ret.Loss = 0

	for i, v := range values {
		if i == 0 && v.Duration != 0 {
			ret.Avg = v.Duration.Seconds()
			ret.Max = v.Duration.Seconds()
			ret.Min = v.Duration.Seconds()
		}
		if i != 0 && v.Duration != 0 {
			ret.Avg += v.Duration.Seconds()
			if v.Duration.Seconds() > ret.Max {
				ret.Max = v.Duration.Seconds()
			}
			if v.Duration.Seconds() < ret.Min {
				ret.Min = v.Duration.Seconds()
			}
		}
		if v.Duration == 0 {
			ret.Loss++
		}
	}
	ret.Avg = ret.Avg / float64(len(values))
	return ret
}

var store = map[string]*HostStore{}

var lock = sync.RWMutex{}

func Get(w rest.ResponseWriter, r *rest.Request) {
    address := r.PathParam("address")
	log.Printf("Get %v", address)
    lock.RLock()
    var host *HostStore
    if store[address] != nil {
        host = &HostStore{}
        *host = *store[address]
    }
    lock.RUnlock()
    if host == nil {
        rest.NotFound(w,r)
        return
    }
    w.WriteJson(host.Calc())
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
	log.Println("Post")
    host := Host{}
    err := r.DecodeJsonPayload(&host)
    if err != nil {
        rest.Error(w, err.Error(), http.StatusInternalServerError)
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
    c := make(chan bool, 1)
    store[host.Address] = &HostStore{host, time.Now(), ring.New(10), c}
    go ping(ra, time.Second*3, c, &lock, store)
    lock.Unlock()
    w.WriteJson(&host)
}

func Delete(w rest.ResponseWriter, r *rest.Request) {
    address := r.PathParam("address")
	log.Printf("Delete %v", address)
    lock.Lock()
	host := store[address]
    if host != nil {
		host.Stop()
		delete(store, address)
		w.WriteHeader(http.StatusOK)
	} else {
        rest.NotFound(w,r)
	}
    lock.Unlock()
}
