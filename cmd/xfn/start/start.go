/*
Copyright 2022 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package start implements the reference Composition Function runner.
// It exposes a gRPC API that may be used to run Composition Functions.
package start

import (
	"net"
	"os"
	"path/filepath"

	"google.golang.org/grpc"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/logging"

	"github.com/crossplane/crossplane/internal/xfn"
	"github.com/crossplane/crossplane/internal/xfn/config"
	"github.com/crossplane/crossplane/internal/xfn/v1alpha1"
	"github.com/crossplane/crossplane/internal/xfn/v1beta1"
)

// Error strings
const (
	errListen = "cannot listen for gRPC connections"
	errServe  = "cannot serve gRPC API"
)

// Command starts a gRPC API to run Composition Functions.
type Command struct {
	CacheDir   string `short:"c" help:"Directory used for caching function images and containers." default:"/xfn"`
	MapRootUID int    `help:"UID that will map to 0 in the function's user namespace. The following 65336 UIDs must be available. Ignored if xfn does not have CAP_SETUID and CAP_SETGID." default:"100000"`
	MapRootGID int    `help:"GID that will map to 0 in the function's user namespace. The following 65336 GIDs must be available. Ignored if xfn does not have CAP_SETUID and CAP_SETGID." default:"100000"`
	Network    string `help:"Network on which to listen for gRPC connections." default:"unix"`
	Address    string `help:"Address at which to listen for gRPC connections." default:"@crossplane/fn/default.sock"`
}

// Run a Composition Function gRPC API.
func (c *Command) Run(global *config.Global, log logging.Logger) error {
	// If we don't have CAP_SETUID or CAP_SETGID, we'll only be able to map our
	// own UID and GID to root inside the user namespace.
	rootUID := os.Getuid()
	rootGID := os.Getgid()
	setuid := xfn.HasCapSetUID() && xfn.HasCapSetGID() // We're using 'setuid' as shorthand for both here.
	if setuid {
		rootUID = c.MapRootUID
		rootGID = c.MapRootGID
	}

	// TODO(negz): Expose a healthz endpoint and otel metrics.
	fv1alpha1 := v1alpha1.NewContainerRunner(
		v1alpha1.SetUID(setuid),
		v1alpha1.MapToRoot(rootUID, rootGID),
		v1alpha1.WithCacheDir(filepath.Clean(c.CacheDir)),
		v1alpha1.WithLogger(log),
		v1alpha1.WithRegistry(global.Registry))
	fv1beta1 := v1beta1.NewContainerRunner(
		v1beta1.SetUID(setuid),
		v1beta1.MapToRoot(rootUID, rootGID),
		v1beta1.WithCacheDir(filepath.Clean(c.CacheDir)),
		v1beta1.WithLogger(log),
		v1beta1.WithRegistry(global.Registry),
		v1beta1.WithDefaultImage(global.Image),
	)

	log.Debug("Listening", "network", c.Network, "address", c.Address)
	lis, err := net.Listen(c.Network, c.Address)
	if err != nil {
		return errors.Wrap(err, errListen)
	}

	// TODO(negz): Limit concurrent function runs?
	srv := grpc.NewServer()
	if err := fv1alpha1.Register(srv); err != nil {
		return errors.Wrap(err, "cannot register v1alpha1")
	}
	if err := fv1beta1.Register(srv); err != nil {
		return errors.Wrap(err, "cannot register v1beta1")
	}

	return errors.Wrap(srv.Serve(lis), errServe)
}
