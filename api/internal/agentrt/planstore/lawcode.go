package planstore

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The laws' own numbering (Owner 2026-11-25: 「以后法条统一叫LAW 开始，不用
// 简写」). Rows 1 to 85 of the register were what had been said, transcribed
// from OWNER_LAW.md and archived as material in round 1 of the inventory
// (#751); the laws the framework holds start at 86. A law is written LAW-k,
// where k counts from that first law: LAW-1 is number 86. The register's
// streams keep their numbers (law:<n>); the code is derived from the number,
// and numbers are never given twice (lawpurge.go), so a code never changes
// and never names two laws.
const lawCodeBase = 85

var lawCodePattern = regexp.MustCompile(`^LAW-([1-9][0-9]*)$`)

// LawCode is law n's code, LAW-k; "" for a row of material (n at most 85) or
// a number that does not read.
func LawCode(n string) string {
	v, err := strconv.Atoi(n)
	if err != nil || v <= lawCodeBase {
		return ""
	}
	return fmt.Sprintf("LAW-%d", v-lawCodeBase)
}

// LawNumberOf reads a reference to a law, its code (LAW-k) or its number,
// as the register's number. Anything else is returned as it was given.
func LawNumberOf(ref string) string {
	ref = strings.TrimSpace(ref)
	if m := lawCodePattern.FindStringSubmatch(ref); m != nil {
		k, _ := strconv.Atoi(m[1])
		return strconv.Itoa(lawCodeBase + k)
	}
	return ref
}

// lawName is how a card or a note names law n: its code, or 「第 n 条」 for
// material.
func lawName(n string) string {
	if c := LawCode(n); c != "" {
		return c
	}
	return "第 " + n + " 条"
}
