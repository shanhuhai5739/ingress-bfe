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

// ServiceLister makes a Store that lists service.
type ServiceLister struct {
	cache.Store
}

// ByKey returns the service matching key in the local service Store.
func (sl *ServiceLister) ByKey(key string) (*apiv1.Service, error) {
	item, exist, err := sl.GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exist {
		return nil, errors.NewNotFound(schema.ParseGroupResource("core.service"), key)
	}
	return item.(*apiv1.Service), nil
}

//ServiceResourceEventHandler is ingress informer handler
type ServiceResourceEventHandler struct {
	updateCh *channels.RingChannel
}

//OnAdd handler endpoints add event
func (sh *ServiceResourceEventHandler) OnAdd(obj interface{}) {}

//OnUpdate handler endpoints update event
func (sh *ServiceResourceEventHandler) OnUpdate(old, cur interface{}) {
	oldSvc := old.(*corev1.Service)
	curSvc := cur.(*corev1.Service)

	if reflect.DeepEqual(oldSvc, curSvc) {
		return
	}

	sh.updateCh.In() <- Event{
		Type: UpdateEvent,
		Obj:  cur,
	}
}

//OnDelete handler endpoints delete event
func (sh *ServiceResourceEventHandler) OnDelete(obj interface{}) {}
