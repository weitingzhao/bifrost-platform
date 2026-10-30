package infracheck

import "strings"

// Setup returns the commands agentd runs before the isolated check. CI uses
// the same pinned packages from its image's wheelhouse without an index.
func Setup(wheelhouse string) [][]string {
	pip := []string{"env", "PIP_CONFIG_FILE=/dev/null", infraVenv + "/bin/python3", "-I", "-m", "pip", "--isolated", "install", "--quiet",
		"--no-cache-dir", "--no-input", "--disable-pip-version-check", "--only-binary=:all:", "--no-deps"}
	if wheelhouse != "" {
		pip = append(pip, "--no-index", "--find-links", wheelhouse)
	}
	return [][]string{
		{"python3", "-I", "-m", "venv", "--clear", infraVenv},
		append(pip, infraPackages...),
	}
}

// SetupScript renders the same argv for sh, including literal wheelhouse paths.
// Stop if venv creation fails instead of installing into an earlier environment.
func SetupScript(wheelhouse string) string {
	var commands []string
	for _, command := range Setup(wheelhouse) {
		words := make([]string, len(command))
		for i, word := range command {
			words[i] = "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
		}
		commands = append(commands, strings.Join(words, " "))
	}
	return strings.Join(commands, " && ") + "\n"
}
