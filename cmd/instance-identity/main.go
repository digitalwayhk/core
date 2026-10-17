// 本命令离线初始化/导入/退役稳定实例身份，不启动业务服务，不删除业务数据。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/identity"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/gofrs/uuid"
)

type importManifest struct {
	State           identity.State `json:"state"`
	StoppedEvidence string         `json:"stopped_evidence"`
	PendingPath     string         `json:"pending_path"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: instance-identity {init-authority|create|import|recover|retire|capacity} --config file")
	}
	command := args[0]
	switch command {
	case "init-authority", "create", "import", "recover", "retire", "capacity":
	default:
		return errors.New("unknown identity operation")
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", "", "只读服务配置")
	stateDir := fs.String("state-dir", "", "独立持久卷目录")
	service := fs.String("service", "", "逻辑服务名")
	instance := fs.String("instance-id", "", "稳定副本 ID")
	manifest := fs.String("manifest", "", "经人工核对的导入/退役清单")
	apply := fs.Bool("apply", false, "明确应用退役")
	dry := fs.Bool("dry-run", false, "只读预览")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	allowed := map[string]bool{"config": true}
	switch command {
	case "create":
		for _, name := range []string{"service", "state-dir", "instance-id"} {
			allowed[name] = true
		}
	case "import":
		allowed["manifest"], allowed["state-dir"] = true, true
	case "recover", "retire":
		allowed["manifest"], allowed["apply"], allowed["dry-run"] = true, true, true
	}
	var flagErr error
	fs.Visit(func(f *flag.Flag) {
		if !allowed[f.Name] {
			flagErr = fmt.Errorf("flag --%s is unsupported for %s", f.Name, command)
		}
	})
	if flagErr != nil {
		return flagErr
	}
	if fs.NArg() != 0 || *configPath == "" {
		return errors.New("explicit --config is required")
	}
	data, err := os.ReadFile(*configPath)
	if err != nil {
		return err
	}
	var c config.ServerConfig
	if err = json.Unmarshal(data, &c); err != nil {
		return err
	}
	if *stateDir != "" {
		if !filepath.IsAbs(*stateDir) {
			return errors.New("absolute --state-dir required")
		}
		c.Cluster.IdentityFile = filepath.Join(*stateDir, "identity.json")
	}
	c.ApplyDefaults()
	if err = c.Validate(); err != nil {
		return err
	}
	if c.Cluster.IdentityMode != "stable" {
		return errors.New("stable configuration required")
	}
	a, name, err := identity.BuildAuthority(c.IDGenerator)
	if err != nil {
		return err
	}
	defer a.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if command == "init-authority" {
		if c.IDGenerator.Provider == "local" {
			if err = os.MkdirAll(filepath.Dir(c.IDGenerator.LocalPath), 0700); err != nil {
				return err
			}
		}
		return a.Initialize(ctx)
	}
	if err = a.Check(ctx); err != nil {
		return err
	}
	switch command {
	case "create":
		if *service == "" || *service != c.Name || *stateDir == "" {
			return errors.New("create requires --service matching configuration and --state-dir")
		}
		if *instance == "" {
			*instance = c.Cluster.InstanceID
			if *instance == "" {
				*instance = uuid.Must(uuid.NewV4()).String()
			}
		}
		if c.Cluster.InstanceID != "" && c.Cluster.InstanceID != *instance {
			return errors.New("configured instance mismatch")
		}
		if _, err = a.Get(ctx, identity.InstanceKey(*service, *instance)); err == nil {
			return errors.New("existing authority binding requires audited import")
		} else if !errors.Is(err, identity.ErrBindingConflict) {
			return err
		}
		return identity.Create(c.Cluster.IdentityFile, *service, *instance)
	case "import":
		data, err = os.ReadFile(*manifest)
		if err != nil {
			return err
		}
		var m importManifest
		if err = json.Unmarshal(data, &m); err != nil {
			return err
		}
		s := m.State
		if m.StoppedEvidence == "" || !filepath.IsAbs(m.PendingPath) || s.Service != c.Name || s.Namespace != c.IDGenerator.Namespace || s.AuthorityEpoch != c.IDGenerator.Epoch || s.Encoding != "legacy-decimal-v1" || !s.Assigned {
			return errors.New("import requires matching identity, explicit pending path, layout and stopped-process evidence")
		}
		worker, err := utils.StableSnowflakeWorkerID(s.MachineID, s.DataCenterID)
		if err != nil {
			return err
		}
		if worker != s.WorkerID {
			return identity.ErrBindingConflict
		}
		b, err := a.Bind(ctx, s.Service, s.InstanceID, s.DataCenterID, s.MachineID, 31, false)
		if err != nil {
			return err
		}
		if err = a.ImportReservation(ctx, b.Key, s.ReservedUntil); err != nil {
			return err
		}
		b, err = a.Get(ctx, b.Key)
		if err != nil {
			return err
		}
		s.Authority = name
		s.BootID = b.BootID
		s.BootEpoch = b.BootEpoch
		s.ReservedUntil = max(s.ReservedUntil, b.ReservedUntil)
		return identity.Import(c.Cluster.IdentityFile, s)
	case "recover":
		if *apply && *dry {
			return errors.New("choose dry-run or apply")
		}
		data, err = os.ReadFile(*manifest)
		if err != nil {
			return err
		}
		var proof identity.RecoveryProof
		if err = json.Unmarshal(data, &proof); err != nil {
			return err
		}
		f, err := identity.Open(c.Cluster.IdentityFile, c.Name, c.Cluster.InstanceID)
		if err != nil {
			return err
		}
		defer f.Close()
		if !f.State.Assigned || f.State.Authority != name || f.State.Namespace != c.IDGenerator.Namespace || f.State.AuthorityEpoch != c.IDGenerator.Epoch || proof.Key != identity.InstanceKey(f.State.Service, f.State.InstanceID) || proof.BootID != f.State.BootID || proof.BootEpoch != f.State.BootEpoch || proof.ReservedUntil < f.State.ReservedUntil {
			return identity.ErrBindingConflict
		}
		return a.RecoverStoppedBoot(ctx, proof, !*apply)
	case "retire":
		if *apply && *dry {
			return errors.New("choose dry-run or apply")
		}
		data, err = os.ReadFile(*manifest)
		if err != nil {
			return err
		}
		var proof identity.RetirementProof
		if err = json.Unmarshal(data, &proof); err != nil {
			return err
		}
		return a.Retire(ctx, proof, !*apply)
	case "capacity":
		count, err := a.Capacity(ctx, c.DataCenterID, c.Cluster.Claim.MachineIDMax)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(count)
	}
	return nil
}
