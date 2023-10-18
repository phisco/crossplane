package main

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/exp/slices"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/logging"

	"github.com/crossplane/crossplane/cmd/crank/internal/graph"
)

const (
	errGetResource = "cannot get requested resource"
	errCliOutput   = "cannot print output"
)

// describeAllowedFields are the fields that can be printed out in the header.
// TODO(phisco): add fieldpath or jsonpath support, keeping well-known fields as defaults maybe.
var describeAllowedFields = []string{"parent", "name", "kind", "namespace", "apiversion", "synced", "ready", "message", "event"}

// describeCmd describes a Kubernetes Crossplane resource.
type describeCmd struct {
	Kind      string   `arg:"" required:"" help:"Kind of resource to describe."`
	Name      string   `arg:"" required:"" help:"Name of specified resource to describe."`
	Namespace string   `short:"n" name:"namespace" help:"Namespace of resource to describe." default:"default"`
	Output    string   `short:"o" name:"output" help:"Output type of graph. Possible output types: tree, table, graph." enum:"tree,table,graph" default:"tree"`
	Fields    []string `short:"f" name:"fields" help:"Fields that are printed out in the header." default:"kind,name"`
}

func (c *describeCmd) Run(logger logging.Logger) error {
	logger = logger.WithValues("Kind", c.Kind, "Name", c.Name)

	// Validate flags and arguments
	if err := c.validate(); err != nil {
		return errors.Wrap(err, "cannot validate fields")
	}

	// set kubeconfig
	kubeconfig, err := ctrl.GetConfig()
	if err != nil {
		logger.Debug(errKubeConfig, "error", err)
		return errors.Wrap(err, errKubeConfig)
	}
	logger.Debug("Found kubeconfig")

	// Get client for k8s package
	client, err := graph.NewClient(kubeconfig)
	if err != nil {
		return errors.Wrap(err, "Couldn't init kubeclient")
	}

	// Init new printer
	p, err := graph.NewPrinter(c.Output)
	if err != nil {
		return errors.Wrap(err, "cannot init new printer")
	}

	// Get Resource object. Contains k8s resource and all its children, also as Resource.
	root, err := client.GetResource(c.Kind, c.Name, c.Namespace)
	if err != nil {
		logger.Debug(errGetResource, "error", err)
		return errors.Wrap(err, errGetResource)
	}

	// Print resources
	err = p.Print(os.Stdout, *root, c.Fields)
	if err != nil {
		return errors.Wrap(err, errCliOutput)
	}

	return nil
}

func (c *describeCmd) validate() error {
	// Check if fields are valid
	for _, field := range c.Fields {
		if !slices.Contains(describeAllowedFields, strings.ToLower(field)) {
			return fmt.Errorf("invalid field set %q, should be one of: %s", field, describeAllowedFields)
		}
	}
	return nil
}
