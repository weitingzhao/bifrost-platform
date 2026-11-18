package instruments

import (
	"errors"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// readSkills reads every <skillsDir>/<name>/SKILL.md. A skill that cannot be
// read or parsed stays in the list under its directory's name, and is reported.
func readSkills(r *reading, infra repoFiles) []Skill {
	skills := []Skill{}
	if infra.err != nil {
		r.failed(sourceName(infraRepo, skillsDir), infra.err)
		return skills
	}
	var files []string
	for p := range infra.files {
		if path.Base(p) == "SKILL.md" && path.Dir(path.Dir(p)) == skillsDir {
			files = append(files, p)
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		r.malformed(sourceName(infraRepo, skillsDir), "no SKILL.md at main")
	}
	for _, p := range files {
		skill := Skill{Name: path.Base(path.Dir(p)), Path: p, Laws: []string{}}
		if raw, ok := r.file(infra, p); ok {
			parseSkill(r, &skill, string(raw))
		}
		skills = append(skills, skill)
	}
	return skills
}

func parseSkill(r *reading, skill *Skill, text string) {
	source := sourceName(infraRepo, skill.Path)
	skill.Lines = strings.Count(text, "\n")
	if text != "" && !strings.HasSuffix(text, "\n") {
		skill.Lines++
	}
	skill.Laws = citedLaws(text)
	front, err := frontmatter(text)
	if err != nil {
		r.malformed(source, err.Error())
		return
	}
	var meta struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(front), &meta); err != nil {
		r.malformed(source, "the frontmatter is not valid YAML")
		return
	}
	skill.Description = strings.Join(strings.Fields(meta.Description), " ")
	if skill.Description == "" {
		r.malformed(source, "the frontmatter has no description")
	}
	switch meta.Name {
	case "":
		r.malformed(source, "the frontmatter has no name")
	case skill.Name:
	default:
		r.malformed(source, "the frontmatter names it "+meta.Name+", its directory "+skill.Name)
		skill.Name = meta.Name
	}
}

func frontmatter(text string) (string, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if strings.TrimSpace(lines[0]) != "---" {
		return "", errors.New("the file does not start with YAML frontmatter")
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.Join(lines[1:i], "\n"), nil
		}
	}
	return "", errors.New("the YAML frontmatter is not closed")
}
