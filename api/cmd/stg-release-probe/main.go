// stg-release-probe checks B5 prerequisites without persisting cluster objects.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/stgpreflight"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	var kubeconfig string
	var options stgpreflight.Options
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Explicit workers kubeconfig; empty uses in-cluster identity")
	flag.StringVar(&options.Revision, "revision", "", "Exact 40-character platform main revision P")
	flag.StringVar(&options.Sample, "sample", "", "Existing held STG PipelineRun name (never created by this probe)")
	flag.StringVar(&options.SampleUID, "sample-uid", "", "Independently recorded UID of the held sample")
	flag.StringVar(&options.WorkersNamespace, "workers-namespace", "bifrost-platform-prod", "Namespace of the actual B5 workers")
	flag.Parse()
	var config *rest.Config
	var err error
	if kubeconfig == "" {
		config, err = rest.InClusterConfig()
	} else {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL client: cannot load the explicit workers identity")
		os.Exit(1)
	}
	config.Timeout = 10 * time.Second
	config.Wrap(stgpreflight.ReadOnlyTransport)
	client, err := httpClient(config)
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL client: cannot initialize workers transport")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	results := stgpreflight.Run(ctx, config, client, options)
	if exitCode := writeResults(os.Stdout, results); exitCode != 0 {
		os.Exit(exitCode)
	}
}

func writeResults(w io.Writer, results []stgpreflight.Result) int {
	exitCode := 0
	for _, r := range results {
		status := "PASS"
		if !r.Passed {
			status, exitCode = "FAIL", 1
		}
		fmt.Fprintf(w, "%s %s: %s\n", status, r.Name, r.Reason)
	}
	return exitCode
}

func httpClient(config *rest.Config) (*http.Client, error) {
	c, err := rest.HTTPClientFor(config)
	if err == nil {
		c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	return c, err
}
