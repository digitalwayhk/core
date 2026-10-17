// Package identity 管理稳定副本身份与持久生成器绑定，不承担服务发现。
package identity

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/digitalwayhk/core/pkg/utils"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

var segment = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

// GeneratorState 是副本卷中的编号和时间保护状态。
type GeneratorState struct {
	Assigned       bool   `json:"assigned"`
	Namespace      string `json:"namespace,omitempty"`
	Authority      string `json:"authority,omitempty"`
	AuthorityEpoch string `json:"authority_epoch,omitempty"`
	Encoding       string `json:"encoding,omitempty"`
	WorkerID       uint16 `json:"worker_id"`
	DataCenterID   uint   `json:"datacenter_id"`
	MachineID      uint   `json:"machine_id"`
	ReservedUntil  int64  `json:"reserved_until"`
}

// State 描述持久副本身份，启动标识不作为注册主键。
type State struct {
	Version        int    `json:"version"`
	Service        string `json:"service"`
	InstanceID     string `json:"instance_id"`
	GeneratorState `json:"generator"`
	BootID         string `json:"boot_id,omitempty"`
	BootEpoch      uint64 `json:"boot_epoch,omitempty"`
}

// File 独占持有身份文件的旁路锁，替换 JSON 文件不会释放锁。
type File struct {
	State  State
	path   string
	lock   *os.File
	mu     sync.Mutex
	closed bool
}

// Create 仅在空的独立状态目录排他创建身份，不覆盖已有数据。
func Create(path, service, instanceID string) error {
	if !filepath.IsAbs(path) || !segment.MatchString(service) || !segment.MatchString(instanceID) {
		return errors.New("identity: invalid path, service or instance")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	lock, err := lockFile(path + ".lock")
	if err != nil {
		return err
	}
	defer unlockFile(lock)
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != filepath.Base(path)+".lock" {
			return errors.New("identity: state directory is not empty; use audited import")
		}
	}
	data, err := json.Marshal(State{Version: 1, Service: service, InstanceID: instanceID})
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}

// Open 加载现有身份，缺失、损坏或不匹配均拒绝启动。
func Open(path, service, instanceID string) (*File, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("identity: absolute path required")
	}
	lock, err := lockFile(path + ".lock")
	if err != nil {
		return nil, err
	}
	file := &File{path: path, lock: lock}
	data, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(data, &file.State)
	}
	if err == nil && (file.State.Version != 1 || file.State.Service != service || !segment.MatchString(file.State.InstanceID) || instanceID != "" && file.State.InstanceID != instanceID) {
		err = errors.New("identity: invalid version or identity mismatch")
	}
	if err == nil {
		err = validateState(file.State)
	}
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("identity: load state: %w", err)
	}
	return file, nil
}

// Save 先同步临时文件再原子替换和同步目录，不覆写配置文件。
func (f *File) Save() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return errors.New("identity: state is closed")
	}
	data, err := json.Marshal(f.State)
	if err != nil {
		return err
	}
	return atomicWrite(f.path, data)
}

// Close 释放身份锁，可以重复调用。
func (f *File) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	return unlockFile(f.lock)
}

func atomicWrite(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".identity-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err = temp.Chmod(0600); err == nil {
		_, err = temp.Write(data)
	}
	if err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// Import 写入经过离线核对的存量身份，不覆盖现有身份或移动 pending。
func Import(path string, state State) error {
	if err := validateState(state); err != nil {
		return err
	}
	if !filepath.IsAbs(path) || state.Version != 1 || !segment.MatchString(state.Service) || !segment.MatchString(state.InstanceID) || !state.Assigned || state.WorkerID > 63 || state.ReservedUntil < 0 {
		return errors.New("identity: invalid import")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	lock, err := lockFile(path + ".lock")
	if err != nil {
		return err
	}
	defer unlockFile(lock)
	if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		return errors.New("identity: import must not overwrite existing state")
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}

func validateState(s State) error {
	if s.Version != 1 || !segment.MatchString(s.Service) || !segment.MatchString(s.InstanceID) {
		return errors.New("identity: invalid state identity")
	}
	if !s.Assigned {
		if s.GeneratorState != (GeneratorState{}) || s.BootID != "" || s.BootEpoch != 0 {
			return errors.New("identity: unassigned state contains generator data")
		}
		return nil
	}
	worker, err := utils.StableSnowflakeWorkerID(s.MachineID, s.DataCenterID)
	if err != nil || worker != s.WorkerID || s.Encoding != "legacy-decimal-v1" || !segment.MatchString(s.Namespace) || !segment.MatchString(s.AuthorityEpoch) || s.Authority == "" || s.ReservedUntil < 0 || (s.BootEpoch == 0 && s.BootID != "") || (s.BootEpoch > 0 && !segment.MatchString(s.BootID)) {
		return errors.New("identity: invalid generator state")
	}
	return nil
}
