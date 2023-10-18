// Package graph implements a Resource struct, that represents a Crossplane k8s resource (usually a claim or composite resource).
// The package also contains helper methods and functions for the Resource struct.
package graph

import (
	"container/list"
	"context"
	"fmt"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/claim"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/composite"
)

// Client struct contains the following k8s client types:
// dynamic, discovery, static and a restmapper
type Client struct {
	dynClient       *dynamic.DynamicClient
	clientset       *kubernetes.Clientset
	rmapper         meta.RESTMapper
	discoveryClient *discovery.DiscoveryClient
}

// Resource struct represents a kubernetes resource.
type Resource struct {
	unstructured.Unstructured
	children           []*Resource
	latestEventMessage string
}

// GetConditionStatus returns the Status of the map with the conditionType as string
// This function takes a certain conditionType as input e.g. "Ready" or "Synced"
func (r *Resource) GetConditionStatus(conditionKey string) string {
	conditions, _, _ := unstructured.NestedSlice(r.Unstructured.Object, "status", "conditions")
	for _, condition := range conditions {
		conditionMap, ok := condition.(map[string]interface{})
		if !ok {
			continue
		}
		conditionType, ok := conditionMap["type"].(string)
		if !ok {
			continue
		}
		conditionStatus, ok := conditionMap["status"].(string)
		if !ok {
			continue
		}

		if conditionType == conditionKey {
			return conditionStatus
		}
	}
	return ""
}

// GetConditionMessage returns the message as string if set under `status.conditions` in the manifest. Else return empty string
func (r *Resource) GetConditionMessage() string {
	conditions, _, _ := unstructured.NestedSlice(r.Unstructured.Object, "status", "conditions")

	for _, item := range conditions {
		if itemMap, ok := item.(map[string]interface{}); ok {
			if message, exists := itemMap["message"]; exists {
				if messageStr, ok := message.(string); ok {
					return messageStr
				}
			}
		}
	}

	return ""
}

// GetEvent returns the latest event of the resource as string
func (r *Resource) GetEvent() string {
	return r.latestEventMessage
}

// GetResourceTree returns the requested Resource and all its children.
func (kc *Client) GetResourceTree(ctx context.Context, rootRef *v1.ObjectReference) (*Resource, error) {
	// Get the root resource
	root, err := kc.getResource(ctx, rootRef)
	if err != nil {
		return nil, errors.Wrap(err, "couldn't get root resource")
	}

	// breadth-first search of children
	queue := list.New()

	queue.PushBack(root)

	for queue.Len() > 0 {
		child := queue.Front()
		res := child.Value.(*Resource)
		refs := getResourceChildrenRefs(res)
		if err != nil {
			return nil, errors.Wrap(err, "couldn't get root resource")
		}
		for i := range refs {
			child, err := kc.getResource(ctx, &refs[i])
			if err != nil {
				return nil, errors.Wrap(err, "couldn't get child resource")
			}
			res.children = append(res.children, child)
			queue.PushBack(child)
		}
		_ = queue.Remove(child)
	}

	return root, nil
}

// getResource returns the requested Resource with latest event message.
func (kc *Client) getResource(ctx context.Context, ref *v1.ObjectReference) (*Resource, error) {
	rm, err := kc.rmapper.RESTMapping(ref.GroupVersionKind().GroupKind(), ref.GroupVersionKind().Version)
	if err != nil {
		return nil, errors.Wrap(err, "couldn't get REST mapping for resource")
	}

	result, err := kc.dynClient.Resource(rm.Resource).Namespace(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "couldn't get resource")
	}
	// Get event
	event, err := kc.getLatestEventMessage(ctx, *ref)
	if err != nil {
		return nil, errors.Wrap(err, "couldn't get event for resource")
	}

	res := &Resource{Unstructured: *result, latestEventMessage: event}
	return res, nil
}

// getResourceChildrenRefs returns the references to the children for the given
// Resource, assuming it's a Crossplane resource, XR or XRC.
func getResourceChildrenRefs(r *Resource) []v1.ObjectReference {
	obj := r.Unstructured
	// collect owner references
	var refs []v1.ObjectReference

	xr := composite.Unstructured{Unstructured: obj}
	refs = append(refs, xr.GetResourceReferences()...)

	xrc := claim.Unstructured{Unstructured: obj}
	if ref := xrc.GetResourceReference(); ref != nil {
		refs = append(refs, v1.ObjectReference{
			APIVersion: ref.APIVersion,
			Kind:       ref.Kind,
			Name:       ref.Name,
			Namespace:  ref.Namespace,
			UID:        ref.UID,
		})
	}
	return refs
}

// The getLatestEventMessage returns the message of the latest Event for the given resource.
func (kc *Client) getLatestEventMessage(ctx context.Context, ref v1.ObjectReference) (string, error) {
	// List events for the resource.
	fieldSelector := fmt.Sprintf("involvedObject.name=%s,involvedObject.kind=%s,involvedObject.apiVersion=%s", ref.Name, ref.Kind, ref.APIVersion)
	if ref.UID != "" {
		fieldSelector = fmt.Sprintf("%s,involvedObject.uid=%s", fieldSelector, ref.UID)
	}
	eventList, err := kc.clientset.CoreV1().Events(ref.Namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return "", errors.Wrap(err, "couldn't get event list for resource")
	}

	// Check if there are any events.
	if len(eventList.Items) == 0 {
		return "", nil
	}

	// TODO(phisco): check there is no smarter way, maybe checking what kubectl describe does
	latestEvent := eventList.Items[0]
	for _, event := range eventList.Items {
		if event.LastTimestamp.After(latestEvent.LastTimestamp.Time) {
			latestEvent = event
		}
	}

	// Get the latest event.
	return latestEvent.Message, nil
}

// MappingFor returns the RESTMapping for the given resource or kind argument.
// Copied over from cli-runtime pkg/resource Builder.
func (kc *Client) MappingFor(resourceOrKindArg string) (*meta.RESTMapping, error) {
	// TODO(phisco): actually use the Builder.
	fullySpecifiedGVR, groupResource := schema.ParseResourceArg(resourceOrKindArg)
	gvk := schema.GroupVersionKind{}
	if fullySpecifiedGVR != nil {
		gvk, _ = kc.rmapper.KindFor(*fullySpecifiedGVR)
	}
	if gvk.Empty() {
		gvk, _ = kc.rmapper.KindFor(groupResource.WithVersion(""))
	}
	if !gvk.Empty() {
		return kc.rmapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	}
	fullySpecifiedGVK, groupKind := schema.ParseKindArg(resourceOrKindArg)
	if fullySpecifiedGVK == nil {
		gvk := groupKind.WithVersion("")
		fullySpecifiedGVK = &gvk
	}
	if !fullySpecifiedGVK.Empty() {
		if mapping, err := kc.rmapper.RESTMapping(fullySpecifiedGVK.GroupKind(), fullySpecifiedGVK.Version); err == nil {
			return mapping, nil
		}
	}
	mapping, err := kc.rmapper.RESTMapping(groupKind, gvk.Version)
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

// NewClient function initializes and returns a Client struct
func NewClient(config *rest.Config) (*Client, error) {
	// Use to get custom resources
	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	httpClient, err := rest.HTTPClientFor(config)
	if err != nil {
		return nil, err
	}

	// Use to discover API resources
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	rmapper, err := apiutil.NewDynamicRESTMapper(config, httpClient)
	if err != nil {
		return nil, err
	}

	return &Client{
		dynClient:       dynClient,
		clientset:       clientset,
		rmapper:         rmapper,
		discoveryClient: discoveryClient,
	}, nil
}
