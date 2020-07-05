package store

import (
	"github.com/eapache/channels"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

// PodLister makes a Store that lists pod.
type PodLister struct {
	cache.Store
}

//PodResourceEventHandler is ingress informer handler
type PodResourceEventHandler struct {
	updateCh *channels.RingChannel
}

//OnAdd handler endpoints add event
func (ph *PodResourceEventHandler) OnAdd(obj interface{}) {
	ph.updateCh.In() <- Event{
		Type: CreateEvent,
		Obj:  obj,
	}
}

//OnUpdate handler endpoints update event
func (ph *PodResourceEventHandler) OnUpdate(old, cur interface{}) {
	oldPod := old.(*corev1.Pod)
	curPod := cur.(*corev1.Pod)

	if oldPod.Status.Phase == curPod.Status.Phase {
		return
	}

	ph.updateCh.In() <- Event{
		Type: UpdateEvent,
		Obj:  cur,
	}
}

//OnDelete handler endpoints delete event
func (ph *PodResourceEventHandler) OnDelete(obj interface{}) {
	ph.updateCh.In() <- Event{
		Type: DeleteEvent,
		Obj:  obj,
	}
}
