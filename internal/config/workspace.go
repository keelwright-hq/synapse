// Package config loads and validates Synapse workspace files (SYN-12).
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/keelwright-hq/synapse/internal/uri"
	"gopkg.in/yaml.v3"
)

const (
	// CurrentVersion is the only supported synapse.yaml version.
	CurrentVersion = 1
	// DefaultFileName is looked up when Load is given a directory.
	DefaultFileName = "synapse.yaml"
)

// Workspace is a multi-repo Synapse workspace definition.
type Workspace struct {
	Version int    `yaml:"version"`
	Repos   []Repo `yaml:"repos"`
	// ConfigPath is the absolute path to the loaded yaml (set by Load).
	ConfigPath string `yaml:"-"`
	// ConfigDir is the directory containing the yaml (paths resolve relative to it).
	ConfigDir string `yaml:"-"`
}

// Repo is one workspace member: a logical repo:// name and a local path.
type Repo struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"` // absolute after Load
}

// Load reads a synapse.yaml file or a directory containing one.
func Load(path string) (*Workspace, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("config: resolve %q: %w", path, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	file := abs
	if info.IsDir() {
		file = filepath.Join(abs, DefaultFileName)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", file, err)
	}
	var ws Workspace
	if err := yaml.Unmarshal(data, &ws); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", file, err)
	}
	ws.ConfigPath = file
	ws.ConfigDir = filepath.Dir(file)
	if err := ws.validate(); err != nil {
		return nil, err
	}
	return &ws, nil
}

func (ws *Workspace) validate() error {
	if ws.Version != CurrentVersion {
		return fmt.Errorf("config: unsupported version %d (want %d); set version: %d in %s",
			ws.Version, CurrentVersion, CurrentVersion, ws.ConfigPath)
	}
	if len(ws.Repos) == 0 {
		return fmt.Errorf("config: %s must list at least one repo under repos:", ws.ConfigPath)
	}
	seen := make(map[string]int, len(ws.Repos))
	for i := range ws.Repos {
		r := &ws.Repos[i]
		if r.Name == "" {
			return fmt.Errorf("config: repos[%d] missing name in %s", i, ws.ConfigPath)
		}
		name, err := uri.NormalizeRepo(r.Name)
		if err != nil {
			return fmt.Errorf("config: repos[%d].name %q is invalid: %w (use [A-Za-z0-9._-]+)", i, r.Name, err)
		}
		r.Name = name
		if prev, ok := seen[name]; ok {
			return fmt.Errorf("config: duplicate repo name %q (repos[%d] and repos[%d]); use distinct logical names",
				name, prev, i)
		}
		seen[name] = i

		if r.Path == "" {
			return fmt.Errorf("config: repos[%d] (%s) missing path in %s", i, name, ws.ConfigPath)
		}
		resolved := r.Path
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(ws.ConfigDir, resolved)
		}
		resolved = filepath.Clean(resolved)
		info, err := os.Stat(resolved)
		if err != nil {
			return fmt.Errorf("config: repos[%d] (%s) path %q: %w", i, name, r.Path, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("config: repos[%d] (%s) path %q is not a directory", i, name, resolved)
		}
		r.Path = resolved
	}
	return nil
}

// RepoRoots returns a map of logical repo name → absolute filesystem root.
func (ws *Workspace) RepoRoots() map[string]string {
	out := make(map[string]string, len(ws.Repos))
	for _, r := range ws.Repos {
		out[r.Name] = r.Path
	}
	return out
}

// Lookup returns the member with the given logical name.
func (ws *Workspace) Lookup(name string) (Repo, error) {
	name, err := uri.NormalizeRepo(name)
	if err != nil {
		return Repo{}, err
	}
	for _, r := range ws.Repos {
		if r.Name == name {
			return r, nil
		}
	}
	return Repo{}, fmt.Errorf("config: repo %q is not a workspace member", name)
}
