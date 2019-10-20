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
	"io/ioutil"

        log "github.com/sirupsen/logrus"
        "github.com/spf13/cobra"
	"github.com/genuinetools/reg/registry"
	"github.com/genuinetools/reg/repoutils"
        "github.com/docker/distribution/manifest/manifestlist"
	"github.com/docker/distribution/manifest/schema2"
)

var (
        Version = "unreleased"
	arch string
	insecure = false
	noSsl = false
	noPing = false
	debug = false
	timeout = time.Minute
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
                Use:   "mirror-registry registry [regexp]",
                Short: "Container registry mirror tool",
		Long: "Mirror-registry will analyse a remote registry and create a yaml file with all containers and tags matching a regex to sync with skopeo to a private registry.",
		Run: createConfig,
		Args: cobra.MinimumNArgs(1),
	}
        rootCmd.Version = Version

	rootCmd.PersistentFlags().StringVarP(&arch, "arch", "a", runtime.GOARCH, "architecture for which the container should be copied, can create poblems with skopeo and multi-arch container images")
	rootCmd.PersistentFlags().BoolVarP(&debug, "debug", "d", debug, "enable debug output")
	rootCmd.PersistentFlags().BoolVarP(&insecure, "insecure", "i", insecure, "do not verify tls certificates")
	rootCmd.PersistentFlags().BoolVarP(&noSsl, "no-ssl", "n", noSsl, "force allow non-ssl")
	rootCmd.PersistentFlags().BoolVar(&noPing, "no-ping", noPing, "Don't ping registry")
	rootCmd.PersistentFlags().DurationVarP(&timeout, "timeout", "t", timeout, "timeout for http requests")
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
	fmt.Printf("Get repositories for %s...\n", reg.Domain)
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

	fmt.Printf("Get the tags for matching repositories...\n")
        wg.Add(len(repos))
        for _, repo := range repos {
                go func(repo string) {
                        // Get the tags.
			if r.MatchString(repo) {
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
			}
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

	fmt.Print("Get architecture for every repository\n")
        wg.Add(len(repoTags))
        for repo := range repoTags {
                go func(repo string) {
			print_repo := []string{}
			for _, tag := range repoTags[repo] {
				ml, err := reg.ManifestList(ctx, repo, tag)
				if err != nil {
					fmt.Fprintf(os.Stderr, err.Error())
					os.Exit(1)
				}
				if (ml.Versioned.SchemaVersion != 2 || strings.Compare(ml.Versioned.MediaType, manifestlist.MediaTypeManifestList) != 0) {
					m, err := reg.ManifestV2(ctx, repo, tag)
					if err != nil {
						fmt.Fprintf(os.Stderr, "%s:%s: - %s\n",
							repo, tag, err.Error())
						continue;
					}
					if (m.Versioned.SchemaVersion != 2 ||
						strings.Compare(m.Versioned.MediaType, schema2.MediaTypeManifest) != 0) {
						fmt.Printf("%s:%s - ignoring, wrong schema vesion or media type\n",
							repo, tag)
						continue
					}
					// We have a Version2 Manifest. Get the Config layer with help of the
					// Digest.
					configBody, err := reg.DownloadLayer(ctx, repo, m.Config.Digest)
					if err != nil {
						fmt.Fprintf(os.Stderr, "DownloadLayer (%s:%s) failed: - %s\n",
							repo, m.Config.Digest, err.Error())
						continue;
					}
					body, err := ioutil.ReadAll(configBody)
					if err != nil {
						fmt.Fprintf(os.Stderr, "ReadAll(%s:%s) failed: - %s\n",
							repo, m.Config.Digest, err.Error())
						continue;
					}
					s := string(body);
					if idx := strings.Index(s, "\"architecture\":\""); idx != -1 {
						s = s[(idx+16):]
						if idx := strings.Index(s, "\""); idx != -1 {
							s = s[:idx]
						}
						// If the architecture is not identical, skip entry
						if strings.Compare (s, arch) != 0 {
							continue
						}
					}
					print_repo = append(print_repo, tag)
					continue
				}
				for _, platform := range ml.Manifests {
					if strings.Compare (platform.Platform.Architecture, arch) == 0 {
						print_repo = append(print_repo, tag)
					}
				}
			}
			if len(print_repo) > 0 {
				// Lock on write to file
				l.Lock()
				fmt.Fprintf(w, "    %s:\n", repo)
				for _, tag := range print_repo {
					fmt.Fprintf(w, "      - %s\n", tag);
				}
				l.Unlock()
			}
			wg.Done()
		}(repo)
	}
	wg.Wait()
	w.Flush()
	f.Sync()
}

func defaultContext() context.Context {
	ctx := context.WithValue(context.Background(), "program.Name", "mirror-registry")
	return context.WithValue(ctx, "program.Version", Version)
}
