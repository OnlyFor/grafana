package rest

import (
	"context"

	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/klog/v2"
)

type DualWriterMode3 struct {
	DualWriter
}

// NewDualWriterMode3 returns a new DualWriter in mode 3.
// Mode 3 represents writing to LegacyStorage and Storage and reading from Storage.
func NewDualWriterMode3(legacy LegacyStorage, storage Storage) *DualWriterMode3 {
	return &DualWriterMode3{*NewDualWriter(legacy, storage)}
}

// Create overrides the behavior of the generic DualWriter and writes to LegacyStorage and Storage.
func (d *DualWriterMode3) Create(ctx context.Context, obj runtime.Object, createValidation rest.ValidateObjectFunc, options *metav1.CreateOptions) (runtime.Object, error) {
	legacy, ok := d.Legacy.(rest.Creater)
	if !ok {
		return nil, errDualWriterCreaterMissing
	}

	created, err := d.Storage.Create(ctx, obj, createValidation, options)
	if err != nil {
		klog.FromContext(ctx).Error(err, "unable to create object in Storage", "mode", 3)
		return created, err
	}

	if _, err := legacy.Create(ctx, obj, createValidation, options); err != nil {
		klog.FromContext(ctx).Error(err, "unable to create object in legacy storage", "mode", 3)
	}
	return created, nil
}

// Get overrides the behavior of the generic DualWriter and retrieves an object from Storage.
func (d *DualWriterMode3) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	return d.Storage.Get(ctx, name, &metav1.GetOptions{})
}

// DeleteCollection overrides the behavior of the generic DualWriter and deletes from both LegacyStorage and Storage.
func (d *DualWriterMode3) DeleteCollection(ctx context.Context, deleteValidation rest.ValidateObjectFunc, options *metav1.DeleteOptions, listOptions *metainternalversion.ListOptions) (runtime.Object, error) {
	legacy, ok := d.Legacy.(rest.CollectionDeleter)
	if !ok {
		return nil, errDualWriterCollectionDeleterMissing
	}

	// #TODO: figure out how to handle partial deletions
	deleted, err := d.Storage.DeleteCollection(ctx, deleteValidation, options, listOptions)
	if err != nil {
		klog.FromContext(ctx).Error(err, "failed to delete collection successfully from Storage", "deletedObjects", deleted)
	}

	if deleted, err := legacy.DeleteCollection(ctx, deleteValidation, options, listOptions); err != nil {
		klog.FromContext(ctx).Error(err, "failed to delete collection successfully from LegacyStorage", "deletedObjects", deleted)
	}

	return deleted, err
}
