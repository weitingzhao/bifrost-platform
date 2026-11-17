package server

import (
	"net/http"
	"sort"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

// GET /api/v1/bus/contract: what the bus promises and to whom, read from
// the registers compiled into the binary (bus/registry): every topic with
// its version, publisher and who may read it; every subscription row; every
// declared user. The Bus page draws it as publisher → topic → subscriber;
// who is live or lost comes from /bus/clients beside it.

type contractTopic struct {
	Tenant    string   `json:"tenant"`
	Type      string   `json:"type"`
	Version   int      `json:"version"`
	Publisher string   `json:"publisher"`
	Guarantee string   `json:"guarantee"`
	Consumers []string `json:"consumers"`
	Exports   []string `json:"exports,omitempty"`
}

type contractRow struct {
	Tenant  string   `json:"tenant"`
	ID      string   `json:"id"`
	On      []string `json:"on"`
	Deliver string   `json:"deliver"`
	To      string   `json:"to,omitempty"`
	Action  string   `json:"action,omitempty"`
	By      string   `json:"by"`
	Envs    []string `json:"envs,omitempty"`
}

type contractClient struct {
	Tenant    string   `json:"tenant"`
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	LostAfter string   `json:"lost_after,omitempty"`
	Envs      []string `json:"envs,omitempty"`
}

type busContract struct {
	Topics        []contractTopic  `json:"topics"`
	Subscriptions []contractRow    `json:"subscriptions"`
	Clients       []contractClient `json:"clients"`
}

func (s *Server) handleBusContract(w http.ResponseWriter, _ *http.Request) {
	tenants, err := registry.Load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	subs, err := registry.LoadSubscriptions()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := busContract{Topics: []contractTopic{}, Subscriptions: []contractRow{}, Clients: []contractClient{}}
	for name, t := range tenants {
		for typ, ch := range t.Channels {
			out.Topics = append(out.Topics, contractTopic{Tenant: name, Type: typ, Version: ch.Version, Publisher: ch.Publisher,
				Guarantee: ch.Guarantee, Consumers: ch.Consumers, Exports: ch.Exports})
		}
	}
	for name, st := range subs {
		for _, r := range st.Rows {
			out.Subscriptions = append(out.Subscriptions, contractRow{Tenant: name, ID: r.ID, On: r.On, Deliver: r.Deliver, To: r.To,
				Action: r.Action, By: r.By, Envs: r.Envs})
		}
		for _, c := range st.Clients {
			out.Clients = append(out.Clients, contractClient{Tenant: name, Name: c.Name, Kind: c.Kind, LostAfter: c.LostAfter, Envs: c.Envs})
		}
	}
	sort.Slice(out.Topics, func(i, j int) bool {
		return out.Topics[i].Tenant+out.Topics[i].Type < out.Topics[j].Tenant+out.Topics[j].Type
	})
	sort.Slice(out.Subscriptions, func(i, j int) bool {
		return strings.Compare(out.Subscriptions[i].Tenant+out.Subscriptions[i].ID, out.Subscriptions[j].Tenant+out.Subscriptions[j].ID) < 0
	})
	sort.Slice(out.Clients, func(i, j int) bool {
		return out.Clients[i].Tenant+out.Clients[i].Name < out.Clients[j].Tenant+out.Clients[j].Name
	})
	writeJSON(w, http.StatusOK, out)
}
