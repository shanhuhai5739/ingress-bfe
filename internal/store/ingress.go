package store

import (
	"fmt"
	"reflect"

	"github.com/baidu/ingress-bfe/internal/annotations"
	corev1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networking "k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog"
)

// IngressLister makes a Store that lists Ingress.
type IngressLister struct {
	cache.Store
}

// ByKey returns the Ingress matching key in the local Ingress Store.
func (il *IngressLister) ByKey(key string) (*networking.Ingress, error) {
	item, exit, err := il.GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exit {
		return nil, errors.NewNotFound(schema.ParseGroupResource("networking.Ingress"), key)
	}
	return item.(*networking.Ingress), nil
}

//IngressResourceEventHandler is ingress informer handler
type IngressResourceEventHandler struct {
	store    *K8sStore
	recorder record.EventRecorder
}

//OnAdd handler ingress add event
func (ih *IngressResourceEventHandler) OnAdd(obj interface{}) {
	ing, _ := toIngress(obj)
	if !IsValid(ing) {
		a, _ := annotations.GetStringAnnotation(annotations.IngressKey, ing)
		klog.Infof("ignoring add for ingress %v based on annotation %v with value %v", ing.Name, IngressKey, a)
		return
	}
	ih.recorder.Eventf(ing, corev1.EventTypeNormal, "CREATE", fmt.Sprintf("Ingress %s/%s", ing.Namespace, ing.Name))

	ih.store.updateSecretIngressMap(ing)
	ih.store.syncSecrets(ing)

	ih.store.updateCh.In() <- Event{
		Type: CreateEvent,
		Obj:  obj,
	}
}

//OnDelete handler ingress delete event
func (ih *IngressResourceEventHandler) OnDelete(obj interface{}) {
	ing, ok := toIngress(obj)
	if !ok {
		// If we reached here it means the ingress was deleted but its final state is unrecorded.
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("couldn't get object from tombstone %#v", obj)
			return
		}
		ing, ok = tombstone.Obj.(*networking.Ingress)
		if !ok {
			klog.Errorf("Tombstone contained object that is not an Ingress: %#v", obj)
			return
		}
	}

	if !IsValid(ing) {
		klog.Infof("ignoring delete for ingress %v based on annotation %v", ing.Name, IngressKey)
		return
	}
	ih.recorder.Eventf(ing, corev1.EventTypeNormal, "DELETE", fmt.Sprintf("Ingress %s/%s", ing.Namespace, ing.Name))

	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(ing)
	if err != nil {
		klog.Warning(err)
	}
	ih.store.secretIngressMap.Delete(key)

	ih.store.updateCh.In() <- Event{
		Type: DeleteEvent,
		Obj:  obj,
	}
}

//OnUpdate handler ingress update event
func (ih *IngressResourceEventHandler) OnUpdate(old, cur interface{}) {
	oldIng, _ := toIngress(old)
	curIng, _ := toIngress(cur)

	validOld := IsValid(oldIng)
	validCur := IsValid(curIng)
	if !validOld && validCur {
		klog.Infof("creating ingress %v based on annotation %v", curIng.Name, IngressKey)
		ih.recorder.Eventf(curIng, corev1.EventTypeNormal, "CREATE", fmt.Sprintf("Ingress %s/%s", curIng.Namespace, curIng.Name))
	} else if validOld && !validCur {
		klog.Infof("removing ingress %v based on annotation %v", curIng.Name, IngressKey)
		ih.OnDelete(old)
		return
	} else if validCur && !reflect.DeepEqual(old, cur) {
		ih.recorder.Eventf(curIng, corev1.EventTypeNormal, "UPDATE", fmt.Sprintf("Ingress %s/%s", curIng.Namespace, curIng.Name))
	} else {
		klog.V(3).Infof("No changes on ingress %v/%v. Skipping update", curIng.Namespace, curIng.Name)
		return
	}

	ih.store.updateSecretIngressMap(curIng)
	ih.store.syncSecrets(curIng)

	ih.store.updateCh.In() <- Event{
		Type: UpdateEvent,
		Obj:  cur,
	}
}

func toIngress(obj interface{}) (*networking.Ingress, bool) {
	oldVersion, inExtension := obj.(*extensionsv1beta1.Ingress)
	if inExtension {
		ing, err := fromExtensions(oldVersion)
		if err != nil {
			klog.Errorf("unexpected error converting Ingress from extensions package: %v", err)
			return nil, false
		}

		SetDefaultNGINXPathType(ing)
		return ing, true
	}

	if ing, ok := obj.(*networking.Ingress); ok {
		SetDefaultNGINXPathType(ing)
		return ing, true
	}

	return nil, false
}

func fromExtensions(old *extensionsv1beta1.Ingress) (*networking.Ingress, error) {
	networkingIngress := &networking.Ingress{}
	runtimeScheme := k8sruntime.NewScheme()
	err := runtimeScheme.Convert(old, networkingIngress, nil)
	if err != nil {
		return nil, err
	}

	return networkingIngress, nil
}

const (
	// IngressKey picks a specific "class" for the Ingress.
	// The controller only processes Ingresses with this annotation either
	// unset, or set to either the configured value or the empty string.
	IngressKey = "kubernetes.io/ingress.class"
)

var (
	// DefaultClassName defines the default class used in the nginx ingress controller
	DefaultClassName = "bfe"

	// IngressClassName sets the runtime ingress class to use
	// An empty string means accept all ingresses without
	// annotation and the ones configured with class nginx
	IngressClassName = "bfe"

	// IsIngressV1Ready indicates if the running Kubernetes version is at least v1.18.0
	IsIngressV1Ready bool

	// IngressClass indicates the class of the Ingress to use as filter
	IngressClass *networking.IngressClass
)

// IsValid returns true if the given Ingress specify the ingress.class
// annotation or IngressClassName resource for Kubernetes >= v1.18
func IsValid(ing *networking.Ingress) bool {
	// 1. with annotation
	ingress, ok := ing.GetAnnotations()[IngressKey]
	if ok {
		// empty annotation and same annotation on ingress
		if ingress == "" && IngressClassName == DefaultClassName {
			return true
		}

		return ingress == IngressClassName
	}

	// 2. k8s < v1.18. Check default annotation
	if !IsIngressV1Ready {
		return IngressClassName == DefaultClassName
	}

	// 3. without annotation and IngressClass. Check default annotation
	if IngressClass == nil {
		return IngressClassName == DefaultClassName
	}

	// 4. with IngressClass
	return IngressClass.Name == *ing.Spec.IngressClassName
}

// SetDefaultNGINXPathType sets a default PathType when is not defined.
func SetDefaultNGINXPathType(ing *networking.Ingress) {
	for _, rule := range ing.Spec.Rules {
		if rule.IngressRuleValue.HTTP == nil {
			continue
		}

		for idx := range rule.IngressRuleValue.HTTP.Paths {
			p := &rule.IngressRuleValue.HTTP.Paths[idx]
			if p.PathType == nil {
				p.PathType = &[]networking.PathType{networking.PathTypePrefix}[0]
			}

			if *p.PathType == networking.PathTypeImplementationSpecific {
				p.PathType = &[]networking.PathType{networking.PathTypePrefix}[0]
			}
		}
	}
}
