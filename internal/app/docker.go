package app

import (
	"context"
	"log"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
)

type DockerClient interface {
	ContainerList(ctx context.Context, options types.ContainerListOptions) ([]types.Container, error)
	ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)
	Events(ctx context.Context, options types.EventsOptions) (<-chan events.Message, <-chan error)
}

func loadRunning(cli DockerClient, registry *Registry) {
	containers, err := cli.ContainerList(context.Background(), types.ContainerListOptions{})
	if err != nil {
		log.Printf("Error listing containers: %v", err)
		return
	}

	for _, c := range containers {
		processContainer(cli, registry, c.ID, true)
	}
}

func watchEvents(cli DockerClient, registry *Registry) {
	f := filters.NewArgs()
	f.Add("type", "container")

	events, errs := cli.Events(context.Background(), types.EventsOptions{Filters: f})

	for {
		select {
		case event := <-events:
			switch event.Action {
			case "start":
				processContainer(cli, registry, event.Actor.ID, true)
			case "die":
				removeContainer(event.Actor.Attributes, registry)
			}
		case err := <-errs:
			log.Printf("Docker event error: %v", err)
			return
		}
	}
}

func getContainerIP(info types.ContainerJSON) string {
	if nw, ok := info.NetworkSettings.Networks["bridge"]; ok && nw.IPAddress != "" {
		return nw.IPAddress
	}

	for _, nw := range info.NetworkSettings.Networks {
		if nw.IPAddress != "" {
			return nw.IPAddress
		}
	}

	return ""
}

func processContainer(cli DockerClient, registry *Registry, id string, add bool) {
	info, err := cli.ContainerInspect(context.Background(), id)
	if err != nil {
		log.Printf("Error inspecting container %s: %v", id, err)
		return
	}

	labels := info.Config.Labels
	ip := getContainerIP(info)
	if ip == "" {
		log.Printf("No IP for container %s", info.Name)
		return
	}

	host, ok := labels["beacon.host"]
	if !ok {
		return
	}

	for _, h := range parseHosts(host) {
		if add {
			registry.Set(h, ip)
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
