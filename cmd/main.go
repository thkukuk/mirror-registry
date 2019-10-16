package main

import (
        "os"
	"fmt"
	"strings"
	"context"
	"time"
	"sort"
        "encoding/json"
	"sync"
	"text/tabwriter"

        log "github.com/sirupsen/logrus"
        "github.com/spf13/cobra"
	"github.com/genuinetools/reg/registry"
	"github.com/genuinetools/reg/repoutils"
)
var (
        Version = "unreleased"
	insecure = false
	noSsl = false
	noPing = false
	debug = false
	timeout time.Duration
	username string
	password string
)


func main() {
        rootCmd := &cobra.Command{
                Use:   "mirror-registry",
                Short: "Container registry mirror tool",
		Long: "Mirror-registry will analyse a remote registry and create a yaml file with all containers and tags matching a regex to sync with skopeo to a private registry.",
		Run: createConfig,
		Args: cobra.ExactArgs(1),
	}
        rootCmd.Version = Version

	rootCmd.PersistentFlags().BoolVarP(&debug, "debug", "d", false, "enable debug output")
	rootCmd.PersistentFlags().BoolVarP(&insecure, "insecure", "i", false, "do not verify tls certificates")
	rootCmd.PersistentFlags().BoolVarP(&noSsl, "no-ssl", "n", false, "force allow non-ssl")
	rootCmd.PersistentFlags().BoolVar(&noSsl, "no-ping", false, "Don't ping registry")
	        rootCmd.PersistentFlags().DurationVarP(&timeout, "timeout", "t", time.Minute, "timeout for http requests")
	rootCmd.PersistentFlags().StringVarP(&username, "username", "u", "", "username for the registry")
	rootCmd.PersistentFlags().StringVarP(&password, "password", "p", "", "password for the registry")

        if err := rootCmd.Execute(); err != nil {
                log.Fatal(err)
                os.Exit(1)
        }
        os.Exit(0)
}

func createRegistryClient(ctx context.Context, registryName string) (*registry.Registry, error) {

	auth, err := repoutils.GetAuthConfig(username, password, registryName)
        if err != nil {
                return nil, err
        }

        // Prevent non-ssl unless explicitly forced
        if !noSsl && strings.HasPrefix(auth.ServerAddress, "http:") {
                return nil, fmt.Errorf("Attempted to use insecure protocol! Use no-ssl option.")
        }

        // Create the registry client.
        log.Infof("registry name: %s", registryName)
        log.Infof("server address: %s", auth.ServerAddress)
        return registry.New(ctx, auth, registry.Opt{
                Domain:   registryName,
                Insecure: insecure,
                Debug:    debug,
                SkipPing: noPing,
                NonSSL:   noSsl,
                Timeout:  timeout,
        })
	return nil, nil
}


func createConfig (cmd *cobra.Command, args []string) {

        registryName := args[0]
	if len(registryName) < 1 {
                fmt.Errorf("pass the domain of the registry")
		os.Exit(1)
        }

	ctx := defaultContext()
	// Create the registry client.
        reg, err := createRegistryClient(ctx, registryName)
        if err != nil {
                fmt.Errorf("Error connecting to registry: %s\n",
			err.Error())
		os.Exit(1)
        }

        // Get the repositories via catalog.
        repos, err := reg.Catalog(ctx, "")
        if err != nil {
                if _, ok := err.(*json.SyntaxError); ok {
                        fmt.Errorf("domain %s is not a valid registry",
				reg.Domain)
                } else {
			fmt.Errorf("Error reading catalog: %s\n",
				err.Error())
		}
		os.Exit(1)
        }

        sort.Strings(repos)

        fmt.Printf("Repositories for %s\n", reg.Domain)

        var (
                l        sync.Mutex
                wg       sync.WaitGroup
                repoTags = map[string][]string{}
        )

        wg.Add(len(repos))
        for _, repo := range repos {
                go func(repo string) {
                        // Get the tags.
                        tags, err := reg.Tags(ctx, repo)
                        if err != nil {
                                fmt.Printf("Get tags of [%s] error: %s", repo, err)
                        }
                        // Sort the tags
                        sort.Strings(tags)

                        // Lock on the write to the map.
                        l.Lock()
                        repoTags[repo] = tags
                        l.Unlock()

                        wg.Done()
                }(repo)
        }
        wg.Wait()

        // Setup the tab writer.
        w := tabwriter.NewWriter(os.Stdout, 20, 1, 3, ' ', 0)

        // Print header.
        fmt.Fprintln(w, "REPO\tTAGS")

        // Sort the repos.
        for _, repo := range repos {
                w.Write([]byte(fmt.Sprintf("%s\t%s\n", repo, strings.Join(repoTags[repo], ", "))))
        }

        w.Flush()
}

func defaultContext() context.Context {
	ctx := context.WithValue(context.Background(), "program.Name", "mirror-registry")
	return context.WithValue(ctx, "program.Version", Version)
}
