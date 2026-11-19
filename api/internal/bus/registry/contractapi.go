package registry

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
)

// GET /api/v1/bus/contract: what the bus promises and to whom, read from
// the registers compiled into the binary: every topic with
// its version, publisher and who may read it; every subscription row; every
// declared user. The Bus page draws it as publisher → topic → subscriber;
// who is live or lost comes from /bus/clients beside it.

type contractTopic struct {
	Tenant    string   `json:"tenant"`
	Type      string   `json:"type"`
	Version   int      `json:"version"`
	Publisher string   `json:"publisher"`
	Domain    string   `json:"domain"`
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
	Domain  string   `json:"domain,omitempty"`
}

type contractClient struct {
	Tenant    string   `json:"tenant"`
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	LostAfter string   `json:"lost_after,omitempty"`
	Envs      []string `json:"envs,omitempty"`
	Domain    string   `json:"domain,omitempty"`
}

type busContract struct {
	Topics        []contractTopic  `json:"topics"`
	Subscriptions []contractRow    `json:"subscriptions"`
	Clients       []contractClient `json:"clients"`
}

// HandleContract is the route.
func HandleContract(w http.ResponseWriter, _ *http.Request) {
	tenants, err := Load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	subs, err := LoadSubscriptions()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := busContract{Topics: []contractTopic{}, Subscriptions: []contractRow{}, Clients: []contractClient{}}
	for name, t := range tenants {
		for typ, ch := range t.Channels {
			out.Topics = append(out.Topics, contractTopic{Tenant: name, Type: typ, Version: ch.Version, Publisher: ch.Publisher, Domain: ch.Domain,
				Guarantee: ch.Guarantee, Consumers: ch.Consumers, Exports: ch.Exports})
		}
	}
	for name, st := range subs {
		for _, r := range st.Rows {
			out.Subscriptions = append(out.Subscriptions, contractRow{Tenant: name, ID: r.ID, On: r.On, Deliver: r.Deliver, To: r.To,
				Action: r.Action, By: r.By, Envs: r.Envs, Domain: r.TargetDomain()})
		}
		for _, c := range st.Clients {
			out.Clients = append(out.Clients, contractClient{Tenant: name, Name: c.Name, Kind: c.Kind, LostAfter: c.LostAfter, Envs: c.Envs, Domain: c.Domain})
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
