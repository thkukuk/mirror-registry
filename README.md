# mirror-registry

mirror-registry is a tool which helps to mirror container images from a
registry to a local, private or air gap registry.

## Description

`mirror-registry` will analyse a remote registry and create a yaml file
with all containers and tags matching a regex to sync with `skopeo` to a
private registry. While this tool understands the architecture flag for
containers, skopeo does not really use this information yet. If a repository
contains multi-arch containers, it will fail if there is no container for
the architecture it is running on, else it will use the architecture which
it is running on.

## Example

To mirror all official openSUSE and openSUSE Kubic images for aarch64,
the images are afterwards available in registry.local as:
`registry.local/registry.opensuse.org/...`.

```
mirror-registry -a arm64 registry.opensuse.org "(^kubic|^opensuse/([^/]+)$)"
skopeo sync --scoped --src yaml --dest docker --dest-creds user:password skopeo.yaml registry.local
```

To mirror only the latest build of official openSUSE Kubic image versions:

```
mirror-registry -m registry.opensuse.org ^kubic
skopeo sync --scoped --src yaml --dest docker --dest-creds user:password skopeo.yaml registry.local
```

