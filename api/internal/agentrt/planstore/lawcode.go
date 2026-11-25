package planstore

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The laws' own numbering: every law is coded from LAW, never abbreviated
// (LAW-1). Rows 1 to 85 of the register were what had been said, transcribed
// from OWNER_LAW.md and archived as material in round 1 of the inventory
// (#751); the laws the framework holds start at 86. A law is written LAW-k,
// where k counts from that first law: LAW-1 is number 86. The register's
// streams keep their numbers (law:<n>); the code is derived from the number,
// and numbers are never given twice (lawpurge.go), so a code never changes
// and never names two laws.
const lawCodeBase = 85

var lawCodePattern = regexp.MustCompile(`^LAW-([1-9][0-9]*)$`)

// The words of the levels: each level has its own word and its own numbering
// (LAW-1): 0 道 DAO, 1 根本法 CORE, 2 基本法 LAW, 3 领域法 ZONE; each word at
// most four letters (LAW-1). Each level counts from 1 on its own, a code is never given twice,
// and a law that changes level is repealed and added again with a new code.
// A law added from round 2 on keeps its code in its record; the laws before
// (86 to 98) keep the codes they had (LAW-k, k = number - 85), except 89, the
// fundamental law, which is CORE-1 (LAW-4 stays unused).
var levelWords = map[int]string{LevelDao: "DAO", LevelFundamental: "CORE", LevelBasic: "LAW", LevelDomain: "ZONE", LevelDetail: "ZONE"}

var anyCodePattern = regexp.MustCompile(`^(DAO|CORE|LAW|ZONE)-([1-9][0-9]*)$`)

// levelWord is the word a law of the given level is coded with; LAW when the
// level is not given.
func levelWord(level *int) string {
	if level != nil {
		if w, ok := levelWords[*level]; ok {
			return w
		}
	}
	return "LAW"
}

// nextCode is the code a new law of the given level takes: one past the
// largest number its word has, among the codes given (laws and tombstones).
func nextCode(codes []string, level *int) string {
	w := levelWord(level)
	top := 0
	for _, c := range codes {
		if m := anyCodePattern.FindStringSubmatch(c); m != nil && m[1] == w {
			k, _ := strconv.Atoi(m[2])
			top = max(top, k)
		}
	}
	return fmt.Sprintf("%s-%d", w, top+1)
}

// LawCode is law n's code as the laws before round 2 had it, derived from the
// number: LAW-k, k = n - 85, and CORE-1 for 89; "" for material. A
// law added later carries its code in its record (Law.Code).
func LawCode(n string) string {
	v, err := strconv.Atoi(n)
	if err != nil || v <= lawCodeBase {
		return ""
	}
	if v == lawCodeBase+4 {
		return "CORE-1"
	}
	return fmt.Sprintf("LAW-%d", v-lawCodeBase)
}

// LawNumberOf reads a reference to one of the laws before round 2 by its
// code (LAW-1 … LAW-13, CORE-1) or a number as the register's number.
// A later code is returned as given; a card names those by number.
func LawNumberOf(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "CORE-1" {
		return strconv.Itoa(lawCodeBase + 4)
	}
	if m := lawCodePattern.FindStringSubmatch(ref); m != nil {
		k, _ := strconv.Atoi(m[1])
		if k <= 13 {
			return strconv.Itoa(lawCodeBase + k)
		}
	}
	return ref
}

// lawName is how a card or a note names law n when only its number is at
// hand: its code before round 2, or 「第 n 条」.
func lawName(n string) string {
	if c := LawCode(n); c != "" {
		return c
	}
	return "第 " + n + " 条"
}
