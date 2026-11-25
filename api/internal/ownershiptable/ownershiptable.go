// Package ownershiptable reads the ownership table as one document from its
// files (2026-11-22, the map line, LAW-10 proposal section 3): the table
// config/domain-ownership.json changed in 190 commits in seven days and
// lines that edit it collide. It is being split into one file per domain,
// config/domains/<domain>.json, each holding that domain's row and the rows
// of what it owns. Every reader reads through Merge, so the split moves rows
// without changing what any reader sees; until the directory has files the
// answer is the main file as it is.
package ownershiptable

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Path is the main file; Dir the per-domain files.
const (
	Path = "config/domain-ownership.json"
	Dir  = "config/domains"
)

// sections are the maps a domain file may add rows to.
var sections = []string{"packages", "paths", "workloads", "tables"}

// PartPaths are the files of Dir among files (repository paths), sorted.
func PartPaths(files []string) []string {
	var out []string
	for _, f := range files {
		if strings.HasPrefix(f, Dir+"/") && strings.HasSuffix(f, ".json") && !strings.Contains(strings.TrimPrefix(f, Dir+"/"), "/") {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// Merge is main with each part added: a part is {"domain": {row}, and any of
// "packages", "paths", "workloads", "tables"}. A domain id, or a row key in a
// section, that is already there is an error naming both places. With no
// parts main is returned as it is.
func Merge(main []byte, parts map[string][]byte) ([]byte, error) {
	if len(parts) == 0 {
		return main, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(main, &doc); err != nil {
		return nil, fmt.Errorf("%s does not parse: %w", Path, err)
	}
	var domains []json.RawMessage
	if raw, ok := doc["domains"]; ok {
		if err := json.Unmarshal(raw, &domains); err != nil {
			return nil, fmt.Errorf("%s: domains: %w", Path, err)
		}
	}
	where := map[string]string{} // "domain:<id>" or "<section>:<key>" -> file
	for _, d := range domains {
		var id struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(d, &id)
		where["domain:"+id.ID] = Path
	}
	maps := map[string]map[string]json.RawMessage{}
	for _, sec := range sections {
		m := map[string]json.RawMessage{}
		if raw, ok := doc[sec]; ok {
			if err := json.Unmarshal(raw, &m); err != nil {
				return nil, fmt.Errorf("%s: %s: %w", Path, sec, err)
			}
		}
		for k := range m {
			where[sec+":"+k] = Path
		}
		maps[sec] = m
	}
	names := make([]string, 0, len(parts))
	for n := range parts {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		var part map[string]json.RawMessage
		if err := json.Unmarshal(parts[name], &part); err != nil {
			return nil, fmt.Errorf("%s does not parse: %w", name, err)
		}
		for k := range part {
			if k != "domain" && !contains(sections, k) {
				return nil, fmt.Errorf("%s: %q is not a section a domain file has (domain, %s)", name, k, strings.Join(sections, ", "))
			}
		}
		if raw, ok := part["domain"]; ok {
			var id struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &id); err != nil || id.ID == "" {
				return nil, fmt.Errorf("%s: its domain row has no id", name)
			}
			if prev, dup := where["domain:"+id.ID]; dup {
				return nil, fmt.Errorf("domain %s is in both %s and %s", id.ID, prev, name)
			}
			where["domain:"+id.ID] = name
			domains = append(domains, raw)
		}
		for _, sec := range sections {
			raw, ok := part[sec]
			if !ok {
				continue
			}
			var m map[string]json.RawMessage
			if err := json.Unmarshal(raw, &m); err != nil {
				return nil, fmt.Errorf("%s: %s: %w", name, sec, err)
			}
			for k, v := range m {
				if prev, dup := where[sec+":"+k]; dup {
					return nil, fmt.Errorf("%s %s is in both %s and %s", sec, k, prev, name)
				}
				where[sec+":"+k] = name
				maps[sec][k] = v
			}
		}
	}
	var err error
	if doc["domains"], err = json.Marshal(domains); err != nil {
		return nil, err
	}
	for _, sec := range sections {
		if len(maps[sec]) == 0 && doc[sec] == nil {
			continue
		}
		if doc[sec], err = json.Marshal(maps[sec]); err != nil {
			return nil, err
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(buf.Bytes()), nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
