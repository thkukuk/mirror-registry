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
	"runtime"
	"regexp"
	"bufio"

        log "github.com/sirupsen/logrus"
        "github.com/spf13/cobra"
	"github.com/genuinetools/reg/registry"
	"github.com/genuinetools/reg/repoutils"
        "github.com/docker/distribution/manifest/manifestlist"
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

type PlatformManifest struct {
        architecture string `json:"architecture"`
        os string `json:"os"`
}

type ManifestResponse struct {
        MediaType string `json:"mediaType"`
        Size int `json:"size"`
        Digest string `json:"digest"`
        Platform PlatformManifest `json:"platform"`
}

type ManifestListResponse struct {
        SchemaVersion int `json:"schemaVersion"`
        MediaType string `json:"mediaType"`
        Manifests []ManifestResponse `json:"manifests"`
}

func main() {
        rootCmd := &cobra.Command{
                Use:   "mirror-registry [regexp]",
                Short: "Container registry mirror tool",
		Long: "Mirror-registry will analyse a remote registry and create a yaml file with all containers and tags matching a regex to sync with skopeo to a private registry.",
		Run: createConfig,
		Args: cobra.MinimumNArgs(1),
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
        return registry.New(ctx, auth, registry.Opt{
                Domain:   registryName,
                Insecure: insecure,
                Debug:    debug,
                SkipPing: noPing,
                NonSSL:   noSsl,
                Timeout:  timeout,
        })
}


func createConfig (cmd *cobra.Command, args []string) {

        registryName := args[0]
	if len(registryName) < 1 {
                fmt.Fprintf(os.Stderr, "pass the domain of the registry\n")
		os.Exit(1)
        }
	regex := ".*"
	if len(args) > 1 {
		regex = args[1]
	}
	r, err := regexp.Compile(regex)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error compiling regexp (%s): %s\n", regex, err.Error())
		os.Exit(1)
	}

	ctx := defaultContext()
	// Create the registry client.
        reg, err := createRegistryClient(ctx, registryName)
        if err != nil {
                fmt.Fprintf(os.Stderr, "Error connecting to registry: %s\n",
			err.Error())
		os.Exit(1)
        }



        // Get the repositories via catalog.
	fmt.Printf("Get repositories for %s\n", reg.Domain)
        repos, err := reg.Catalog(ctx, "")
        if err != nil {
                if _, ok := err.(*json.SyntaxError); ok {
                        fmt.Fprintf(os.Stderr, "domain %s is not a valid registry\n",
				reg.Domain)
                } else {
			fmt.Fprintf(os.Stderr, "Error reading catalog: %s\n",
				err.Error())
		}
		os.Exit(1)
        }

        sort.Strings(repos)

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
                                fmt.Fprintf(os.Stderr, "Get tags of [%s] error: %s\n", repo, err)
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

	f, err := os.Create("skopeo.yml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot create skopeo.yml: %s\n", err.Error())
		os.Exit(1)
	}
	defer f.Close()

	w := bufio.NewWriter(f)

	fmt.Fprintf(w, "%s:\n", reg.Domain)
	if len(username) > 0 && len(password) > 0 {
		fmt.Fprintf(w, "  credentials:\n")
		fmt.Fprintf(w, "    username: %s", username)
		fmt.Fprintf(w, "    password: %s", password)
	}
	if insecure {
		fmt.Fprintf(w, "  tls-verify: false\n")
	} else {
		fmt.Fprintf(w, "  tls-verify: true\n")
	}
	fmt.Fprintf(w, "  images:\n")

        for _, repo := range repos {
		if !r.MatchString(repo) {
			continue
		}

		print_repo := true;
		for _, tag := range repoTags[repo] {
			ml, err := reg.ManifestList(ctx, repo, tag)
			if err != nil {
				fmt.Fprintf(os.Stderr, err.Error())
				os.Exit(1)
			}
			if (ml.Versioned.SchemaVersion != 2 && strings.Compare(ml.Versioned.MediaType,
				manifestlist.MediaTypeManifestList) != 0) {
					if ml.Versioned.SchemaVersion == 0 {
						_, err := reg.Manifest(ctx, repo, tag)
						if err != nil {
							fmt.Fprintf(os.Stderr, "%s:%s: - %s\n",
								repo, tag, err.Error())
							continue;
						}
						if print_repo {
							fmt.Fprintf(w, "    %s:\n", repo)
							print_repo = false
						}
						fmt.Fprintf(w, "      - %s\n", tag);
						continue
					}
					fmt.Printf("%s:%s - ignoring, wrong schema vesion or media type\n",
						repo, tag)
					continue
			}
			for _, platform := range ml.Manifests {
				if strings.Compare (platform.Platform.Architecture, runtime.GOARCH) == 0 {
					if print_repo {
						fmt.Fprintf(w, "    %s:\n", repo)
						print_repo = false
					}
					fmt.Fprintf(w, "      - %s\n", tag)
				}
			}
		}
        }
	w.Flush()
	f.Sync()
}

func defaultContext() context.Context {
	ctx := context.WithValue(context.Background(), "program.Name", "mirror-registry")
	return context.WithValue(ctx, "program.Version", Version)
}
