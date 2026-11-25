package catalog

import (
	"encoding/json"
	"sort"
)

// Criteria are the map line's completion criteria in the plan (R6:6.9) as
// numbers a done rule can read from the status (2026-11-25, Plan: the
// reading atom reads catalog.status; four criteria closed on the numbers
// already there, the rest waited for fields). Each field measures; none
// judges: the threshold is the plan's.
type Criteria struct {
	// Index (perceive first 3): the table of contents' classes by check;
	// every class carries one (ParseIndex refuses a class without), so
	// Classes = Scanned + RegistryOnly + Unseen when Measured.
	Index CriteriaIndex `json:"index"`
	// BlindSpots (perceive first 2): the cluster's workloads listed one by
	// one and how many only a namespace's row covers; the platform
	// database's tables listed; the repositories with no table counted.
	BlindSpots CriteriaBlindSpots `json:"blind_spots"`
	// SelfDescribed (LAW-77): the domains that wrote what they are (about)
	// and what one comes to them for (tasks), and those that did not.
	SelfDescribed CriteriaSelf `json:"self_described"`
	// OverCap (LAW-79): the domains with more buildings than MaxPerLayer,
	// and the most buildings a domain has before the cap.
	OverCap       []string `json:"over_cap"`
	MostBuildings int      `json:"most_buildings"`
}

type CriteriaIndex struct {
	Measured     bool `json:"measured"`
	Classes      int  `json:"classes"`
	Scanned      int  `json:"scanned"`
	RegistryOnly int  `json:"registry_only"`
	Unseen       int  `json:"unseen"`
}

type CriteriaBlindSpots struct {
	ClusterListed      int  `json:"cluster_listed"`
	ClusterByNamespace int  `json:"cluster_by_namespace"`
	DatabaseListed     bool `json:"database_listed"`
	DatabaseTables     int  `json:"database_tables"`
	ReposWithoutTable  int  `json:"repos_without_table"`
}

type CriteriaSelf struct {
	Domains   int      `json:"domains"`
	WithAbout int      `json:"with_about"`
	WithTasks int      `json:"with_tasks"`
	Without   []string `json:"without"`
}

func (s *Scanner) criteria(domains []contractDomain, inventory []RepoCount) Criteria {
	c := Criteria{OverCap: []string{}, SelfDescribed: CriteriaSelf{Without: []string{}}}
	if raw, err := s.kept().Read(keptIndex); err == nil && raw != nil {
		var ix Index
		if json.Unmarshal(raw, &ix) == nil {
			c.Index = CriteriaIndex{Measured: true, Classes: len(ix.Classes), Scanned: ix.Scanned, RegistryOnly: ix.RegistryOnly, Unseen: ix.Unseen}
		}
	}
	if p, _ := s.Last(ClusterRepo); p != nil && p.Measured {
		c.BlindSpots.ClusterListed = len(p.Covered)
		for _, x := range p.Covered {
			if x.ByNamespace {
				c.BlindSpots.ClusterByNamespace++
			}
		}
	}
	if p, _ := s.Last(DatabaseRepo); p != nil && p.Measured {
		c.BlindSpots.DatabaseListed, c.BlindSpots.DatabaseTables = true, p.Total
	}
	for _, r := range inventory {
		if r.Whole {
			c.BlindSpots.ReposWithoutTable++
		}
	}
	for _, d := range domains {
		page, ok := s.DomainOf(d.ID)
		if !ok {
			continue
		}
		c.SelfDescribed.Domains++
		if page.About != "" {
			c.SelfDescribed.WithAbout++
		}
		if len(page.Tasks) > 0 {
			c.SelfDescribed.WithTasks++
		}
		if page.About == "" || len(page.Tasks) == 0 {
			c.SelfDescribed.Without = append(c.SelfDescribed.Without, d.ID)
		}
		if n := len(page.Buildings) + page.BuildingsOver - boolInt(page.BuildingsOver > 0); n > c.MostBuildings {
			c.MostBuildings = n
		}
		if page.BuildingsOver > 0 {
			c.OverCap = append(c.OverCap, d.ID)
		}
	}
	sort.Strings(c.SelfDescribed.Without)
	sort.Strings(c.OverCap)
	return c
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
