package releases

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"gopkg.in/yaml.v3"
)

const absentText = "No STG"

type imageLane struct {
	Lane string     `yaml:"lane"`
	Envs []imageEnv `yaml:"envs"`
}

type imageEnv struct {
	Env         string        `yaml:"env"`
	Deployments []imageDeploy `yaml:"deployments"`
	Planes      []imagePlane  `yaml:"planes"`
}

type imageDeploy struct {
	Namespace string `yaml:"namespace"`
	Name      string `yaml:"name"`
}

type imagePlane struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}

type imageFile struct {
	Lanes []imageLane `yaml:"lanes"`
}

// ImageCell is one STG or PROD cell on the Releases page.
type ImageCell struct {
	Lane   string `json:"lane"`
	Env    string `json:"env"`
	Absent bool   `json:"absent,omitempty"`
	Text   string `json:"text"`
	Title  string `json:"title,omitempty"`
	Error  string `json:"error,omitempty"`
}

func loadImageFile(configDir string) (imageFile, error) {
	raw, err := os.ReadFile(filepath.Join(configDir, "running-images.yaml"))
	if err != nil {
		return imageFile{}, err
	}
	var f imageFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return imageFile{}, err
	}
	return f, nil
}

func imageTag(image string) string {
	image = strings.TrimSpace(image)
	if i := strings.LastIndex(image, "@"); i >= 0 {
		return image[i+1:]
	}
	slash := strings.LastIndex(image, "/")
	colon := strings.LastIndex(image, ":")
	if colon > slash {
		return image[colon+1:]
	}
	return image
}

// RunningImages reads the configured Deployments and operator-plane /health
// versions. Envs not listed for a lane come back absent.
func (s *Service) RunningImages(ctx context.Context) ([]ImageCell, error) {
	file, err := loadImageFile(s.configDir)
	if err != nil {
		return nil, err
	}
	var kube kubernetes.Interface
	if s.clients != nil {
		kube, _, err = s.clients()
		if err != nil {
			kube = nil
		}
	}
	var out []ImageCell
	for _, lane := range file.Lanes {
		have := map[string]imageEnv{}
		for _, env := range lane.Envs {
			have[strings.ToLower(env.Env)] = env
		}
		for _, envName := range []string{"stg", "prod"} {
			env, ok := have[envName]
			if !ok {
				text := "No PROD"
				if envName == "stg" {
					text = absentText
				}
				out = append(out, ImageCell{Lane: lane.Lane, Env: envName, Absent: true, Text: text})
				continue
			}
			out = append(out, s.readImageEnv(ctx, kube, lane.Lane, env))
		}
	}
	return out, nil
}

func (s *Service) readImageEnv(ctx context.Context, kube kubernetes.Interface, lane string, env imageEnv) ImageCell {
	cell := ImageCell{Lane: lane, Env: strings.ToLower(env.Env)}
	var texts []string
	var titles []string
	if len(env.Planes) > 0 {
		client := &http.Client{Timeout: 3 * time.Second}
		for _, plane := range env.Planes {
			ver, err := planeVersion(ctx, client, plane.URL)
			name := plane.Name
			if name == "" {
				name = plane.URL
			}
			if err != nil {
				texts = append(texts, name+" unavailable")
				titles = append(titles, name+" "+plane.URL+" "+err.Error())
				continue
			}
			texts = append(texts, name+" "+ver)
			titles = append(titles, name+" "+plane.URL+" "+ver)
		}
	}
	for _, dep := range env.Deployments {
		if kube == nil {
			texts = append(texts, dep.Name+" unread")
			titles = append(titles, dep.Namespace+"/"+dep.Name+" kubernetes client unavailable")
			continue
		}
		got, err := kube.AppsV1().Deployments(dep.Namespace).Get(ctx, dep.Name, metav1.GetOptions{})
		if err != nil {
			texts = append(texts, dep.Name+" unread")
			titles = append(titles, dep.Namespace+"/"+dep.Name+" "+err.Error())
			continue
		}
		if len(got.Spec.Template.Spec.Containers) == 0 {
			texts = append(texts, dep.Name+" unread")
			continue
		}
		image := got.Spec.Template.Spec.Containers[0].Image
		texts = append(texts, dep.Name+" "+imageTag(image))
		titles = append(titles, dep.Namespace+"/"+dep.Name+" "+image)
	}
	cell.Text = strings.Join(texts, " · ")
	cell.Title = strings.Join(titles, "\n")
	if cell.Text == "" {
		cell.Error = "no deployments or planes configured"
		cell.Text = "—"
	}
	return cell
}

func planeVersion(ctx context.Context, client *http.Client, base string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/health", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	if payload.Version == "" {
		return "", fmt.Errorf("health has no version")
	}
	return payload.Version, nil
}
