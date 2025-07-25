/*
Copyright 2025 Rancher Labs, Inc.

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

// Code generated by main. DO NOT EDIT.

package v1beta2

import (
	"context"
	"time"

	scheme "github.com/harvester/networkfs-manager/pkg/generated/clientset/versioned/scheme"
	v1beta2 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta2"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// ShareManagersGetter has a method to return a ShareManagerInterface.
// A group's client should implement this interface.
type ShareManagersGetter interface {
	ShareManagers(namespace string) ShareManagerInterface
}

// ShareManagerInterface has methods to work with ShareManager resources.
type ShareManagerInterface interface {
	Create(ctx context.Context, shareManager *v1beta2.ShareManager, opts v1.CreateOptions) (*v1beta2.ShareManager, error)
	Update(ctx context.Context, shareManager *v1beta2.ShareManager, opts v1.UpdateOptions) (*v1beta2.ShareManager, error)
	UpdateStatus(ctx context.Context, shareManager *v1beta2.ShareManager, opts v1.UpdateOptions) (*v1beta2.ShareManager, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*v1beta2.ShareManager, error)
	List(ctx context.Context, opts v1.ListOptions) (*v1beta2.ShareManagerList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1beta2.ShareManager, err error)
	ShareManagerExpansion
}

// shareManagers implements ShareManagerInterface
type shareManagers struct {
	client rest.Interface
	ns     string
}

// newShareManagers returns a ShareManagers
func newShareManagers(c *LonghornV1beta2Client, namespace string) *shareManagers {
	return &shareManagers{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the shareManager, and returns the corresponding shareManager object, and an error if there is any.
func (c *shareManagers) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1beta2.ShareManager, err error) {
	result = &v1beta2.ShareManager{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("sharemanagers").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of ShareManagers that match those selectors.
func (c *shareManagers) List(ctx context.Context, opts v1.ListOptions) (result *v1beta2.ShareManagerList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1beta2.ShareManagerList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("sharemanagers").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested shareManagers.
func (c *shareManagers) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("sharemanagers").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a shareManager and creates it.  Returns the server's representation of the shareManager, and an error, if there is any.
func (c *shareManagers) Create(ctx context.Context, shareManager *v1beta2.ShareManager, opts v1.CreateOptions) (result *v1beta2.ShareManager, err error) {
	result = &v1beta2.ShareManager{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("sharemanagers").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(shareManager).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a shareManager and updates it. Returns the server's representation of the shareManager, and an error, if there is any.
func (c *shareManagers) Update(ctx context.Context, shareManager *v1beta2.ShareManager, opts v1.UpdateOptions) (result *v1beta2.ShareManager, err error) {
	result = &v1beta2.ShareManager{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("sharemanagers").
		Name(shareManager.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(shareManager).
		Do(ctx).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *shareManagers) UpdateStatus(ctx context.Context, shareManager *v1beta2.ShareManager, opts v1.UpdateOptions) (result *v1beta2.ShareManager, err error) {
	result = &v1beta2.ShareManager{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("sharemanagers").
		Name(shareManager.Name).
		SubResource("status").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(shareManager).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the shareManager and deletes it. Returns an error if one occurs.
func (c *shareManagers) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("sharemanagers").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *shareManagers) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Namespace(c.ns).
		Resource("sharemanagers").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched shareManager.
func (c *shareManagers) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1beta2.ShareManager, err error) {
	result = &v1beta2.ShareManager{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("sharemanagers").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}
