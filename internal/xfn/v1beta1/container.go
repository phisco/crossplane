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

package v1beta1

import (
	"google.golang.org/grpc"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/crossplane/crossplane-runtime/pkg/logging"

	"github.com/crossplane/crossplane/apis/apiextensions/fn/proto/v1beta1"
	"github.com/crossplane/crossplane/internal/xfn/v1beta1/proto"
)

const defaultCacheDir = "/xfn"

// ContainerRunFunctionRequestConfig is a request to run a Composition Function
// packaged as an OCI image.
type ContainerRunFunctionRequestConfig struct {
	metav1.TypeMeta `json:",inline"`

	Spec proto.ContainerRunFunctionRequestConfigSpec `json:"spec,omitempty"`
}

// An ContainerRunner runs a Composition Function packaged as an OCI image by
// extracting it and running it as a 'rootless' container.
type ContainerRunner struct {
	v1beta1.UnimplementedFunctionRunnerServiceServer

	log logging.Logger

	rootUID      int
	rootGID      int
	setuid       bool // Specifically, CAP_SETUID and CAP_SETGID.
	cache        string
	registry     string
	defaultImage *string
}

// A ContainerRunnerOption configures a new ContainerRunner.
type ContainerRunnerOption func(*ContainerRunner)

// MapToRoot configures what UID and GID should map to root (UID/GID 0) in the
// user namespace in which the function will be run.
func MapToRoot(uid, gid int) ContainerRunnerOption {
	return func(r *ContainerRunner) {
		r.rootUID = uid
		r.rootGID = gid
	}
}

// SetUID indicates that the container runner should attempt operations that
// require CAP_SETUID and CAP_SETGID, for example creating a user namespace that
// maps arbitrary UIDs and GIDs to the parent namespace.
func SetUID(s bool) ContainerRunnerOption {
	return func(r *ContainerRunner) {
		r.setuid = s
	}
}

// WithDefaultImage specifies the default image that should be used to run
// functions if no image is specified in the request.
func WithDefaultImage(image string) ContainerRunnerOption {
	return func(r *ContainerRunner) {
		if image == "" {
			return
		}
		r.defaultImage = &image
	}
}

// WithCacheDir specifies the directory used for caching function images and
// containers.
func WithCacheDir(d string) ContainerRunnerOption {
	return func(r *ContainerRunner) {
		r.cache = d
	}
}

// WithRegistry specifies the default registry used to retrieve function images and
// containers.
func WithRegistry(dr string) ContainerRunnerOption {
	return func(r *ContainerRunner) {
		r.registry = dr
	}
}

// WithLogger configures which logger the container runner should use. Logging
// is disabled by default.
func WithLogger(l logging.Logger) ContainerRunnerOption {
	return func(cr *ContainerRunner) {
		cr.log = l
	}
}

// NewContainerRunner returns a new Runner that runs functions as rootless
// containers.
func NewContainerRunner(o ...ContainerRunnerOption) *ContainerRunner {
	r := &ContainerRunner{cache: defaultCacheDir, log: logging.NewNopLogger()}
	for _, fn := range o {
		fn(r)
	}

	return r
}

// Register the container runner with the supplied gRPC server.
func (r *ContainerRunner) Register(srv *grpc.Server) error {
	// TODO(negz): Limit concurrent function runs?
	v1beta1.RegisterFunctionRunnerServiceServer(srv, r)
	return nil
}
