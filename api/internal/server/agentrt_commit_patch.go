package server

import (
	gitsha1 "crypto/sha1" // Git object identity, not a credential or signature.
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type evidenceHunk struct {
	oldStart int
	old, new []string
}
type evidencePatch struct {
	old, new string
	binary   bool
	hunks    []evidenceHunk
}

var evidenceHunkStart = regexp.MustCompile(`^@@ -([0-9]+)(?:,([0-9]+))? \+[0-9]+(?:,[0-9]+)? @@`)

// Hunk counts and a full path set alone cannot detect a response ending between
// two complete hunks of the same file. Apply the received text to the parent
// blob and require the resulting Git object ID from the child tree as well.
// diffFacts has already rejected unknown lines and incomplete hunk counts.
func completeDiffContent(get func(string, int64) ([]byte, error), blobs string, before, after map[string]commitTreeEntry, diff []byte) error {
	var patches []evidencePatch
	var p *evidencePatch
	var h *evidenceHunk
	var previous byte
	for _, line := range strings.Split(strings.TrimSuffix(string(diff), "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			patches = append(patches, evidencePatch{})
			p = &patches[len(patches)-1]
			h = nil
			rest := strings.TrimPrefix(line, "diff --git ")
			if n := len(rest); n%2 == 1 && strings.HasPrefix(rest, "a/") && " "+strings.Replace(rest[:n/2], "a/", "b/", 1) == rest[n/2:] {
				p.old = rest[2 : n/2]
				p.new = p.old
			}
		case strings.HasPrefix(line, "@@"):
			m := evidenceHunkStart.FindStringSubmatch(line)
			if p == nil || m == nil {
				return fmt.Errorf("gitea: invalid patch hunk")
			}
			start, err := strconv.Atoi(m[1])
			if err != nil {
				return err
			}
			if m[2] != "0" {
				start--
			}
			if start < 0 {
				return fmt.Errorf("gitea: invalid patch offset")
			}
			p.hunks = append(p.hunks, evidenceHunk{oldStart: start})
			h = &p.hunks[len(p.hunks)-1]
			previous = 0
		case h != nil:
			if line == `\ No newline at end of file` {
				if previous != ' ' && previous != '+' && previous != '-' {
					return fmt.Errorf("gitea: misplaced newline marker")
				}
				if previous == ' ' || previous == '-' {
					h.old[len(h.old)-1] = strings.TrimSuffix(h.old[len(h.old)-1], "\n")
				}
				if previous == ' ' || previous == '+' {
					h.new[len(h.new)-1] = strings.TrimSuffix(h.new[len(h.new)-1], "\n")
				}
				previous = 0
				continue
			}
			if line == "" {
				return fmt.Errorf("gitea: empty patch line")
			}
			previous = line[0]
			if previous == ' ' || previous == '-' {
				h.old = append(h.old, line[1:]+"\n")
			}
			if previous == ' ' || previous == '+' {
				h.new = append(h.new, line[1:]+"\n")
			}
		case p != nil:
			switch {
			case strings.HasPrefix(line, "--- "):
				p.old = strings.TrimPrefix(strings.TrimSuffix(line[4:], "\t"), "a/")
				if p.old == "/dev/null" {
					p.old = ""
				}
			case strings.HasPrefix(line, "+++ "):
				p.new = strings.TrimPrefix(strings.TrimSuffix(line[4:], "\t"), "b/")
				if p.new == "/dev/null" {
					p.new = ""
				}
			case strings.HasPrefix(line, "rename from "):
				p.old = strings.TrimPrefix(line, "rename from ")
			case strings.HasPrefix(line, "rename to "):
				p.new = strings.TrimPrefix(line, "rename to ")
			case strings.HasPrefix(line, "copy from "):
				p.old = strings.TrimPrefix(line, "copy from ")
			case strings.HasPrefix(line, "copy to "):
				p.new = strings.TrimPrefix(line, "copy to ")
			case strings.HasPrefix(line, "new file mode "):
				p.old = ""
			case strings.HasPrefix(line, "deleted file mode "):
				p.new = ""
			case strings.HasPrefix(line, "Binary files "):
				p.binary = true
			}
		}
	}
	cache := map[string]string{}
	readBytes := 0
	for _, p := range patches {
		old, oldOK := before[p.old]
		next, newOK := after[p.new]
		if p.old != "" && !oldOK || p.new != "" && !newOK || p.old == "" && p.new == "" {
			return fmt.Errorf("gitea: patch endpoint is absent from its tree")
		}
		if p.binary || oldOK && newOK && old.SHA == next.SHA && len(p.hunks) == 0 {
			continue
		} // Unknown binary size already takes the maximum review limit.
		content := ""
		if oldOK {
			if old.Type == "commit" {
				content = "Subproject commit " + old.SHA + "\n"
			} else {
				var ok bool
				if content, ok = cache[old.SHA]; !ok {
					body, err := get(blobs+old.SHA, 2*maxCommitDiff)
					if err != nil {
						return err
					}
					var doc struct {
						SHA, Encoding, Content string
						Size                   int
					}
					if err = json.Unmarshal(body, &doc); err != nil {
						return err
					}
					raw, err := base64.StdEncoding.DecodeString(doc.Content)
					if err != nil || doc.SHA != old.SHA || doc.Encoding != "base64" || doc.Size != len(raw) || gitBlobID(string(raw)) != old.SHA {
						return fmt.Errorf("gitea: incomplete or invalid parent blob")
					}
					readBytes += len(raw)
					if readBytes > maxCommitDiff {
						return fmt.Errorf("gitea: parent blob budget exceeded")
					}
					content = string(raw)
					cache[old.SHA] = content
				}
			}
		}
		lines := strings.SplitAfter(content, "\n")
		if lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		var result strings.Builder
		offset := 0
		for _, h := range p.hunks {
			if h.oldStart < offset || h.oldStart+len(h.old) > len(lines) {
				return fmt.Errorf("gitea: patch offset outside parent blob")
			}
			if strings.Join(lines[h.oldStart:h.oldStart+len(h.old)], "") != strings.Join(h.old, "") {
				return fmt.Errorf("gitea: patch does not match parent blob")
			}
			result.WriteString(strings.Join(lines[offset:h.oldStart], ""))
			result.WriteString(strings.Join(h.new, ""))
			offset = h.oldStart + len(h.old)
		}
		result.WriteString(strings.Join(lines[offset:], ""))
		if !newOK {
			if result.Len() != 0 {
				return fmt.Errorf("gitea: incomplete deletion patch")
			}
		} else if next.Type == "commit" {
			if result.String() != "Subproject commit "+next.SHA+"\n" {
				return fmt.Errorf("gitea: incomplete submodule patch")
			}
		} else if gitBlobID(result.String()) != next.SHA {
			return fmt.Errorf("gitea: incomplete patch does not produce child blob")
		}
	}
	return nil
}

func gitBlobID(content string) string {
	return fmt.Sprintf("%x", gitsha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(content), content))))
}
