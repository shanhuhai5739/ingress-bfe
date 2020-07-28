package store

import (
	"reflect"

	"github.com/eapache/channels"
	apiv1 "k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
)

// ConfigMapLister makes a Store that lists Configmaps.
type ConfigMapLister struct {
	cache.Store
}

// ByKey returns the ConfigMap matching key in the local ConfigMap Store.
func (cml *ConfigMapLister) ByKey(key string) (*apiv1.ConfigMap, error) {
	s, exists, err := cml.GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(schema.ParseGroupResource("core.configmap"), key)
	}
	return s.(*apiv1.ConfigMap), nil
}

//ConfigMapResourceEventHandler is ingress informer handler
type ConfigMapResourceEventHandler struct {
	updateCh *channels.RingChannel
}

//OnAdd handler endpoints add event
func (ch *ConfigMapResourceEventHandler) OnAdd(obj interface{}) {
	cfgMap := obj.(*corev1.ConfigMap)
	ch.handleCfgMapEvent(cfgMap)
}

//OnUpdate handler endpoints update event
func (ch *ConfigMapResourceEventHandler) OnUpdate(old, cur interface{}) {
	if reflect.DeepEqual(old, cur) {
		return
	}
	cfgMap := cur.(*corev1.ConfigMap)

	ch.handleCfgMapEvent(cfgMap)
}

//OnDelete handler endpoints delete event
func (ch *ConfigMapResourceEventHandler) OnDelete(obj interface{}) {
}

func (ch *ConfigMapResourceEventHandler) handleCfgMapEvent(cfgMap *corev1.ConfigMap) {
	ch.updateCh.In() <- Event{
		Type: ConfigurationEvent,
		Obj:  cfgMap,
	}
}
