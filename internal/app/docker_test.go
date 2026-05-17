package app

import (
	"context"
	"fmt"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/network"
)

// ---------------------------------------------------------------------------
// mockDockerClient
// ---------------------------------------------------------------------------

type mockDockerClient struct {
	containers []types.Container
	inspectFn  func(id string) (types.ContainerJSON, error)
	eventsCh   chan events.Message
	errsCh     chan error
}

func (m *mockDockerClient) ContainerList(_ context.Context, _ types.ContainerListOptions) ([]types.Container, error) {
	return m.containers, nil
}

func (m *mockDockerClient) ContainerInspect(_ context.Context, id string) (types.ContainerJSON, error) {
	if m.inspectFn != nil {
		return m.inspectFn(id)
	}
	return types.ContainerJSON{}, fmt.Errorf("not found: %s", id)
}

func (m *mockDockerClient) Events(_ context.Context, _ types.EventsOptions) (<-chan events.Message, <-chan error) {
	return m.eventsCh, m.errsCh
}

func makeInspectResult(id, name, ip, beaconHost string) types.ContainerJSON {
	labels := map[string]string{}
	if beaconHost != "" {
		labels["beacon.host"] = beaconHost
	}
	return types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			ID:   id,
			Name: "/" + name,
		},
		Config: &container.Config{
			Labels: labels,
		},
		NetworkSettings: &types.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"bridge": {IPAddress: ip},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// getContainerIP
// ---------------------------------------------------------------------------

func makeContainerJSON(networks map[string]string) types.ContainerJSON {
	nwMap := make(map[string]*network.EndpointSettings)
	for name, ip := range networks {
		nwMap[name] = &network.EndpointSettings{IPAddress: ip}
	}
	return types.ContainerJSON{
		NetworkSettings: &types.NetworkSettings{
			Networks: nwMap,
		},
	}
}

func TestGetContainerIP_BridgeNetwork(t *testing.T) {
	t.Parallel()
	info := makeContainerJSON(map[string]string{"bridge": "172.17.0.5"})
	ip := getContainerIP(info)
	if ip != "172.17.0.5" {
		t.Errorf("expected 172.17.0.5, got %s", ip)
	}
}

func TestGetContainerIP_BridgeTakesPriorityOverOthers(t *testing.T) {
	t.Parallel()
	info := makeContainerJSON(map[string]string{
		"bridge": "172.17.0.5",
		"custom": "10.0.0.5",
	})
	ip := getContainerIP(info)
	if ip != "172.17.0.5" {
		t.Errorf("expected bridge IP 172.17.0.5, got %s", ip)
	}
}

func TestGetContainerIP_NonBridgeNetwork(t *testing.T) {
	t.Parallel()
	info := makeContainerJSON(map[string]string{"custom": "10.0.0.2"})
	ip := getContainerIP(info)
	if ip != "10.0.0.2" {
		t.Errorf("expected 10.0.0.2, got %s", ip)
	}
}

func TestGetContainerIP_NoNetworks(t *testing.T) {
	t.Parallel()
	info := makeContainerJSON(map[string]string{})
	ip := getContainerIP(info)
	if ip != "" {
		t.Errorf("expected empty string for no networks, got %s", ip)
	}
}

func TestGetContainerIP_EmptyIPSkipped(t *testing.T) {
	t.Parallel()
	info := makeContainerJSON(map[string]string{
		"bridge": "",
		"custom": "192.168.1.5",
	})
	ip := getContainerIP(info)
	if ip != "192.168.1.5" {
		t.Errorf("expected 192.168.1.5 (bridge has empty IP), got %s", ip)
	}
}

func TestGetContainerIP_NilNetworks(t *testing.T) {
	t.Parallel()
	info := types.ContainerJSON{
		NetworkSettings: &types.NetworkSettings{
			Networks: nil,
		},
	}
	ip := getContainerIP(info)
	if ip != "" {
		t.Errorf("expected empty string for nil networks, got %s", ip)
	}
}

// ---------------------------------------------------------------------------
// processContainer
// ---------------------------------------------------------------------------

func makeInspectFn(id, name, ip, beaconHost string) func(string) (types.ContainerJSON, error) {
	labels := map[string]string{}
	if beaconHost != "" {
		labels["beacon.host"] = beaconHost
	}
	info := types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			ID:   id,
			Name: "/" + name,
		},
		Config: &container.Config{
			Labels: labels,
		},
		NetworkSettings: &types.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"bridge": {IPAddress: ip},
			},
		},
	}
	return func(gotID string) (types.ContainerJSON, error) {
		if gotID != id {
			return types.ContainerJSON{}, fmt.Errorf("unexpected id: %s", gotID)
		}
		return info, nil
	}
}

func TestProcessContainer_RegistersBeaconHost(t *testing.T) {
	t.Parallel()
	cli := &mockDockerClient{
		inspectFn: makeInspectFn("abc123", "myapp", "172.17.0.2", "myapp.beacon"),
	}
	r := NewRegistry()
	processContainer(cli, r, "abc123", true)

	ip, ok := r.Get("myapp.beacon")
	if !ok {
		t.Fatal("expected myapp.beacon to be registered")
	}
	if ip != "172.17.0.2" {
		t.Errorf("expected 172.17.0.2, got %s", ip)
	}
}

func TestProcessContainer_SkipsWithoutBeaconHostLabel(t *testing.T) {
	t.Parallel()
	cli := &mockDockerClient{
		inspectFn: makeInspectFn("abc123", "myapp", "172.17.0.2", ""),
	}
	r := NewRegistry()
	processContainer(cli, r, "abc123", true)

	// registry must remain empty
	_, ok := r.Get("myapp.beacon")
	if ok {
		t.Error("container without beacon.host label should not be registered")
	}
}

func TestProcessContainer_MultipleHosts(t *testing.T) {
	t.Parallel()
	cli := &mockDockerClient{
		inspectFn: makeInspectFn("abc123", "myapp", "172.17.0.2", "myapp.beacon, www.beacon"),
	}
	r := NewRegistry()
	processContainer(cli, r, "abc123", true)

	for _, host := range []string{"myapp.beacon", "www.beacon"} {
		if _, ok := r.Get(host); !ok {
			t.Errorf("expected %s to be registered", host)
		}
	}
}

func TestProcessContainer_RemovesHosts(t *testing.T) {
	t.Parallel()
	cli := &mockDockerClient{
		inspectFn: makeInspectFn("abc123", "myapp", "172.17.0.2", "myapp.beacon"),
	}
	r := NewRegistry()
	processContainer(cli, r, "abc123", true)
	processContainer(cli, r, "abc123", false)

	if _, ok := r.Get("myapp.beacon"); ok {
		t.Error("expected myapp.beacon to be removed")
	}
}

func TestProcessContainer_SkipsEmptyIP(t *testing.T) {
	t.Parallel()
	cli := &mockDockerClient{
		inspectFn: makeInspectFn("abc123", "myapp", "", "myapp.beacon"),
	}
	r := NewRegistry()
	processContainer(cli, r, "abc123", true)

	if _, ok := r.Get("myapp.beacon"); ok {
		t.Error("container with no IP should not be registered")
	}
}

func TestProcessContainer_InspectError(t *testing.T) {
	t.Parallel()
	cli := &mockDockerClient{
		inspectFn: func(id string) (types.ContainerJSON, error) {
			return types.ContainerJSON{}, fmt.Errorf("not found")
		},
	}
	r := NewRegistry()
	// should not panic
	processContainer(cli, r, "badid", true)
}

// ---------------------------------------------------------------------------
// removeContainer
// ---------------------------------------------------------------------------

func TestRemoveContainer_RemovesLabelledHost(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Set("my-app.beacon", "172.17.0.2")

	removeContainer(map[string]string{"beacon.host": "my-app.beacon"}, r)

	_, ok := r.Get("my-app.beacon")
	if ok {
		t.Error("expected entry to be removed")
	}
}

func TestRemoveContainer_MultipleHosts(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Set("app.beacon", "172.17.0.2")
	r.Set("www.beacon", "172.17.0.2")

	removeContainer(map[string]string{"beacon.host": "app.beacon,www.beacon"}, r)

	for _, host := range []string{"app.beacon", "www.beacon"} {
		if _, ok := r.Get(host); ok {
			t.Errorf("expected %s to be removed", host)
		}
	}
}

func TestRemoveContainer_NoLabelIsNoOp(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Set("other.beacon", "172.17.0.5")

	// no beacon.host attribute
	removeContainer(map[string]string{"some.other.label": "value"}, r)

	if _, ok := r.Get("other.beacon"); !ok {
		t.Error("unrelated entry should not be removed")
	}
}

func TestRemoveContainer_EmptyAttrs(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	// should not panic
	removeContainer(map[string]string{}, r)
}

func TestRemoveContainer_NilAttrs(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	// should not panic
	removeContainer(nil, r)
}

func TestRemoveContainer_TrimsWhitespace(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Set("app.beacon", "172.17.0.2")
	r.Set("api.beacon", "172.17.0.3")

	removeContainer(map[string]string{"beacon.host": " app.beacon , api.beacon "}, r)

	for _, host := range []string{"app.beacon", "api.beacon"} {
		if _, ok := r.Get(host); ok {
			t.Errorf("expected %s to be removed after whitespace trim", host)
		}
	}
}

func TestRemoveContainer_EmptyCommaValues(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Set("app.beacon", "172.17.0.2")

	// extra commas produce empty strings — should not panic
	removeContainer(map[string]string{"beacon.host": "app.beacon,,,"}, r)

	if _, ok := r.Get("app.beacon"); ok {
		t.Error("expected app.beacon to be removed")
	}
}

// ---------------------------------------------------------------------------
// loadRunning
// ---------------------------------------------------------------------------

func TestLoadRunning_RegistersAllContainersWithLabel(t *testing.T) {
	t.Parallel()
	cli := &mockDockerClient{
		containers: []types.Container{
			{ID: "id1"},
			{ID: "id2"},
		},
		inspectFn: func(id string) (types.ContainerJSON, error) {
			hosts := map[string]string{
				"id1": "app1.beacon",
				"id2": "app2.beacon",
			}
			ips := map[string]string{
				"id1": "172.17.0.2",
				"id2": "172.17.0.3",
			}
			return types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{ID: id, Name: "/" + id},
				Config:            &container.Config{Labels: map[string]string{"beacon.host": hosts[id]}},
				NetworkSettings: &types.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"bridge": {IPAddress: ips[id]},
					},
				},
			}, nil
		},
	}
	r := NewRegistry()
	loadRunning(cli, r)

	for host, expectedIP := range map[string]string{
		"app1.beacon": "172.17.0.2",
		"app2.beacon": "172.17.0.3",
	} {
		ip, ok := r.Get(host)
		if !ok {
			t.Errorf("expected %s to be registered", host)
			continue
		}
		if ip != expectedIP {
			t.Errorf("%s: expected %s, got %s", host, expectedIP, ip)
		}
	}
}
