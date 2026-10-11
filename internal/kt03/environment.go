package kt03

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"transport-monitoring/internal/kt02"
)

func (r *runner) external(program string, args ...string) ([]byte, error) {
	c := exec.CommandContext(r.ctx, program, args...)
	c.Dir = r.root
	b, err := c.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, b)
	}
	return b, nil
}
func (r *runner) environment() error {
	b, err := r.external("docker", "info", "--format", "{{json .}}")
	if err != nil {
		return err
	}
	var info map[string]any
	if err = json.Unmarshal(b, &info); err != nil {
		return err
	}
	b, err = r.external("docker", "compose", "--profile", "test", "ps", "-q", "db")
	if err != nil {
		return err
	}
	id := strings.TrimSpace(string(b))
	if id == "" {
		return fmt.Errorf("database container is not running")
	}
	b, err = r.external("docker", "inspect", id, "--format", "{{json .}}")
	if err != nil {
		return err
	}
	var container struct {
		Image      string
		Config     struct{ Image string }
		HostConfig struct{ NanoCpus, CpuQuota, CpuPeriod, Memory, ShmSize int64 }
	}
	if err = json.Unmarshal(b, &container); err != nil {
		return err
	}
	goVersion, err := r.external("go", "version")
	if err != nil {
		return err
	}
	compose, err := r.external("docker", "compose", "version", "--short")
	if err != nil {
		return err
	}
	pg, err := r.sql("postgres", `SELECT jsonb_build_object('version',version(),'settings',
 (SELECT jsonb_object_agg(name,jsonb_build_object('value',setting,'unit',unit)) FROM pg_settings
 WHERE name IN ('shared_buffers','work_mem','maintenance_work_mem','effective_cache_size','max_connections','max_parallel_workers_per_gather','max_parallel_workers','max_worker_processes','jit','fsync','synchronous_commit','TimeZone')));`)
	if err != nil {
		return err
	}
	return r.save("environment.json", map[string]any{
		"captured_at_utc": time.Now().UTC().Format(time.RFC3339), "go_version": strings.TrimSpace(string(goVersion)), "compose_version": strings.TrimSpace(string(compose)),
		"docker":     map[string]any{"server_version": info["ServerVersion"], "operating_system": info["OperatingSystem"], "architecture": info["Architecture"], "cpus": info["NCPU"], "memory_bytes": info["MemTotal"]},
		"container":  map[string]any{"image_tag": container.Config.Image, "image_id": container.Image, "cpu_nano_limit": container.HostConfig.NanoCpus, "cpu_quota": container.HostConfig.CpuQuota, "cpu_period": container.HostConfig.CpuPeriod, "memory_limit_bytes": container.HostConfig.Memory, "shared_memory_bytes": container.HostConfig.ShmSize},
		"postgresql": json.RawMessage(strings.TrimSpace(pg))})
}

func (r *runner) regression() (retErr error) {
	if err := r.invalidate("regression.json"); err != nil {
		return err
	}
	path := filepath.Join(r.root, "submission/kt02-validation.json")
	before, err := os.ReadFile(path)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	defer func() {
		var e error
		if exists {
			e = os.WriteFile(path, before, 0644)
		} else {
			e = os.Remove(path)
			if errors.Is(e, os.ErrNotExist) {
				e = nil
			}
		}
		retErr = errors.Join(retErr, e)
	}()
	var out bytes.Buffer
	err = kt02.Run(r.ctx, []string{"test", "--root", r.root}, &out, os.Stderr)
	if e := r.saveText("regression.txt", out.String()); e != nil {
		return e
	}
	if err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fmt.Println("PASS KT-02 regression on migrations 001–005")
	return r.saveText("regression.json", string(b))
}
