// Copyright Project Harbor Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cache

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/remotes"
	"github.com/containerd/containerd/v2/plugins/content/local"
	nydusify "github.com/containerd/nydus-snapshotter/pkg/converter"
	"github.com/containerd/platforms"
	nydusutils "github.com/goharbor/acceleration-service/pkg/driver/nydus/utils"
	"github.com/goharbor/acceleration-service/pkg/utils"
	digest "github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func TestFetchImportsSingleManifestCache(t *testing.T) {
	ctx := context.Background()
	cs := newTestContentStore(t)

	sourceDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageLayer,
		Digest:    digest.FromString("source-layer"),
		Size:      11,
	}
	targetDesc := newCacheLayer("target-layer")
	targetDesc.Annotations[nydusify.LayerAnnotationNydusSourceDigest] = sourceDesc.Digest.String()

	manifestDesc, manifestBytes := marshalCacheManifest(t, []ocispec.Descriptor{targetDesc}, "v1")
	fetcher := &fakeFetcher{
		blobs: map[digest.Digest][]byte{
			manifestDesc.Digest: manifestBytes,
			sourceDesc.Digest:   []byte("source"),
		},
		descs: map[digest.Digest]ocispec.Descriptor{
			manifestDesc.Digest: *manifestDesc,
			sourceDesc.Digest:   sourceDesc,
		},
	}
	provider := &fakeProvider{
		cs: cs,
		resolver: &fakeResolver{
			name:    "example.com/cache:tag",
			desc:    *manifestDesc,
			fetcher: fetcher,
		},
	}
	rc := newTestRemoteCache(provider, 10)

	desc, err := rc.Fetch(ctx, platforms.Default())
	require.NoError(t, err)
	require.Equal(t, ocispec.MediaTypeImageManifest, desc.MediaType)

	item := rc.getBySource(sourceDesc.Digest)
	require.NotNil(t, item)
	require.Equal(t, sourceDesc.Digest, item.Source.Digest)
	require.Equal(t, targetDesc.Digest, item.Target.Digest)
	require.Equal(t, targetDesc.Digest.String(), item.Target.Annotations[nydusify.LayerAnnotationUncompressed])
}

func TestUpdateCreatesSinglePlatformManifestCache(t *testing.T) {
	ctx := context.Background()
	cs := newTestContentStore(t)
	provider := &fakeProvider{cs: cs}
	rc := newTestRemoteCache(provider, 10)

	sourceDesc := newSourceLayer("source-layer")
	targetDesc := newCacheLayer("target-layer")
	rc.set(sourceDesc, targetDesc)

	sourceManifestDesc := writeImageManifest(ctx, t, cs, []ocispec.Descriptor{sourceDesc}, linuxAMD64Image())

	cacheDesc, manifests, err := rc.update(ctx, sourceManifestDesc, sourceManifestDesc, nil, platforms.Default())
	require.NoError(t, err)
	require.Empty(t, manifests)
	require.Equal(t, ocispec.MediaTypeImageManifest, cacheDesc.MediaType)

	var manifest ocispec.Manifest
	_, err = utils.ReadJSON(ctx, cs, &manifest, *cacheDesc)
	require.NoError(t, err)
	require.Equal(t, ocispec.MediaTypeImageManifest, manifest.MediaType)
	require.Equal(t, "v1", manifest.Annotations[LayerAnnotationCacheVersion])
	require.Len(t, manifest.Layers, 1)
	require.Equal(t, targetDesc.Digest, manifest.Layers[0].Digest)
	require.Equal(t, sourceDesc.Digest.String(), manifest.Layers[0].Annotations[nydusify.LayerAnnotationNydusSourceDigest])
}

func TestUpdateMergesExistingSingleManifestCache(t *testing.T) {
	ctx := context.Background()
	cs := newTestContentStore(t)
	provider := &fakeProvider{cs: cs}
	rc := newTestRemoteCache(provider, 10)

	oldTargetDesc := newCacheLayer("old-target-layer")
	existingCacheDesc := writeCacheManifest(ctx, t, cs, []ocispec.Descriptor{oldTargetDesc}, "v1")

	sourceDesc := newSourceLayer("source-layer")
	targetDesc := newCacheLayer("target-layer")
	rc.set(sourceDesc, targetDesc)

	sourceManifestDesc := writeImageManifest(ctx, t, cs, []ocispec.Descriptor{sourceDesc}, linuxAMD64Image())

	cacheDesc, manifests, err := rc.update(ctx, sourceManifestDesc, sourceManifestDesc, existingCacheDesc, platforms.Default())
	require.NoError(t, err)
	require.Empty(t, manifests)
	require.Equal(t, ocispec.MediaTypeImageManifest, cacheDesc.MediaType)

	var manifest ocispec.Manifest
	_, err = utils.ReadJSON(ctx, cs, &manifest, *cacheDesc)
	require.NoError(t, err)
	require.Equal(t, "v1", manifest.Annotations[LayerAnnotationCacheVersion])
	require.Len(t, manifest.Layers, 2)
	require.Equal(t, oldTargetDesc.Digest, manifest.Layers[0].Digest)
	require.Equal(t, targetDesc.Digest, manifest.Layers[1].Digest)
}

type fakeProvider struct {
	cs       content.Store
	resolver remotes.Resolver
}

func (p *fakeProvider) Resolver(_ string) (remotes.Resolver, error) {
	return p.resolver, nil
}

func (p *fakeProvider) Pull(_ context.Context, _ string) error {
	return nil
}

func (p *fakeProvider) Push(_ context.Context, _ ocispec.Descriptor, _ string) error {
	return nil
}

func (p *fakeProvider) ContentStore() content.Store {
	return p.cs
}

type fakeResolver struct {
	name    string
	desc    ocispec.Descriptor
	fetcher remotes.Fetcher
}

func (r *fakeResolver) Resolve(_ context.Context, _ string) (string, ocispec.Descriptor, error) {
	return r.name, r.desc, nil
}

func (r *fakeResolver) Fetcher(_ context.Context, _ string) (remotes.Fetcher, error) {
	return r.fetcher, nil
}

func (r *fakeResolver) Pusher(_ context.Context, _ string) (remotes.Pusher, error) {
	return nil, nil
}

type fakeFetcher struct {
	blobs map[digest.Digest][]byte
	descs map[digest.Digest]ocispec.Descriptor
}

func (f *fakeFetcher) Fetch(_ context.Context, desc ocispec.Descriptor) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f.blobs[desc.Digest])), nil
}

func (f *fakeFetcher) FetchByDigest(_ context.Context, dgst digest.Digest, _ ...remotes.FetchByDigestOpts) (io.ReadCloser, ocispec.Descriptor, error) {
	return io.NopCloser(bytes.NewReader(f.blobs[dgst])), f.descs[dgst], nil
}

func newTestRemoteCache(provider Provider, size int) *RemoteCache {
	return &RemoteCache{
		Ref:      "example.com/cache:tag",
		provider: provider,
		records:  map[digest.Digest]*Item{},
		size:     size,
		version:  "v1",
	}
}

func newTestContentStore(t *testing.T) content.Store {
	t.Helper()
	cs, err := local.NewStore(t.TempDir())
	require.NoError(t, err)
	return cs
}

func newSourceLayer(seed string) ocispec.Descriptor {
	return ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageLayer,
		Digest:    digest.FromString(seed),
		Size:      int64(len(seed)),
	}
}

func newCacheLayer(seed string) ocispec.Descriptor {
	return ocispec.Descriptor{
		MediaType: "application/vnd.oci.image.layer.nydus.blob.v1",
		Digest:    digest.FromString(seed),
		Size:      int64(len(seed)),
		Annotations: map[string]string{
			"containerd.io/snapshot/nydus-blob": "true",
		},
	}
}

func linuxAMD64Image() ocispec.Image {
	return ocispec.Image{
		Platform: ocispec.Platform{
			Architecture: "amd64",
			OS:           "linux",
		},
		RootFS: ocispec.RootFS{
			Type: "layers",
		},
	}
}

func writeImageManifest(ctx context.Context, t *testing.T, cs content.Store, layers []ocispec.Descriptor, image ocispec.Image) *ocispec.Descriptor {
	t.Helper()
	configDesc, configBytes, err := nydusutils.MarshalToDesc(image, ocispec.MediaTypeImageConfig)
	require.NoError(t, err)
	require.NoError(t, content.WriteBlob(ctx, cs, configDesc.Digest.String(), bytes.NewReader(configBytes), *configDesc))

	manifest := ocispec.Manifest{
		Versioned: specs.Versioned{
			SchemaVersion: 2,
		},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    *configDesc,
		Layers:    layers,
	}
	manifestDesc, manifestBytes, err := nydusutils.MarshalToDesc(manifest, ocispec.MediaTypeImageManifest)
	require.NoError(t, err)
	require.NoError(t, content.WriteBlob(ctx, cs, manifestDesc.Digest.String(), bytes.NewReader(manifestBytes), *manifestDesc))
	return manifestDesc
}

func writeCacheManifest(ctx context.Context, t *testing.T, cs content.Store, layers []ocispec.Descriptor, version string) *ocispec.Descriptor {
	t.Helper()
	manifestDesc, manifestBytes := marshalCacheManifest(t, layers, version)
	require.NoError(t, content.WriteBlob(ctx, cs, manifestDesc.Digest.String(), bytes.NewReader(manifestBytes), *manifestDesc))
	return manifestDesc
}

func marshalCacheManifest(t *testing.T, layers []ocispec.Descriptor, version string) (*ocispec.Descriptor, []byte) {
	t.Helper()
	configDesc, configBytes, err := nydusutils.MarshalToDesc(ocispec.ImageConfig{}, ocispec.MediaTypeImageConfig)
	require.NoError(t, err)

	manifest := ocispec.Manifest{
		Versioned: specs.Versioned{
			SchemaVersion: 2,
		},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    *configDesc,
		Layers:    layers,
		Annotations: map[string]string{
			LayerAnnotationCacheVersion: version,
		},
	}
	manifestDesc, manifestBytes, err := nydusutils.MarshalToDesc(manifest, ocispec.MediaTypeImageManifest)
	require.NoError(t, err)
	_ = configBytes
	return manifestDesc, manifestBytes
}
