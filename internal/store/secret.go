package store

import (
	"reflect"

	apiv1 "k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog"
)

// SecretLister makes a Store that lists Ingress.
type SecretLister struct {
	cache.Store
}

// ByKey returns the Ingress matching key in the local Ingress Store.
func (il *SecretLister) ByKey(key string) (*apiv1.Secret, error) {
	item, exit, err := il.GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exit {
		return nil, errors.NewNotFound(schema.ParseGroupResource("core.secret"), key)
	}
	return item.(*apiv1.Secret), nil
}

//SecretResourceEventHandler is ingress informer handler
type SecretResourceEventHandler struct {
	store    *K8sStore
	recorder record.EventRecorder
}

//OnAdd handler secret add event
func (sh *SecretResourceEventHandler) OnAdd(obj interface{}) {
	sec := obj.(*corev1.Secret)
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(sec)
	if err != nil {
		klog.Warning(err)
	}

	// find references in ingresses and update local ssl certs
	if ings := sh.store.secretIngressMap.Reference(key); len(ings) > 0 {
		klog.Infof("secret %v was added and it is used in ingress annotations. Parsing...", key)
		for _, ingKey := range ings {
			ing, err := sh.store.getIngress(ingKey)
			if err != nil {
				klog.Errorf("could not find Ingress %v in local store", ingKey)
				continue
			}
			sh.store.syncSecrets(ing)
		}
		sh.store.updateCh.In() <- Event{
			Type: CreateEvent,
			Obj:  obj,
		}
	}
}

//OnUpdate handler secret update event
func (sh *SecretResourceEventHandler) OnUpdate(old, cur interface{}) {
	if !reflect.DeepEqual(old, cur) {
		sec := cur.(*corev1.Secret)
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(sec)
		if err != nil {
			klog.Warning(err)
		}
		// find references in ingresses and update local ssl certs
		if ings := sh.store.secretIngressMap.Reference(key); len(ings) > 0 {
			klog.Infof("secret %v was updated and it is used in ingress annotations. Parsing...", key)
			for _, ingKey := range ings {
				ing, err := sh.store.getIngress(ingKey)
				if err != nil {
					klog.Errorf("could not find Ingress %v in local store", ingKey)
					continue
				}
				sh.store.syncSecrets(ing)
			}
			sh.store.updateCh.In() <- Event{
				Type: UpdateEvent,
				Obj:  cur,
			}
		}
	}
}

//OnDelete handler secret delete event
func (sh *SecretResourceEventHandler) OnDelete(obj interface{}) {
	sec, ok := obj.(*corev1.Secret)
	if !ok {
		// If we reached here it means the secret was deleted but its final state is unrecorded.
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("couldn't get object from tombstone %#v", obj)
			return
		}
		sec, ok = tombstone.Obj.(*corev1.Secret)
		if !ok {
			klog.Errorf("Tombstone contained object that is not a Secret: %#v", obj)
			return
		}
	}
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(sec)
	if err != nil {
		klog.Warning(err)
	}
	sh.store.sslStore.Delete(key)
	// find references in ingresses
	if ings := sh.store.secretIngressMap.Reference(key); len(ings) > 0 {
		klog.Infof("secret %v was deleted and it is used in ingress annotations. Parsing...", key)
		sh.store.updateCh.In() <- Event{
			Type: DeleteEvent,
			Obj:  obj,
		}
	}
}
