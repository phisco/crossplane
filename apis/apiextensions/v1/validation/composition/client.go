package composition

import (
	"context"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ClientWithFallbackReader struct {
	client client.Client
	reader client.Reader
}

func NewClientWithFallbackReader(client client.Client, reader client.Reader) *ClientWithFallbackReader {
	return &ClientWithFallbackReader{client: client, reader: reader}
}

func (m *ClientWithFallbackReader) GetClient() client.Client {
	return m.client
}

func (m *ClientWithFallbackReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if err := m.client.Get(ctx, key, obj, opts...); err == nil {
		return nil
	}
	return m.reader.Get(ctx, key, obj, opts...)
}

func (m *ClientWithFallbackReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	// we are not setting up the indexers for the client, so it is expected not to work with options like MatchingFields
	if err := m.client.List(ctx, list, opts...); err == nil && meta.LenList(list) > 0 {
		return nil
	}
	return m.reader.List(ctx, list, opts...)
}

func (m *ClientWithFallbackReader) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	obj.SetResourceVersion("")
	return m.client.Create(ctx, obj, opts...)
}

func (m *ClientWithFallbackReader) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	_ = m.client.Delete(ctx, obj, opts...)
	return nil
}

func (m *ClientWithFallbackReader) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	version := obj.GetResourceVersion()
	if err := m.Create(ctx, obj); err == nil {
		return nil
	}
	obj.SetResourceVersion(version)
	return m.client.Update(ctx, obj, opts...)
}

func (m *ClientWithFallbackReader) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	_ = m.Create(ctx, obj)
	return m.client.Patch(ctx, obj, patch, opts...)
}

func (m *ClientWithFallbackReader) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	_ = m.client.DeleteAllOf(ctx, obj, opts...)
	return nil
}

func (m *ClientWithFallbackReader) Status() client.SubResourceWriter {
	return &nopSubResourceClient{}
}

func (m *ClientWithFallbackReader) SubResource(subResource string) client.SubResourceClient {
	return &nopSubResourceClient{}
}

func (m *ClientWithFallbackReader) Scheme() *runtime.Scheme {
	return m.client.Scheme()
}

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
