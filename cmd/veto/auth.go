package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/config"
	"github.com/aiveto/veto/jsonopts"
	"github.com/spf13/cobra"
)

type (
	authLoginCmd struct {
		config string
		scheme string
		device bool
	}

	authSetCmd struct {
		config string
		scheme string
	}
)

func newAuthCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "auth",
		Short: "Store a user token or a pasted token.",
	}
	c.AddCommand(newAuthLoginCommand(), newAuthSetCommand())
	return c
}

func newAuthLoginCommand() *cobra.Command {
	cmd := &authLoginCmd{}
	c := &cobra.Command{
		Use:   "login",
		Short: "Sign in once and store a refresh token.",
		Run: func(*cobra.Command, []string) {
			runAuthLogin(*cmd)
		},
	}
	c.Flags().StringVar(&cmd.config, "config", "", "Path to veto.yaml.")
	c.Flags().StringVar(&cmd.scheme, "scheme", "", "OpenAPI security scheme name.")
	c.Flags().BoolVar(&cmd.device, "device", false, "Use the device code flow.")
	return c
}

func newAuthSetCommand() *cobra.Command {
	cmd := &authSetCmd{}
	c := &cobra.Command{
		Use:   "set",
		Short: "Store a token read from stdin.",
		Run: func(*cobra.Command, []string) {
			runAuthSet(*cmd)
		},
	}
	c.Flags().StringVar(&cmd.config, "config", "", "Path to veto.yaml.")
	c.Flags().StringVar(&cmd.scheme, "scheme", "", "OpenAPI security scheme name.")
	return c
}

func runAuthLogin(cmd authLoginCmd) {
	scheme, dir, json, err := configuredScheme(cmd.config, cmd.scheme)
	if err != nil {
		fmt.Fprintf(os.Stderr, "auth login: %v\n", err)
		exitMain(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	err = auth.Login(ctx, auth.LoginOptions{
		Scheme: scheme,
		Dir:    dir,
		Device: cmd.device || !auth.HasBrowser(),
		JSON:   json,
	})
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "auth login: %v\n", err)
		exitMain(1)
	}
}

func runAuthSet(cmd authSetCmd) {
	if cmd.scheme == "" {
		fmt.Fprintln(os.Stderr, "auth set: scheme required")
		exitMain(1)
	}
	dir := auth.DefaultTokenDir()
	if cmd.config != "" {
		cfg, err := loadAuthConfig(cmd.config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "auth set: %v\n", err)
			exitMain(1)
		}
		dir = tokenDir(cfg)
	} else if d := os.Getenv("VETO_TOKEN_DIR"); d != "" {
		dir = d
	}
	fmt.Fprintln(os.Stderr, "Paste the token and press enter.")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		fmt.Fprintf(os.Stderr, "auth set: %v\n", err)
		exitMain(1)
	}
	if err := auth.SetToken(dir, cmd.scheme, line); err != nil {
		fmt.Fprintf(os.Stderr, "auth set: %v\n", err)
		exitMain(1)
	}
}

func configuredScheme(configPath, name string) (auth.Scheme, string, jsonopts.Set, error) {
	if configPath == "" {
		return auth.Scheme{}, "", jsonopts.Set{}, errors.New("config required")
	}
	if name == "" {
		return auth.Scheme{}, "", jsonopts.Set{}, errors.New("scheme required")
	}
	cfg, err := loadAuthConfig(configPath)
	if err != nil {
		return auth.Scheme{}, "", jsonopts.Set{}, err
	}
	src, ok := cfg.Auth[name]
	if !ok {
		return auth.Scheme{}, "", jsonopts.Set{}, fmt.Errorf("auth scheme %s is unset", name)
	}
	if src.Kind() != "login" {
		return auth.Scheme{}, "", jsonopts.Set{}, fmt.Errorf("auth scheme %s is not login", name)
	}
	return auth.Scheme{
		Name:                   name,
		Source:                 "login",
		ClientID:               src.ClientID,
		ClientSecretEnv:        src.ClientSecretEnv,
		AuthorizationURL:       src.AuthorizationURL,
		TokenURL:               src.TokenURL,
		Issuer:                 src.Issuer,
		DeviceAuthorizationURL: src.DeviceAuthorizationURL,
		RedirectURL:            src.RedirectURL,
		Scopes:                 append([]string(nil), src.Scopes...),
		Audience:               src.Audience,
		Header:                 src.Header,
		AuthToken:              src.AuthToken,
		UserToken:              src.UserToken,
		UserHeader:             src.UserHeader,
	}, tokenDir(cfg), cfg.JSONSet(), nil
}

func loadAuthConfig(path string) (config.File, error) {
	return config.Load(path)
}
