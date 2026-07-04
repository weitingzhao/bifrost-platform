package ibgateway

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func (s *Service) redisCLI(args ...string) (string, error) {
	redisURL := fmt.Sprintf("redis://platform:%s@127.0.0.1:6379", s.cfg.RedisPlatformPass)
	cmdArgs := []string{
		"exec", "-n", dataNamespace, "deploy/" + redisDeployName, "--",
		"redis-cli", "-u", redisURL,
	}
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.Command("kubectl", cmdArgs...)
	cmd.Env = append(os.Environ(), "KUBECONFIG="+s.cfg.Kubeconfig)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func parseRedisHash(raw string) map[string]string {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	out := make(map[string]string)
	for i := 0; i+1 < len(lines); i += 2 {
		key := strings.TrimSpace(lines[i])
		val := strings.TrimSpace(lines[i+1])
		if key != "" {
			out[key] = val
		}
	}
	return out
}
