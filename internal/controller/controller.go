package controller

import (
	"io"
	"sync"

	"github.com/baidu/ingress-bfe/internal/config"
	"github.com/baidu/ingress-bfe/internal/pod"
	"github.com/baidu/ingress-bfe/internal/store"
	"k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
)

type BfeController struct {
	config config.Configuration

	kubeClient *kubernetes.Clientset

	indexer       cache.Indexer
	informer      cache.Controller
	syncQueue     workqueue.RateLimitingInterface
	resourceStore store.Store
	podInfo       *pod.Info
	once          sync.Once
	stopCh        chan struct{}
}

func NewBfeController(kubeClient *kubernetes.Clientset, restClient rest.Interface, cfg config.Configuration) (controller *BfeController) {
	controller = &BfeController{
		kubeClient: kubeClient,
		config:     cfg,
	}

	podInfo, err := pod.GetPodDetails(kubeClient)
	if err != nil {
		klog.Exitf("Unexpected error obtaining pod information: %v", err)
	}
	controller.podInfo = podInfo

	ingressListWatcher := cache.NewListWatchFromClient(restClient, "ingresses", cfg.Namespace, fields.Everything())
	indexer, informer := cache.NewIndexerInformer(ingressListWatcher, &v1beta1.Ingress{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.onAdded,
		UpdateFunc: controller.onUpdated,
		DeleteFunc: controller.onDeleted,
	}, cache.Indexers{})
	controller.indexer = indexer
	controller.informer = informer

	controller.resourceStore, err = store.NewStore(controller.kubeClient, podInfo.Namespace, nil)
	if err != nil {
		klog.Exitf("Unexpected error store.NewStore: %v", err)
	}

	return controller
}

func (b *BfeController) Run(stopCh chan struct{}) {
	// TODO handle crash
	// TODO informer run
	// TODO reload
}

func (b *BfeController) Exit() {
	b.once.Do(func() {
		close(b.stopCh)
	})
}

func (b *BfeController) onLoadConfig(obj io.Reader) {
	b.syncQueue.Add(LoadConfigAction{
		config: obj,
	})
}

func (b *BfeController) onAdded(obj interface{}) {
	b.syncQueue.Add(AddedAction{
		resource: obj,
	})
}

func (b *BfeController) onUpdated(old interface{}, new interface{}) {
	b.syncQueue.Add(UpdatedAction{
		resource:    new,
		oldResource: old,
	})
}

func (b *BfeController) onDeleted(obj interface{}) {
	b.syncQueue.Add(DeletedAction{
		resource: obj,
	})
}
