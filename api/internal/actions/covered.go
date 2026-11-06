package actions

import (
	"path"
	"strings"
	"unicode"
)

// CoveredBy reports the first manifest path already covered by the loaded apply
// policy. It only reads shell-like text; it never expands or executes it.
// This is a guard against habit, not a shell security boundary.
func CoveredBy(command string) (repo, manifest string, ok bool) {
	p := currentPolicy()
	if p == nil {
		return "", "", false
	}
	tokens := commandTokens(command)
	var previous []string
	var segment []string
	pipe := false
	for i := 0; i <= len(tokens); i++ {
		if i < len(tokens) && !strings.HasPrefix(tokens[i], "\x00") {
			segment = append(segment, tokens[i])
			continue
		}
		for j, word := range segment {
			if path.Base(word) != "kubectl" {
				continue
			}
			args := segment[j+1:]
			sub := kubectlSubcommand(args)
			if sub != "apply" && sub != "create" && sub != "replace" {
				continue
			}
			server, force := false, false
			var files []string
			for k := 0; k < len(args); k++ {
				flag, value, equals := strings.Cut(args[k], "=")
				if flag == "--server-side" && (!equals || value == "true") {
					server = true
				}
				if flag == "--force-conflicts" && (!equals || value == "true") {
					force = true
				}
				switch flag {
				case "-f", "--filename", "-k", "--kustomize":
					if !equals && k+1 < len(args) {
						k++
						value = args[k]
					}
					files = append(files, strings.Split(value, ",")...)
				}
			}
			if server && force {
				continue
			}
			for _, file := range files {
				if file == "-" && pipe && len(previous) >= 3 && path.Base(previous[0]) == "kustomize" && previous[1] == "build" {
					file = previous[2]
				}
				if file == "" || file == "-" {
					continue
				}
				file = strings.TrimPrefix(file, "./")
				// Bare paths refer to the primary checkout (the first policy repo).
				// Other repos can allow all of k8s, so searching those for a
				// bare infra path would incorrectly cover platform RBAC.
				explicit := ""
				if len(p.Apply.Repos) > 0 {
					explicit = p.Apply.Repos[0].Name
				}
				parts := strings.Split(file, "/")
			outer:
				for n, part := range parts {
					for _, r := range p.Apply.Repos {
						if part == r.Name {
							explicit = r.Name
							file = strings.Join(parts[n+1:], "/")
							break outer
						}
					}
				}
				for _, r := range p.Apply.Repos {
					if explicit != "" && explicit != r.Name {
						continue
					}
					for _, prefix := range r.Prefixes {
						prefix = strings.Trim(prefix, "/")
						if file == prefix || strings.HasPrefix(file, prefix+"/") {
							return r.Name, file, true
						}
					}
				}
			}
		}
		previous = segment
		segment = nil
		pipe = i < len(tokens) && tokens[i] == "\x00|"
	}
	return "", "", false
}

func kubectlSubcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			return arg
		}
		flag, _, equal := strings.Cut(arg, "=")
		if equal {
			continue
		}
		switch flag {
		case "--context", "-n", "--namespace", "--kubeconfig", "--cluster", "--user", "--server", "-s", "--token", "--certificate-authority", "--client-certificate", "--client-key", "--request-timeout", "--as", "--as-group", "--as-uid", "--tls-server-name", "--cache-dir", "--v", "-v":
			i++
		}
	}
	return ""
}

// commandTokens removes quotes and escapes, preserving shell operators only
// when they occur outside quotes.
func commandTokens(s string) []string {
	var out []string
	var word strings.Builder
	var quote rune
	active, escape := false, false
	flush := func() {
		if active {
			out = append(out, word.String())
			word.Reset()
			active = false
		}
	}
	chars := []rune(s)
	for i := 0; i < len(chars); i++ {
		c := chars[i]
		if escape {
			if c != '\n' {
				word.WriteRune(c)
				active = true
			}
			escape = false
			continue
		}
		if c == '\\' && quote != '\'' {
			escape = true
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				word.WriteRune(c)
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			active = true
			continue
		}
		if c == '|' || c == ';' || c == '&' && i+1 < len(chars) && chars[i+1] == '&' {
			flush()
			op := string(c)
			if i+1 < len(chars) && chars[i+1] == c && c != ';' {
				op += string(c)
				i++
			}
			out = append(out, "\x00"+op)
			continue
		}
		if c == '\n' {
			flush()
			out = append(out, "\x00;")
			continue
		}
		if unicode.IsSpace(c) {
			flush()
			continue
		}
		word.WriteRune(c)
		active = true
	}
	flush()
	return out
}
