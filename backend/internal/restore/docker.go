package restore

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dbvault/dbvault/backend/internal/engine"
)

const sandboxLabel = "dbvault.verify"

// DockerSandbox starts a temporary PostgreSQL container per restore test,
// matching the backup's major version, and removes it afterwards. It talks
// to the Docker Engine API directly (no Docker SDK dependency).
type DockerSandbox struct {
	client        *http.Client
	base          string
	network       string
	imageTemplate string
}

// NewDockerSandbox connects to host (unix:///var/run/docker.sock or tcp://host:port).
func NewDockerSandbox(host, network, imageTemplate string) (*DockerSandbox, error) {
	u, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("invalid VERIFY_DOCKER_HOST: %w", err)
	}
	d := &DockerSandbox{network: network, imageTemplate: imageTemplate}
	switch u.Scheme {
	case "unix":
		sock := u.Path
		d.client = &http.Client{Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", sock)
			},
		}}
		d.base = "http://docker"
	case "tcp", "http":
		d.client = &http.Client{}
		d.base = "http://" + u.Host
	default:
		return nil, fmt.Errorf("unsupported VERIFY_DOCKER_HOST scheme %q", u.Scheme)
	}
	return d, nil
}

func (d *DockerSandbox) Name() string { return "docker" }

func (d *DockerSandbox) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, d.base+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("docker API unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		var e struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(msg, &e) == nil && e.Message != "" {
			return &dockerError{status: resp.StatusCode, msg: e.Message}
		}
		return &dockerError{status: resp.StatusCode, msg: strings.TrimSpace(string(msg))}
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

type dockerError struct {
	status int
	msg    string
}

func (e *dockerError) Error() string { return fmt.Sprintf("docker: %s (HTTP %d)", e.msg, e.status) }

func (d *DockerSandbox) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, d.base+"/_ping", nil)
	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("docker is not reachable: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("docker ping returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// image returns the sandbox image. VERIFY_DOCKER_IMAGE (with {major})
// overrides the PostgreSQL image; other engines use their driver's default.
func (d *DockerSandbox) image(drv engine.Driver, spec engine.SandboxSpec, major int) string {
	if drv.Name() == engine.Postgres && d.imageTemplate != "" {
		if major <= 0 {
			major = 17
		}
		return strings.ReplaceAll(d.imageTemplate, "{major}", strconv.Itoa(major))
	}
	return spec.Image
}

func (d *DockerSandbox) ensureImage(ctx context.Context, image string) error {
	if err := d.do(ctx, http.MethodGet, "/images/"+url.PathEscape(image)+"/json", nil, nil); err == nil {
		return nil
	}
	name, tag := image, "latest"
	if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") {
		name, tag = image[:i], image[i+1:]
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, d.base+"/images/create?fromImage="+url.QueryEscape(name)+"&tag="+url.QueryEscape(tag), nil)
	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("pull %s: %w", image, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("pull %s: %s", image, strings.TrimSpace(string(msg)))
	}
	// The pull streams JSON progress messages; errors arrive in-band.
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		var m struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &m) == nil && m.Error != "" {
			return fmt.Errorf("pull %s: %s", image, m.Error)
		}
	}
	return sc.Err()
}

func (d *DockerSandbox) Provision(ctx context.Context, drv engine.Driver, major int) (*Instance, error) {
	password := randomHex(16)
	spec := drv.Sandbox(major, password)
	image := d.image(drv, spec, major)
	if err := d.ensureImage(ctx, image); err != nil {
		return nil, err
	}
	portKey := strconv.Itoa(spec.Port) + "/tcp"
	name := "dbvault-verify-" + randomHex(6)
	hostConfig := map[string]any{
		"AutoRemove": false,
		"ShmSize":    256 << 20,
	}
	if d.network != "" {
		hostConfig["NetworkMode"] = d.network
	} else {
		hostConfig["PortBindings"] = map[string]any{portKey: []map[string]string{{"HostIp": "127.0.0.1", "HostPort": ""}}}
	}
	var created struct {
		ID string `json:"Id"`
	}
	err := d.do(ctx, http.MethodPost, "/containers/create?name="+name, map[string]any{
		"Image":        image,
		"Env":          spec.Env,
		"Labels":       map[string]string{sandboxLabel: "true"},
		"ExposedPorts": map[string]any{portKey: map[string]any{}},
		"HostConfig":   hostConfig,
	}, &created)
	if err != nil {
		return nil, fmt.Errorf("create sandbox container: %w", err)
	}
	inst := &Instance{
		DBName:      spec.Database,
		Description: "temporary Docker container " + image,
		destroy: func(ctx context.Context) error {
			return d.do(ctx, http.MethodDelete, "/containers/"+created.ID+"?force=true&v=true", nil, nil)
		},
	}
	if err := d.do(ctx, http.MethodPost, "/containers/"+created.ID+"/start", nil, nil); err != nil {
		_ = inst.Destroy()
		return nil, fmt.Errorf("start sandbox container: %w", err)
	}
	var info struct {
		NetworkSettings struct {
			Ports map[string][]struct {
				HostIP   string `json:"HostIp"`
				HostPort string `json:"HostPort"`
			} `json:"Ports"`
		} `json:"NetworkSettings"`
	}
	if err := d.do(ctx, http.MethodGet, "/containers/"+created.ID+"/json", nil, &info); err != nil {
		_ = inst.Destroy()
		return nil, err
	}
	target := engine.Target{Engine: drv.Name(), Username: spec.Username, Password: spec.Password, Database: spec.Database, SSLMode: spec.SSLMode}
	if d.network != "" {
		target.Host, target.Port = name, spec.Port
	} else {
		bindings := info.NetworkSettings.Ports[portKey]
		if len(bindings) == 0 {
			_ = inst.Destroy()
			return nil, errors.New("sandbox container has no published port")
		}
		target.Host = "127.0.0.1"
		target.Port, _ = strconv.Atoi(bindings[0].HostPort)
	}
	inst.Target = target
	if err := waitReady(ctx, drv, target, 180*time.Second); err != nil {
		_ = inst.Destroy()
		return nil, err
	}
	return inst, nil
}

// RemoveStale deletes sandbox containers left behind by a crashed worker.
func (d *DockerSandbox) RemoveStale(ctx context.Context, olderThan time.Duration) (int, error) {
	filters, _ := json.Marshal(map[string][]string{"label": {sandboxLabel + "=true"}})
	var list []struct {
		ID      string `json:"Id"`
		Created int64  `json:"Created"`
	}
	if err := d.do(ctx, http.MethodGet, "/containers/json?all=true&filters="+url.QueryEscape(string(filters)), nil, &list); err != nil {
		return 0, err
	}
	n := 0
	for _, c := range list {
		if time.Since(time.Unix(c.Created, 0)) > olderThan {
			if err := d.do(ctx, http.MethodDelete, "/containers/"+c.ID+"?force=true&v=true", nil, nil); err == nil {
				n++
			}
		}
	}
	return n, nil
}
