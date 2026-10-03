package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/aiveto/veto/config"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/valkeystore"
	"github.com/spf13/cobra"
)

func newApproveCommand() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "approve <id>",
		Short: "Record approval for a pending confirmation.",
		Args:  cobra.ExactArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			approved, err := approveID(args[0], configPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "approve: %v\n", err)
				exitMain(1)
			}
			fmt.Println(approved)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "Path to veto.yaml. Reads approval_store and approval_ttl.")
	return cmd
}

func approveID(id, configPath string) (string, error) {
	state := policy.NewState()
	if err := applyApprovalCLI(state, configPath); err != nil {
		return "", err
	}
	return state.Approve(id)
}

func applyApprovalCLI(s *policy.State, configPath string) error {
	if configPath == "" {
		if _, err := os.Stat("veto.yaml"); err == nil {
			configPath = "veto.yaml"
		}
	}
	if configPath != "" {
		cfg, err := config.Load(configPath)
		if err != nil {
			return err
		}
		return applyApprovalConfig(s, cfg)
	}
	return applyApprovalEnv(s)
}

// applyApprovalEnv points state at the store serve and approve share.
// VETO_APPROVAL_STORE is a Valkey or Redis URL. Otherwise the directory and signing secret come from the environment.
func applyApprovalEnv(s *policy.State) error {
	return applyApproval(s, 0, os.Getenv("VETO_APPROVAL_STORE"))
}

func applyApprovalConfig(s *policy.State, cfg config.File) error {
	url := os.Getenv("VETO_APPROVAL_STORE")
	if url == "" && cfg.ApprovalStore != "" {
		url = os.Getenv(cfg.ApprovalStore)
		if url == "" {
			return fmt.Errorf("approval store %s is unset", cfg.ApprovalStore)
		}
	}
	return applyApproval(s, cfg.ApprovalTTL, url)
}

func applyApproval(s *policy.State, ttl time.Duration, storeURL string) error {
	if s == nil {
		return errors.New("missing approval state")
	}
	if storeURL != "" {
		st, err := valkeystore.Dial(storeURL)
		if err != nil {
			return err
		}
		s.SetStore(st)
	} else {
		dir := os.Getenv("VETO_APPROVAL_NONCE_DIR")
		if dir == "" && os.Getenv("VETO_APPROVAL_SECRET") == "" {
			var err error
			dir, err = policy.DefaultApprovalDir()
			if err != nil {
				return err
			}
		}
		if dir != "" {
			s.SetNonceDir(dir)
		}
	}
	if secret := os.Getenv("VETO_APPROVAL_SECRET"); secret != "" {
		return s.SetSigner([]byte(secret), ttl)
	}
	return nil
}
