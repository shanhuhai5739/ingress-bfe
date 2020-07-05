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

//EndpointLister makes a Store that lists Ingress.
type EndpointLister struct {
	cache.Store
}

//ByKey returns the Endpoints matching key in the local Endpoints Store.
func (il *EndpointLister) ByKey(key string) (*apiv1.Endpoints, error) {
	item, exit, err := il.GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exit {
		return nil, errors.NewNotFound(schema.ParseGroupResource("core.endpoints"), key)
	}
	return item.(*apiv1.Endpoints), nil
}

//EndpointsResourceEventHandler is ingress informer handler
type EndpointsResourceEventHandler struct {
	updateCh *channels.RingChannel
}

//OnAdd handler endpoints add event
func (eh *EndpointsResourceEventHandler) OnAdd(obj interface{}) {
	eh.updateCh.In() <- Event{
		Type: CreateEvent,
		Obj:  obj,
	}
}

//OnUpdate handler endpoints update event
func (eh *EndpointsResourceEventHandler) OnUpdate(old, cur interface{}) {
	oep := old.(*corev1.Endpoints)
	cep := cur.(*corev1.Endpoints)
	if !reflect.DeepEqual(cep.Subsets, oep.Subsets) {
		eh.updateCh.In() <- Event{
			Type: UpdateEvent,
			Obj:  cur,
		}
	}
}

//OnDelete handler endpoints delete event
func (eh *EndpointsResourceEventHandler) OnDelete(obj interface{}) {
	eh.updateCh.In() <- Event{
		Type: DeleteEvent,
		Obj:  obj,
	}
}
