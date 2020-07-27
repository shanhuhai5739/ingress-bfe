package store

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/eapache/channels"
	corev1 "k8s.io/api/core/v1"
	networking "k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	clientcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog"
)

//IngressFilterFunc decide Ingress omitted or not
type IngressFilterFunc func(*networking.Ingress) bool

//Store is interface ,they have method to gather information about ingress,service,secret resource.
type Store interface {
	//GetSecret return Secret value of key
	GetSecret(key string) (*corev1.Secret, error)
	//GetService return service value of key
	GetService(key string) (*corev1.Service, error)
	//GetServiceEndpoints return endpoints value of key
	GetServiceEndpoints(key string) (*corev1.Endpoints, error)
	//ListIngresses return a list of ingress in store
	ListIngresses(IngressFilterFunc) []*networking.Ingress
	//Run start Store gather information about resource
	Run(stopCh chan struct{})
	// GetLocalSSLCert returns the local copy of a SSLCert
	GetLocalSSLCert(name string) (*SSLCert, error)
}

//EventType name of event type
type EventType string

const (
	// CreateEvent event associated with new objects in an informer
	CreateEvent EventType = "CREATE"
	// UpdateEvent event associated with an object update in an informer
	UpdateEvent EventType = "UPDATE"
	// DeleteEvent event associated when an object is removed from an informer
	DeleteEvent EventType = "DELETE"
	// ConfigurationEvent event associated when a controller configuration object is created or updated
	ConfigurationEvent EventType = "CONFIGURATION"
)

//Event holds the context of an event
type Event struct {
	Type EventType
	Obj  interface{}
}

//Informer containts all required SharedIndexInformers
type Informer struct {
	Ingress   cache.SharedIndexInformer
	Endpoint  cache.SharedIndexInformer
	Service   cache.SharedIndexInformer
	Secret    cache.SharedIndexInformer
	ConfigMap cache.SharedIndexInformer
}

//Run start informer
func (i *Informer) Run(stopCh chan struct{}) {
	go i.Endpoint.Run(stopCh)
	go i.Service.Run(stopCh)
	go i.Secret.Run(stopCh)
	go i.ConfigMap.Run(stopCh)

	if !cache.WaitForCacheSync(stopCh,
		i.Endpoint.HasSynced,
		i.Service.HasSynced,
		i.Secret.HasSynced,
		i.ConfigMap.HasSynced,
	) {
		runtime.HandleError(fmt.Errorf("timeout waiting for caches to sync"))
	}
	time.Sleep(1 * time.Second)

	go i.Ingress.Run(stopCh)
	if !cache.WaitForCacheSync(stopCh,
		i.Ingress.HasSynced,
	) {
		runtime.HandleError(fmt.Errorf("timeout waiting for caches to sync"))
	}

}

//Lister contains all required resource listers
type Lister struct {
	Ingress   IngressLister
	Service   ServiceLister
	Endpoint  EndpointLister
	Secret    SecretLister
	Pod       PodLister
	ConfigMap ConfigMapLister
}

//K8sStore internal Storer implementation using informers and thread safe stores
type K8sStore struct {
	informers *Informer
	listers   *Lister
	updateCh  *channels.RingChannel
	// secretIngressMap contains information about which ingress references a
	// secret in the annotations.
	secretIngressMap ObjectRefMap
	// sslStore 存储ingress使用的证书,在证书更新时，验证证书是否有改变
	sslStore *LocalCertStore
	// syncSecretMu protects against simultaneous invocations of syncSecret
	syncSecretMu *sync.Mutex
}

//NewStore create a new K8sStore
func NewStore(
	kubeClient kubernetes.Interface,
	namespace string,
	resycPeriod time.Duration,
	updateCh *channels.RingChannel,
) (store *K8sStore) {
	store = &K8sStore{
		informers:        &Informer{},
		listers:          &Lister{},
		updateCh:         updateCh,
		secretIngressMap: NewObjectRefMap(),
		sslStore:         NewLocalCertStore(),
		syncSecretMu:     &sync.Mutex{},
	}

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&clientcorev1.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(namespace),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{
		Component: "bfe-ingress-controller",
	})

	// As we currently do not filter out kubernetes objects we list, we can
	// retrieve a huge amount of data from the API server.
	// In a cluster using HELM < v3 configmaps are used to store binary data.
	// If you happen to have a lot of HELM releases in the cluster it will make
	// the memory consumption of nginx-ingress-controller explode.
	// In order to avoid that we filter out labels OWNER=TILLER.
	tweakListOptionsFunc := func(options *metav1.ListOptions) {
		if len(options.LabelSelector) > 0 {
			options.LabelSelector += ",OWNER!=TILLER"
		} else {
			options.LabelSelector = "OWNER!=TILLER"
		}
	}

	informerFactory := informers.NewSharedInformerFactoryWithOptions(kubeClient, resycPeriod,
		informers.WithNamespace(namespace),
		informers.WithTweakListOptions(tweakListOptionsFunc),
	)

	//TODO: 判断kubernetes版本使用不同的ingress
	store.informers.Ingress = informerFactory.Networking().V1beta1().Ingresses().Informer()
	store.listers.Ingress.Store = store.informers.Ingress.GetStore()
	store.informers.Ingress.AddEventHandler(&IngressResourceEventHandler{
		store:    store,
		recorder: recorder,
	})

	store.informers.Endpoint = informerFactory.Core().V1().Endpoints().Informer()
	store.listers.Endpoint.Store = store.informers.Endpoint.GetStore()
	store.informers.Endpoint.AddEventHandler(&EndpointsResourceEventHandler{
		updateCh: store.updateCh,
	})

	store.informers.Secret = informerFactory.Core().V1().Secrets().Informer()
	store.listers.Secret.Store = store.informers.Secret.GetStore()
	store.informers.Secret.AddEventHandler(&SecretResourceEventHandler{
		store:    store,
		recorder: recorder,
	})

	store.informers.Service = informerFactory.Core().V1().Secrets().Informer()
	store.listers.Service.Store = store.informers.Service.GetStore()
	store.informers.Service.AddEventHandler(&ServiceResourceEventHandler{
		updateCh: store.updateCh,
	})

	store.informers.ConfigMap = informerFactory.Core().V1().ConfigMaps().Informer()
	store.listers.ConfigMap.Store = store.informers.ConfigMap.GetStore()
	store.informers.ConfigMap.AddEventHandler(&ConfigMapResourceEventHandler{
		updateCh: store.updateCh,
	})

	return
}

// Run initiates the synchronization of the informers and the initial
// synchronization of the secrets.
func (s *K8sStore) Run(stopCh chan struct{}) {
	// start informers
	s.informers.Run(stopCh)
}

//GetSecret return Secret value of key
func (s *K8sStore) GetSecret(key string) (*corev1.Secret, error) {
	return s.listers.Secret.ByKey(key)
}

//GetService return service value of key
func (s *K8sStore) GetService(key string) (*corev1.Service, error) {
	return s.listers.Service.ByKey(key)
}

//GetServiceEndpoints return endpoints value of key
func (s *K8sStore) GetServiceEndpoints(key string) (*corev1.Endpoints, error) {
	return s.listers.Endpoint.ByKey(key)
}

//ListIngresses return a list of ingress in store
func (s *K8sStore) ListIngresses(filter IngressFilterFunc) []*networking.Ingress {
	ingresses := make([]*networking.Ingress, 0)
	for _, item := range s.listers.Ingress.List() {
		ing := item.(*networking.Ingress)

		if filter != nil && filter(ing) {
			continue
		}

		ingresses = append(ingresses, ing)
	}
	// sort Ingresses using the CreationTimestamp field
	sort.SliceStable(ingresses, func(i, j int) bool {
		ir := ingresses[i].CreationTimestamp
		jr := ingresses[j].CreationTimestamp
		if ir.Equal(&jr) {
			in := fmt.Sprintf("%v/%v", ingresses[i].Namespace, ingresses[i].Name)
			jn := fmt.Sprintf("%v/%v", ingresses[j].Namespace, ingresses[j].Name)
			klog.V(3).Infof("Ingress %v and %v have identical CreationTimestamp", in, jn)
			return in > jn
		}
		return ir.Before(&jr)
	})

	return ingresses
}

func (s *K8sStore) updateSecretIngressMap(ing *networking.Ingress) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(ing)
	if err != nil {
		klog.Warning(err)
	}
	// delete all existing references first
	s.secretIngressMap.Delete(key)

	var refSecrets []string

	for _, tls := range ing.Spec.TLS {
		secrName := tls.SecretName
		if secrName != "" {
			secrKey := fmt.Sprintf("%v/%v", ing.Namespace, secrName)
			refSecrets = append(refSecrets, secrKey)
		}
	}
	// populate map with all secret references
	s.secretIngressMap.Insert(key, refSecrets...)
}

// GetLocalSSLCert returns the local copy of a SSLCert
func (s *K8sStore) GetLocalSSLCert(key string) (*SSLCert, error) {
	return s.sslStore.ByKey(key)
}

//syncSecrets 产生更新证书Event
func (s *K8sStore) syncSecrets(ing *networking.Ingress) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(ing)
	if err != nil {
		klog.Warning(err)
	}
	for _, secrKey := range s.secretIngressMap.ReferencedBy(key) {
		s.syncSecret(secrKey)
	}
}

func (s *K8sStore) getIngress(key string) (*networking.Ingress, error) {
	ing, err := s.listers.Ingress.ByKey(key)
	if err != nil {
		return nil, err
	}

	return ing, nil
}
