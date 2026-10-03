package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/aiveto/veto/policy"
	"github.com/spf13/cobra"
)

func newApproveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "approve <id>",
		Short: "Record approval for a pending confirmation.",
		Args:  cobra.ExactArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			approved, err := approveID(args[0])
			if err != nil {
				fmt.Fprintf(os.Stderr, "approve: %v\n", err)
				exitMain(1)
			}
			fmt.Println(approved)
		},
	}
}

func approveID(id string) (string, error) {
	state := policy.NewState()
	if err := applyApprovalEnv(state, 0); err != nil {
		return "", err
	}
	return state.Approve(id)
}

// applyApprovalEnv points state at the approval directory serve and approve share.
// The directory and the signing secret come from the environment. ttl is the signed approval lifetime.
// Zero ttl keeps the 15 minute default. An empty secret leaves the approval unsigned.
func applyApprovalEnv(s *policy.State, ttl time.Duration) error {
	if s == nil {
		return errors.New("missing approval state")
	}
	dir := os.Getenv("VETO_APPROVAL_NONCE_DIR")
	secret := os.Getenv("VETO_APPROVAL_SECRET")
	if dir == "" && secret == "" {
		var err error
		dir, err = policy.DefaultApprovalDir()
		if err != nil {
			return err
		}
	}
	if dir != "" {
		s.SetNonceDir(dir)
	}
	if secret != "" {
		return s.SetSigner([]byte(secret), ttl)
	}
	return nil
}
