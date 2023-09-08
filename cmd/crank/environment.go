package main

import (
	"context"
	"fmt"
	"github.com/alecthomas/kong"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
	"log"
	ctrl "sigs.k8s.io/controller-runtime"
)

// environmentCmd handles the environment subcommand.
type environmentCmd struct {
	Render renderCmd `cmd:"" help:"Render a Composite resource's environment."`
}

// renderCmd handles the render subcommand.
type renderCmd struct {
	// Name of the Composite resource.
	ResourceOrKind string `arg:"" help:"Kind of Composite resource."`
	Name           string `arg:"" help:"Name of Composite resource."`
}

// Run runs the render cmd.
func (c *renderCmd) Run(k *kong.Context, logger logging.Logger) error {
	logger = logger.WithValues("ResourceOrKind", c.ResourceOrKind, "Name", c.Name)
	logger.Debug("Rendering environment")
	kubeConfig, err := ctrl.GetConfig()
	if err != nil {
		logger.Debug(errKubeConfig, "error", err)
		return errors.Wrap(err, errKubeConfig)
	}
	logger.Debug("Found kubeconfig")

	kube, err := dynamic.NewForConfig(kubeConfig)
	if err != nil {
		logger.Debug(errKubeClient, "error", err)
		return errors.Wrap(err, errKubeClient)
	}
	//schema.GroupVersionResource{
	//	Group:    "",
	//	Version:  "v1",
	//	Resource: "pods",
	//}
	dc := discovery.NewDiscoveryClientForConfigOrDie(kubeConfig)
	gr, err := restmapper.GetAPIGroupResources(dc)
	if err != nil {
		log.Fatal(err)
	}
	r, err := mappingFor(restmapper.NewDiscoveryRESTMapper(gr), c.ResourceOrKind)
	if err != nil {
		log.Fatal(err)
	}
	logger.Debug("Found resource", "resource", r.Resource.String(), "gvk", r.GroupVersionKind.String())

	resource, err := kube.Resource(r.Resource).Get(context.Background(), c.Name, metav1.GetOptions{})
	if err != nil {
		log.Fatal(err)
	}
	logger.Debug("Found resource", "name", resource.GetName(), "namespace", resource.GetNamespace(), "gvk", resource.GetObjectKind().GroupVersionKind().String())

	//compositeUnstructured := &composite.Unstructured{Unstructured: *resource}
	//ref := compositeUnstructured.GetCompositionRevisionReference()

	//nc := func() resource2.Composite {
	//	return composite.New(composite.WithGroupVersionKind(r.GroupVersionKind))
	//}
	//reconciler := &composite2.Reconciler{
	//	client:       client.NewDryRunClient(client.),
	//	newComposite: nc,

	//	revision: revision{
	//		CompositionRevisionFetcher: NewAPIRevisionFetcher(resource.ClientApplicator{Client: kube, Applicator: resource.NewAPIPatchingApplicator(kube)}),
	//		CompositionRevisionValidator: CompositionRevisionValidatorFn(func(rev *v1.CompositionRevision) error {
	//			// TODO(negz): Presumably this validation will eventually be
	//			// removed in favor of the new Composition validation
	//			// webhook.
	//			// This is the last remaining use of conv.FromRevisionSpec -
	//			// we can stop generating that once this is removed.
	//			conv := &v1.GeneratedRevisionSpecConverter{}
	//			comp := &v1.Composition{Spec: conv.FromRevisionSpec(rev.Spec)}
	//			_, errs := comp.Validate()
	//			return errs.ToAggregate()
	//		}),
	//	},

	//	environment: environment{
	//		EnvironmentFetcher: NewNilEnvironmentFetcher(),
	//	},

	//	composite: compositeResource{
	//		Finalizer:           resource.NewAPIFinalizer(kube, finalizer),
	//		CompositionSelector: NewAPILabelSelectorResolver(kube),
	//		EnvironmentSelector: NewNoopEnvironmentSelector(),
	//		Configurator:        NewConfiguratorChain(NewAPINamingConfigurator(kube), NewAPIConfigurator(kube)),

	//		// TODO(negz): In practice this is a filtered publisher that will
	//		// never filter any keys. Is there an unfiltered variant we could
	//		// use by default instead?
	//		ConnectionPublisher: NewAPIFilteredSecretPublisher(kube, []string{}),
	//	},

	//	resource: NewPTComposer(kube),

	//	log:    logging.NewNopLogger(),
	//	record: event.NewNopRecorder(),

	//	pollInterval: defaultPollInterval,
	//}
	return nil
}

func mappingFor(restMapper meta.RESTMapper, resourceOrKindArg string) (*meta.RESTMapping, error) {
	fullySpecifiedGVR, groupResource := schema.ParseResourceArg(resourceOrKindArg)
	gvk := schema.GroupVersionKind{}

	if fullySpecifiedGVR != nil {
		gvk, _ = restMapper.KindFor(*fullySpecifiedGVR)
	}
	if gvk.Empty() {
		gvk, _ = restMapper.KindFor(groupResource.WithVersion(""))
	}
	if !gvk.Empty() {
		return restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	}

	fullySpecifiedGVK, groupKind := schema.ParseKindArg(resourceOrKindArg)
	if fullySpecifiedGVK == nil {
		gvk := groupKind.WithVersion("")
		fullySpecifiedGVK = &gvk
	}

	if !fullySpecifiedGVK.Empty() {
		if mapping, err := restMapper.RESTMapping(fullySpecifiedGVK.GroupKind(), fullySpecifiedGVK.Version); err == nil {
			return mapping, nil
		}
	}

	mapping, err := restMapper.RESTMapping(groupKind, gvk.Version)
	if err != nil {
		// if we error out here, it is because we could not match a resource or a kind
		// for the given argument. To maintain consistency with previous behavior,
		// announce that a resource type could not be found.
		// if the error is _not_ a *meta.NoKindMatchError, then we had trouble doing discovery,
		// so we should return the original error since it may help a user diagnose what is actually wrong
		if meta.IsNoMatchError(err) {
			return nil, fmt.Errorf("the server doesn't have a resource type %q", groupResource.Resource)
		}
		return nil, err
	}

	return mapping, nil
}
