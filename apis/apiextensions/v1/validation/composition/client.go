package composition

import (
	"context"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ClientWithFallbackReader struct {
	fake   client.Client
	reader client.Reader
}

func NewClientWithFallbackReader(fake client.Client, reader client.Reader) *ClientWithFallbackReader {
	return &ClientWithFallbackReader{fake: fake, reader: reader}
}

func (m ClientWithFallbackReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if err := m.fake.Get(ctx, key, obj, opts...); err == nil {
		return nil
	}
	return m.reader.Get(ctx, key, obj, opts...)
}

func (m ClientWithFallbackReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	// we are not setting up the indexers for the fake client, so it is expected not to work with options like MatchingFields
	if err := m.fake.List(ctx, list, opts...); err == nil && meta.LenList(list) > 0 {
		return nil
	}
	return m.reader.List(ctx, list, opts...)
}

func (m ClientWithFallbackReader) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	obj.SetResourceVersion("")
	return m.fake.Create(ctx, obj, opts...)
}

func (m ClientWithFallbackReader) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	_ = m.fake.Delete(ctx, obj, opts...)
	return nil
}

func (m ClientWithFallbackReader) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	version := obj.GetResourceVersion()
	if err := m.Create(ctx, obj); err == nil {
		return nil
	}
	obj.SetResourceVersion(version)
	return m.fake.Update(ctx, obj, opts...)
}

func (m ClientWithFallbackReader) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	_ = m.Create(ctx, obj)
	return m.fake.Patch(ctx, obj, patch, opts...)
}

func (m ClientWithFallbackReader) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	_ = m.fake.DeleteAllOf(ctx, obj, opts...)
	return nil
}

func (m ClientWithFallbackReader) Status() client.SubResourceWriter {
	return nopSubResourceClient{}
}

func (m ClientWithFallbackReader) SubResource(subResource string) client.SubResourceClient {
	return nopSubResourceClient{}
}

func (m ClientWithFallbackReader) Scheme() *runtime.Scheme {
	return m.fake.Scheme()
}

func (m ClientWithFallbackReader) RESTMapper() meta.RESTMapper {
	return m.fake.RESTMapper()
}

type nopSubResourceClient struct{}

func (n nopSubResourceClient) Get(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceGetOption) error {
	return nil
}

func (n nopSubResourceClient) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	return nil
}

func (n nopSubResourceClient) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return nil
}

func (n nopSubResourceClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	return nil
}
