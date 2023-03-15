package composition

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ClientWithFallbackReader is a client that for read operations will first try to use the provided client and then
// fallback in case of error to the provided reader, and for write operations it will always use only the provided
// client. Subresources are ignored.
type ClientWithFallbackReader struct {
	client client.Client
	reader client.Reader
}

// NewClientWithFallbackReader returns a new ClientWithFallbackReader.
func NewClientWithFallbackReader(client client.Client, reader client.Reader) *ClientWithFallbackReader {
	return &ClientWithFallbackReader{client: client, reader: reader}
}

// GetClient returns the primary client.
func (m *ClientWithFallbackReader) GetClient() client.Client {
	return m.client
}

// Get returns the object from the primary client, if it fails it will fallback to the reader.
func (m *ClientWithFallbackReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if err := m.client.Get(ctx, key, obj, opts...); err == nil {
		return nil
	}
	return m.reader.Get(ctx, key, obj, opts...)
}

// List returns the list of objects from the primary client, if it fails it will fallback to the reader.
func (m *ClientWithFallbackReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	// we are not setting up the indexers for the client, so it is expected not to work with options like MatchingFields
	if err := m.client.List(ctx, list, opts...); err == nil && meta.LenList(list) > 0 {
		return nil
	}
	return m.reader.List(ctx, list, opts...)
}

// Create creates the object using the primary client. It will always set the resource version to empty.
func (m *ClientWithFallbackReader) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	obj.SetResourceVersion("")
	return m.client.Create(ctx, obj, opts...)
}

// Delete deletes the object using the primary client. It will always return nil.
func (m *ClientWithFallbackReader) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	_ = m.client.Delete(ctx, obj, opts...)
	return nil
}

// Update updates the object using the primary client. It will always first try to create the object and then update it,
// given that the resource may not exist yet for the primary client. E.g. a resource was read from the reader and then updated.
func (m *ClientWithFallbackReader) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	// TODO(phisco): maybe we should create/update after Gets and Lists instead of doing it here.
	version := obj.GetResourceVersion()
	if err := m.Create(ctx, obj); err == nil {
		return nil
	}
	obj.SetResourceVersion(version)
	return m.client.Update(ctx, obj, opts...)
}

// Patch patches the object using the primary client. It will always first try to create the object and then patch it,
// given that the resource may not exist yet for the primary client. E.g. a resource was read from the reader and then patched.
func (m *ClientWithFallbackReader) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	return m.client.Patch(ctx, obj, patch, opts...)
}

// DeleteAllOf deletes all objects matching the provided object using the primary client. It will always return nil.
func (m *ClientWithFallbackReader) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	_ = m.client.DeleteAllOf(ctx, obj, opts...)
	return nil
}

// Status returns a NOP SubResourceWriter, as we don't support subresources.
func (m *ClientWithFallbackReader) Status() client.SubResourceWriter {
	return &nopSubResourceClient{}
}

// SubResource returns a NOP SubResourceClient, as we don't support subresources.
func (m *ClientWithFallbackReader) SubResource(subResource string) client.SubResourceClient {
	return &nopSubResourceClient{}
}

// Scheme returns the scheme of the primary client.
func (m *ClientWithFallbackReader) Scheme() *runtime.Scheme {
	return m.client.Scheme()
}

// RESTMapper returns the RESTMapper of the primary client.
func (m *ClientWithFallbackReader) RESTMapper() meta.RESTMapper {
	return m.client.RESTMapper()
}

type nopSubResourceClient struct{}

func (n *nopSubResourceClient) Get(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceGetOption) error {
	return nil
}

func (n *nopSubResourceClient) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	return nil
}

func (n *nopSubResourceClient) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return nil
}

func (n *nopSubResourceClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	return nil
}
