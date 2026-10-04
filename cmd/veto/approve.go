package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/aiveto/veto/config"
	"github.com/aiveto/veto/jsonopts"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/valkey"
	"github.com/spf13/cobra"
)

func newApproveCommand() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "approve <id>",
		Short: "Record approval for a pending confirmation.",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			approved, err := approveID(cmd.Context(), args[0], configPath)
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

func approveID(ctx context.Context, id, configPath string) (string, error) {
	state := policy.NewState()
	if err := applyApprovalCLI(ctx, state, configPath); err != nil {
		return "", err
	}
	return state.Approve(ctx, id)
}

func applyApprovalCLI(ctx context.Context, s *policy.State, configPath string) error {
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
		return applyApprovalConfig(ctx, s, cfg)
	}
	return applyApprovalEnv(ctx, s)
}

// applyApprovalEnv points state at the store serve and approve share.
// VETO_APPROVAL_STORE is a Valkey or Redis URL. Otherwise the directory and signing secret come from the environment.
func applyApprovalEnv(ctx context.Context, s *policy.State) error {
	return applyApproval(ctx, s, 0, os.Getenv("VETO_APPROVAL_STORE"), jsonopts.Set{})
}

func applyApprovalConfig(ctx context.Context, s *policy.State, cfg config.File) error {
	url := os.Getenv("VETO_APPROVAL_STORE")
	if url == "" && cfg.ApprovalStore != "" {
		url = os.Getenv(cfg.ApprovalStore)
		if url == "" {
			return fmt.Errorf("approval store %s is unset", cfg.ApprovalStore)
		}
	}
	return applyApproval(ctx, s, cfg.ApprovalTTL, url, cfg.JSONSet())
}

func applyApproval(ctx context.Context, s *policy.State, ttl time.Duration, storeURL string, json jsonopts.Set) error {
	dir := ""
	if storeURL == "" {
		dir = os.Getenv("VETO_APPROVAL_NONCE_DIR")
		if dir == "" && os.Getenv("VETO_APPROVAL_SECRET") == "" {
			var err error
			dir, err = policy.DefaultApprovalDir()
			if err != nil {
				return err
			}
		}
	}
	return s.Open(ctx, policy.StoreOptions{
		URL:    storeURL,
		Dir:    dir,
		Secret: []byte(os.Getenv("VETO_APPROVAL_SECRET")),
		TTL:    ttl,
		JSON:   json,
		Dial: func(ctx context.Context, url string) (policy.Store, error) {
			store, err := valkey.Dial(ctx, url)
			if err != nil {
				return nil, err
			}
			store.JSON = json
			return store, nil
		},
	})
}
