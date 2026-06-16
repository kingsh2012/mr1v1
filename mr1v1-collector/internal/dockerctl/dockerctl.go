// Package dockerctl manages the lifecycle of per-match rehlds containers via
// the Docker Engine API (/var/run/docker.sock). See
// AGENT_ARCHITECTURE_DESIGN.md sections 3/5/7.
package dockerctl

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

// LabelMatchID is set on every container created for a match, so it can be
// looked up again at teardown time without the agent having to keep its own
// match_id -> container_id map across restarts.
const LabelMatchID = "mr1v1.match_id"

// Client wraps the Docker Engine API client.
type Client struct {
	cli *client.Client
}

// New connects to the local Docker daemon via /var/run/docker.sock.
func New() (*Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("connect docker daemon: %w", err)
	}
	return &Client{cli: cli}, nil
}

// Close closes the underlying Docker client connection.
func (c *Client) Close() error {
	return c.cli.Close()
}

// Spec describes the rehlds container to create for a single match.
// Fields map directly to the environment variable contract in
// AGENT_ARCHITECTURE_DESIGN.md section 5.
type Spec struct {
	MatchID    string
	ServerName string
	Port       int
	P0SteamID  string
	P1SteamID  string
	// Image is the rehlds image to run. Image tag/pull strategy is still
	// TBD (see AGENT_ARCHITECTURE_DESIGN.md section 5) - for now the agent
	// assumes the image is already present locally and does not pull it.
	Image string
	// GatewayHTTP is the agent's own /record endpoint, injected so the
	// container's start.sh can point amxx at it (network_mode: host means
	// 127.0.0.1 from inside the container reaches the host).
	GatewayHTTP string
	// RCONPassword is generated per-match by the agent and injected so the
	// agent can later RCON into the container to trigger the destroy
	// countdown (see AGENT_ARCHITECTURE_DESIGN.md section 6).
	RCONPassword string
}

func containerName(matchID string) string {
	return "mr1v1-match-" + matchID
}

// CreateAndStart creates and starts a container for the given match,
// returning its container ID. The container runs with network_mode: host
// and is labeled with the match_id for later lookup.
func (c *Client) CreateAndStart(ctx context.Context, spec Spec) (string, error) {
	env := []string{
		"MATCH_ID=" + spec.MatchID,
		"P0_STEAMID=" + spec.P0SteamID,
		"P1_STEAMID=" + spec.P1SteamID,
		"SERVER_NAME=" + spec.ServerName,
		fmt.Sprintf("PORT=%d", spec.Port),
		"GATEWAY_HTTP=" + spec.GatewayHTTP,
		"RCON_PASSWORD=" + spec.RCONPassword,
	}

	if err := c.ensureImage(ctx, spec.Image); err != nil {
		return "", fmt.Errorf("pull image %s: %w", spec.Image, err)
	}

	resp, err := c.cli.ContainerCreate(ctx,
		&container.Config{
			Image:  spec.Image,
			Env:    env,
			Labels: map[string]string{LabelMatchID: spec.MatchID},
		},
		&container.HostConfig{
			NetworkMode: "host",
			AutoRemove:  false,
		},
		nil, nil, containerName(spec.MatchID))
	if err != nil {
		return "", fmt.Errorf("create container for match %s: %w", spec.MatchID, err)
	}

	if err := c.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("start container %s for match %s: %w", resp.ID, spec.MatchID, err)
	}
	return resp.ID, nil
}

// ensureImage pulls the image if it is not already present locally.
// It discards the pull output stream but logs progress at INFO level.
func (c *Client) ensureImage(ctx context.Context, ref string) error {
	_, _, err := c.cli.ImageInspectWithRaw(ctx, ref)
	if err == nil {
		return nil // already present
	}
	slog.Info("pulling image", "ref", ref)
	rc, err := c.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return err
	}
	defer rc.Close()
	_, _ = io.Copy(io.Discard, rc) // must drain to completion
	slog.Info("image pulled", "ref", ref)
	return nil
}

// ListMatchIDs returns the match_ids of all currently running mr1v1 match containers.
func (c *Client) ListMatchIDs(ctx context.Context) ([]string, error) {
	f := filters.NewArgs(filters.Arg("label", LabelMatchID))
	containers, err := c.cli.ContainerList(ctx, container.ListOptions{Filters: f})
	if err != nil {
		return nil, fmt.Errorf("list match containers: %w", err)
	}
	ids := make([]string, 0, len(containers))
	for _, ct := range containers {
		if mid := ct.Labels[LabelMatchID]; mid != "" {
			ids = append(ids, mid)
		}
	}
	return ids, nil
}

// StopAndRemoveByMatchID finds the container labeled with matchID, stops it
// (with the given grace period) and removes it. Returns nil if no matching
// container exists (already torn down).
func (c *Client) StopAndRemoveByMatchID(ctx context.Context, matchID string, timeout time.Duration) error {
	f := filters.NewArgs(filters.Arg("label", LabelMatchID+"="+matchID))
	containers, err := c.cli.ContainerList(ctx, container.ListOptions{All: true, Filters: f})
	if err != nil {
		return fmt.Errorf("list containers for match %s: %w", matchID, err)
	}
	if len(containers) == 0 {
		return nil
	}

	for _, ct := range containers {
		timeoutSec := int(timeout.Seconds())
		if err := c.cli.ContainerStop(ctx, ct.ID, container.StopOptions{Timeout: &timeoutSec}); err != nil {
			return fmt.Errorf("stop container %s for match %s: %w", ct.ID, matchID, err)
		}
		if err := c.cli.ContainerRemove(ctx, ct.ID, container.RemoveOptions{Force: true}); err != nil {
			return fmt.Errorf("remove container %s for match %s: %w", ct.ID, matchID, err)
		}
	}
	return nil
}
