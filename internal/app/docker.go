package app

import (
	"context"
	"fmt"
	"log"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
)

type DockerClient interface {
	ContainerList(ctx context.Context, options types.ContainerListOptions) ([]types.Container, error)
	ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)
	Events(ctx context.Context, options types.EventsOptions) (<-chan events.Message, <-chan error)
}

func loadRunning(cli DockerClient, registry *Registry, networks []string) {
	containers, err := cli.ContainerList(context.Background(), types.ContainerListOptions{})
	if err != nil {
		log.Printf("Error listing containers: %v", err)
		return
	}

	for _, c := range containers {
		processContainer(cli, registry, networks, c.ID, true)
	}
}

// watchEvents follows container start/die events. Events since `since` are
// replayed, so a container that starts between loadRunning and the
// subscription (e.g. Beacon's own container) is not missed.
func watchEvents(cli DockerClient, registry *Registry, networks []string, since time.Time) {
	f := filters.NewArgs()
	f.Add("type", "container")

	events, errs := cli.Events(context.Background(), types.EventsOptions{
		Filters: f,
		Since:   fmt.Sprintf("%d.%09d", since.Unix(), since.Nanosecond()),
	})

	for {
		select {
		case event := <-events:
			switch event.Action {
			case "start":
				processContainer(cli, registry, networks, event.Actor.ID, true)
			case "die":
				removeContainer(event.Actor.Attributes, registry)
			}
		case err := <-errs:
			log.Printf("Docker event error: %v", err)
			return
		}
	}
}

// getContainerIP returns the container's IP on the first preferred network it
// is on or, if none, on the first network (by name) that has one. It also
// returns the network used.
func getContainerIP(info types.ContainerJSON, preferred []string) (string, string) {
	networks := info.NetworkSettings.Networks

	for _, name := range preferred {
		if nw, ok := networks[name]; ok && nw != nil && nw.IPAddress != "" {
			return nw.IPAddress, name
		}
	}

	names := make([]string, 0, len(networks))
	for name := range networks {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if nw := networks[name]; nw != nil && nw.IPAddress != "" {
			return nw.IPAddress, name
		}
	}

	return "", ""
}

func processContainer(cli DockerClient, registry *Registry, networks []string, id string, add bool) {
	info, err := cli.ContainerInspect(context.Background(), id)
	if err != nil {
		log.Printf("Error inspecting container %s: %v", id, err)
		return
	}

	labels := info.Config.Labels
	ip, used := getContainerIP(info, networks)
	if ip == "" {
		log.Printf("No IP for container %s", info.Name)
		return
	}

	host, ok := labels["beacon.host"]
	if !ok {
		return
	}

	if !slices.Contains(networks, used) && registry.Verbose {
		log.Printf("[DNS] %s: not on network %s, using %q", strings.TrimPrefix(info.Name, "/"), strings.Join(networks, ", "), used)
	}

	for _, h := range parseHosts(host) {
		if add {
			registry.SetRecord(Record{
				Host:      h,
				Target:    ip,
				Container: strings.TrimPrefix(info.Name, "/"),
				Labels:    labels,
			})
		} else {
			registry.Delete(h)
		}
	}
}

func removeContainer(attrs map[string]string, registry *Registry) {
	host, ok := attrs["beacon.host"]
	if !ok {
		return
	}

	for _, h := range parseHosts(host) {
		registry.Delete(h)
	}
}
